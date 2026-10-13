package api

import (
	"testing"
	"time"

	"credivect/core/internal/domain"
)

func fullApplication() domain.CreditApplication {
	return domain.CreditApplication{
		ID:          "01a12cb4-385f-7e4d-b675-266b5a311745",
		ApplicantID: "01a12cb4-385d-7733-8b15-70390b1d8a42",
		Applicant: &domain.Applicant{
			ID:                 "01a12cb4-385d-7733-8b15-70390b1d8a42",
			ExternalID:         "seed-015-victor-tanaka",
			SSNBlindIndex:      "blind-index-value",
			FirstName:          "Victor",
			LastName:           "Tanaka",
			Email:              "victor.tanaka15@example.com",
			DateOfBirth:        "1996-05-28",
			MonthlyIncomeCents: 184354,
			CurrencyCode:       "USD",
			EmploymentStatus:   "EMPLOYED",
			CreatedAt:          time.Unix(1760000000, 0).UTC(),
		},
		RequestedAmountCents: 2055939,
		CurrencyCode:         "USD",
		LoanTermMonths:       57,
		LoanPurpose:          "HOME_IMPROVEMENT",
		Status:               domain.StatusApproved,
		WorkflowID:           "underwriting-01a12cb4",
		Version:              3,
		BureauReport: &domain.BureauReport{
			ID: "01a12cb4-3a88-76a3-861f-02d90f001d76", ApplicationID: "01a12cb4",
			BureauName: "CREDIVECT-SIM", BureauScore: 560, TotalDebtCents: 41387,
			RevolvingBalanceCents: 843588, RevolvingLimitCents: 2966205, RevolvingUtilizationBps: 2844,
			Delinquencies24m: 3, Inquiries6m: 9, TotalTrades: 11, OpenTrades: 6,
			BankruptcyFlag: true, AgeOfOldestTradeMonths: 101, RawPayload: `{"secret":"file"}`,
			ValidTime: time.Unix(1760000001, 0).UTC(), TransactionTime: time.Unix(1760000001, 0).UTC(),
		},
		ScoringResult: &domain.ScoringResult{
			ID: "01a12cb4-3bb8", RiskTier: "NEAR_PRIME", ScorecardPoints: 618, RecommendedOutcome: "MANUAL_REVIEW",
		},
		Decision:  &domain.Decision{Outcome: "APPROVED", DecisionType: "MANUAL_OVERRIDE", ApprovedLimitCents: 1542000},
		CreatedAt: time.Unix(1760000002, 0).UTC(),
		UpdatedAt: time.Unix(1760000003, 0).UTC(),
	}
}

func TestRedactForBorrowerDropsApplicantPII(t *testing.T) {
	got := redactForBorrower(fullApplication())
	a := got.Applicant
	if a == nil {
		t.Fatal("applicant must stay present so the borrower page can address the file")
	}
	if a.Email != "" || a.DateOfBirth != "" || a.ExternalID != "" || a.EmploymentStatus != "" {
		t.Errorf("identity fields leaked: %+v", a)
	}
	if a.MonthlyIncomeCents != 0 {
		t.Errorf("income leaked: %d", a.MonthlyIncomeCents)
	}
	if a.FirstName != "Victor" || a.LastName != "Tanaka" {
		t.Errorf("the applicant's own name was dropped: %+v", a)
	}
}

func TestRedactForBorrowerDropsConsumerReportDetail(t *testing.T) {
	got := redactForBorrower(fullApplication())
	b := got.BureauReport
	if b == nil {
		t.Fatal("bureau report must stay present: the status page shows the reported score")
	}
	if b.TotalDebtCents != 0 || b.RevolvingBalanceCents != 0 || b.RevolvingLimitCents != 0 ||
		b.RevolvingUtilizationBps != 0 || b.Delinquencies24m != 0 || b.Inquiries6m != 0 ||
		b.TotalTrades != 0 || b.OpenTrades != 0 || b.AgeOfOldestTradeMonths != 0 || b.BankruptcyFlag {
		t.Errorf("tradeline detail leaked: %+v", b)
	}
	if b.RawPayload != "" {
		t.Errorf("raw bureau payload leaked: %s", b.RawPayload)
	}
	if b.BureauName != "CREDIVECT-SIM" || b.BureauScore != 560 {
		t.Errorf("the FCRA disclosure agency/score were dropped: %+v", b)
	}
}

// The borrower status page is built from these fields; redaction must not break it.
func TestRedactForBorrowerKeepsTheBorrowerView(t *testing.T) {
	got := redactForBorrower(fullApplication())
	if got.ID == "" || got.Status != domain.StatusApproved || got.WorkflowID == "" {
		t.Errorf("application header dropped: %+v", got)
	}
	if got.RequestedAmountCents != 2055939 || got.LoanTermMonths != 57 || got.CurrencyCode != "USD" {
		t.Errorf("loan terms dropped: %+v", got)
	}
	if got.ScoringResult == nil || got.ScoringResult.RiskTier != "NEAR_PRIME" ||
		got.ScoringResult.ScorecardPoints != 618 || got.ScoringResult.RecommendedOutcome != "MANUAL_REVIEW" {
		t.Errorf("scoring detail dropped: %+v", got.ScoringResult)
	}
	if got.Decision == nil || got.Decision.DecisionType != "MANUAL_OVERRIDE" || got.Decision.ApprovedLimitCents != 1542000 {
		t.Errorf("decision dropped: %+v", got.Decision)
	}
}

func TestRedactForBorrowerDoesNotMutateTheCaller(t *testing.T) {
	app := fullApplication()
	_ = redactForBorrower(app)
	if app.Applicant.Email == "" || app.BureauReport.TotalDebtCents == 0 {
		t.Error("redaction mutated the caller's record, which would corrupt a shared read")
	}
}

// The applicant's external reference used to be the first eight hex characters of
// their UUIDv7. Those 32 bits are the millisecond timestamp shifted down, so two
// people originating inside the same 65-second window were handed the same
// reference and the second insert died on the unique constraint as an HTTP 500.
func TestApplicantExternalReferenceCannotCollide(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		ref := "user-" + domain.NewUUIDv7()
		if seen[ref] {
			t.Fatalf("two applicants were handed the same external reference %s", ref)
		}
		seen[ref] = true
		if len(ref) > 100 {
			t.Fatalf("reference %q exceeds the 100 character column", ref)
		}
	}
}
