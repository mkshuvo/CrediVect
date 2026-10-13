// Command dev-idp is a development-only identity provider. It exists so that
// AUTH_MODE=oidc can be exercised - by a person on a laptop, and by the end-to-end
// checks - without renting a tenant at Auth0 or standing up Keycloak.
//
// The signing key is generated at startup and thrown away at exit, and the
// endpoint hands out a token for any subject and role it is asked for. That is
// the whole point of it and the whole reason it must never be exposed: anyone who
// can reach it can sign in as anyone. It listens on loopback unless told
// otherwise.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"credivect/core/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9355", "address to listen on; loopback keeps this tool on one machine")
	issuer := flag.String("issuer", "", "issuer to publish and sign, defaulting to the listening address")
	audience := flag.String("audience", "credivect-api", "audience claim to mint")
	kid := flag.String("kid", "", "key id published in the key set, defaulting to a fingerprint of the generated key")
	ttl := flag.Duration("ttl", time.Hour, "lifetime of a minted token")
	flag.Parse()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("dev-idp: could not generate a signing key: %v", err)
	}
	// The key id names the key, not this tool. Every start generates a new
	// signing key, so a fixed id would let a verifier that cached the previous
	// public key go on rejecting the new tokens; a real provider rotates the id
	// along with the key for exactly that reason.
	if strings.TrimSpace(*kid) == "" {
		*kid = fingerprint(key)
	}
	log.Printf("dev-idp signing with key id %s", *kid)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "use": "sig", "kid": *kid, "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})

	// /token is the point of the tool: one call, one bearer credential that the
	// core service will accept exactly as it would accept Auth0's.
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// no_role mints a genuine identity with nothing that names a platform
		// role, which is how the "signed in but not authorised" answer gets
		// exercised without a second provider.
		noRole := strings.EqualFold(q.Get("no_role"), "true")
		role := strings.ToUpper(strings.TrimSpace(q.Get("role")))
		if !noRole {
			switch role {
			case auth.RoleBorrower, auth.RoleUnderwriter, auth.RoleAdmin:
			default:
				http.Error(w, "role must be one of BORROWER, UNDERWRITER, ADMIN (or pass no_role=true)", http.StatusBadRequest)
				return
			}
		}
		sub := strings.TrimSpace(q.Get("sub"))
		if sub == "" {
			sub = "dev-subject-" + role
		}
		now := time.Now().UTC()
		mapClaims := jwt.MapClaims{
			"sub":   sub,
			"iss":   *issuer,
			"aud":   *audience,
			"exp":   jwt.NewNumericDate(now.Add(*ttl)),
			"iat":   jwt.NewNumericDate(now),
			"email": strings.TrimSpace(q.Get("email")),
			"name":  strings.TrimSpace(q.Get("name")),
		}
		if !noRole {
			mapClaims["role"] = role
		}
		// A real provider labels every signature with the key that made it, and
		// the verifier depends on it: without a key id there is nothing to tell
		// a rotated key from a forged one.
		signed := jwt.NewWithClaims(jwt.SigningMethodRS256, mapClaims)
		signed.Header["kid"] = *kid
		raw, err := signed.SignedString(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, raw)
		log.Printf("minted a %s token for subject %s", roleLabel(noRole, role), sub)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "credivect dev identity provider\n\n  issuer   %s\n  key set  %s/.well-known/jwks.json\n  token    %s/token?role=UNDERWRITER&sub=desk-1&email=desk@partners.example&name=Priya%%20Raman\n  no role  %s/token?no_role=true&sub=nobody-1\n\n",
			*issuer, *issuer, *issuer, *issuer)
	})

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	host := *addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	host = strings.Replace(host, "0.0.0.0", "127.0.0.1", 1)
	if *issuer == "" {
		*issuer = "http://" + host
	}
	log.Printf("dev-idp listening on %s as issuer %s; stop it with Ctrl-C", host, *issuer)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("dev-idp: %v", err)
	}
}

func roleLabel(noRole bool, role string) string {
	if noRole {
		return "roleless"
	}
	return role
}

// fingerprint is the RFC 7638 style key id: the first bytes of the SHA-256 of
// the public key in DER, which changes whenever the key does.
func fingerprint(key *rsa.PrivateKey) string {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "dev-idp-key"
	}
	sum := sha256.Sum256(der)
	return "dev-" + hex.EncodeToString(sum[:6])
}
