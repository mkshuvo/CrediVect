package domain

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// counterSpace is the width of the rand_a field, used here as a per-millisecond
// sequence so ids minted inside one clock tick stay in insert order.
const counterSpace = 1 << 12

var (
	uuidMu   sync.Mutex
	uuidMS   uint64
	uuidSeq  uint64
	uuidTail [8]byte
)

// seedEpoch starts a fresh millisecond epoch: new random rand_b bits and a random
// counter origin, so ids remain unguessable as well as ordered.
func seedEpoch(ms uint64) {
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic("credivect: crypto/rand unavailable: " + err.Error())
	}
	uuidMS = ms
	copy(uuidTail[:], rnd[:8])
	uuidSeq = uint64(rnd[8])<<4 | uint64(rnd[9]&0x0F)
}

// NewUUIDv7 generates an RFC 9562 time-ordered UUIDv7.
//
// The first 48 bits are a Unix millisecond timestamp and the 12 bits after the
// version are a sequence within that millisecond, so a burst of ids still sorts
// by creation time. Postgres B-tree locality on the application, scoring run and
// audit ledger primary keys depends on that ordering.
func NewUUIDv7() string {
	uuidMu.Lock()
	defer uuidMu.Unlock()

	ms := uint64(time.Now().UnixMilli())
	if ms <= uuidMS {
		// A stalled or rewound clock must not rewind the id sequence.
		ms = uuidMS
	}
	switch {
	case ms != uuidMS:
		seedEpoch(ms)
	case uuidSeq >= counterSpace:
		seedEpoch(uuidMS + 1)
	}

	seq := uuidSeq
	uuidSeq++

	var uuid [16]byte
	uuid[0] = byte(uuidMS >> 40)
	uuid[1] = byte(uuidMS >> 32)
	uuid[2] = byte(uuidMS >> 24)
	uuid[3] = byte(uuidMS >> 16)
	uuid[4] = byte(uuidMS >> 8)
	uuid[5] = byte(uuidMS)
	uuid[6] = 0x70 | byte(seq>>8)
	uuid[7] = byte(seq)
	copy(uuid[8:], uuidTail[:])
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
