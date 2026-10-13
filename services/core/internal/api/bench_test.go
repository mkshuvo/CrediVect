package api

import (
	"testing"
	"time"

	"credivect/core/internal/domain"
)

// Redaction runs on the way out of every token-scoped read. It copies the
// applicant and the credit file, so the benchmark exists to keep that copy from
// becoming the expensive part of an otherwise cheap request.
func BenchmarkRedactForBorrower(b *testing.B) {
	now := time.Date(2026, time.October, 13, 8, 0, 0, 0, time.UTC)
	app := domain.CreditApplication{
		ID:                   "01918a2b-4c5d-7000-8000-000000000001",
		ApplicantID:          "01918a2b-4c5d-7000-8000-000000000002",
		RequestedAmountCents: 2400000,
		CurrencyCode:         "USD",
		LoanTermMonths:       36,
		LoanPurpose:          "DEBT_CONSOLIDATION",
		Status:               domain.StatusInReview,
		CreatedAt:            now,
		UpdatedAt:            now,
		Applicant: &domain.Applicant{
			ID:                 "01918a2b-4c5d-7000-8000-000000000002",
			ExternalID:         "user-01918a2b-4c5d-7000-8000-000000000002",
			FirstName:          "Priya",
			LastName:           "Raman",
			Email:              "priya.raman@example.com",
			DateOfBirth:        "1991-04-17",
			MonthlyIncomeCents: 780000,
			EmploymentStatus:   "FULL_TIME",
			CreatedAt:          now,
		},
		BureauReport: &domain.BureauReport{
			ID:                      "01918a2b-4c5d-7000-8000-000000000003",
			ApplicationID:           "01918a2b-4c5d-7000-8000-000000000001",
			BureauName:              "CREDIVECT-SIM",
			BureauScore:             712,
			TotalDebtCents:          1840000,
			RevolvingBalanceCents:   920000,
			RevolvingLimitCents:     2100000,
			RevolvingUtilizationBps: 4381,
			Delinquencies24m:        1,
			Inquiries6m:             3,
			TotalTrades:             14,
			OpenTrades:              9,
			AgeOfOldestTradeMonths:  101,
			RawPayload:              `{"file":"with consumer detail"}`,
			ValidTime:               now,
			TransactionTime:         now,
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		redacted := redactForBorrower(app)
		if redacted.BureauReport.RawPayload != "" {
			b.Fatal("raw bureau payload survived redaction")
		}
	}
}
