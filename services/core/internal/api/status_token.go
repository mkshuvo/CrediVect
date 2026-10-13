package api

import (
	"net/http"
	"strings"

	"credivect/core/internal/auth"
	"credivect/core/internal/security"
)

// The borrower-facing reads - status page, workflow position, FCRA adverse
// action notice - each describe one named consumer. Origination is anonymous, so
// the applicant cannot hold a session; what they hold instead is a capability
// token minted at submission and stored only as its digest. A staff session is
// the other way in, and it sees the complete record.

// presentedToken is read from the header only. A credential in a query string
// ends up in access logs, browser history and referrer headers, so the notice
// download sends it the same way as every other borrower read: the browser
// fetches the text and hands it to a download, which can carry a header.
func presentedToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Status-Token"))
}

// readScope is the answer to "in what capacity did this caller get in", which
// decides how much of the record they are shown.
type readScope int

const (
	// scopeToken: proved only that they hold the credential issued at
	// submission. They see the borrower's view, not the underwriter's.
	scopeToken readScope = iota
	// scopeOwner: a signed-in borrower on the account that originated this case.
	// Their own file, so nothing is withheld beyond the sealed identifier.
	scopeOwner
	// scopeStaff: underwriting, which is entitled to the whole record.
	scopeStaff
)

// authorizeBorrowerRead decides whether the caller may read one application and
// in what capacity. It writes the rejection itself, so no handler can forget to.
//
// A lookup failure is treated as "no access": an unknown id and a database
// problem both end in 401 rather than revealing which applications exist.
func (s *Server) authorizeBorrowerRead(w http.ResponseWriter, r *http.Request, applicationID string) (readScope, bool) {
	if claims, err := auth.From(r.Context()); err == nil {
		if claims.Role == auth.RoleUnderwriter || claims.Role == auth.RoleAdmin {
			return scopeStaff, true
		}
		// A borrower session is only ever an owner, and ownership is a database
		// fact rather than something the token asserts.
		if ownerID, err := s.repo.ApplicantIDForUser(r.Context(), claims.UserID); err == nil && ownerID != "" {
			owner, err := s.repo.ApplicantIDForApplication(r.Context(), applicationID)
			if err == nil && owner == ownerID {
				return scopeOwner, true
			}
		}
	}

	tokenValid := false
	if stored, err := s.repo.StatusTokenHash(r.Context(), applicationID); err == nil {
		tokenValid = security.StatusTokenMatches(stored, presentedToken(r))
	}
	if !tokenValid {
		writeError(w, http.StatusUnauthorized,
			"this record belongs to the applicant; present the status token issued at submission, sign in to the account that applied, or use an underwriter session")
		return scopeToken, false
	}
	return scopeToken, true
}
