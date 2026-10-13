package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// publishedKey is one entry of the fake provider's key set: the private half is
// used to sign test tokens, the public half is what the endpoint serves.
type publishedKey struct {
	kid  string
	priv any
	jwk  map[string]any
}

// fakeIdP is an OAuth2/OIDC authority small enough to reason about: it publishes
// a JWKS document and signs tokens with the keys listed in it.
type fakeIdP struct {
	server *httptest.Server

	mu   sync.Mutex
	keys []publishedKey
	// fetches counts key-set reads, so the cache behaviour is observable rather
	// than assumed.
	fetches int
}

func newFakeIdP(t *testing.T) *fakeIdP {
	idp := &fakeIdP{}
	idp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		idp.mu.Lock()
		defer idp.mu.Unlock()
		idp.fetches++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": idp.keysJWK()})
	}))
	t.Cleanup(idp.server.Close)
	return idp
}

func (idp *fakeIdP) keysJWK() []map[string]any {
	out := make([]map[string]any, 0, len(idp.keys))
	for _, k := range idp.keys {
		out = append(out, k.jwk)
	}
	return out
}

func (idp *fakeIdP) addRSA(t *testing.T, kid string, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.keys = append(idp.keys, publishedKey{
		kid:  kid,
		priv: key,
		jwk: map[string]any{
			"kty": "RSA", "use": "sig", "kid": kid, "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		},
	})
	return key
}

func (idp *fakeIdP) addEC(t *testing.T, kid string, curve elliptic.Curve, alg string) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	size := (curve.Params().BitSize + 7) / 8
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.keys = append(idp.keys, publishedKey{
		kid:  kid,
		priv: key,
		jwk: map[string]any{
			"kty": "EC", "use": "sig", "kid": kid, "alg": alg,
			"crv": curveName(curve),
			"x":   base64.RawURLEncoding.EncodeToString(padCoord(key.X.Bytes(), size)),
			"y":   base64.RawURLEncoding.EncodeToString(padCoord(key.Y.Bytes(), size)),
		},
	})
	return key
}

// addSymmetric plants a shared secret in the published document. A verifier that
// parsed it could be fooled into checking an HS256 token against it, which is the
// standard route from "this key set signs RS256" to "anyone with the secret is
// an admin".
func (idp *fakeIdP) addSymmetric(t *testing.T, kid string, secret []byte) {
	t.Helper()
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.keys = append(idp.keys, publishedKey{
		kid:  kid,
		priv: secret,
		jwk: map[string]any{
			"kty": "oct", "use": "sig", "kid": kid, "alg": "HS256",
			"k": base64.RawURLEncoding.EncodeToString(secret),
		},
	})
}

func (idp *fakeIdP) drop(kid string) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	kept := idp.keys[:0]
	for _, k := range idp.keys {
		if k.kid != kid {
			kept = append(kept, k)
		}
	}
	idp.keys = kept
}

func (idp *fakeIdP) fetchCount() int {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	return idp.fetches
}

// sign mints a token the way a provider would, including the header key id. The
// claims are returned so a test can override fields before signing.
func (idp *fakeIdP) sign(t *testing.T, kid, alg string, claims jwt.MapClaims) string {
	t.Helper()
	return idp.signWith(t, kid, alg, idp.keyMaterial(t, kid), claims)
}

// signWith mints a token under a declared key id using whatever key material the
// caller hands it. That is how a forgery reuses a published kid with an
// algorithm the provider never signed with it.
func (idp *fakeIdP) signWith(t *testing.T, kid, alg string, key any, claims jwt.MapClaims) string {
	t.Helper()
	method := jwt.GetSigningMethod(alg)
	if method == nil {
		t.Fatalf("no signing method %q", alg)
	}
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

func (idp *fakeIdP) keyMaterial(t *testing.T, kid string) any {
	t.Helper()
	idp.mu.Lock()
	defer idp.mu.Unlock()
	for _, k := range idp.keys {
		if k.kid == kid {
			return k.priv
		}
	}
	t.Fatalf("no key id %q is published", kid)
	return nil
}

func (idp *fakeIdP) claims(role string) jwt.MapClaims {
	now := time.Now().UTC()
	return jwt.MapClaims{
		"sub":   "8f2c1d0a-4b7e-4d33-9d1f-6d2c2f0a1b9e",
		"iss":   idp.server.URL,
		"aud":   "credivect-api",
		"exp":   jwt.NewNumericDate(now.Add(time.Hour)),
		"iat":   jwt.NewNumericDate(now),
		"email": "analyst@partners.example",
		"name":  "Priya Raman",
		"role":  role,
	}
}

func curveName(c elliptic.Curve) string {
	switch c {
	case elliptic.P256():
		return "P-256"
	case elliptic.P384():
		return "P-384"
	case elliptic.P521():
		return "P-521"
	}
	return "unknown"
}

func padCoord(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func verifierFor(idp *fakeIdP) (*IDTokenVerifier, error) {
	return NewIDTokenVerifier(OIDCSettings{
		Issuer:   idp.server.URL,
		Audience: "credivect-api",
	})
}

func TestVerifierAcceptsProviderRS256Token(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	claims, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter)))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Role != RoleUnderwriter || claims.Email != "analyst@partners.example" || claims.Name != "Priya Raman" {
		t.Errorf("claims mapped wrong: %+v", claims)
	}
	if claims.Provider != ProviderOIDC || claims.ProviderIssuer != idp.server.URL {
		t.Errorf("provider attribution lost: %+v", claims)
	}
	if claims.Subject != "8f2c1d0a-4b7e-4d33-9d1f-6d2c2f0a1b9e" {
		t.Errorf("subject %q is not the provider's own id", claims.Subject)
	}
	// The account id is what the schema keys on, so it has to be a UUID even
	// though the provider's subject is not.
	if _, err := uuidParts(claims.UserID); err != nil {
		t.Errorf("account id %q is not a UUID: %v", claims.UserID, err)
	}
	if claims.UserID == claims.Subject {
		t.Error("the provider subject was used as the account id")
	}
}

func TestVerifierAcceptsEllipticCurveTokens(t *testing.T) {
	cases := []struct {
		alg   string
		curve elliptic.Curve
	}{
		{"ES256", elliptic.P256()},
		{"ES384", elliptic.P384()},
		{"ES512", elliptic.P521()},
	}
	for _, tc := range cases {
		idp := newFakeIdP(t)
		idp.addEC(t, "ec-1", tc.curve, tc.alg)
		v, err := verifierFor(idp)
		if err != nil {
			t.Fatalf("%s: build verifier: %v", tc.alg, err)
		}
		claims, err := v.Verify(idp.sign(t, "ec-1", tc.alg, idp.claims(RoleAdmin)))
		if err != nil {
			t.Errorf("%s: %v", tc.alg, err)
			continue
		}
		if claims.Role != RoleAdmin {
			t.Errorf("%s: role %q", tc.alg, claims.Role)
		}
	}
}

func TestVerifierRejectsSymmetricConfusion(t *testing.T) {
	idp := newFakeIdP(t)
	rsaKey := idp.addRSA(t, "key-1", 2048)
	secret := []byte("a shared secret the provider happens to publish")
	idp.addSymmetric(t, "sym-1", secret)

	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	// Two independent forgeries: sign an HS256 token with the published secret,
	// and sign one with the RSA public key bytes. Neither may be accepted, and
	// the legitimate RS256 token still has to work afterwards.
	publicBytes, err := json.Marshal(rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	for _, attempt := range []struct {
		name   string
		kid    string
		secret []byte
	}{
		{"published oct key", "sym-1", secret},
		{"rsa public key as HMAC secret", "key-1", publicBytes},
	} {
		forged := idp.signWith(t, attempt.kid, "HS256", attempt.secret, idp.claims(RoleAdmin))
		if _, err := v.Verify(forged); err == nil {
			t.Errorf("%s: an HS256 token was accepted", attempt.name)
		}
	}
	if _, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter))); err != nil {
		t.Errorf("a planted symmetric key broke the real token: %v", err)
	}
}

func TestVerifierRejectsUnsignedAndUnknownKeyTokens(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	t.Run("unknown key id", func(t *testing.T) {
		if _, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims(RoleAdmin))); err != nil {
			t.Fatalf("control token failed: %v", err)
		}
		// Same signature, different declared key id.
		swapped := reheader(idp.sign(t, "key-1", "RS256", idp.claims(RoleAdmin)),
			`{"alg":"RS256","typ":"JWT","kid":"not-published"}`)
		if _, err := v.Verify(swapped); err == nil {
			t.Error("a token claiming an unpublished key id was accepted")
		}
	})

	t.Run("alg none", func(t *testing.T) {
		if _, err := v.Verify(unsigned(idp.sign(t, "key-1", "RS256", idp.claims(RoleAdmin)))); err == nil {
			t.Error("an unsigned token was accepted")
		}
	})

	t.Run("garbage", func(t *testing.T) {
		for _, raw := range []string{"", "not-a-token", "a.b.c"} {
			if _, err := v.Verify(raw); err == nil {
				t.Errorf("%q was accepted", raw)
			}
		}
	})
}

func TestVerifierEnforcesIssuerAudienceAndLifetime(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"another issuer", func(c jwt.MapClaims) { c["iss"] = "https://somewhere-else.example" }},
		{"another audience", func(c jwt.MapClaims) { c["aud"] = "the-billing-service" }},
		{"expired", func(c jwt.MapClaims) {
			c["exp"] = jwt.NewNumericDate(time.Now().UTC().Add(-10 * time.Minute))
		}},
		{"no expiry", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"no subject", func(c jwt.MapClaims) { delete(c, "sub") }},
		{"blank subject", func(c jwt.MapClaims) { c["sub"] = "   " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := idp.claims(RoleUnderwriter)
			tc.mutate(claims)
			if _, err := v.Verify(idp.sign(t, "key-1", "RS256", claims)); err == nil {
				t.Errorf("token with %s was accepted", tc.name)
			}
		})
	}

	// The clock-skew allowance is small and finite: a token that expired within
	// it still works, because two servers drifting a few seconds is normal.
	nearly := idp.claims(RoleUnderwriter)
	nearly["exp"] = jwt.NewNumericDate(time.Now().UTC().Add(-10 * time.Second))
	if _, err := v.Verify(idp.sign(t, "key-1", "RS256", nearly)); err != nil {
		t.Errorf("a token expired 10 seconds ago should verify within the skew: %v", err)
	}
}

func TestVerifierRoleClaimMapping(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	t.Run("no role claim", func(t *testing.T) {
		claims := idp.claims(RoleUnderwriter)
		delete(claims, "role")
		_, err := v.Verify(idp.sign(t, "key-1", "RS256", claims))
		if !errors.Is(err, ErrNoRole) {
			t.Fatalf("expected ErrNoRole, got %v", err)
		}
	})

	t.Run("provider role names are not platform roles", func(t *testing.T) {
		_, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims("support-agent")))
		if !errors.Is(err, ErrNoRole) {
			t.Errorf("expected ErrNoRole, got %v", err)
		}
	})

	t.Run("group array takes the strongest role", func(t *testing.T) {
		claims := idp.claims("")
		claims["role"] = []any{"BORROWER", "UNDERWRITER", "customer-support"}
		got, err := v.Verify(idp.sign(t, "key-1", "RS256", claims))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if got.Role != RoleUnderwriter {
			t.Errorf("role %q, expected %s", got.Role, RoleUnderwriter)
		}

		claims["role"] = []any{"BORROWER", "ADMIN"}
		got, err = v.Verify(idp.sign(t, "key-1", "RS256", claims))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if got.Role != RoleAdmin {
			t.Errorf("role %q, expected %s", got.Role, RoleAdmin)
		}
	})

	t.Run("lowercase is tolerated", func(t *testing.T) {
		got, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims("admin")))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if got.Role != RoleAdmin {
			t.Errorf("role %q", got.Role)
		}
	})

	t.Run("custom claim name", func(t *testing.T) {
		custom, err := NewIDTokenVerifier(OIDCSettings{
			Issuer: idp.server.URL, Audience: "credivect-api", RoleClaim: "https://credivect/role",
		})
		if err != nil {
			t.Fatalf("build verifier: %v", err)
		}
		claims := idp.claims(RoleBorrower)
		claims["https://credivect/role"] = claims["role"]
		delete(claims, "role")
		got, err := custom.Verify(idp.sign(t, "key-1", "RS256", claims))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if got.Role != RoleBorrower {
			t.Errorf("role %q", got.Role)
		}
	})
}

func TestVerifierFollowsKeyRotation(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := NewIDTokenVerifier(OIDCSettings{
		Issuer: idp.server.URL, Audience: "credivect-api", KeyTTL: time.Millisecond, MinRefresh: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	if _, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter))); err != nil {
		t.Fatalf("first key failed: %v", err)
	}

	// The provider retires key-1 and publishes key-2, without this service being
	// restarted or reconfigured.
	idp.addRSA(t, "key-2", 2048)
	idp.drop("key-1")
	if _, err := v.Verify(idp.sign(t, "key-2", "RS256", idp.claims(RoleUnderwriter))); err != nil {
		t.Fatalf("rotated key failed: %v", err)
	}
}

func TestVerifierCachesTheKeySet(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	v, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	before := idp.fetchCount()
	for i := 0; i < 20; i++ {
		if _, err := v.Verify(idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter))); err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	if after := idp.fetchCount(); after != before {
		t.Errorf("20 authentications took the provider from %d to %d key-set reads", before, after)
	}

	// Unknown key ids must not become a way to hammer the provider: the first one
	// may refetch, the next ones inside the refresh window must not.
	before = idp.fetchCount()
	v.cfg.MinRefresh = time.Hour
	for i := 0; i < 25; i++ {
		token := idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter))
		forged := reheader(token, fmt.Sprintf(`{"alg":"RS256","typ":"JWT","kid":"forged-%d"}`, i))
		if _, err := v.Verify(forged); err == nil {
			t.Fatal("a forged key id was accepted")
		}
	}
	if reads := idp.fetchCount() - before; reads > 1 {
		t.Errorf("25 forged key ids cost %d provider reads, expected at most 1", reads)
	}
}

func TestVerifierRejectsWeakKeysAndCleartext(t *testing.T) {
	small := newFakeIdP(t)
	small.addRSA(t, "weak", 1024)
	v, err := verifierFor(small)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	if _, err := v.Verify(small.sign(t, "weak", "RS256", small.claims(RoleAdmin))); err == nil {
		t.Error("a 1024-bit RSA key was accepted")
	}

	// A key set with nothing verifiable in it is a configuration failure, not a
	// service that authenticates nobody.
	empty := newFakeIdP(t)
	if _, err := verifierFor(empty); err == nil {
		t.Error("a provider publishing no usable keys started anyway")
	}

	// Cleartext is refused everywhere but loopback, where a local Keycloak or the
	// test provider above lives, and it is accepted elsewhere only when the
	// operator says so out loud.
	for _, raw := range []string{"http://idp.example.com/.well-known/jwks.json", "ftp://idp.example/keys"} {
		if err := checkKeySetURL(raw, false); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
	if err := checkKeySetURL("http://idp.example.com/keys", true); err != nil {
		t.Errorf("the explicit opt-in was ignored: %v", err)
	}
	for _, raw := range []string{"https://idp.example.com/keys", "http://localhost:8080/keys", "http://127.0.0.1:8080/keys"} {
		if err := checkKeySetURL(raw, false); err != nil {
			t.Errorf("%q was refused: %v", raw, err)
		}
	}
}

func TestVerifierConstructionRequiresIssuerAndAudience(t *testing.T) {
	if _, err := NewIDTokenVerifier(OIDCSettings{Audience: "credivect-api"}); err == nil {
		t.Error("a provider with no issuer was accepted")
	}
	if _, err := NewIDTokenVerifier(OIDCSettings{Issuer: "https://idp.example"}); err == nil {
		t.Error("a provider with no audience was accepted")
	}
}

// TestDerivedAccountIsStableAcrossVerifierInstances is the property that makes
// just-in-time provisioning safe to run on more than one replica: the same
// identity arriving at two processes must resolve to the same account row.
func TestDerivedAccountIsStableAcrossVerifierInstances(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	first, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	second, err := NewIDTokenVerifier(OIDCSettings{Issuer: idp.server.URL, Audience: "credivect-api"})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	token := idp.sign(t, "key-1", "RS256", idp.claims(RoleUnderwriter))
	a, err := first.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	b, err := second.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if a.UserID != b.UserID {
		t.Errorf("one identity resolved to %s and %s", a.UserID, b.UserID)
	}
}

func TestMiddlewareChainTriesEveryProvider(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addRSA(t, "key-1", 2048)
	remote, err := verifierFor(idp)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	local := NewTokenService(testSecret, time.Hour)
	guard := NewMiddleware(local, remote)

	responds := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := From(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, claims.Provider+":"+claims.Role)
	})

	for _, tc := range []struct {
		name  string
		token string
		want  int
		body  string
	}{
		{"local session", mustIssue(t, local), http.StatusOK, "local:UNDERWRITER"},
		{"provider session", idp.sign(t, "key-1", "RS256", idp.claims(RoleAdmin)), http.StatusOK, "oidc:ADMIN"},
		{"unsigned", "header.payload.signature", http.StatusUnauthorized, ""},
		{"provider token without a role", idp.sign(t, "key-1", "RS256", noRoleClaims(idp)), http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			guard.Authenticate(responds).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, expected %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.body != "" && rec.Body.String() != tc.body {
				t.Errorf("body %q, expected %q", rec.Body.String(), tc.body)
			}
		})
	}

	t.Run("no credential stays anonymous", func(t *testing.T) {
		rec := httptest.NewRecorder()
		guard.Authenticate(responds).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous request reached the handler with status %d", rec.Code)
		}
	})
}

func noRoleClaims(idp *fakeIdP) jwt.MapClaims {
	claims := idp.claims(RoleUnderwriter)
	delete(claims, "role")
	return claims
}

func mustIssue(t *testing.T, svc *TokenService) string {
	t.Helper()
	raw, _, err := svc.Issue(Claims{UserID: "0195b3f8-0000-7000-8000-000000000001", Role: RoleUnderwriter, Email: "desk@credivect.local", Name: "D"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return raw
}

// unsigned keeps the payload of a properly signed token and swaps its header for
// one that asks the verifier to check no signature at all.
func unsigned(token string) string {
	return reheader(token, `{"alg":"none","typ":"JWT","kid":"key-1"}`)
}

// reheader replaces the header of a token, which is how a forgery declares a key
// id or an algorithm the provider never published for that signature.
func reheader(token, header string) string {
	parts := strings.Split(token, ".")
	return base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + parts[1] + "." + parts[2]
}

// uuidParts is the local check that a derived account id is parseable as a UUID
// without pulling the domain package into these tests.
func uuidParts(raw string) ([16]byte, error) {
	var out [16]byte
	hexDigits := strings.ReplaceAll(raw, "-", "")
	if len(hexDigits) != 32 {
		return out, fmt.Errorf("%q has %d hex digits", raw, len(hexDigits))
	}
	for i := 0; i < 32; i += 2 {
		var value int
		if _, err := fmt.Sscanf(hexDigits[i:i+2], "%02x", &value); err != nil {
			return out, err
		}
		out[i/2] = byte(value)
	}
	return out, nil
}
