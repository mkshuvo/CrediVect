package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// UUIDv7 generates an RFC 9562 time-ordered UUIDv7.
// The first 48 bits represent Unix millisecond timestamp, guaranteeing chronological B-Tree locality.
func NewUUIDv7() string {
	var uuid [16]byte
	
	// 48-bit timestamp in milliseconds
	timestamp := uint64(time.Now().UnixMilli())
	uuid[0] = byte(timestamp >> 40)
	uuid[1] = byte(timestamp >> 32)
	uuid[2] = byte(timestamp >> 24)
	uuid[3] = byte(timestamp >> 16)
	uuid[4] = byte(timestamp >> 8)
	uuid[5] = byte(timestamp)

	// Fill remaining 10 bytes with cryptographically secure random values
	_, _ = rand.Read(uuid[6:])

	// Version 7: set bits 4-7 of byte 6 to 0111
	uuid[6] = (uuid[6] & 0x0F) | 0x70

	// Variant: set bits 6-7 of byte 8 to 10
	uuid[8] = (uuid[8] & 0x3F) | 0x80

	buf := make([]byte, 36)
	hex.Encode(buf[0:8], uuid[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], uuid[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], uuid[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], uuid[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], uuid[10:16])

	return string(buf)
}
