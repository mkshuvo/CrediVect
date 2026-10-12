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
type Middleware struct {
	tokens *TokenService
}

func NewMiddleware(tokens *TokenService) *Middleware {
	return &Middleware{tokens: tokens}
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := m.tokens.Parse(raw)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired credentials")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
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
