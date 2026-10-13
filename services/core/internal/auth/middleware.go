package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type ctxKey int

const claimsKey ctxKey = 1

var ErrNoClaims = errors.New("no principal in context")

// Middleware authenticates requests that carry a bearer token and gates the
// privileged routes. Submitting an application may stay anonymous; reading the
// underwriter queue, adjudicating, and governance all require a role.
//
// A credential is offered to each verifier in turn and the first acceptance
// wins, so a deployment that runs local accounts and an external provider at the
// same time needs no routing decision in front of the handlers.
type Middleware struct {
	verifiers []Verifier
}

func NewMiddleware(verifiers ...Verifier) *Middleware {
	return &Middleware{verifiers: verifiers}
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		var unauthorised error
		for _, verifier := range m.verifiers {
			claims, err := verifier.Verify(raw)
			if err == nil {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
				return
			}
			// A genuine identity that simply has no role here is a different
			// answer from a forged token, and the caller can act on it.
			if errors.Is(err, ErrNoRole) {
				unauthorised = err
			}
		}
		if unauthorised != nil {
			writeJSONError(w, http.StatusForbidden, "signed in, but not authorised: this identity has no role on the platform")
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired credentials")
	})
}

// RequireRole rejects a request whose principal is missing or lacks one of the
// listed roles.
func (m *Middleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := From(r.Context())
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			for _, role := range roles {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSONError(w, http.StatusForbidden, "role not authorised for this operation")
		}))
	}
}

func From(ctx context.Context) (*Claims, error) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	if !ok || c == nil {
		return nil, ErrNoClaims
	}
	return c, nil
}

func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + message + `"}`))
}
