package workflow

import (
	"fmt"
	"time"

	"credivect/core/internal/domain"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowUnderwriting is the type name registered with Temporal.
	WorkflowUnderwriting = "UnderwritingWorkflow"
	// SignalReviewerDecision carries the human verdict into a paused saga.
	SignalReviewerDecision = "reviewer-decision"
	// QueryUnderwritingState lets the queue read where a saga currently sits.
	QueryUnderwritingState = "underwriting-state"
)

// ReviewWindow is the human-in-the-loop pause. Unanswered applications are
// declined with an adverse action notice rather than left open forever.
var ReviewWindow = 72 * time.Hour

type UnderwritingInput struct {
	ApplicationID string `json:"application_id"`
}

type ReviewerDecision struct {
	Decision      string `json:"decision"` // APPROVE, DECLINE
	ApprovedLimit int64  `json:"approved_limit_cents"`
	ApprovedAPR   int32  `json:"approved_apr_bps"`
	Notes         string `json:"notes"`
	UnderwriterID string `json:"underwriter_id"`
}

type State struct {
	Stage        string    `json:"stage"`
	RiskTier     string    `json:"risk_tier,omitempty"`
	Scorecard    int32     `json:"scorecard_points,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	WaitingSince time.Time `json:"waiting_since,omitempty"`
}

func Underwriting(ctx workflow.Context, in UnderwritingInput) (string, error) {
	if in.ApplicationID == "" {
		return "", fmt.Errorf("underwriting requires an application id")
	}
	state := State{Stage: "QUEUED"}
	if err := workflow.SetQueryHandler(ctx, QueryUnderwritingState, func() (State, error) { return state, nil }); err != nil {
		return "", err
	}

	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    time.Minute,
		MaximumAttempts:    5,
	}
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         retryPolicy,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var app domain.CreditApplication
	state.Stage = "LOADING_APPLICATION"
	if err := workflow.ExecuteActivity(ctx, activities.LoadApplication, in.ApplicationID).Get(ctx, &app); err != nil {
		return "", fmt.Errorf("load application: %w", err)
	}

	var bureau domain.BureauReport
	state.Stage = "PULLING_BUREAU_REPORT"
	bureauAO := ao
	bureauAO.RetryPolicy = &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    6,
	}
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, bureauAO), activities.PullBureauReport, in.ApplicationID).Get(ctx, &bureau); err != nil {
		return "", fmt.Errorf("bureau pull failed after retries: %w", err)
	}

	var features domain.FeatureVector
	state.Stage = "COMPUTING_FEATURES"
	if err := workflow.ExecuteActivity(ctx, activities.ComputeFeatureVector, app, bureau).Get(ctx, &features); err != nil {
		return "", fmt.Errorf("feature vector: %w", err)
	}

	var result domain.ScoringResult
	state.Stage = "SCORING"
	scoreAO := ao
	scoreAO.RetryPolicy = &temporal.RetryPolicy{InitialInterval: 500 * time.Millisecond, BackoffCoefficient: 2.0, MaximumAttempts: 3}
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, scoreAO), activities.ScoreApplication, app, bureau).Get(ctx, &result); err != nil {
		return "", fmt.Errorf("scoring: %w", err)
	}
	state.RiskTier = result.RiskTier
	state.Scorecard = result.ScorecardPoints

	switch result.RecommendedOutcome {
	case "APPROVE":
		state.Stage = "DECISIONING"
		err := workflow.ExecuteActivity(ctx, activities.RecordOutcome, OutcomeInput{
			ApplicationID: app.ID, Status: string(domain.StatusApproved), Outcome: "APPROVED",
			ApprovedLimitCents: result.MaxApprovedLimitCents, ApprovedAPRBps: result.RecommendedAPRBps,
			DecisionType: "AUTOMATED", ScoringRunID: result.ID, EventType: "DECISION_AUTOMATED",
			Metadata: map[string]any{"risk_tier": result.RiskTier, "scorecard_points": result.ScorecardPoints},
		}).Get(ctx, nil)
		state.Outcome = "APPROVED"
		return "APPROVED", err

	case "DECLINE":
		state.Stage = "DECISIONING"
		err := workflow.ExecuteActivity(ctx, activities.RecordOutcome, OutcomeInput{
			ApplicationID: app.ID, Status: string(domain.StatusDeclined), Outcome: "DECLINED",
			DecisionType: "AUTOMATED", ScoringRunID: result.ID, EventType: "DECISION_AUTOMATED",
			Metadata: map[string]any{"risk_tier": result.RiskTier, "reason_codes": reasonCodes(result)},
		}).Get(ctx, nil)
		state.Outcome = "DECLINED"
		return "DECLINED", err

	case "MANUAL_REVIEW":
		return waitForReviewer(ctx, app, result, &state)

	default:
		return "", fmt.Errorf("unknown recommended outcome %q", result.RecommendedOutcome)
	}
}

// waitForReviewer parks the saga for up to ReviewWindow. The underwriter queue
// signals the verdict; if nobody acts, the application is declined on the
// record rather than silently abandoned.
func waitForReviewer(ctx workflow.Context, app domain.CreditApplication, result domain.ScoringResult, state *State) (string, error) {
	state.Stage = "AWAITING_REVIEWER"
	state.WaitingSince = workflow.Now(ctx)

	if err := workflow.ExecuteActivity(ctx, activities.RecordOutcome, OutcomeInput{
		ApplicationID: app.ID, Status: string(domain.StatusInReview), DecisionType: "",
		ScoringRunID: result.ID, EventType: "QUEUED_FOR_MANUAL_REVIEW",
		Metadata: map[string]any{"risk_tier": result.RiskTier, "review_window_hours": int(ReviewWindow / time.Hour)},
	}).Get(ctx, nil); err != nil {
		return "", err
	}

	decisions := workflow.GetSignalChannel(ctx, SignalReviewerDecision)
	selector := workflow.NewSelector(ctx)
	var outcome string
	var failure error

	selector.AddReceive(decisions, func(c workflow.ReceiveChannel, more bool) {
		var decision ReviewerDecision
		c.Receive(ctx, &decision)
		status, res := "DECLINED", "DECLINED"
		if decision.Decision == "APPROVE" {
			status, res = "APPROVED", "APPROVED"
		}
		failure = workflow.ExecuteActivity(ctx, activities.RecordOutcome, OutcomeInput{
			ApplicationID: app.ID, Status: status, Outcome: res,
			ApprovedLimitCents: decision.ApprovedLimit, ApprovedAPRBps: decision.ApprovedAPR,
			DecisionType: "MANUAL_OVERRIDE", UnderwriterID: decision.UnderwriterID, Notes: decision.Notes,
			ScoringRunID: result.ID, EventType: "UNDERWRITER_OVERRIDE_SIGNAL",
			Metadata: map[string]any{"underwriter_id": decision.UnderwriterID, "risk_tier": result.RiskTier},
		}).Get(ctx, nil)
		outcome = res
	})

	selector.AddFuture(workflow.NewTimer(ctx, ReviewWindow), func(f workflow.Future) {
		failure = workflow.ExecuteActivity(ctx, activities.RecordOutcome, OutcomeInput{
			ApplicationID: app.ID, Status: string(domain.StatusDeclined), Outcome: "DECLINED",
			DecisionType: "AUTOMATED", ScoringRunID: result.ID,
			Notes:     fmt.Sprintf("no reviewer action within %s; application expired", ReviewWindow),
			EventType: "REVIEW_WINDOW_EXPIRED",
			Metadata:  map[string]any{"risk_tier": result.RiskTier, "expired_after_hours": int(ReviewWindow / time.Hour)},
		}).Get(ctx, nil)
		outcome = "EXPIRED_DECLINED"
	})

	selector.Select(ctx)
	state.Outcome = outcome
	return outcome, failure
}

func reasonCodes(result domain.ScoringResult) []string {
	codes := make([]string, 0, len(result.AdverseActionReasons))
	for _, r := range result.AdverseActionReasons {
		codes = append(codes, r.ReasonCode)
	}
	return codes
}
