// Command e2e drives one complete loan through a running CrediVect stack and
// checks the invariants the product promises, rather than the ones a unit test
// can invent. It exists because every gate here is a seam between two services:
// origination hands off to the saga, the saga hands off to the scoring engine,
// the read path hands the same record to three different audiences, and the
// ledger is only tamper-evident if the hash that was written is the hash that
// comes back.
//
// It runs against the real API over HTTP with the seeded accounts, so it needs
// the stack up (make up && make seed). Any failed assertion exits non-zero,
// which is what makes `make e2e` usable as a release gate.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"credivect/core/internal/domain"
)

type harness struct {
	base   string
	client *http.Client
	passed int
	failed int
}

func (h *harness) ok(check string, detail ...string) {
	h.passed++
	line := "PASS  " + check
	if len(detail) > 0 {
		line += " (" + strings.Join(detail, ", ") + ")"
	}
	fmt.Println(line)
}

func (h *harness) fail(check string, err error, detail ...string) {
	h.failed++
	msg := "FAIL  " + check + ": " + err.Error()
	if len(detail) > 0 {
		msg += " [" + strings.Join(detail, ", ") + "]"
	}
	fmt.Println(msg)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Printf("FAIL  ignoring %s=%q: %v\n", key, v, err)
		return fallback
	}
	return d
}

// randomHex keeps every run of the harness on a fresh consumer file: the blind
// index is unique per applicant, so a reused SSN would be treated as a
// re-application and the checks below would compare against stale income.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

type apiError struct {
	status int
	body   string
}

func (a *apiError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", a.status, truncate(a.body, 300))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (h *harness) do(method, path string, body any, headers map[string]string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, h.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, raw, err
	}
	return resp.StatusCode, raw, nil
}

// call fails on any status other than the one expected.
func (h *harness) call(method, path string, body any, want int, headers map[string]string) ([]byte, error) {
	status, raw, err := h.do(method, path, body, headers)
	if err != nil {
		return raw, err
	}
	if status != want {
		return raw, &apiError{status: status, body: string(raw)}
	}
	return raw, nil
}

func (h *harness) login(email, password string) (map[string]any, error) {
	raw, err := h.call(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, http.StatusOK, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("login response was not JSON: %w", err)
	}
	if _, has := out["token"]; !has {
		return nil, fmt.Errorf("login response carried no session token")
	}
	return out, nil
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func statusCredential(token string) map[string]string {
	return map[string]string{"X-Status-Token": token}
}

// decodeApplication targets the real domain types rather than a loose map, so a
// renamed JSON field fails here instead of reading as a silent nil.
func decodeApplication(raw []byte) (domain.CreditApplication, error) {
	var app domain.CreditApplication
	if err := json.Unmarshal(raw, &app); err != nil {
		return app, fmt.Errorf("application response was not JSON: %w", err)
	}
	return app, nil
}

type submitResponse struct {
	Application domain.CreditApplication `json:"application"`
	WorkflowID  string                   `json:"workflow_id"`
	Status      string                   `json:"status"`
	StatusToken string                   `json:"status_token"`
}

func originationRequest(ssn, email string, incomeCents int64) map[string]any {
	return map[string]any{
		"first_name": "Elena", "last_name": "Vasquez", "email": email,
		"date_of_birth": "1988-07-19", "ssn": ssn,
		"monthly_income_cents": incomeCents, "requested_amount_cents": 1850000,
		"loan_term_months": 36, "loan_purpose": "DEBT_CONSOLIDATION",
		"employment_status": "FULL_TIME", "consent_fcra": true,
	}
}

func (h *harness) submit(body map[string]any, token string) (submitResponse, error) {
	headers := map[string]string{"Idempotency-Key": "e2e-" + randomHex(8)}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	raw, err := h.call(http.MethodPost, "/api/v1/applications", body, http.StatusCreated, headers)
	if err != nil {
		return submitResponse{}, err
	}
	var out submitResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("origination response was not JSON: %w", err)
	}
	return out, nil
}

// checkHealth is the precondition: without every dependency up, the rest of the
// run measures a broken stack rather than the product.
func (h *harness) checkHealth() string {
	raw, err := h.call(http.MethodGet, "/api/v1/health", nil, http.StatusOK, nil)
	if err != nil {
		h.fail("stack health", err)
		return ""
	}
	var body struct {
		Checks       map[string]string `json:"checks"`
		ModelVersion string            `json:"model_version"`
		EventStream  bool              `json:"event_stream"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		h.fail("stack health response parsed", err)
		return ""
	}
	// The aggregate is a key of the same map as the dependencies, so it is read
	// out rather than required to be UP twice.
	var down []string
	for name, state := range body.Checks {
		if name == "status" {
			continue
		}
		if state != "UP" {
			down = append(down, name+"="+state)
		}
	}
	if len(down) > 0 || body.Checks["status"] != "UP" || !body.EventStream {
		h.fail("every dependency reports UP", fmt.Errorf("status %q, event_stream=%t, down: %s",
			body.Checks["status"], body.EventStream, strings.Join(down, " ")))
		return ""
	}
	h.ok("every dependency reports UP", "model "+body.ModelVersion)
	return body.ModelVersion
}

// checkAnonFlow is the borrower who never signed in: the capability token is
// their only credential, so it has to work on the header, and nothing else may
// read the record.
func (h *harness) checkAnonFlow(sub submitResponse, ssn string) bool {
	id := sub.Application.ID
	if len(sub.StatusToken) != 43 {
		h.fail("origination returned a 43 character capability token",
			fmt.Errorf("got %d characters", len(sub.StatusToken)))
	} else {
		h.ok("origination returned a 43 character capability token")
	}

	if _, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id, nil,
		http.StatusUnauthorized, nil); err != nil {
		h.fail("an anonymous read of the record is refused", err)
	} else {
		h.ok("an anonymous read of the record is refused")
	}

	// A credential in a query string is written to access logs and browser
	// history, so the API has to ignore it rather than accept the easier path.
	if _, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id+"&t="+sub.StatusToken, nil,
		http.StatusUnauthorized, nil); err != nil {
		h.fail("a capability token in the query string is ignored", err)
	} else {
		h.ok("a capability token in the query string is ignored")
	}

	if _, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id, nil,
		http.StatusUnauthorized, statusCredential(strings.Repeat("x", 43))); err != nil {
		h.fail("a forged capability token is refused", err)
	} else {
		h.ok("a forged capability token is refused")
	}

	// The token holder sees the decision, not the underwriting file.
	redactedRaw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id, nil,
		http.StatusOK, statusCredential(sub.StatusToken))
	if err != nil {
		h.fail("the capability token reads the record", err)
		return false
	}
	redacted, err := decodeApplication(redactedRaw)
	if err != nil {
		h.fail("token-scoped read parsed", err)
		return false
	}
	if redacted.Applicant != nil && (redacted.Applicant.Email != "" || redacted.Applicant.MonthlyIncomeCents != 0 ||
		redacted.Applicant.ExternalID != "" || redacted.Applicant.EmploymentStatus != "") {
		h.fail("token-scoped read is redacted", fmt.Errorf("applicant still carries %q / income %d",
			redacted.Applicant.Email, redacted.Applicant.MonthlyIncomeCents))
	} else {
		h.ok("token-scoped read is redacted", "no email, income, external id or employment")
	}
	if redacted.BureauReport != nil && (redacted.BureauReport.RawPayload != "" ||
		redacted.BureauReport.TotalDebtCents != 0 || redacted.BureauReport.RevolvingLimitCents != 0) {
		h.fail("token-scoped read hides the credit file detail",
			fmt.Errorf("bureau payload %d bytes, debt %d", len(redacted.BureauReport.RawPayload),
				redacted.BureauReport.TotalDebtCents))
	} else {
		h.ok("token-scoped read hides the credit file detail")
	}
	if redacted.BureauReport != nil && redacted.BureauReport.BureauScore == 0 {
		h.fail("token-scoped read keeps the bureau score", fmt.Errorf("score was zero"))
	} else {
		h.ok("token-scoped read keeps the bureau score")
	}
	// The score the borrower is entitled to see must never be the SSN's, and the
	// request body value has to survive the round trip exactly: zero floats.
	if redacted.RequestedAmountCents != 1850000 || redacted.CurrencyCode != "USD" {
		h.fail("money survived the round trip in integer cents",
			fmt.Errorf("got %d %s", redacted.RequestedAmountCents, redacted.CurrencyCode))
	} else {
		h.ok("money survived the round trip in integer cents", "1850000 USD")
	}
	for _, leak := range []string{ssn} {
		if strings.Contains(string(redactedRaw), leak) {
			h.fail("the submitted SSN is never echoed back", fmt.Errorf("found %s in the response body", leak))
		} else {
			h.ok("the submitted SSN is never echoed back")
		}
	}
	return true
}

// awaitDecision polls the saga through the token holder's own endpoint and times
// the whole pipeline, which is the number the SLA is written against.
func (h *harness) awaitDecision(id, token string, budget time.Duration) (domain.CreditApplication, time.Duration, error) {
	start := time.Now()
	deadline := start.Add(budget)
	var last domain.CreditApplication
	for time.Now().Before(deadline) {
		raw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id, nil,
			http.StatusOK, statusCredential(token))
		if err != nil {
			return last, 0, err
		}
		last, err = decodeApplication(raw)
		if err != nil {
			return last, 0, err
		}
		if last.ScoringResult != nil && last.Status != domain.StatusSubmitted {
			return last, time.Since(start), nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last, time.Since(start), fmt.Errorf("saga did not reach a decision within %s (status %s, scored=%t)",
		budget, last.Status, last.ScoringResult != nil)
}

// checkScoring asserts the engine actually scored this file, that its output is
// inside the documented ranges, and that the version answering is the version
// that is registered as active.
func (h *harness) checkScoring(app domain.CreditApplication, activeModel string, took time.Duration) {
	res := app.ScoringResult
	if res == nil {
		h.fail("the saga produced a score", fmt.Errorf("scoring_result was absent"))
		return
	}
	h.ok("the saga produced a score", fmt.Sprintf("status %s after %s", app.Status, took.Round(time.Millisecond)))

	if res.ScorecardPoints < 300 || res.ScorecardPoints > 850 {
		h.fail("scorecard points stay inside 300-850", fmt.Errorf("got %d", res.ScorecardPoints))
	} else {
		h.ok("scorecard points stay inside 300-850", fmt.Sprintf("%d", res.ScorecardPoints))
	}
	var pd float64
	if _, err := fmt.Sscanf(res.CalibratedPD, "%g", &pd); err != nil {
		h.fail("calibrated PD parses as a fixed-point decimal", err)
	} else if pd < 0 || pd > 1 {
		h.fail("calibrated PD is a probability", fmt.Errorf("got %s", res.CalibratedPD))
	} else {
		h.ok("calibrated PD is a probability", res.CalibratedPD)
	}
	if res.ModelVersion == "" || res.ModelVersion != activeModel {
		h.fail("the serving engine reports the registered model version",
			fmt.Errorf("serving %q, registered %q", res.ModelVersion, activeModel))
	} else {
		h.ok("the serving engine reports the registered model version", res.ModelVersion)
	}
	if len(res.FeatureAttributions) == 0 {
		h.fail("the decision carries a per-feature attribution", fmt.Errorf("none returned"))
	} else {
		h.ok("the decision carries a per-feature attribution",
			fmt.Sprintf("%d features", len(res.FeatureAttributions)))
	}
	if res.RiskTier == "" || res.RecommendedOutcome == "" {
		h.fail("the decision carries a tier and a recommendation", fmt.Errorf("tier %q outcome %q", res.RiskTier, res.RecommendedOutcome))
	} else {
		h.ok("the decision carries a tier and a recommendation", res.RiskTier, res.RecommendedOutcome)
	}
	if res.MaxApprovedLimitCents < 0 || res.MaxApprovedLimitCents%1 != 0 {
		h.fail("the modelled limit is integer cents", fmt.Errorf("got %d", res.MaxApprovedLimitCents))
	} else {
		h.ok("the modelled limit is integer cents", fmt.Sprintf("%d", res.MaxApprovedLimitCents))
	}
	if res.AdverseActionReasons == nil {
		h.fail("reason codes were decoded (absent means the field name changed)", fmt.Errorf("nil slice"))
	} else {
		h.ok("reason codes were decoded", fmt.Sprintf("%d recorded", len(res.AdverseActionReasons)))
	}
}

// checkStaffRead confirms the same row is a different record to a different
// audience: the underwriting desk needs the file the borrower is shown redacted.
func (h *harness) checkStaffRead(id, staffToken, email string) {
	raw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+id, nil, http.StatusOK, bearer(staffToken))
	if err != nil {
		h.fail("a staff session reads the full record", err)
		return
	}
	app, err := decodeApplication(raw)
	if err != nil {
		h.fail("staff read parsed", err)
		return
	}
	if app.Applicant == nil || app.Applicant.Email != email || app.Applicant.MonthlyIncomeCents == 0 {
		h.fail("a staff session sees the applicant detail", fmt.Errorf("email %q", applicantEmail(app)))
		return
	}
	if app.BureauReport == nil || app.BureauReport.RawPayload == "" || app.BureauReport.TotalDebtCents == 0 {
		h.fail("a staff session sees the bureau file", fmt.Errorf("payload bytes %d", bureauPayloadLen(app)))
		return
	}
	h.ok("a staff session sees the full underwriting file", "income, debt and raw payload present")
}

func applicantEmail(app domain.CreditApplication) string {
	if app.Applicant == nil {
		return ""
	}
	return app.Applicant.Email
}

func bureauPayloadLen(app domain.CreditApplication) int {
	if app.BureauReport == nil {
		return -1
	}
	return len(app.BureauReport.RawPayload)
}

// checkNotice covers the statutory letter. Declined and referred cases must
// produce it; an approved case must refuse, because issuing an adverse action
// notice for an approval is itself a compliance defect.
func (h *harness) checkNotice(id, token string, app domain.CreditApplication) {
	adverse := app.Status == domain.StatusDeclined || app.Status == domain.StatusInReview
	status, raw, err := h.do(http.MethodGet, "/api/v1/applications/adverse-action.txt?id="+id, nil,
		statusCredential(token))
	if err != nil {
		h.fail("notice download request reached the API", err)
		return
	}
	body := string(raw)
	if adverse {
		if status != http.StatusOK || !strings.Contains(body, "ADVERSE ACTION NOTICE") {
			h.fail("a scored adverse case returns the statutory notice",
				fmt.Errorf("HTTP %d, body started %q", status, truncate(body, 60)))
			return
		}
		for _, heading := range []string{"Fair Credit Reporting Act", "Principal reasons", "Your rights", "model version"} {
			if !strings.Contains(body, heading) {
				h.fail("notice carries its required disclosures", fmt.Errorf("missing %q", heading))
				return
			}
		}
		h.ok("notice carries the FCRA disclosures", fmt.Sprintf("%d bytes", len(body)))
		return
	}
	if status == http.StatusConflict && strings.Contains(body, "adverse action notice applies") {
		h.ok("an approved case refuses an adverse action notice", truncate(body, 80))
		return
	}
	h.fail("an approved case refuses an adverse action notice",
		fmt.Errorf("HTTP %d, body started %q", status, truncate(body, 60)))
}

func (h *harness) sessionApplicant(token string) (string, error) {
	raw, err := h.call(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK, bearer(token))
	if err != nil {
		return "", err
	}
	var me map[string]any
	if err := json.Unmarshal(raw, &me); err != nil {
		return "", fmt.Errorf("profile response was not JSON: %w", err)
	}
	bound, has := me["applicant_id"]
	if !has {
		return "", fmt.Errorf("the profile carried no applicant_id")
	}
	id, _ := bound.(string)
	if id == "" {
		return "", fmt.Errorf("the account is not bound to a credit record yet")
	}
	return id, nil
}

// checkBorrowerAccount covers the Phase 7 promise using the rules that actually
// govern the binding. The seeded account already owns the record the seeder filed
// for it, so what must hold is that the link exists, that a later filing for a
// different consumer never moves it, and that the cases behind the link read
// unredacted on the session alone.
func (h *harness) checkBorrowerAccount(borrowerToken, ssn, email string) bool {
	linkedID, err := h.sessionApplicant(borrowerToken)
	if err != nil {
		h.fail("the borrower session is bound to a credit record", err)
		return false
	}
	h.ok("the borrower session is bound to a credit record", linkedID)

	sub, err := h.submit(originationRequest(ssn, email, 905000), borrowerToken)
	if err != nil {
		h.fail("a signed-in borrower can originate", err)
		return false
	}
	h.ok("a signed-in borrower can originate", "case "+sub.Application.ID)

	// The link is only ever created by an account that has none. Repointing it on
	// every filing would let a submission for a different consumer steal the
	// account's place, so the id has to survive this one unchanged.
	after, err := h.sessionApplicant(borrowerToken)
	if err != nil {
		h.fail("the link survives a second filing", err)
		return false
	}
	if after != linkedID {
		h.fail("the link survives a second filing", fmt.Errorf("%s became %s", linkedID, after))
		return false
	}
	if sub.Application.ApplicantID == linkedID {
		h.fail("a filing for a new consumer creates its own record",
			fmt.Errorf("resolved to the account's existing %s", linkedID))
		return false
	}
	h.ok("the link survives a second filing", "still "+after)

	queueRaw, err := h.call(http.MethodGet, "/api/v1/me/applications", nil, http.StatusOK, bearer(borrowerToken))
	if err != nil {
		h.fail("the borrower queue is readable", err)
		return false
	}
	var queue struct {
		Applications []domain.CreditApplication `json:"applications"`
		Count        int                        `json:"count"`
		Linked       bool                       `json:"applicant_linked"`
	}
	if err := json.Unmarshal(queueRaw, &queue); err != nil {
		h.fail("the borrower queue parsed", err)
		return false
	}
	if !queue.Linked || queue.Count < 1 || len(queue.Applications) == 0 {
		h.fail("the borrower queue reports the bound record", fmt.Errorf("linked=%t count=%d", queue.Linked, queue.Count))
		return false
	}
	for _, a := range queue.Applications {
		if a.ID == sub.Application.ID {
			h.fail("the queue stays scoped to the account's record",
				fmt.Errorf("listed a case filed against a different consumer file: %s", a.ID))
			return false
		}
		if a.ApplicantID != linkedID {
			h.fail("every queued case belongs to the linked record",
				fmt.Errorf("case %s is applicant %s", a.ID, a.ApplicantID))
			return false
		}
	}
	h.ok("the borrower queue is scoped to the linked record", fmt.Sprintf("%d case(s)", queue.Count))

	// The owner reads their own record unredacted, with no capability token at
	// all: the session is the credential.
	ownerID := queue.Applications[0].ID
	ownerRaw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+ownerID, nil,
		http.StatusOK, bearer(borrowerToken))
	if err != nil {
		h.fail("the owner session reads a case without a token", err)
		return false
	}
	owner, err := decodeApplication(ownerRaw)
	if err != nil {
		h.fail("owner read parsed", err)
		return false
	}
	if owner.Applicant == nil || owner.Applicant.Email == "" || owner.Applicant.MonthlyIncomeCents <= 0 ||
		owner.Applicant.DateOfBirth == "" {
		h.fail("the owner session sees its own unredacted data",
			fmt.Errorf("email %q income %d", applicantEmail(owner), incomeOf(owner)))
		return false
	}
	h.ok("the owner session sees its own unredacted data",
		fmt.Sprintf("%s, income %d", applicantEmail(owner), incomeOf(owner)))

	if owner.BureauReport == nil || owner.BureauReport.RawPayload == "" {
		h.fail("the owner session sees the bureau file used to score it",
			fmt.Errorf("payload bytes %d", bureauPayloadLen(owner)))
		return false
	}
	h.ok("the owner session sees the bureau file used to score it",
		fmt.Sprintf("%d bytes", len(owner.BureauReport.RawPayload)))
	return true
}

func incomeOf(app domain.CreditApplication) int64 {
	if app.Applicant == nil {
		return -1
	}
	return app.Applicant.MonthlyIncomeCents
}

// checkForeignRecord proves ownership is resolved per request: a borrower
// session is not a pass to every record, only to the one bound to the account.
func (h *harness) checkForeignRecord(borrowerToken, foreignID string) {
	if _, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+foreignID, nil,
		http.StatusUnauthorized, bearer(borrowerToken)); err != nil {
		h.fail("a borrower session cannot read someone else's record", err)
		return
	}
	h.ok("a borrower session cannot read someone else's record")
}

// checkGovernance walks the ledger and the latency rollup the dashboard shows.
func (h *harness) checkGovernance(staffToken, adminToken string) {
	raw, err := h.call(http.MethodGet, "/api/v1/governance/verify", nil, http.StatusOK, bearer(staffToken))
	if err != nil {
		h.fail("the audit chain verifies", err)
		return
	}
	var chain domain.ChainVerification
	if err := json.Unmarshal(raw, &chain); err != nil {
		h.fail("the audit chain response parsed", err)
		return
	}
	if !chain.Verified || chain.EntriesChecked < 1 {
		h.fail("the audit chain verifies", fmt.Errorf("verified=%t entries=%d reason %q",
			chain.Verified, chain.EntriesChecked, chain.Reason))
		return
	}
	h.ok("the audit chain verifies", fmt.Sprintf("%d entries, head %s", chain.EntriesChecked, truncate(chain.HeadHash, 16)))

	latencyRaw, err := h.call(http.MethodGet, "/api/v1/governance/latency?samples=2000", nil,
		http.StatusOK, bearer(staffToken))
	if err != nil {
		h.fail("the latency rollup is readable", err)
		return
	}
	var latency struct {
		ModelVersion string `json:"model_version"`
		SLATargetMS  int    `json:"sla_target_ms"`
		Measured     struct {
			Samples   int     `json:"samples"`
			P50Micros float64 `json:"p50_micros"`
			P95Micros float64 `json:"p95_micros"`
			P99Micros float64 `json:"p99_micros"`
			MaxMicros float64 `json:"max_micros"`
		} `json:"measured"`
		SLAMet bool `json:"sla_met"`
	}
	if err := json.Unmarshal(latencyRaw, &latency); err != nil {
		h.fail("the latency rollup parsed", err)
		return
	}
	if latency.Measured.Samples == 0 {
		h.fail("the latency rollup has samples to report", fmt.Errorf("zero scored runs"))
	} else if !latency.SLAMet {
		h.fail("scoring stays inside its SLA", fmt.Errorf("p95 %.1f ms against a %d ms target",
			latency.Measured.P95Micros/1000, latency.SLATargetMS))
	} else {
		h.ok("scoring stays inside its SLA", fmt.Sprintf("p50 %.2f ms, p95 %.2f ms, p99 %.2f ms over %d runs (target %d ms)",
			latency.Measured.P50Micros/1000, latency.Measured.P95Micros/1000, latency.Measured.P99Micros/1000,
			latency.Measured.Samples, latency.SLATargetMS))
	}

	// Drift evaluation is admin-only, and it recomputes PSI from the live
	// population instead of reading a stored number.
	// A recomputed measurement is stored, so the endpoint answers 201.
	if _, err := h.call(http.MethodPost, "/api/v1/governance/drift", map[string]any{},
		http.StatusCreated, bearer(adminToken)); err != nil {
		h.fail("an admin can recompute drift", err)
	} else {
		h.ok("an admin can recompute drift")
	}
	driftRaw, err := h.call(http.MethodGet, "/api/v1/governance/drift?limit=5", nil, http.StatusOK, bearer(staffToken))
	if err != nil {
		h.fail("drift metrics are readable", err)
		return
	}
	var drift []domain.DriftMetric
	if err := json.Unmarshal(driftRaw, &drift); err != nil || len(drift) == 0 {
		h.fail("drift metrics came back as a list", fmt.Errorf("entries %d, err %v", len(drift), err))
		return
	}
	h.ok("drift metrics came back as a list", fmt.Sprintf("%d features", len(drift)))

	// Underwriters are not admins: the gate has to refuse the write.
	if _, err := h.call(http.MethodPost, "/api/v1/governance/drift", map[string]any{},
		http.StatusForbidden, bearer(staffToken)); err != nil {
		h.fail("a underwriter cannot recompute drift", err)
	} else {
		h.ok("a underwriter cannot recompute drift")
	}
}

// checkQueueAndWorkflow reads the two operator views that the dashboard polls.
func (h *harness) checkQueueAndWorkflow(staffToken, appID, token string) {
	raw, err := h.call(http.MethodGet, "/api/v1/applications?limit=5&offset=0", nil, http.StatusOK, bearer(staffToken))
	if err != nil {
		h.fail("the underwriting queue is readable", err)
		return
	}
	var page struct {
		Applications []domain.CreditApplication `json:"applications"`
		Count        int                        `json:"count"`
		ByStatus     map[string]int             `json:"by_status"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		h.fail("the underwriting queue parsed", err)
		return
	}
	if page.Count < 1 || len(page.Applications) == 0 || len(page.ByStatus) == 0 {
		h.fail("the underwriting queue is populated", fmt.Errorf("count %d, status buckets %d", page.Count, len(page.ByStatus)))
		return
	}
	h.ok("the underwriting queue is populated", fmt.Sprintf("%d cases, %d status buckets", page.Count, len(page.ByStatus)))

	stateRaw, err := h.call(http.MethodGet, "/api/v1/applications/workflow-state?id="+appID, nil,
		http.StatusOK, statusCredential(token))
	if err != nil {
		h.fail("the capability token can read the saga stage", err)
		return
	}
	var state map[string]any
	_ = json.Unmarshal(stateRaw, &state)
	if _, has := state["stage"]; !has {
		h.fail("the saga stage is reported", fmt.Errorf("no stage field in %s", truncate(string(stateRaw), 120)))
		return
	}
	h.ok("the saga stage is reported", fmt.Sprint(state["stage"]))
}

// checkIdempotency: the claim is spent by a processed request, so a replay of
// the same key returns the stored answer instead of booking a second case.
func (h *harness) checkIdempotency() {
	key := "e2e-idem-" + randomHex(8)
	body := originationRequest("9"+randomDigits(8), "dupe."+randomHex(3)+"@example.com", 640000)

	first, err := h.call(http.MethodPost, "/api/v1/applications", body, http.StatusCreated,
		map[string]string{"Idempotency-Key": key})
	if err != nil {
		h.fail("the first submission of an idempotency key is booked", err)
		return
	}
	var firstApp submitResponse
	_ = json.Unmarshal(first, &firstApp)

	replay, err := h.call(http.MethodPost, "/api/v1/applications", body, http.StatusCreated,
		map[string]string{"Idempotency-Key": key})
	if err != nil {
		h.fail("replaying the key returns the booked case", err)
		return
	}
	if !bytes.Equal(first, replay) {
		h.fail("replaying the key returns the identical answer", fmt.Errorf("bodies differed"))
		return
	}
	var replayApp submitResponse
	_ = json.Unmarshal(replay, &replayApp)
	if replayApp.Application.ID != firstApp.Application.ID {
		h.fail("replaying the key did not create a second case",
			fmt.Errorf("%s then %s", firstApp.Application.ID, replayApp.Application.ID))
		return
	}
	h.ok("replaying an idempotency key returns the same case", firstApp.Application.ID)
}

func randomDigits(n int) string {
	const digits = "0123456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	for i, b := range buf {
		buf[i] = digits[int(b)%len(digits)]
	}
	return string(buf)
}

// checkValidation guards the boundary: an unauthorised pull without consent, or a
// malformed body, must be refused before anything is stored.
func (h *harness) checkValidation() {
	noConsent := originationRequest("8"+randomDigits(8), "noc."+randomHex(3)+"@example.com", 500000)
	noConsent["consent_fcra"] = false
	if _, err := h.call(http.MethodPost, "/api/v1/applications", noConsent, http.StatusUnprocessableEntity, nil); err != nil {
		h.fail("a submission without FCRA consent is refused", err)
	} else {
		h.ok("a submission without FCRA consent is refused")
	}

	unknown := originationRequest("7"+randomDigits(8), "unk."+randomHex(3)+"@example.com", 500000)
	unknown["browser_fingerprint"] = "not a field we accept"
	if _, err := h.call(http.MethodPost, "/api/v1/applications", unknown, http.StatusBadRequest, nil); err != nil {
		h.fail("an unknown field in the request body is refused", err)
	} else {
		h.ok("an unknown field in the request body is refused")
	}

	badAmount := originationRequest("6"+randomDigits(8), "amt."+randomHex(3)+"@example.com", 500000)
	badAmount["requested_amount_cents"] = 0
	if _, err := h.call(http.MethodPost, "/api/v1/applications", badAmount, http.StatusUnprocessableEntity, nil); err != nil {
		h.fail("a zero requested amount is refused", err)
	} else {
		h.ok("a zero requested amount is refused")
	}
}

// idpToken asks the development identity provider for a credential. It is a
// plain text body because the whole point is that nothing in this service minted
// the token.
func (h *harness) idpToken(idpURL string, params ...string) (string, error) {
	resp, err := h.client.Get(idpURL + "/token?" + strings.Join(params, "&"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d, body started %q", resp.StatusCode, truncate(string(body), 80))
	}
	return strings.TrimSpace(string(body)), nil
}

var uuidv5Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// checkFederatedSession drives the delegated identity path against the running
// stack. The unit tests prove the cryptographic decisions; only a live run can
// show that a subject string from another authority becomes a real account row
// in Postgres, that the foreign keys to it hold, and that the local provider is
// still accepted alongside it.
func (h *harness) checkFederatedSession(idpURL, localStaffToken, knownCaseID string) {
	idpURL = strings.TrimRight(idpURL, "/")
	suffix := randomHex(4)

	deskToken, err := h.idpToken(idpURL,
		"role=UNDERWRITER",
		"sub=e2e-desk-"+suffix,
		"email=desk."+suffix+"@partners.example",
		"name=Desk%20Analyst")
	if err != nil {
		h.fail("the identity provider is reachable", err)
		return
	}
	h.ok("the identity provider is reachable", idpURL)

	raw, err := h.call(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK, bearer(deskToken))
	if err != nil {
		h.fail("a token signed by the identity provider opens a session", err)
		return
	}
	var me struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(raw, &me); err != nil {
		h.fail("the delegated session profile parsed", err)
		return
	}
	if me.Provider != "oidc" || me.Role != "UNDERWRITER" {
		h.fail("the delegated session reports its provider and role",
			fmt.Errorf("provider %q role %q", me.Provider, me.Role))
		return
	}
	h.ok("a token signed by the identity provider opens a session", "provider "+me.Provider+" role "+me.Role)

	// The provider's subject is "e2e-desk-<hex>", which is not a UUID; the
	// account id has to be the derived one or nothing downstream can key on it.
	if !uuidv5Re.MatchString(me.ID) {
		h.fail("the federated subject maps to a UUID account id", fmt.Errorf("id %q", me.ID))
	} else {
		h.ok("the federated subject maps to a UUID account id", me.ID)
	}

	if _, err := h.call(http.MethodGet, "/api/v1/applications", nil, http.StatusOK, bearer(deskToken)); err != nil {
		h.fail("an externally authenticated underwriter can open the queue", err)
	} else {
		h.ok("an externally authenticated underwriter can open the queue")
	}

	detailRaw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+knownCaseID, nil,
		http.StatusOK, bearer(deskToken))
	if err != nil {
		h.fail("an externally authenticated underwriter sees the staff view of a case", err)
	} else {
		record, err := decodeApplication(detailRaw)
		if err != nil {
			h.fail("the delegated staff read parsed", err)
		} else if incomeOf(record) == 0 {
			h.fail("the delegated staff read is unredacted", fmt.Errorf("income %d", incomeOf(record)))
		} else {
			h.ok("an externally authenticated underwriter sees the staff view of a case",
				fmt.Sprintf("income %d", incomeOf(record)))
		}
	}

	// Delegated authentication has to be additive: the seeded accounts and every
	// borrower session still sign in against the local secret.
	if _, err := h.call(http.MethodGet, "/api/v1/applications", nil, http.StatusOK,
		bearer(localStaffToken)); err != nil {
		h.fail("the local provider still authenticates beside the identity provider", err)
	} else {
		h.ok("the local provider still authenticates beside the identity provider")
	}

	roleless, err := h.idpToken(idpURL, "no_role=true", "sub=e2e-nobody-"+suffix)
	if err != nil {
		h.fail("a roleless token could be minted", err)
	} else if _, err := h.call(http.MethodGet, "/api/v1/auth/me", nil, http.StatusForbidden,
		bearer(roleless)); err != nil {
		h.fail("a genuine identity with no platform role is forbidden, not unauthenticated", err)
	} else {
		h.ok("a genuine identity with no platform role is forbidden, not unauthenticated")
	}

	tampered := deskToken[:len(deskToken)-2] + "AQ"
	if _, err := h.call(http.MethodGet, "/api/v1/applications", nil, http.StatusUnauthorized,
		bearer(tampered)); err != nil {
		h.fail("a tampered provider signature is rejected", err)
	} else {
		h.ok("a tampered provider signature is rejected")
	}

	// The account row a federated borrower gets is created on the fly. Filing
	// while signed in is what links it to the credit record, and the link is a
	// foreign key into users, so this single check proves provisioning reached
	// the database rather than only the request context.
	borrowerSSN := "7" + randomDigits(8)
	borrowerEmail := "federated." + suffix + "@example.com"
	idpBorrower, err := h.idpToken(idpURL,
		"role=BORROWER",
		"sub=e2e-borrower-"+suffix,
		"email="+borrowerEmail,
		"name=Federated%20Borrower")
	if err != nil {
		h.fail("a borrower token could be minted", err)
		return
	}
	filed, err := h.submit(originationRequest(borrowerSSN, borrowerEmail, 615000), idpBorrower)
	if err != nil {
		h.fail("an externally authenticated borrower can originate", err)
		return
	}
	h.ok("an externally authenticated borrower can originate", "case "+filed.Application.ID)

	queueRaw, err := h.call(http.MethodGet, "/api/v1/me/applications", nil, http.StatusOK, bearer(idpBorrower))
	if err != nil {
		h.fail("the federated borrower has a personal queue", err)
		return
	}
	var queue struct {
		Applications []domain.CreditApplication `json:"applications"`
		Count        int                        `json:"count"`
		Linked       bool                       `json:"applicant_linked"`
	}
	if err := json.Unmarshal(queueRaw, &queue); err != nil {
		h.fail("the federated borrower queue parsed", err)
		return
	}
	found := false
	for _, a := range queue.Applications {
		if a.ID == filed.Application.ID {
			found = true
		}
	}
	if !queue.Linked || !found {
		h.fail("the account provisioned for the federated subject owns the case it filed",
			fmt.Errorf("linked=%t count=%d", queue.Linked, queue.Count))
		return
	}
	h.ok("the account provisioned for the federated subject owns the case it filed", fmt.Sprintf("%d case(s)", queue.Count))

	ownerRaw, err := h.call(http.MethodGet, "/api/v1/applications/get?id="+filed.Application.ID, nil,
		http.StatusOK, bearer(idpBorrower))
	if err != nil {
		h.fail("the federated owner reads their own record", err)
		return
	}
	owner, err := decodeApplication(ownerRaw)
	if err != nil {
		h.fail("the federated owner read parsed", err)
		return
	}
	if incomeOf(owner) != 615000 {
		h.fail("the federated owner sees their own unredacted data", fmt.Errorf("income %d", incomeOf(owner)))
		return
	}
	h.ok("the federated owner sees their own unredacted data", "income 615000")
}

func (h *harness) summary() int {
	fmt.Printf("\n%d checks passed, %d failed\n", h.passed, h.failed)
	if h.failed > 0 {
		fmt.Println("RESULT: FAIL")
		return 1
	}
	fmt.Println("RESULT: PASS")
	return 0
}

func main() {
	base := strings.TrimRight(envOr("CORE_URL", "http://localhost:8600"), "/")
	budget := envDuration("E2E_DECISION_TIMEOUT", 90*time.Second)

	h := &harness{
		base:   base,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	fmt.Printf("CrediVect end-to-end validation against %s\n\n", base)

	activeModel := h.checkHealth()
	if activeModel == "" && h.failed > 0 {
		os.Exit(h.summary())
	}

	staffLogin, err := h.login(envOr("E2E_UNDERWRITER_EMAIL", "sarah.jenkins@credivect.example"),
		envOr("E2E_UNDERWRITER_PASSWORD", "credivect-underwriter"))
	if err != nil {
		h.fail("an underwriter can sign in", err)
		os.Exit(h.summary())
	}
	staffToken, _ := staffLogin["token"].(string)
	h.ok("an underwriter can sign in", fmt.Sprint(staffLogin["user"]))

	adminLogin, err := h.login(envOr("E2E_ADMIN_EMAIL", "risk.admin@credivect.example"),
		envOr("E2E_ADMIN_PASSWORD", "credivect-admin"))
	if err != nil {
		h.fail("an admin can sign in", err)
	}
	adminToken, _ := adminLogin["token"].(string)

	borrowerEmail := envOr("E2E_BORROWER_EMAIL", "alex.chen@example.com")
	borrowerLogin, err := h.login(borrowerEmail, envOr("E2E_BORROWER_PASSWORD", "credivect-borrower"))
	if err != nil {
		h.fail("a borrower can sign in", err)
		os.Exit(h.summary())
	}
	borrowerToken, _ := borrowerLogin["token"].(string)
	h.ok("a borrower can sign in", borrowerEmail)

	if _, err := h.call(http.MethodGet, "/api/v1/applications", nil, http.StatusForbidden,
		bearer(borrowerToken)); err != nil {
		h.fail("a borrower cannot open the underwriting queue", err)
	} else {
		h.ok("a borrower cannot open the underwriting queue")
	}

	h.checkValidation()
	h.checkIdempotency()

	// Case A: anonymous origination, followed only by the capability token.
	anonSSN := "9" + randomDigits(8)
	anonEmail := "anon." + randomHex(4) + "@example.com"
	anon, err := h.submit(originationRequest(anonSSN, anonEmail, 780000), "")
	if err != nil {
		h.fail("an anonymous borrower can originate", err)
		os.Exit(h.summary())
	}
	h.ok("an anonymous borrower can originate", "case "+anon.Application.ID+" workflow "+anon.WorkflowID)

	if h.checkAnonFlow(anon, anonSSN) {
		decided, took, err := h.awaitDecision(anon.Application.ID, anon.StatusToken, budget)
		if err != nil {
			h.fail("the saga reaches a decision", err)
		} else {
			h.checkScoring(decided, activeModel, took)
			h.checkNotice(anon.Application.ID, anon.StatusToken, decided)
		}
		h.checkStaffRead(anon.Application.ID, staffToken, anonEmail)
		h.checkQueueAndWorkflow(staffToken, anon.Application.ID, anon.StatusToken)
	}

	// Case B: the same person signed in, which is what binds account to record.
	ownSSN := "5" + randomDigits(8)
	ownEmail := "owner." + randomHex(4) + "@example.com"
	bound := h.checkBorrowerAccount(borrowerToken, ownSSN, ownEmail)
	if bound {
		h.checkForeignRecord(borrowerToken, anon.Application.ID)
	}

	h.checkGovernance(staffToken, adminToken)

	// Delegated identity needs a provider in front of the core service, so it is
	// opt-in rather than a check that fails on a stack that never asked for one.
	if idpURL := strings.TrimSpace(os.Getenv("E2E_IDP_URL")); idpURL != "" {
		h.checkFederatedSession(idpURL, staffToken, anon.Application.ID)
	} else {
		fmt.Print("\nSKIP  delegated identity checks: run a dev-idp, point AUTH_MODE=oidc at it, and set E2E_IDP_URL\n")
	}

	os.Exit(h.summary())
}
