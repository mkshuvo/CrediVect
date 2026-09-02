package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"credivect/core/internal/api"
	"credivect/core/internal/cache"
	"credivect/core/internal/client"
	"credivect/core/internal/domain"
	"credivect/core/internal/storage"
)

// CORS middleware
func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	repo := storage.NewRepository()
	dragonfly := cache.NewDragonflyClient()
	scoring := client.NewScoringClient()

	// Seed an initial near-prime application so underwriter dashboard has live data on boot
	seedInitialData(repo)

	server := api.NewServer(repo, dragonfly, scoring)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP","service":"credivect-core"}`))
	})

	// Auth routes (Pluggable Local Provider)
	mux.HandleFunc("/api/v1/auth/login", server.HandleLogin)
	mux.HandleFunc("/api/v1/auth/me", server.HandleMe)

	// Applications & Underwriting routes
	mux.HandleFunc("/api/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			server.HandleCreateApplication(w, r)
		} else {
			server.HandleListApplications(w, r)
		}
	})
	mux.HandleFunc("/api/v1/applications/get", server.HandleGetApplication)
	mux.HandleFunc("/api/v1/applications/review", server.HandleReviewDecision)
	mux.HandleFunc("/api/v1/governance/ledger", server.HandleAuditLedger)

	log.Printf("🚀 CrediVect Core API Gateway listening on port %s", port)
	if err := http.ListenAndServe(":"+port, enableCORS(mux)); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func seedInitialData(repo *storage.Repository) {
	appID := "01918a2b-4c5d-7000-8000-000000000099"
	applicantID := "01918a2b-4c5d-7000-8000-000000000098"

	applicant := domain.Applicant{
		ID:                 applicantID,
		ExternalID:         "user-alex-chen",
		FirstName:          "Alex",
		LastName:           "Chen",
		Email:              "alex.chen@example.com",
		DateOfBirth:        "1994-06-15",
		MonthlyIncomeCents: 500000, // $5,000.00
		CurrencyCode:       "USD",
		EmploymentStatus:   "EMPLOYED",
	}
	repo.SaveApplicant(applicant)

	app := domain.CreditApplication{
		ID:                   appID,
		ApplicantID:          applicantID,
		Applicant:            &applicant,
		RequestedAmountCents: 1000000, // $10,000.00
		CurrencyCode:         "USD",
		LoanTermMonths:       36,
		LoanPurpose:          "DEBT_CONSOLIDATION",
		Status:               domain.StatusInReview, // Pending underwriter review
		WorkflowID:           "wf-alex-chen-99",
		Version:              1,
		BureauReport: &domain.BureauReport{
			ID:                      domain.NewUUIDv7(),
			ApplicationID:           appID,
			BureauName:              "EXPERIAN",
			BureauScore:             674,
			TotalDebtCents:          170000, // $1,700.00
			RevolvingBalanceCents:   425000, // $4,250.00
			RevolvingLimitCents:     1000000, // $10,000.00
			RevolvingUtilizationBps: 4250,    // 42.50%
			Delinquencies24m:        1,
			Inquiries6m:             2,
			TotalTrades:             8,
			OpenTrades:              5,
		},
		ScoringResult: &domain.ScoringResult{
			ScorecardPoints:       674,
			CalibratedPD:          "0.046500",
			PDBasisPoints:         465,
			RiskTier:              "NEAR_PRIME",
			RecommendedOutcome:    "MANUAL_REVIEW",
			MaxApprovedLimitCents: 750000, // $7,500.00
			RecommendedAPRBps:     1240,   // 12.40%
			FeatureAttributions: []domain.FeatureAttribution{
				{FeatureName: "revolving_utilization_bps", FeatureValue: "42.50%", ShapValue: 0.28, Rank: 1},
				{FeatureName: "delinquencies_24m", FeatureValue: "1", ShapValue: 0.15, Rank: 2},
				{FeatureName: "dti_bps", FeatureValue: "34.00%", ShapValue: -0.12, Rank: 3},
				{FeatureName: "inquiries_6m", FeatureValue: "2", ShapValue: -0.08, Rank: 4},
			},
			AdverseActionReasons: []domain.AdverseActionReason{
				{ReasonCode: "AA01", Description: "Proportion of balances to credit limits on revolving accounts is too high", PriorityRank: 1},
				{ReasonCode: "AA02", Description: "Delinquency reported on credit accounts", PriorityRank: 2},
			},
			ExecutionTimeMicros:   720,
			ModelVersion:          "v1.0.0",
		},
	}
	repo.SaveApplication(app)
	repo.AppendAuditRecord(appID, "APPLICATION_EVALUATED", "SYSTEM", fmt.Sprintf(`{"app_id":"%s","status":"IN_REVIEW"}`, appID))
}
