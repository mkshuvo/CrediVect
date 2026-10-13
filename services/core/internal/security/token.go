package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// statusTokenBytes is 256 bits, so a token cannot be found by search and carries
// the full strength of the SHA-256 digest it is stored as.
const statusTokenBytes = 32

// NewStatusToken mints an unguessable capability for one application. The value
// is returned once to the caller and never stored: only its digest goes to the
// database, so a read of the table cannot be replayed as access.
func NewStatusToken() (string, error) {
	raw := make([]byte, statusTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashStatusToken is the value persisted in credit_applications.status_token_hash.
func HashStatusToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// StatusTokenMatches compares a presented token against the stored digest. Both
// sides are 64-character lowercase hex, so the comparison is constant time and a
// missing digest (a row created before tokens existed) can never match.
func StatusTokenMatches(storedHash, token string) bool {
	if storedHash == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashStatusToken(token)), []byte(storedHash)) == 1
}
