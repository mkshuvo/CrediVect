package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Roles the platform distinguishes. Borrowers submit applications,
// underwriters adjudicate, admins additionally read governance.
const (
	RoleBorrower    = "BORROWER"
	RoleUnderwriter = "UNDERWRITER"
	RoleAdmin       = "ADMIN"
)

type Claims struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type TokenService struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl, issuer: "credivect-core"}
}

// Issue mints an HS256 token. AUTH_MODE=local is the only provider wired; the
// validator takes keys from the same secret, so a Clerk or Auth0 deployment
// replaces this service rather than the handlers.
func (s *TokenService) Issue(c Claims) (string, time.Time, error) {
	exp := time.Now().UTC().Add(s.ttl)
	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    s.issuer,
		Subject:   c.UserID,
		Audience:  jwt.ClaimStrings{"credivect-api"},
		ExpiresAt: jwt.NewNumericDate(exp),
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ID:        fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	raw, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return raw, exp, nil
}

func (s *TokenService) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing algorithm %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithAudience("credivect-api"), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.UserID == "" || claims.Role == "" {
		return nil, fmt.Errorf("token is missing subject or role")
	}
	return claims, nil
}
