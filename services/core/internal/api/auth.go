package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"credivect/core/internal/auth"

	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleLogin verifies the credential against a bcrypt hash and mints an HS256
// token carrying the role. The local provider is the only one wired; swapping in
// Clerk or Auth0 replaces TokenService with a JWKS verifier, not these handlers.
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRe.MatchString(req.Email) || len(req.Password) == 0 {
		writeFieldErrors(w, map[string]string{"email": "a valid email and password are required"})
		return
	}

	user, err := s.repo.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		// Identical response for unknown account and wrong password: do not
		// leak which addresses have accounts.
		time.Sleep(150 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, expiresAt, err := s.tokens.Issue(auth.Claims{
		UserID: user.ID, Email: user.Email, Name: user.FullName, Role: user.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue a session token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt,
		"user":       map[string]any{"id": user.ID, "email": user.Email, "name": user.FullName, "role": user.Role},
	})
}

func (s *Server) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims, err := s.principal(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": claims.UserID, "email": claims.Email, "name": claims.Name, "role": claims.Role,
	})
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	checks := map[string]string{"postgres": "DOWN", "dragonfly": "DOWN", "temporal": "DOWN", "scoring": "DOWN"}
	if err := s.repo.Pool().Ping(ctx); err == nil {
		checks["postgres"] = "UP"
	}
	if err := s.cache.Ping(ctx); err == nil {
		checks["dragonfly"] = "UP"
	}
	if err := s.temporal.Ping(ctx); err == nil {
		checks["temporal"] = "UP"
	}
	if s.scoring.ModelVersion() != "" {
		checks["scoring"] = "UP"
	}
	if checks["postgres"] == "UP" && checks["scoring"] == "UP" {
		checks["status"] = "UP"
	} else {
		checks["status"] = "DEGRADED"
	}
	model := s.scoring.ModelVersion()
	writeJSON(w, statusFor(checks["status"]), map[string]any{
		"service": "credivect-core", "checks": checks, "model_version": model,
		"event_stream": s.publisher != nil, "bureau_provider": s.bureauName,
	})
}

func statusFor(state string) int {
	if state == "UP" {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

var errUnsupported = errors.New("unsupported")
