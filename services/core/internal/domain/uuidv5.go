package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// NamespaceDNS is the RFC 4122 DNS namespace: names under a naming authority
// rather than a document path.
const NamespaceDNS = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

// NamespaceFederatedIdentity scopes the account ids derived for externally
// authenticated principals. It is deliberately the DNS namespace and must never
// change: a different namespace derives a different id, and every credit record
// and review attached to the old one would belong to an unknown account.
const NamespaceFederatedIdentity = NamespaceDNS

// NewUUIDv5 derives an RFC 4122 version 5 (SHA-1 name-based) UUID.
//
// A principal authenticated by an external identity provider carries that
// provider's subject string, which is not a UUID, while the account tables key
// on UUID. Deriving the id from the issuer and subject instead of generating one
// is what makes the same person resolve to the same account on every core
// replica and after every restart, without a lookup table to keep in sync.
func NewUUIDv5(namespace string, elements ...string) (string, error) {
	raw, err := hex.DecodeString(strings.ReplaceAll(namespace, "-", ""))
	if err != nil || len(raw) != 16 {
		return "", fmt.Errorf("namespace %q is not a UUID", namespace)
	}
	var name strings.Builder
	// A single name is hashed exactly as RFC 4122 specifies. When more than one
	// element is given they are joined with 0x00, so ("a|b","c") cannot derive
	// the same identifier as ("a","b|c").
	for i, element := range elements {
		if i > 0 {
			name.WriteByte(0)
		}
		name.WriteString(element)
	}
	sum := sha1.Sum(append(raw, name.String()...))
	var uuid [16]byte
	copy(uuid[:], sum[:16])
	uuid[6] = (uuid[6] & 0x0F) | 0x50
	uuid[8] = (uuid[8] & 0x3F) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
