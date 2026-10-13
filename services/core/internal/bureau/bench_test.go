package bureau

import (
	"context"
	"testing"
)

// The pull is the only thing in the saga that stands in for a third party, and
// with the simulator configured for no injected latency its own derivation is
// pure CPU: hashing the blind index into a seed, then rebuilding the whole
// consumer file from it.
func BenchmarkDeterministicFetch(b *testing.B) {
	p := NewDeterministicProvider()
	ctx := context.Background()
	req := Request{
		ApplicationID:        "01918a2b-4c5d-7000-8000-000000000001",
		ApplicantID:          "01918a2b-4c5d-7000-8000-000000000002",
		SSNBlindIndex:        "b7f3c1d9e2a44658f0a1c2d3e4f50617",
		FirstName:            "Priya",
		LastName:             "Raman",
		DateOfBirth:          "1991-04-17",
		MonthlyIncomeCents:   780000,
		RequestedAmountCents: 2400000,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Fetch(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSeedAndSampling(b *testing.B) {
	for i := 0; i < b.N; i++ {
		seed := seedFor("b7f3c1d9e2a44658f0a1c2d3e4f50617")
		_ = roundCents(seed)
	}
}
