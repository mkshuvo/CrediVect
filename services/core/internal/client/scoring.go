package client

import (
	"context"
	"fmt"
	"time"

	"credivect/core/internal/domain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	scoringv1 "credivect/core/internal/proto/scoringv1"
)

// ScoringClient is the single path to a probability of default. It owns no
// scoring logic: the Python service is the only engine, so the API gateway
// cannot drift from the trained model artifacts.
type ScoringClient struct {
	conn      *grpc.ClientConn
	stub      scoringv1.ScoringServiceClient
	timeout   time.Duration
	modelVers string
}

func NewScoringClient(host string, timeout time.Duration) (*ScoringClient, error) {
	conn, err := grpc.NewClient(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial scoring service %s: %w", host, err)
	}
	c := &ScoringClient{conn: conn, stub: scoringv1.NewScoringServiceClient(conn), timeout: timeout}
	if err := c.probe(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// probe blocks until the scoring servicer answers, so the container either
// boots fully wired or fails its health check loudly.
func (c *ScoringClient) probe() error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := c.stub.HealthCheck(ctx, &scoringv1.HealthCheckRequest{})
		cancel()
		if err == nil && resp.GetStatus() == "SERVING" {
			c.modelVers = resp.GetVersion()
			return nil
		}
		if err == nil {
			err = fmt.Errorf("scoring reported status %q", resp.GetStatus())
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("scoring service never became SERVING: %w", lastErr)
}

func (c *ScoringClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ModelVersion is the artifact version reported by the serving engine.
func (c *ScoringClient) ModelVersion() string { return c.modelVers }

// CalculateRisk ships the application and bureau tradelines to the Python
// dual engine and returns its verdict verbatim.
func (c *ScoringClient) CalculateRisk(ctx context.Context, app domain.CreditApplication, bureau domain.BureauReport) (domain.ScoringResult, error) {
	var income int64
	var applicantID string
	if app.Applicant != nil {
		income = app.Applicant.MonthlyIncomeCents
		applicantID = app.Applicant.ID
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.stub.CalculateRisk(callCtx, &scoringv1.CalculateRiskRequest{
		ApplicationId:           app.ID,
		ApplicantId:             applicantID,
		RequestedAmountCents:    app.RequestedAmountCents,
		MonthlyIncomeCents:      income,
		CurrencyCode:            app.CurrencyCode,
		LoanTermMonths:          app.LoanTermMonths,
		LoanPurpose:             app.LoanPurpose,
		TotalDebtCents:          bureau.TotalDebtCents,
		RevolvingBalanceCents:   bureau.RevolvingBalanceCents,
		RevolvingLimitCents:     bureau.RevolvingLimitCents,
		RevolvingUtilizationBps: bureau.RevolvingUtilizationBps,
		Delinquencies_24M:       bureau.Delinquencies24m,
		Inquiries_6M:            bureau.Inquiries6m,
		TotalTrades:             bureau.TotalTrades,
		OpenTrades:              bureau.OpenTrades,
		AgeOfOldestTradeMonths:  bureau.AgeOfOldestTradeMonths,
		BankruptcyFlag:          bureau.BankruptcyFlag,
		BureauScore:             bureau.BureauScore,
	})
	if err != nil {
		return domain.ScoringResult{}, fmt.Errorf("scoring rpc for %s: %w", app.ID, err)
	}

	result := domain.ScoringResult{
		ApplicationID:         resp.GetApplicationId(),
		ScorecardPoints:       resp.GetScorecardPoints(),
		CalibratedPD:          resp.GetCalibratedPd(),
		PDBasisPoints:         resp.GetPdBasisPoints(),
		RawMLLogit:            resp.GetRawLogit(),
		RiskTier:              resp.GetRiskTier(),
		RecommendedOutcome:    resp.GetRecommendedOutcome(),
		MaxApprovedLimitCents: resp.GetMaxApprovedLimitCents(),
		RecommendedAPRBps:     resp.GetRecommendedAprBps(),
		ExecutionTimeMicros:   resp.GetExecutionTimeMicros(),
		ModelVersion:          resp.GetModelVersion(),
	}
	for _, a := range resp.GetFeatureAttributions() {
		result.FeatureAttributions = append(result.FeatureAttributions, domain.FeatureAttribution{
			FeatureName:  a.GetFeatureName(),
			FeatureValue: a.GetFeatureValue(),
			ShapValue:    a.GetShapValue(),
			Rank:         a.GetRank(),
		})
	}
	for _, r := range resp.GetAdverseActionReasons() {
		result.AdverseActionReasons = append(result.AdverseActionReasons, domain.AdverseActionReason{
			ReasonCode:   r.GetReasonCode(),
			Description:  r.GetDescription(),
			PriorityRank: r.GetPriorityRank(),
		})
	}
	return result, nil
}
