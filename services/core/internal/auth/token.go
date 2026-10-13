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
	// Provider and ProviderIssuer record who vouched for this principal. They
	// are set by the verifier that accepted the token, never read from a token
	// payload, so a self-asserted "oidc" cannot appear in a session. The name
	// avoids the embedded registered "iss" claim, which must keep marshalling.
	Provider       string `json:"-"`
	ProviderIssuer string `json:"-"`
	// Subject is the identity provider's own user id when one vouched for this
	// principal. UserID is the platform account row it maps to, which is what
	// every query and foreign key uses; keeping both means a support answer can
	// name the person in the provider's terms without losing the local id.
	Subject string `json:"-"`
	jwt.RegisteredClaims
}

// Verifier is one way a bearer credential can be authenticated. The core service
// runs a chain of them: the local issuer always verifies its own minted tokens,
// and AUTH_MODE=oidc adds a provider whose keys are published rather than
// shared. Adding Clerk, Auth0 or Entra ID is a new implementation of this
// interface plus configuration, not a change to a handler.
type Verifier interface {
	Verify(raw string) (*Claims, error)
}

type TokenService struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl, issuer: "credivect-core"}
}

// Issue mints an HS256 token for a local account. The remote provider never
// mints anything: it only verifies tokens its own authority signed, which is why
// AUTH_MODE=oidc adds a verifier and leaves this path as it is.
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
	claims.Provider = ProviderLocal
	return claims, nil
}

// Verify lets the local issuer stand in the verifier chain beside a remote one.
func (s *TokenService) Verify(raw string) (*Claims, error) {
	return s.Parse(raw)
}
