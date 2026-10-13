package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"credivect/core/internal/domain"
	"credivect/core/internal/security"
	"credivect/core/internal/storage"
	"credivect/core/internal/workflow"
)

var firstNames = []string{"Alex", "Priya", "Marcus", "Dana", "Elena", "Tobias", "Nia", "Victor", "Hana", "Owen"}
var lastNames = []string{"Chen", "Raman", "Whitfield", "Okafor", "Castellanos", "Lindqvist", "Brooks", "Sokolov", "Tanaka", "Delgado"}
var purposes = []string{"DEBT_CONSOLIDATION", "MEDICAL", "HOME_IMPROVEMENT", "AUTO", "EDUCATION", "SMALL_BUSINESS"}

// incomeBands spans the file from thin subprime income to affluent prime income
// so the seeded book lands in every risk tier the policy defines.
var incomeBands = [][2]int64{
	{800_00, 2_500_00},
	{2_500_00, 4_500_00},
	{4_500_00, 7_500_00},
	{7_500_00, 12_000_00},
	{12_000_00, 28_000_00},
}

func originate(ctx context.Context, repo *storage.Repository, hasher *security.Hasher,
	runtime *workflow.Runtime, rng *rand.Rand, index int) (domain.CreditApplication, string, error) {

	band := incomeBands[index%len(incomeBands)]
	income := band[0] + int64(rng.Int63n(band[1]-band[0]))
	amount := 100_000 + int64(rng.Int63n(2_900_000)) // $1,000 - $30,000
	term := int32(6 + rng.Intn(79))
	first := firstNames[rng.Intn(len(firstNames))]
	last := lastNames[rng.Intn(len(lastNames))]
	dob := fmt.Sprintf("%04d-%02d-%02d", 1968+rng.Intn(36), 1+rng.Intn(12), 1+rng.Intn(28))
	ssn := fmt.Sprintf("%09d", 100_000_000+rng.Intn(899_999_999))
	email := fmt.Sprintf("%s.%s%d@example.com", lower(first), lower(last), index)
	if index == 0 {
		// The first case is the one the seeded borrower account is bound to, so it
		// carries that person's identity rather than a random one.
		first, last, dob, email = "Alex", "Chen", "1991-03-14", "alex.chen@example.com"
	}
	external := fmt.Sprintf("seed-%03d-%s-%s", index, lower(first), lower(last))

	blind := hasher.BlindIndex(ssn)
	sealed, err := hasher.Encrypt(ssn)
	if err != nil {
		return domain.CreditApplication{}, "", err
	}

	now := time.Now().UTC().Add(-time.Duration(index) * time.Minute)
	applicant := domain.Applicant{
		ID: domain.NewUUIDv7(), ExternalID: external, SSNBlindIndex: blind,
		FirstName: first, LastName: last, Email: email,
		DateOfBirth: dob, MonthlyIncomeCents: income, CurrencyCode: "USD",
		EmploymentStatus: "EMPLOYED", CreatedAt: now,
	}
	if err := repo.InsertApplicant(ctx, applicant, blind, sealed); err != nil {
		return domain.CreditApplication{}, "", fmt.Errorf("insert applicant: %w", err)
	}

	app := domain.CreditApplication{
		ID: domain.NewUUIDv7(), ApplicantID: applicant.ID, Applicant: &applicant,
		RequestedAmountCents: amount, CurrencyCode: "USD", LoanTermMonths: term,
		LoanPurpose: purposes[rng.Intn(len(purposes))], Status: domain.StatusSubmitted,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	app.WorkflowID = "underwriting-" + app.ID
	if err := repo.InsertApplication(ctx, app); err != nil {
		return app, "", fmt.Errorf("insert application: %w", err)
	}

	// The seeder mints the same credential the API issues at origination, so a
	// seeded case is walkable from the borrower's side rather than only from the
	// underwriter's.
	statusToken, err := security.NewStatusToken()
	if err != nil {
		return app, "", fmt.Errorf("status token: %w", err)
	}
	if err := repo.SetStatusTokenHash(ctx, app.ID, security.HashStatusToken(statusToken)); err != nil {
		return app, "", fmt.Errorf("store status token: %w", err)
	}
	if _, err := repo.AppendAudit(ctx, app.ID, "APPLICATION_SUBMITTED", "SEED", map[string]any{
		"requested_amount_cents": amount, "loan_term_months": term, "loan_purpose": app.LoanPurpose,
	}); err != nil {
		return app, "", fmt.Errorf("audit submit: %w", err)
	}
	if _, runID, err := runtime.StartUnderwriting(ctx, app.ID); err != nil {
		return app, "", fmt.Errorf("start saga: %w", err)
	} else if runID == "" {
		return app, "", fmt.Errorf("saga returned no run id")
	}
	return app, statusToken, nil
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

// waitForBook polls until every seeded application has left SUBMITTED, which
// means the saga has at least pulled the bureau file and scored it.
func waitForBook(ctx context.Context, repo *storage.Repository, mvID string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		counts, err := repo.CountApplicationsByStatus(ctx)
		if err != nil {
			log.Printf("seed: status poll failed: %v", err)
			return
		}
		if counts[string(domain.StatusSubmitted)] == 0 {
			log.Printf("seed: book settled %v", counts)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	log.Printf("seed: book did not fully settle within %s; some sagas are still running", timeout)
}

// signalOpenReviews exercises the human-in-the-loop edge of the saga exactly the
// way the underwriter queue does: a Temporal signal, not a direct row write.
func signalOpenReviews(ctx context.Context, repo *storage.Repository, runtime *workflow.Runtime) {
	apps, err := repo.ListApplications(ctx, string(domain.StatusInReview), "", 2, 0)
	if err != nil {
		log.Printf("seed: could not list review queue: %v", err)
		return
	}
	for i, app := range apps {
		decision := workflow.ReviewerDecision{Notes: "seeded adjudication", UnderwriterID: seededUnderwriterID}
		if i == 0 && app.ScoringResult != nil {
			decision.Decision = "APPROVE"
			decision.ApprovedLimit = app.ScoringResult.MaxApprovedLimitCents
			decision.ApprovedAPR = app.ScoringResult.RecommendedAPRBps
		} else {
			decision.Decision = "DECLINE"
			decision.Notes = "seeded adjudication: insufficient income stability"
		}
		if err := runtime.SignalReviewer(ctx, app.WorkflowID, "", decision); err != nil {
			log.Printf("seed: signal to %s failed: %v", app.WorkflowID, err)
			continue
		}
		log.Printf("seed: signalled %s on %s", decision.Decision, app.ID)
	}
	time.Sleep(3 * time.Second)
}

const seededUnderwriterID = "01918a2b-4c5d-7000-8000-0000000000a1"

func truncate(ctx context.Context, repo *storage.Repository) error {
	// TRUNCATE bypasses the append-only row triggers, which is what a deliberate
	// reseed of a development book needs.
	_, err := repo.Pool().Exec(ctx, `
		TRUNCATE TABLE drift_metrics, decisions, adverse_action_reasons, shap_explanations,
		               scoring_runs, feature_vectors, bureau_reports, audit_ledger,
		               credit_applications, applicants RESTART IDENTITY CASCADE`)
	return err
}
