package api

import (
	"log/slog"
	"net/http"

	"credivect/core/internal/auth"
	"credivect/core/internal/domain"
)

// A borrower account is a name and a password until it is bound to the consumer
// record the person applied against. That binding is the whole of the ownership
// model: it is created here, at origination, by someone who is already proved to
// control the account and who is supplying the credit file in the request body.
// No endpoint lets a session claim a record after the fact, because "I know this
// applicant's email address" is exactly the assertion an attacker can make.

// linkBorrowerAccount binds the calling account to the applicant record just
// originated. Failing to link is not a reason to fail the submission: the case is
// booked, its status token works, and the account view simply stays empty.
func (s *Server) linkBorrowerAccount(r *http.Request, applicantID string) {
	claims, err := auth.From(r.Context())
	if err != nil || claims.Role != auth.RoleBorrower {
		return
	}
	linked, err := s.repo.LinkUserToApplicant(r.Context(), claims.UserID, applicantID)
	if err != nil {
		slog.Warn("could not link the borrower account to its applicant record",
			"user_id", claims.UserID, "error", err)
		return
	}
	if linked {
		slog.Info("borrower account linked to applicant record", "user_id", claims.UserID)
	}
}

// HandleMyApplications lists the signed-in borrower's own cases, newest first.
// Unlike the staff queue this one is scoped to a person, and the records come
// back unredacted because they are the caller's own credit file.
func (s *Server) HandleMyApplications(w http.ResponseWriter, r *http.Request) {
	claims, err := auth.From(r.Context())
	if err != nil || claims.Role != auth.RoleBorrower {
		writeError(w, http.StatusUnauthorized, "a borrower session is required")
		return
	}
	applicantID, err := s.repo.ApplicantIDForUser(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve the record this account owns")
		return
	}
	if applicantID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"applications":     []domain.CreditApplication{},
			"count":            0,
			"applicant_linked": false,
			"note": "this account has not originated an application while signed in, so no credit " +
				"record is bound to it yet; apply while signed in and the case will appear here",
		})
		return
	}
	apps, err := s.repo.ListApplicantApplications(r.Context(), applicantID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read your applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applications": apps, "count": len(apps), "applicant_linked": true,
	})
}
