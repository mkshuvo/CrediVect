package storage

import (
	"encoding/json"
	"testing"
	"time"
)

// Every write to the ledger hashes its payload and folds it into the previous
// row, and verification re-reads the whole chain, so this is the cost of an
// append and of an audit on the same formula. The chain is walked in memory
// here: the point is the hashing, which is what an operator pays on a growing
// book.
func BenchmarkAuditChainHashing(b *testing.B) {
	metadata := map[string]any{
		"event":         "APPLICATION_SCORED",
		"score":         712,
		"calibrated_pd": 0.0418732,
		"risk_tier":     "PRIME",
		"model_version": "v1.1.0-77ab0b7b",
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		b.Fatalf("marshal: %v", err)
	}
	base := time.Date(2026, time.October, 13, 8, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	prev := hexSHA([]byte("genesis"))
	for i := 0; i < b.N; i++ {
		at := base.Add(time.Duration(i) * time.Millisecond)
		prev = chainHash(prev, hexSHA(payload), at)
	}
}

func BenchmarkPayloadHashing(b *testing.B) {
	payload := []byte(`{"application_id":"01918a2b-4c5d-7000-8000-000000000001","actor":"SYSTEM"}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = hexSHA(payload)
	}
}
