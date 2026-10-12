package workflow

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

// Runtime owns the Temporal client used to start sagas and deliver reviewer
// signals, plus the worker that executes them.
type Runtime struct {
	client   client.Client
	queue    string
	activity *Activities
}

func NewRuntime(host, namespace, queue string) (*Runtime, error) {
	if namespace == "" {
		namespace = "default"
	}
	c, err := client.Dial(client.Options{HostPort: host, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("connect temporal at %s: %w", host, err)
	}
	return &Runtime{client: c, queue: queue}, nil
}

func (r *Runtime) Close() { r.client.Close() }

func (r *Runtime) Client() client.Client { return r.client }

// Ping asks the service for its health, used by the readiness endpoint.
func (r *Runtime) Ping(ctx context.Context) error {
	_, err := r.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	return err
}

// StartWorker registers the saga and its activities, then keeps the task queue
// running in this process until ctx is cancelled. It blocks; call it from a
// goroutine. w.Start() returning means the poller is up, not that it is done.
func (r *Runtime) StartWorker(ctx context.Context, acts *Activities) error {
	r.activity = acts
	w := worker.New(r.client, r.queue, worker.Options{})
	w.RegisterWorkflow(Underwriting)
	w.RegisterActivity(acts)
	if err := w.Start(); err != nil {
		return fmt.Errorf("start temporal worker on queue %q: %w", r.queue, err)
	}
	log.Printf("temporal worker listening on task queue %q", r.queue)
	<-ctx.Done()
	// Let in-flight activities finish before the poller deregisters.
	w.Stop()
	return context.Cause(ctx)
}

// StartUnderwriting launches the saga for a persisted application and returns
// the workflow id so the API can hand it to the UI.
func (r *Runtime) StartUnderwriting(ctx context.Context, appID string) (string, string, error) {
	wid := "underwriting-" + appID
	run, err := r.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       wid,
		TaskQueue:                r.queue,
		WorkflowExecutionTimeout: 7 * 24 * time.Hour,
		WorkflowRunTimeout:       72*time.Hour + 30*time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}, Underwriting, UnderwritingInput{ApplicationID: appID})
	if err != nil {
		return "", "", fmt.Errorf("start underwriting saga for %s: %w", appID, err)
	}
	return wid, run.GetRunID(), nil
}

// SignalReviewer delivers the human verdict to a paused saga.
func (r *Runtime) SignalReviewer(ctx context.Context, workflowID, runID string, decision ReviewerDecision) error {
	if workflowID == "" {
		return fmt.Errorf("workflow id is required to signal a reviewer decision")
	}
	if err := r.client.SignalWorkflow(ctx, workflowID, runID, SignalReviewerDecision, decision); err != nil {
		return fmt.Errorf("signal %s: %w", workflowID, err)
	}
	return nil
}

// State reads the workflow's current stage through its query handler.
func (r *Runtime) State(ctx context.Context, workflowID, runID string) (State, error) {
	var state State
	if workflowID == "" {
		return state, fmt.Errorf("workflow id is required")
	}
	resp, err := r.client.QueryWorkflow(ctx, workflowID, runID, QueryUnderwritingState)
	if err != nil {
		return state, fmt.Errorf("query %s: %w", workflowID, err)
	}
	if err := resp.Get(&state); err != nil {
		return state, err
	}
	return state, nil
}
