package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"credivect/core/internal/auth"
)

// Every handler authorises its own caller, so nothing in the handler tests proves
// the mux actually wraps the route in the first place. This is the regression
// that let the FCRA notice endpoints read a named consumer's file with no
// credential at all: the guard existed, one registration just forgot it.
//
// Only the middleware is exercised here. A request that got past the gate would
// reach a handler with no repository behind it, so each case below is a request
// that must be answered by the gate.
func TestRouteTableKeepsItsGates(t *testing.T) {
	tokens := auth.NewTokenService("route-table-secret", time.Hour)
	borrower, _, err := tokens.Issue(auth.Claims{UserID: "u-1", Role: auth.RoleBorrower})
	if err != nil {
		t.Fatalf("could not issue a test session: %v", err)
	}
	s := &Server{guard: auth.NewMiddleware(tokens)}
	mux := s.Routes()

	do := func(method, path, authHeader string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if authHeader != "" {
			req.Header.Set("Authorization", "Bearer "+authHeader)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	staffOnly := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/applications"},
		{http.MethodPost, "/api/v1/applications/review?id=01a1369a-ef64-7ac7-8f40-e1a030b62505"},
		{http.MethodGet, "/api/v1/governance/ledger"},
		{http.MethodGet, "/api/v1/governance/drift"},
		{http.MethodPost, "/api/v1/governance/drift?feature=OVERALL_PD"},
		{http.MethodGet, "/api/v1/governance/latency"},
		{http.MethodGet, "/api/v1/governance/model"},
	}
	borrowerReads := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/applications/get?id=01a1369a-ef64-7ac7-8f40-e1a030b62505"},
		{http.MethodGet, "/api/v1/applications/adverse-action?id=01a1369a-ef64-7ac7-8f40-e1a030b62505"},
		{http.MethodGet, "/api/v1/applications/adverse-action.txt?id=01a1369a-ef64-7ac7-8f40-e1a030b62505"},
		{http.MethodGet, "/api/v1/applications/workflow-state?id=01a1369a-ef64-7ac7-8f40-e1a030b62505"},
	}

	for _, rt := range staffOnly {
		if code := do(rt.method, rt.path, ""); code != http.StatusUnauthorized {
			t.Errorf("%s %s with no session: got %d, want 401", rt.method, rt.path, code)
		}
		// A borrower is authenticated but not staff: the gate must say forbidden,
		// which is a different answer from "we could not identify you".
		if code := do(rt.method, rt.path, borrower); code != http.StatusForbidden {
			t.Errorf("%s %s as BORROWER: got %d, want 403", rt.method, rt.path, code)
		}
	}

	// The borrower's own queue belongs to the borrower: an underwriter session
	// must not be able to list somebody else's cases through it.
	underwriter, _, err := tokens.Issue(auth.Claims{UserID: "u-2", Role: auth.RoleUnderwriter})
	if err != nil {
		t.Fatalf("could not issue a test session: %v", err)
	}
	if code := do(http.MethodGet, "/api/v1/me/applications", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/me/applications with no session: got %d, want 401", code)
	}
	if code := do(http.MethodGet, "/api/v1/me/applications", underwriter); code != http.StatusForbidden {
		t.Errorf("GET /api/v1/me/applications as UNDERWRITER: got %d, want 403", code)
	}

	// A credential that will not parse is not an anonymous request: the gate has
	// to reject it rather than fall through to the handler. Origination is
	// anonymous-capable, so it passes no token when there is none to pass, but a
	// broken one must not be downgraded to anonymous either: that would book the
	// case and silently lose the link between it and the account that filed it.
	gated := append(append([]struct{ method, path string }{}, staffOnly...), borrowerReads...)
	gated = append(gated, struct{ method, path string }{http.MethodPost, "/api/v1/applications"})
	for _, rt := range gated {
		if code := do(rt.method, rt.path, "not-a-session-token"); code != http.StatusUnauthorized {
			t.Errorf("%s %s with a malformed token: got %d, want 401", rt.method, rt.path, code)
		}
	}
}
