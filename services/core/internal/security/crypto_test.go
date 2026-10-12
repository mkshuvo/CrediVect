package security

import (
	"encoding/hex"
	"strings"
	"testing"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func newTestHasher(t *testing.T) *Hasher {
	t.Helper()
	h, err := NewHasher(testKey)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	return h
}

func TestNewHasherRejectsBadKey(t *testing.T) {
	for _, key := range []string{"", "not-hex", strings.Repeat("ab", 31), strings.Repeat("ab", 33)} {
		if _, err := NewHasher(key); err == nil {
			t.Errorf("NewHasher(%q) accepted a key that is not 32 bytes of hex", key)
		}
	}
}

func TestBlindIndexIsDeterministicAndNormalising(t *testing.T) {
	h := newTestHasher(t)

	a := h.BlindIndex("123-45-6789")
	b := h.BlindIndex("123456789")
	c := h.BlindIndex(" 123 45 6789 ")
	if a != b || b != c {
		t.Errorf("blind index must depend only on the digits: %s vs %s vs %s", a, b, c)
	}
	if len(a) != 64 {
		t.Errorf("blind index must be a hex sha256, got %d characters", len(a))
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("blind index is not hex: %v", err)
	}
	if a == h.BlindIndex("987654321") {
		t.Error("different SSNs produced the same blind index")
	}
}

func TestBlindIndexIsKeyed(t *testing.T) {
	// Without the key in the derivation every deployment would share an index
	// space, so a leaked table could be correlated against a purchased SSN list.
	h1, _ := NewHasher(testKey)
	other := strings.Repeat("ff", 32)
	h2, err := NewHasher(other)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	if h1.BlindIndex("123456789") == h2.BlindIndex("123456789") {
		t.Error("blind index does not depend on the master key")
	}
}

func TestEncryptRoundTripAndNonceUniqueness(t *testing.T) {
	h := newTestHasher(t)

	first, err := h.Encrypt("123-45-6789")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := h.Encrypt("123-45-6789")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if first == second {
		t.Error("identical plaintexts produced identical ciphertext; the nonce is not random")
	}
	for _, sealed := range []string{first, second} {
		plain, err := h.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if plain != "123456789" {
			t.Errorf("round trip returned %q, want the digits only", plain)
		}
	}
}

func TestDecryptDetectsTampering(t *testing.T) {
	h := newTestHasher(t)
	sealed, err := h.Encrypt("123456789")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := h.Decrypt("not base64 !!"); err == nil {
		t.Error("Decrypt accepted a value that is not base64")
	}
	if _, err := h.Decrypt(sealed[:len(sealed)-4]); err == nil {
		t.Error("Decrypt accepted a truncated envelope")
	}

	// Flip one bit in the payload. GCM must refuse it rather than return a
	// plausible-looking wrong number.
	tampered := []byte(sealed)
	tampered[len(tampered)/2] ^= 'A' ^ 'B'
	if _, err := h.Decrypt(string(tampered)); err == nil {
		t.Error("Decrypt accepted a modified ciphertext")
	}

	other, _ := NewHasher(strings.Repeat("00", 32))
	if _, err := other.Decrypt(sealed); err == nil {
		t.Error("a different key decrypted the envelope")
	}
}
