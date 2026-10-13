package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"credivect/core/internal/auth"
	"credivect/core/internal/domain"
	"credivect/core/internal/security"
	"credivect/core/internal/workflow"
)

type CreateApplicationRequest struct {
	FirstName            string `json:"first_name"`
	LastName             string `json:"last_name"`
	Email                string `json:"email"`
	DateOfBirth          string `json:"date_of_birth"`
	SSN                  string `json:"ssn"`
	MonthlyIncomeCents   int64  `json:"monthly_income_cents"`
	RequestedAmountCents int64  `json:"requested_amount_cents"`
	LoanTermMonths       int32  `json:"loan_term_months"`
	LoanPurpose          string `json:"loan_purpose"`
	EmploymentStatus     string `json:"employment_status"`
	ConsentFCRA          bool   `json:"consent_fcra"`
}

// HandleCreateApplication persists the submission, starts the durable
// underwriting saga and returns before scoring finishes. The queue and status
// page then read the saga's writes, so a browser close or a core restart cannot
// lose the application.
func (s *Server) HandleCreateApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if allowed, err := s.cache.AllowRequest(r.Context(), "submit:"+clientKey(r), s.rateLimit, s.rateWindow); err != nil {
		writeError(w, http.StatusServiceUnavailable, "idempotency store unavailable")
		return
	} else if !allowed {
		writeError(w, http.StatusTooManyRequests, "submission rate limit exceeded; try again later")
		return
	}

	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key != "" {
		if cached, found, err := s.cache.ReplayIdempotency(r.Context(), key); err == nil && found {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotent-Replay", "true")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(cached)
			return
		}
		claimed, err := s.cache.ClaimIdempotency(r.Context(), key, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "idempotency store unavailable")
			return
		}
		if !claimed {
			writeError(w, http.StatusConflict, "duplicate submission inside the 24 hour idempotency window")
			return
		}
	}

	var req CreateApplicationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.releaseKey(key)
		writeError(w, http.StatusBadRequest, "request body must be valid JSON: "+err.Error())
		return
	}
	req.LoanPurpose = strings.ToUpper(strings.TrimSpace(req.LoanPurpose))

	// A claim is only spent by a request the server actually processes. Releasing
	// it here lets the borrower correct the offending field and resubmit with the
	// same key instead of being told the submission is a duplicate.
	if fields := validateApplication(&req); fields != nil {
		s.releaseKey(key)
		writeFieldErrors(w, fields)
		return
	}
	if !req.ConsentFCRA {
		s.releaseKey(key)
		writeFieldErrors(w, map[string]string{"consent_fcra": "FCRA permissible-purpose consent is required to pull a credit file"})
		return
	}

	blindIndex := s.hasher.BlindIndex(req.SSN)
	applicantID := domain.NewUUIDv7()
	var reapplication bool
	if existing, err := s.repo.FindApplicantByBlindIndex(r.Context(), blindIndex); err == nil {
		// A second pull on the same consumer file is a re-application, not a new
		// borrower record. The applicant identity is the existing row's id: the
		// unique blind index below refuses a second row for the same person, and
		// every later read follows this id to find both cases.
		req.FirstName, req.LastName, req.Email = existing.FirstName, existing.LastName, existing.Email
		applicantID, reapplication = existing.ID, true
	}

	sealed, err := s.hasher.Encrypt(req.SSN)
	if err != nil {
		s.releaseKey(key)
		writeError(w, http.StatusInternalServerError, "unable to protect the submitted identifier")
		return
	}
	req.SSN = ""

	now := time.Now().UTC()
	applicant := domain.Applicant{
		ID: applicantID, ExternalID: "user-" + applicantID,
		SSNBlindIndex: blindIndex, FirstName: strings.TrimSpace(req.FirstName),
		LastName: strings.TrimSpace(req.LastName), Email: strings.ToLower(strings.TrimSpace(req.Email)),
		DateOfBirth: req.DateOfBirth, MonthlyIncomeCents: req.MonthlyIncomeCents,
		CurrencyCode: "USD", EmploymentStatus: normaliseEmployment(req.EmploymentStatus), CreatedAt: now,
	}
	if reapplication {
		// The file the score is computed from has to be current, so a
		// re-application refreshes the income and employment the borrower declares
		// rather than scoring them against a stale record.
		if err := s.repo.RefreshApplicant(r.Context(), applicant); err != nil {
			s.storageFailure(w, key, "could not refresh the applicant record", err)
			return
		}
	} else if err := s.repo.InsertApplicant(r.Context(), applicant, blindIndex, sealed); err != nil {
		s.storageFailure(w, key, "could not store the applicant record", err)
		return
	}

	appID := domain.NewUUIDv7()
	app := domain.CreditApplication{
		ID: appID, ApplicantID: applicantID, Applicant: &applicant,
		RequestedAmountCents: req.RequestedAmountCents, CurrencyCode: "USD",
		LoanTermMonths: req.LoanTermMonths, LoanPurpose: req.LoanPurpose,
		Status: domain.StatusSubmitted, WorkflowID: "underwriting-" + appID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.InsertApplication(r.Context(), app); err != nil {
		s.storageFailure(w, key, "could not store the application", err)
		return
	}

	// The capability token is the applicant's only credential for the rest of
	// this case. It is generated here, returned exactly once, and persisted as a
	// digest so neither a table read nor a backup grants access.
	statusToken, err := security.NewStatusToken()
	if err != nil {
		s.releaseKey(key)
		writeError(w, http.StatusInternalServerError, "could not issue the application status credential")
		return
	}
	if err := s.repo.SetStatusTokenHash(r.Context(), appID, security.HashStatusToken(statusToken)); err != nil {
		s.storageFailure(w, key, "could not store the application status credential", err)
		return
	}

	// An application originated by a signed-in borrower binds the account to the
	// consumer record it was filed against, and that is the only way the link is
	// ever created. Nothing here trusts a claimed email address: submitting while
	// authenticated proves the person controls both the account and the file.
	s.linkBorrowerAccount(r, applicantID)

	submitted := map[string]any{
		"requested_amount_cents": app.RequestedAmountCents, "loan_term_months": app.LoanTermMonths,
		"loan_purpose": app.LoanPurpose, "currency_code": app.CurrencyCode,
	}
	if _, err := s.repo.AppendAudit(r.Context(), appID, "APPLICATION_SUBMITTED", "API", submitted); err != nil {
		s.storageFailure(w, key, "could not write the audit ledger entry", err)
		return
	}

	runID, _, err := s.temporal.StartUnderwriting(r.Context(), appID)
	if err != nil {
		s.releaseKey(key)
		writeError(w, http.StatusServiceUnavailable, "underwriting saga could not be started: "+err.Error())
		return
	}
	app.WorkflowID = runID

	response := map[string]any{
		"application": app, "workflow_id": runID, "status": string(domain.StatusSubmitted),
		"status_token": statusToken,
	}
	body, err := json.Marshal(response)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "response encoding failed")
		return
	}
	if key != "" {
		if err := s.cache.SaveIdempotency(r.Context(), key, body, 24*time.Hour); err != nil {
			s.releaseKey(key)
		}
	}
	writeRaw(w, http.StatusCreated, body)
}

func (s *Server) releaseKey(key string) {
	if key != "" {
		_ = s.cache.ReleaseIdempotency(context.Background(), key)
	}
}

// storageFailure answers a failed write. The client is told nothing about the
// database, so the cause only survives for an operator if it is logged here.
func (s *Server) storageFailure(w http.ResponseWriter, key, message string, err error) {
	slog.Error(message, "error", err)
	s.releaseKey(key)
	writeError(w, http.StatusInternalServerError, message)
}

func (s *Server) HandleListApplications(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 1_000_000)
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	tier := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("tier")))

	apps, err := s.repo.ListApplications(r.Context(), status, tier, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read applications: "+err.Error())
		return
	}
	counts, err := s.repo.CountApplicationsByStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read application counts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applications": apps, "count": len(apps), "limit": limit, "offset": offset, "by_status": counts,
	})
}

func (s *Server) HandleGetApplication(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	scope, ok := s.authorizeBorrowerRead(w, r, id)
	if !ok {
		return
	}
	app, err := s.repo.GetApplication(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if scope == scopeToken {
		app = redactForBorrower(app)
	}
	writeJSON(w, http.StatusOK, app)
}

// redactForBorrower narrows an application to the borrower's own view. The
// status page needs the score, the decision and the explanations, and none of
// the underwriting file. That matters because the capability token travels in a
// URL for the notice download, so it is more exposed than a session would be.
func redactForBorrower(app domain.CreditApplication) domain.CreditApplication {
	if app.Applicant != nil {
		a := *app.Applicant
		a.ExternalID, a.Email, a.DateOfBirth, a.EmploymentStatus = "", "", "", ""
		a.MonthlyIncomeCents = 0
		app.Applicant = &a
	}
	if app.BureauReport != nil {
		b := *app.BureauReport
		b.TotalDebtCents, b.RevolvingBalanceCents, b.RevolvingLimitCents = 0, 0, 0
		b.RevolvingUtilizationBps, b.Delinquencies24m, b.Inquiries6m = 0, 0, 0
		b.TotalTrades, b.OpenTrades, b.AgeOfOldestTradeMonths = 0, 0, 0
		b.BankruptcyFlag = false
		b.RawPayload = ""
		app.BureauReport = &b
	}
	return app
}

type UnderwriterReviewRequest struct {
	Decision      string `json:"decision"`
	ApprovedLimit int64  `json:"approved_limit_cents"`
	ApprovedAPR   int32  `json:"approved_apr_bps"`
	Notes         string `json:"notes"`
}

// HandleReviewDecision is the human-in-the-loop edge of the saga: it never
// writes a verdict itself, it signals the parked workflow, which owns the
// transition. That keeps the 72 hour timer and the override consistent.
func (s *Server) HandleReviewDecision(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	claims, err := s.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req UnderwriterReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON: "+err.Error())
		return
	}
	req.Decision = strings.ToUpper(strings.TrimSpace(req.Decision))
	if req.Decision != "APPROVE" && req.Decision != "DECLINE" {
		writeFieldErrors(w, map[string]string{"decision": "must be APPROVE or DECLINE"})
		return
	}
	if req.Decision == "APPROVE" {
		if req.ApprovedLimit <= 0 {
			writeFieldErrors(w, map[string]string{"approved_limit_cents": "an approval requires a limit greater than zero"})
			return
		}
		if req.ApprovedAPR <= 0 || req.ApprovedAPR > 3600 {
			writeFieldErrors(w, map[string]string{"approved_apr_bps": "an approval requires an APR between 1 and 3600 bps"})
			return
		}
	}
	if len(req.Notes) > 2000 {
		writeFieldErrors(w, map[string]string{"notes": "must not exceed 2000 characters"})
		return
	}

	app, err := s.repo.GetApplication(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	if app.Status != domain.StatusInReview {
		writeError(w, http.StatusConflict, fmt.Sprintf("application is %s and is not awaiting review", app.Status))
		return
	}
	signalErr := s.temporal.SignalReviewer(r.Context(), app.WorkflowID, "", workflow.ReviewerDecision{
		Decision: req.Decision, ApprovedLimit: req.ApprovedLimit, ApprovedAPR: req.ApprovedAPR,
		Notes: req.Notes, UnderwriterID: claims.UserID,
	})
	if signalErr != nil {
		writeError(w, http.StatusServiceUnavailable, "the review signal could not be delivered to the underwriting saga: "+signalErr.Error())
		return
	}
	// Signal accepted; the workflow owns the status transition and audit write.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"application_id": app.ID, "workflow_id": app.WorkflowID,
		"signal": workflow.SignalReviewerDecision, "decision": req.Decision,
	})
}

func (s *Server) HandleWorkflowState(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	if _, ok := s.authorizeBorrowerRead(w, r, id); !ok {
		return
	}
	app, err := s.repo.GetApplication(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "application not found")
		return
	}
	state, err := s.temporal.State(r.Context(), app.WorkflowID, "")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "workflow state unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) principal(r *http.Request) (*auth.Claims, error) {
	c, err := auth.From(r.Context())
	if err != nil {
		return nil, err
	}
	return c, nil
}

func normaliseEmployment(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	switch v {
	case "EMPLOYED", "SELF_EMPLOYED", "RETIRE", "STUDENT", "UNEMPLOYED":
		if v == "RETIRE" {
			return "RETIRED"
		}
		return v
	default:
		return "EMPLOYED"
	}
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
