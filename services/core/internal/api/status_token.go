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

// presentedToken is accepted in a header, which keeps it out of URLs, browser
// history and access logs, and in a query parameter, which a plain download
// anchor has no other way to send.
func presentedToken(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("X-Status-Token")); h != "" {
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// authorizeBorrowerRead decides whether the caller may read one application and
// whether the record must be narrowed to the borrower's own view. It writes the
// rejection itself, so no handler can forget to.
//
// A digest lookup failure is treated as "no digest", which denies the read: an
// unknown id and a database problem both end in 401 rather than revealing which
// applications exist.
func (s *Server) authorizeBorrowerRead(w http.ResponseWriter, r *http.Request, applicationID string) (staff, ok bool) {
	if _, isStaff := staffClaims(r); isStaff {
		return true, true
	}

	tokenValid := false
	if stored, err := s.repo.StatusTokenHash(r.Context(), applicationID); err == nil {
		tokenValid = security.StatusTokenMatches(stored, presentedToken(r))
	}
	if !tokenValid {
		writeError(w, http.StatusUnauthorized,
			"this record belongs to the applicant; present the status token issued at submission, or an underwriter session")
		return false, false
	}
	return false, true
}

func staffClaims(r *http.Request) (role string, isStaff bool) {
	claims, err := auth.From(r.Context())
	if err != nil {
		return "", false
	}
	if claims.Role == auth.RoleUnderwriter || claims.Role == auth.RoleAdmin {
		return claims.Role, true
	}
	return claims.Role, false
}
