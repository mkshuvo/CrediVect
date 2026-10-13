package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"credivect/core/internal/domain"

	"github.com/golang-jwt/jwt/v5"
)

// ErrNoRole is the one rejection a caller may act on: the credential is genuine,
// but the identity behind it is not authorised for this platform. It earns 403
// rather than 401, so a user sees "ask for access" instead of "wrong password".
var ErrNoRole = errors.New("identity has no platform role")

// ProviderLocal and ProviderOIDC record which verifier accepted the bearer
// credential. The distinction decides whether the account row has to be
// provisioned on the fly, and it is reported in health and logs.
const (
	ProviderLocal = "local"
	ProviderOIDC  = "oidc"
)

// clockSkew absorbs the drift between this service's clock and the identity
// provider's. Anything larger is a misconfiguration, not jitter.
const clockSkew = 30 * time.Second

// maxJWKSDocument bounds the key document we are willing to read: a key set is a
// handful of public keys, so a large body is either a broken provider or a
// hostile one.
const maxJWKSDocument = 1 << 20

// verifiedAlgorithms is the asymmetric set this platform accepts. HMAC
// algorithms are deliberately absent: the same key document that proves an
// RS256 token could otherwise be used as the secret of an HS256 one.
var verifiedAlgorithms = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}

// OIDCSettings points the core service at an external identity provider. Auth0,
// Keycloak, Entra ID, Okta and Zitadel all publish this shape; Clerk's session
// tokens use the same verification rules with a different discovery path.
type OIDCSettings struct {
	Issuer    string
	JWKSURL   string
	Audience  string
	RoleClaim string
	// KeyTTL is how long a fetched key set stays acceptable, and MinRefresh is
	// the shortest interval between two fetches, so a stream of forged key ids
	// cannot turn authentication into a denial-of-service against the provider.
	KeyTTL     time.Duration
	MinRefresh time.Duration
	Timeout    time.Duration
	// AllowCleartext permits a key set served over plain HTTP from a host that
	// is not loopback. It exists for a development identity provider running
	// beside the service on a developer's machine; whoever sets it is deciding
	// that the network between the two is trusted.
	AllowCleartext bool
}

// IDTokenVerifier authenticates bearer tokens minted by an external provider:
// signature against that provider's published keys, then issuer, audience and
// lifetime, then the platform's own claim mapping.
type IDTokenVerifier struct {
	cfg     OIDCSettings
	client  *http.Client
	mu      sync.Mutex
	keys    map[string]any
	fetched time.Time
}

// NewIDTokenVerifier checks the settings and fetches the key set once. Failing
// here rather than at the first login is deliberate: an unreachable provider is
// a deployment error, and it should surface while the service is starting.
func NewIDTokenVerifier(cfg OIDCSettings) (*IDTokenVerifier, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("OIDC_ISSUER is required")
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("OIDC_AUDIENCE is required")
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "role"
	}
	if cfg.KeyTTL <= 0 {
		cfg.KeyTTL = 5 * time.Minute
	}
	if cfg.MinRefresh <= 0 {
		cfg.MinRefresh = 30 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.JWKSURL == "" {
		base := strings.TrimSuffix(cfg.Issuer, "/")
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "https://" + base
		}
		cfg.JWKSURL = base + "/.well-known/jwks.json"
	}
	if err := checkKeySetURL(cfg.JWKSURL, cfg.AllowCleartext); err != nil {
		return nil, err
	}
	v := &IDTokenVerifier{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		keys:   map[string]any{},
	}
	if err := v.refresh(); err != nil {
		return nil, err
	}
	return v, nil
}

// checkKeySetURL keeps the key document on TLS: whoever can write the bytes of a
// provider's public keys can mint sessions, so reading them in the clear hands
// authentication to anyone on the path. Loopback is exempt because that is where
// a local identity provider or a test server lives; anything further needs the
// operator's explicit AllowCleartext.
func checkKeySetURL(raw string, allowCleartext bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("OIDC_JWKS_URL %q is not a valid URL: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("OIDC_JWKS_URL %q has no host", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("OIDC_JWKS_URL %q must be http or https", raw)
	}
	if allowCleartext || isLoopback(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("OIDC_JWKS_URL %q is cleartext; use https, or set OIDC_ALLOW_CLEARTEXT_KEYS for a provider on this machine", raw)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Verify authenticates one bearer token and maps its claims onto the platform's
// own principal.
func (v *IDTokenVerifier) Verify(raw string) (*Claims, error) {
	mapClaims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, mapClaims, v.keyFunc,
		jwt.WithValidMethods(verifiedAlgorithms),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkew),
	)
	if err != nil {
		return nil, fmt.Errorf("identity token rejected: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("identity token rejected: signature did not verify")
	}
	rawSub, _ := mapClaims["sub"].(string)
	sub := strings.TrimSpace(rawSub)
	if sub == "" {
		return nil, fmt.Errorf("identity token has no subject")
	}
	role := roleFrom(mapClaims, v.cfg.RoleClaim)
	if role == "" {
		return nil, fmt.Errorf("%w: claim %q is absent or names no known role", ErrNoRole, v.cfg.RoleClaim)
	}
	// A provider's subject is an opaque string of its own choosing, while the
	// account tables key on UUID. Deriving the account id from the issuer and
	// subject rather than generating one and storing the mapping keeps the same
	// person on the same account across replicas, restarts and re-logins.
	accountID, err := domain.NewUUIDv5(domain.NamespaceFederatedIdentity, v.cfg.Issuer, sub)
	if err != nil {
		return nil, err
	}
	claims := &Claims{
		UserID:         accountID,
		Subject:        sub,
		Email:          stringClaim(mapClaims, "email"),
		Name:           stringClaim(mapClaims, "name"),
		Role:           role,
		Provider:       ProviderOIDC,
		ProviderIssuer: v.cfg.Issuer,
	}
	if claims.Name == "" {
		claims.Name = claims.Email
	}
	return claims, nil
}

// keyFunc resolves the key id the token was signed with, refreshing the cached
// key set when the id is unknown so a provider can rotate keys without a
// redeploy.
func (v *IDTokenVerifier) keyFunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("identity token has no key id")
	}
	key, err := v.keyFor(kid)
	if err != nil {
		return nil, err
	}
	// The method and the key type have to agree. WithValidMethods already
	// excludes the symmetric algorithms, and refusing to hand an RSA key to an
	// ECDSA check is the second lock on the same door.
	switch t.Method.Alg() {
	case "RS256", "RS384", "RS512":
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key id %q is not an RSA key", kid)
		}
		if err := checkRSASize(rsaKey); err != nil {
			return nil, err
		}
	case "ES256", "ES384", "ES512":
		ecKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key id %q is not an elliptic curve key", kid)
		}
		if err := checkCurveFor(ecKey, t.Method.Alg()); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported signing algorithm %v", t.Header["alg"])
	}
	return key, nil
}

func (v *IDTokenVerifier) keyFor(kid string) (any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.keys) == 0 || time.Since(v.fetched) > v.cfg.KeyTTL {
		if time.Since(v.fetched) >= v.cfg.MinRefresh || v.keys == nil {
			if err := v.refreshLocked(); err != nil {
				return nil, err
			}
		}
	}
	key, ok := v.keys[kid]
	if ok {
		return key, nil
	}
	if time.Since(v.fetched) < v.cfg.MinRefresh {
		return nil, fmt.Errorf("unknown key id %q; the cached key set is not old enough to refetch", kid)
	}
	if err := v.refreshLocked(); err != nil {
		return nil, err
	}
	if key, ok = v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("the identity provider does not publish key id %q", kid)
}

func (v *IDTokenVerifier) refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshLocked()
}

// refreshLocked fetches and replaces the key set. It runs with v.mu held: one
// request at a time may talk to the provider, and the rest wait for its answer
// rather than each starting their own fetch.
func (v *IDTokenVerifier) refreshLocked() error {
	ctx, cancel := context.WithTimeout(context.Background(), v.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not read the key set from %s: %w", v.cfg.JWKSURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("key set request returned HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSDocument)).Decode(&doc); err != nil {
		return fmt.Errorf("key set is not JSON: %w", err)
	}
	keys := make(map[string]any, len(doc.Keys))
	for _, raw := range doc.Keys {
		if raw.Kid == "" || (raw.Use != "" && raw.Use != "sig") {
			continue
		}
		key, err := parseJWK(raw)
		if err != nil {
			// One unreadable entry must not disable the others; a provider can
			// publish a key type this build does not support.
			continue
		}
		keys[raw.Kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("key set at %s publishes no usable signing keys", v.cfg.JWKSURL)
	}
	v.keys = keys
	v.fetched = time.Now().UTC()
	return nil
}

type rawJWK struct {
	// An "oct" entry is a symmetric secret. It is never parsed, so a provider
	// cannot hand this service a key that doubles as a signing secret.
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func decodeBigInt(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("empty key parameter")
	}
	return new(big.Int).SetBytes(b), nil
}

func checkRSASize(key *rsa.PublicKey) error {
	if key.N == nil {
		return fmt.Errorf("RSA key has no modulus")
	}
	// A 1024-bit RSA key is export-grade and has been unforgivable since 2013.
	if bits := key.N.BitLen(); bits < 2048 {
		return fmt.Errorf("RSA key is %d bits, minimum 2048", bits)
	}
	return nil
}

func checkCurveFor(key *ecdsa.PublicKey, alg string) error {
	if key.Curve == nil {
		return fmt.Errorf("EC key has no curve")
	}
	expected := map[string]int{"ES256": 256, "ES384": 384, "ES512": 521}[alg]
	if expected == 0 {
		return fmt.Errorf("unsupported EC algorithm %s", alg)
	}
	if size := key.Curve.Params().BitSize; size != expected {
		return fmt.Errorf("curve is %d bits, %s requires %d", size, alg, expected)
	}
	return nil
}

// parseJWK turns one published key into a verification key. Only RSA and the
// three NIST curves this platform verifies are recognised; an "oct" entry is a
// symmetric secret and is refused outright, because a key map that holds it is a
// key map from which an HS256 forgery can be checked.
func parseJWK(raw rawJWK) (any, error) {
	switch raw.Kty {
	case "RSA":
		n, err := decodeBigInt(raw.N)
		if err != nil {
			return nil, err
		}
		e, err := decodeBigInt(raw.E)
		if err != nil {
			return nil, err
		}
		if !e.IsInt64() || e.Int64() <= 1 {
			return nil, fmt.Errorf("RSA public exponent %s is unusable", e)
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		curve, err := curveFor(raw.Crv)
		if err != nil {
			return nil, err
		}
		x, err := decodeBigInt(raw.X)
		if err != nil {
			return nil, err
		}
		y, err := decodeBigInt(raw.Y)
		if err != nil {
			return nil, err
		}
		if !curve.IsOnCurve(x, y) {
			return nil, fmt.Errorf("EC point is not on curve %s", raw.Crv)
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	default:
		return nil, fmt.Errorf("unsupported key type %q", raw.Kty)
	}
}

func curveFor(name string) (elliptic.Curve, error) {
	switch name {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported curve %q", name)
	}
}

// roleFrom reads the platform role out of an identity token. Providers put group
// membership in a single string or in an array, so both shapes are accepted and
// the strongest recognised role wins: a principal listed as both UNDERWRITER and
// ADMIN is an ADMIN, and a claim holding "customer-support" or any other
// provider-side name resolves to nothing.
func roleFrom(claims jwt.MapClaims, claim string) string {
	values := []string{}
	switch v := claims[claim].(type) {
	case string:
		values = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	}
	best := ""
	for _, value := range values {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case RoleAdmin:
			return RoleAdmin
		case RoleUnderwriter:
			best = RoleUnderwriter
		case RoleBorrower:
			if best == "" {
				best = RoleBorrower
			}
		}
	}
	return best
}

func stringClaim(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return strings.TrimSpace(s)
}
