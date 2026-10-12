package workflow

import (
	"context"
	"fmt"
	"time"

	"credivect/core/internal/bureau"
	"credivect/core/internal/client"
	"credivect/core/internal/domain"
	"credivect/core/internal/events"
	"credivect/core/internal/storage"
)

// Activities are the only place the saga touches the outside world. Each one is
// individually retryable and idempotent, which is what makes the workflow
// durable across a core restart.
type Activities struct {
	repo      *storage.Repository
	scoring   *client.ScoringClient
	bureau    bureau.Provider
	publisher *events.Publisher
}

func NewActivities(repo *storage.Repository, scoring *client.ScoringClient, provider bureau.Provider, publisher *events.Publisher) *Activities {
	return &Activities{repo: repo, scoring: scoring, bureau: provider, publisher: publisher}
}

func (a *Activities) LoadApplication(ctx context.Context, appID string) (domain.CreditApplication, error) {
	app, err := a.repo.GetApplication(ctx, appID)
	if err != nil {
		return app, err
	}
	if app.Applicant == nil {
		return app, fmt.Errorf("application %s has no applicant row", appID)
	}
	return app, nil
}

// PullBureauReport fetches the consumer file and persists it. Re-running after
// a retry writes a second snapshot row (bitemporal history) rather than
// corrupting the first, which is why the workflow reads the newest one.
func (a *Activities) PullBureauReport(ctx context.Context, appID string) (domain.BureauReport, error) {
	app, err := a.LoadApplication(ctx, appID)
	if err != nil {
		return domain.BureauReport{}, err
	}
	report, err := a.bureau.Fetch(ctx, bureau.Request{
		ApplicationID:        app.ID,
		ApplicantID:          app.ApplicantID,
		SSNBlindIndex:        app.Applicant.SSNBlindIndex,
		FirstName:            app.Applicant.FirstName,
		LastName:             app.Applicant.LastName,
		DateOfBirth:          app.Applicant.DateOfBirth,
		MonthlyIncomeCents:   app.Applicant.MonthlyIncomeCents,
		RequestedAmountCents: app.RequestedAmountCents,
	})
	if err != nil {
		return report, fmt.Errorf("bureau pull for %s: %w", appID, err)
	}
	report.ID = domain.NewUUIDv7()
	report.ApplicationID = appID
	report.RawPayload = bureauPayloadJSON(report)
	if err := a.repo.InsertBureauReport(ctx, report); err != nil {
		return report, err
	}
	if err := a.appendAudit(ctx, appID, "BUREAU_REPORT_RETRIEVED", map[string]any{
		"bureau": report.BureauName, "bureau_score": report.BureauScore,
		"revolving_utilization_bps": report.RevolvingUtilizationBps,
		"delinquencies_24m":         report.Delinquencies24m, "age_of_oldest_trade_months": report.AgeOfOldestTradeMonths,
	}); err != nil {
		return report, err
	}
	return report, nil
}

func (a *Activities) ComputeFeatureVector(ctx context.Context, app domain.CreditApplication, b domain.BureauReport) (domain.FeatureVector, error) {
	if app.Applicant == nil || app.Applicant.MonthlyIncomeCents <= 0 {
		return domain.FeatureVector{}, fmt.Errorf("feature vector requires monthly income")
	}
	dti := bpsOf(b.TotalDebtCents, app.Applicant.MonthlyIncomeCents)
	annualIncome := app.Applicant.MonthlyIncomeCents * 12
	fv := domain.FeatureVector{
		ID:                      domain.NewUUIDv7(),
		ApplicationID:           app.ID,
		DTIBps:                  dti,
		RevolvingUtilizationBps: b.RevolvingUtilizationBps,
		Delinquencies24m:        b.Delinquencies24m,
		Inquiries6m:             b.Inquiries6m,
		AgeOfOldestTradeMonths:  b.AgeOfOldestTradeMonths,
		IncomeToLoanBps:         bpsOf(annualIncome, app.RequestedAmountCents),
		ComputedAt:              time.Now().UTC(),
	}
	if err := a.repo.InsertFeatureVector(ctx, fv); err != nil {
		return fv, err
	}
	return fv, nil
}

// ScoreApplication calls the Python dual engine and stores the run, its TreeSHAP
// attributions and its FCRA reason codes.
func (a *Activities) ScoreApplication(ctx context.Context, app domain.CreditApplication, b domain.BureauReport) (domain.ScoringResult, error) {
	mv, err := a.repo.ActiveModelVersion(ctx)
	if err != nil {
		return domain.ScoringResult{}, err
	}
	res, err := a.scoring.CalculateRisk(ctx, app, b)
	if err != nil {
		return res, err
	}
	if res.ModelVersion != mv.Version {
		return res, fmt.Errorf("serving engine reports model %s but the registered active version is %s", res.ModelVersion, mv.Version)
	}
	if err := a.repo.InsertScoringRun(ctx, app.ID, mv.ID, &res); err != nil {
		return res, err
	}
	if err := a.appendAudit(ctx, app.ID, "APPLICATION_SCORED", map[string]any{
		"scorecard_points": res.ScorecardPoints, "calibrated_pd": res.CalibratedPD,
		"pd_basis_points": res.PDBasisPoints, "risk_tier": res.RiskTier,
		"recommended_outcome": res.RecommendedOutcome, "execution_time_micros": res.ExecutionTimeMicros,
		"model_version": res.ModelVersion, "scoring_run_id": res.ID,
	}); err != nil {
		return res, err
	}
	return res, nil
}

func (a *Activities) appendAudit(ctx context.Context, appID, eventType string, metadata map[string]any) error {
	entry, err := a.repo.AppendAudit(ctx, appID, eventType, "SYSTEM", metadata)
	if err != nil {
		return err
	}
	if a.publisher != nil {
		return a.publisher.Publish(ctx, events.FromAudit(entry, metadata))
	}
	return nil
}

func bpsOf(numerator, denominator int64) int32 {
	if denominator <= 0 {
		return 10000
	}
	v := float64(numerator) * 10000.0 / float64(denominator)
	return int32(roundHalfEven(v))
}
