package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "unit-test-signing-secret-not-a-production-key"

func newTokens(ttl time.Duration) *TokenService {
	return NewTokenService(testSecret, ttl)
}

func claimsFor(role string) Claims {
	return Claims{UserID: "0195b3f8-0000-7000-8000-000000000001", Email: "u@example.com", Name: "U", Role: role}
}

func TestIssueParseRoundTrip(t *testing.T) {
	svc := newTokens(time.Hour)
	raw, exp, err := svc.Issue(claimsFor(RoleUnderwriter))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if exp.Before(time.Now().UTC()) {
		t.Errorf("expiry %s is in the past", exp)
	}
	got, err := svc.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.UserID != "0195b3f8-0000-7000-8000-000000000001" || got.Role != RoleUnderwriter || got.Email != "u@example.com" {
		t.Errorf("claims did not survive the round trip: %+v", got)
	}
	if got.Issuer != "credivect-core" {
		t.Errorf("issuer %q", got.Issuer)
	}
}

func TestParseRejectsUntrustedTokens(t *testing.T) {
	svc := newTokens(time.Hour)
	good, _, err := svc.Issue(claimsFor(RoleAdmin))
	if err != nil {
		t.Fatal(err)
	}

	forged := NewTokenService("a completely different secret", time.Hour)
	other, _, err := forged.Issue(claimsFor(RoleAdmin))
	if err != nil {
		t.Fatal(err)
	}

	expiredSvc := NewTokenService(testSecret, -time.Minute)
	expired, _, err := expiredSvc.Issue(claimsFor(RoleAdmin))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"not a jwt", "hello"},
		{"signed by another secret", other},
		{"expired", expired},
		{"truncated", good[:len(good)-4]},
		{"tampered payload", tamper(t, good)},
		{"unsigned alg none", unsignedNone(t, good)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.Parse(c.token); err == nil {
				t.Errorf("accepted a %s token", c.name)
			}
		})
	}
}

// tamper rewrites the role claim to ADMIN and leaves the signature untouched.
func tamper(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a 3 part jwt, got %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	body["role"] = RoleAdmin
	body["exp"] = float64(time.Now().UTC().Add(time.Hour).Unix())
	mutated, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(mutated)
	return strings.Join(parts, ".")
}

// unsignedNone rebuilds the header with alg=none and drops the signature, the
// classic JWT bypass.
func unsignedNone(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a 3 part jwt, got %q", token)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	return header + "." + parts[1] + "."
}

func TestRequireRoleGates(t *testing.T) {
	svc := newTokens(time.Hour)
	mw := NewMiddleware(svc)
	admin, _, err := svc.Issue(claimsFor(RoleAdmin))
	if err != nil {
		t.Fatal(err)
	}
	borrower, _, err := svc.Issue(claimsFor(RoleBorrower))
	if err != nil {
		t.Fatal(err)
	}
	expiredSvc := NewTokenService(testSecret, -time.Hour)
	stale, _, err := expiredSvc.Issue(claimsFor(RoleAdmin))
	if err != nil {
		t.Fatal(err)
	}

	OK := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := From(r.Context())
		if err != nil || claims.Role != RoleAdmin {
			t.Errorf("authorized request reached the handler without admin claims: %+v %v", claims, err)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + admin, http.StatusUnauthorized},
		{"garbage token", "Bearer not.a.jwt", http.StatusUnauthorized},
		{"expired admin", "Bearer " + stale, http.StatusUnauthorized},
		{"borrower on underwriter route", "Bearer " + borrower, http.StatusForbidden},
		{"admin allowed", "Bearer " + admin, http.StatusNoContent},
	}
	gate := mw.RequireRole(RoleUnderwriter, RoleAdmin)(OK)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
			if c.header != "" {
				r.Header.Set("Authorization", c.header)
			}
			w := httptest.NewRecorder()
			gate.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Errorf("got %d, want %d (%s)", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestAuthenticateLetsAnonymousThroughOpenRoutes(t *testing.T) {
	// POST /applications is public: an absent token must not be an error, but no
	// principal may be invented either.
	mw := NewMiddleware(newTokens(time.Hour))
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, err := From(r.Context()); !errors.Is(err, ErrNoClaims) {
			t.Errorf("expected ErrNoClaims for an anonymous request, got %v", err)
		}
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/applications", nil)
	mw.Authenticate(next).ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Fatal("anonymous request was blocked on a public route")
	}
}
