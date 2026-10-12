package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"credivect/core/internal/domain"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// The durable-saga claims are the ones a screenshot cannot show: a manual review
// case really parks on a timer and really takes a human signal, an unanswered
// case really expires to a decline, and a bureau that never answers really fails
// the saga instead of inventing a score. These run the workflow against mocked
// activities in Temporal's in-process test environment, which advances a virtual
// clock, so a 72 hour review window costs no wall time.

type recordedCall struct {
	Status       string
	Outcome      string
	DecisionType string
	EventType    string
	LimitCents   int64
	APRBps       int32
	Underwriter  string
}

type sagaHarness struct {
	t                 *testing.T
	env               *testsuite.TestWorkflowEnvironment
	suite             testsuite.WorkflowTestSuite
	recorded          []recordedCall
	score             domain.ScoringResult
	bureauUnavailable bool
}

func newSagaHarness(t *testing.T, result domain.ScoringResult) *sagaHarness {
	t.Helper()
	h := &sagaHarness{t: t, score: result}
	h.env = h.suite.NewTestWorkflowEnvironment()
	h.env.SetTestTimeout(30 * time.Second)

	h.env.RegisterActivityWithOptions(h.loadApplication, activity.RegisterOptions{Name: "LoadApplication"})
	h.env.RegisterActivityWithOptions(h.pullBureau, activity.RegisterOptions{Name: "PullBureauReport"})
	h.env.RegisterActivityWithOptions(h.features, activity.RegisterOptions{Name: "ComputeFeatureVector"})
	h.env.RegisterActivityWithOptions(h.scoreApplication, activity.RegisterOptions{Name: "ScoreApplication"})
	h.env.RegisterActivityWithOptions(h.recordOutcome, activity.RegisterOptions{Name: "RecordOutcome"})
	return h
}

func (h *sagaHarness) loadApplication(ctx context.Context, appID string) (domain.CreditApplication, error) {
	return domain.CreditApplication{
		ID:                   appID,
		ApplicantID:          "user-1",
		Status:               domain.StatusSubmitted,
		RequestedAmountCents: 1_500_00,
		WorkflowID:           "underwriting-" + appID,
		Applicant: &domain.Applicant{
			ID: "user-1", SSNBlindIndex: "blind-index", FirstName: "Sam", LastName: "Rivera",
			DateOfBirth: "1990-01-01", MonthlyIncomeCents: 600_000,
		},
	}, nil
}

var errBureauUnavailable = errors.New("bureau provider timed out")

func (h *sagaHarness) pullBureau(ctx context.Context, appID string) (domain.BureauReport, error) {
	if h.bureauUnavailable {
		return domain.BureauReport{}, errBureauUnavailable
	}
	return domain.BureauReport{ID: "bureau-1", ApplicationID: appID, BureauName: "Experian", BureauScore: 640}, nil
}

func (h *sagaHarness) features(ctx context.Context, app domain.CreditApplication, b domain.BureauReport) (domain.FeatureVector, error) {
	return domain.FeatureVector{ApplicationID: app.ID}, nil
}

func (h *sagaHarness) scoreApplication(ctx context.Context, app domain.CreditApplication, b domain.BureauReport) (domain.ScoringResult, error) {
	out := h.score
	out.ApplicationID = app.ID
	return out, nil
}

func (h *sagaHarness) recordOutcome(ctx context.Context, in OutcomeInput) (domain.AuditLedgerEntry, error) {
	h.recorded = append(h.recorded, recordedCall{
		Status: in.Status, Outcome: in.Outcome, DecisionType: in.DecisionType,
		EventType: in.EventType, LimitCents: in.ApprovedLimitCents, APRBps: in.ApprovedAPRBps,
		Underwriter: in.UnderwriterID,
	})
	return domain.AuditLedgerEntry{ID: "audit-entry", EventType: in.EventType}, nil
}

// run executes the saga and returns the outcome it reported.
func (h *sagaHarness) run() (string, error) {
	h.env.ExecuteWorkflow(Underwriting, UnderwritingInput{ApplicationID: "app-1"})
	if !h.env.IsWorkflowCompleted() {
		h.t.Fatal("the saga did not complete; the test forgot to signal or advance the clock")
	}
	if err := h.env.GetWorkflowError(); err != nil {
		return "", err
	}
	var out string
	if err := h.env.GetWorkflowResult(&out); err != nil {
		h.t.Fatal(err)
	}
	return out, nil
}

func TestSagaApprovesAutomatically(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{
		ID: "run-1", RecommendedOutcome: "APPROVE", RiskTier: "PRIME",
		ScorecardPoints: 700, MaxApprovedLimitCents: 1_500_00, RecommendedAPRBps: 950,
	})

	out, err := h.run()
	if err != nil {
		t.Fatalf("saga failed: %v", err)
	}
	if out != "APPROVED" {
		t.Errorf("outcome %q, want APPROVED", out)
	}
	if len(h.recorded) != 1 {
		t.Fatalf("expected exactly one recorded decision, got %+v", h.recorded)
	}
	rec := h.recorded[0]
	if rec.Status != string(domain.StatusApproved) || rec.DecisionType != "AUTOMATED" ||
		rec.LimitCents != 1_500_00 || rec.APRBps != 950 {
		t.Errorf("the decision was written from the wrong fields: %+v", rec)
	}
}

func TestSagaDeclinesWithReasonCodes(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{
		ID: "run-1", RecommendedOutcome: "DECLINE", RiskTier: "SUBPRIME", ScorecardPoints: 540,
		AdverseActionReasons: []domain.AdverseActionReason{{ReasonCode: "AA01"}, {ReasonCode: "AA04"}},
	})

	out, err := h.run()
	if err != nil {
		t.Fatalf("saga failed: %v", err)
	}
	if out != "DECLINED" || len(h.recorded) != 1 || h.recorded[0].Status != string(domain.StatusDeclined) {
		t.Errorf("outcome %q, recorded %+v", out, h.recorded)
	}
}

func TestUnknownOutcomeFailsTheSaga(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{ID: "run-1", RecommendedOutcome: "MAYBE"})

	if _, err := h.run(); err == nil {
		t.Fatal("an outcome the policy does not define was accepted")
	} else if !strings.Contains(err.Error(), "unknown recommended outcome") {
		t.Errorf("error %q does not name the unknown outcome", err)
	}
}

// A near-prime file must stop for a human rather than auto-decline.
func TestSagaWaitsForTheReviewerSignal(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{
		ID: "run-1", RecommendedOutcome: "MANUAL_REVIEW", RiskTier: "NEAR_PRIME",
		ScorecardPoints: 630, MaxApprovedLimitCents: 1_125_00, RecommendedAPRBps: 1240,
	})

	// Delivered on the virtual clock once the saga is parked, so the reviewer
	// branch wins the race against the 72 hour timer.
	h.env.RegisterDelayedCallback(func() {
		h.env.SignalWorkflow(SignalReviewerDecision, ReviewerDecision{
			Decision: "APPROVE", ApprovedLimit: 900_00, ApprovedAPR: 1300,
			Notes: "conditions met", UnderwriterID: "uw-1",
		})
	}, time.Hour)

	out, err := h.run()
	if err != nil {
		t.Fatalf("saga failed: %v", err)
	}
	if out != "APPROVED" {
		t.Errorf("outcome %q, want APPROVED", out)
	}
	if len(h.recorded) != 2 {
		t.Fatalf("expected a review queue entry and a manual decision, got %+v", h.recorded)
	}
	queue, decision := h.recorded[0], h.recorded[1]
	if queue.Status != string(domain.StatusInReview) || queue.EventType != "QUEUED_FOR_MANUAL_REVIEW" || queue.DecisionType != "" {
		t.Errorf("the parked row must be IN_REVIEW with no decision type: %+v", queue)
	}
	if decision.Outcome != "APPROVED" || decision.DecisionType != "MANUAL_OVERRIDE" ||
		decision.LimitCents != 900_00 || decision.APRBps != 1300 || decision.Underwriter != "uw-1" {
		t.Errorf("the reviewer's terms were not carried into the decision: %+v", decision)
	}
}

func TestReviewWindowIsSeventyTwoHours(t *testing.T) {
	if ReviewWindow != 72*time.Hour {
		t.Fatalf("the product documents a 72 hour pause, the code says %v", ReviewWindow)
	}
}

func TestUnreviewedCaseExpiresToDeclined(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{
		ID: "run-1", RecommendedOutcome: "MANUAL_REVIEW", RiskTier: "NEAR_PRIME",
		ScorecardPoints: 620, MaxApprovedLimitCents: 1_125_00, RecommendedAPRBps: 1240,
	})

	// Nobody signs in, so the mock clock runs past the window on its own.
	out, err := h.run()
	if err != nil {
		t.Fatalf("saga failed: %v", err)
	}
	if out != "EXPIRED_DECLINED" {
		t.Errorf("outcome %q, want EXPIRED_DECLINED", out)
	}
	if len(h.recorded) != 2 || h.recorded[1].EventType != "REVIEW_WINDOW_EXPIRED" {
		t.Fatalf("the expiry was not recorded: %+v", h.recorded)
	}
	if h.recorded[1].Status != string(domain.StatusDeclined) || h.recorded[1].Outcome != "DECLINED" {
		t.Errorf("an expired review must decline on the record: %+v", h.recorded[1])
	}
}

func TestBureauFailureExhaustsRetriesAndFailsTheSaga(t *testing.T) {
	h := newSagaHarness(t, domain.ScoringResult{ID: "run-1", RecommendedOutcome: "APPROVE"})
	h.bureauUnavailable = true

	_, err := h.run()
	if err == nil {
		t.Fatal("the saga completed although the bureau never answered")
	}
	if !strings.Contains(err.Error(), "bureau pull failed after retries") {
		t.Errorf("error %q does not report the bureau stage", err)
	}
	if len(h.recorded) != 0 {
		t.Errorf("no decision may be recorded when the file was never pulled, got %+v", h.recorded)
	}
}
