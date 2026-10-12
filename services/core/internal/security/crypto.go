package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// Hasher derives the SSN blind index and the AES-256-GCM ciphertext from one
// 32 byte master key. Each derived key is the full width of its primitive, and
// the labels keep the two independent so the same secret is never reused for
// two primitives.
type Hasher struct {
	indexKey []byte
	encKey   []byte
	aead     cipher.AEAD
}

func NewHasher(hexKey string) (*Hasher, error) {
	master, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("master key is not hex: %w", err)
	}
	if len(master) != 32 {
		return nil, fmt.Errorf("master key must decode to 32 bytes, got %d", len(master))
	}
	h := &Hasher{
		indexKey: derive(master, "blind-index"),
		encKey:   derive(master, "ssn-aes-gcm"),
	}
	block, err := aes.NewCipher(h.encKey)
	if err != nil {
		return nil, err
	}
	h.aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func derive(master []byte, label string) []byte {
	// The full SHA-256 width is kept: the encryption half must be a 32 byte
	// AES-256 key, not a truncated 16 byte AES-128 one.
	sum := sha256.Sum256(append(append([]byte(label), ':'), master...))
	return sum[:]
}

// BlindIndex is an HMAC-SHA256 of the normalised SSN digits: searchable
// equality lookup without ever storing the identifier in the clear.
func (h *Hasher) BlindIndex(ssn string) string {
	digits := onlyDigits(ssn)
	mac := hmac.New(sha256.New, h.indexKey)
	mac.Write([]byte(digits))
	return hex.EncodeToString(mac.Sum(nil))
}

// Encrypt seals the SSN and returns base64(nonce || ciphertext || tag).
func (h *Hasher) Encrypt(ssn string) (string, error) {
	digits := onlyDigits(ssn)
	nonce := make([]byte, h.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.aead.Seal(nonce, nonce, []byte(digits), nil)), nil
}

func (h *Hasher) Decrypt(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	if len(raw) < h.aead.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:h.aead.NonceSize()], raw[h.aead.NonceSize():]
	plain, err := h.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func onlyDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
