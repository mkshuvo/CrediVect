package security

import (
	"regexp"
	"testing"
)

func TestNewStatusTokenIsUnguessable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		tok, err := NewStatusToken()
		if err != nil {
			t.Fatalf("NewStatusToken: %v", err)
		}
		if len(tok) != 43 {
			t.Errorf("token length = %d, want 43 (32 bytes, base64url without padding)", len(tok))
		}
		// URL-safe so it can travel in a query string for the notice download.
		if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(tok) {
			t.Fatalf("token %q is not URL safe", tok)
		}
		if seen[tok] {
			t.Fatalf("token %q was issued twice", tok)
		}
		seen[tok] = true
	}
}

func TestHashStatusTokenIsStableAndHex64(t *testing.T) {
	h := HashStatusToken("any-token")
	if len(h) != 64 || h != HashStatusToken("any-token") {
		t.Fatalf("digest = %q, want a stable 64 character hex string", h)
	}
	if h == HashStatusToken("other-token") {
		t.Fatal("two different tokens share a digest")
	}
}

func TestStatusTokenMatchesOnlyTheExactToken(t *testing.T) {
	tok, err := NewStatusToken()
	if err != nil {
		t.Fatal(err)
	}
	digest := HashStatusToken(tok)

	if !StatusTokenMatches(digest, tok) {
		t.Error("the issued token does not match its own digest")
	}
	for name, presented := range map[string]string{
		"empty":            "",
		"prefix of itself": tok[:len(tok)-1],
		"extended":         tok + "x",
		"one byte flipped": tok[:len(tok)-1] + flipLast(tok[len(tok)-1:]),
		"unrelated":        "totally-different-token",
	} {
		if StatusTokenMatches(digest, presented) {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestStatusTokenMatchesWithoutADigest covers rows created before capability
// tokens existed: with nothing stored, no presented value may open the record.
func TestStatusTokenMatchesWithoutADigest(t *testing.T) {
	tok, err := NewStatusToken()
	if err != nil {
		t.Fatal(err)
	}
	if StatusTokenMatches("", tok) {
		t.Error("an empty stored digest matched a token")
	}
	if StatusTokenMatches("", "") {
		t.Error("two empty digests matched")
	}
}

func flipLast(b string) string {
	if b == "A" {
		return "B"
	}
	return "A"
}
