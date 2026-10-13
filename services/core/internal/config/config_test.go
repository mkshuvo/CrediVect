package config

import (
	"strings"
	"testing"
	"time"
)

// Load is the only place a misconfiguration can be caught before it becomes a
// runtime surprise, so the environment it reads is worth pinning down.
func minimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_URL", "postgres://user:pass@localhost:5432/credivect")
	t.Setenv("JWT_SECRET", "unit-test-secret-not-a-production-key")
	t.Setenv("SSN_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	for _, key := range []string{"AUTH_MODE", "OIDC_ISSUER", "OIDC_JWKS_URL", "OIDC_AUDIENCE", "OIDC_ROLE_CLAIM", "OIDC_KEY_TTL", "OIDC_ALLOW_CLEARTEXT_KEYS"} {
		t.Setenv(key, "")
	}
}

func TestLoadAuthenticationModes(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"local by default", nil, ""},
		{"oidc needs an issuer", map[string]string{"AUTH_MODE": "oidc"}, "OIDC_ISSUER"},
		{"oidc with an issuer", map[string]string{"AUTH_MODE": "oidc", "OIDC_ISSUER": "https://idp.example/"}, ""},
		{"oidc with an issuer", map[string]string{"AUTH_MODE": "oidc", "OIDC_ISSUER": "https://idp.example/"}, ""},
		{"an unknown mode", map[string]string{"AUTH_MODE": "password"}, "AUTH_MODE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			minimalEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("AUTH_MODE=%q was accepted", tc.env["AUTH_MODE"])
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadOIDCDefaultsAndTrimming(t *testing.T) {
	minimalEnv(t)
	t.Setenv("AUTH_MODE", "OIDC")
	t.Setenv("OIDC_ISSUER", "https://idp.example/acme/")
	t.Setenv("OIDC_ROLE_CLAIM", "https://credivect/role")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AuthMode != "oidc" {
		t.Errorf("AUTH_MODE is %q; the value is case insensitive", cfg.AuthMode)
	}
	// A trailing slash on the issuer is how it is written in every console, and
	// the key set path is appended to it.
	if cfg.OIDCIssuer != "https://idp.example/acme" {
		t.Errorf("issuer kept as %q", cfg.OIDCIssuer)
	}
	if cfg.OIDCAudience != "credivect-api" || cfg.OIDCRoleClaim != "https://credivect/role" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if cfg.OIDCKeyTTL != 5*time.Minute {
		t.Errorf("key ttl %s", cfg.OIDCKeyTTL)
	}
	if cfg.OIDCCleartext {
		t.Error("cleartext key reads must be off until asked for")
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("FLAG_ON", "YES")
	t.Setenv("FLAG_OFF", "0")
	t.Setenv("FLAG_JUNK", "maybe")
	if !envBool("FLAG_ON", false) {
		t.Error("YES was not read as true")
	}
	if envBool("FLAG_OFF", true) {
		t.Error("0 was not read as false")
	}
	if !envBool("FLAG_JUNK", true) || envBool("FLAG_JUNK", false) {
		t.Error("an unrecognised value must keep the default it was given")
	}
}
