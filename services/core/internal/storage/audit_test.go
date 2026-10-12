package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The ledger's tamper-evidence rests on one hash formula. These tests pin it to
// vectors computed outside this package: if a refactor changes the formula, the
// existing rows stop verifying, and that must be a migration decision, not a
// side effect.

func TestChainHashMatchesAnIndependentVector(t *testing.T) {
	// SHA256(64 zeros || SHA256({"a":1}) || "2026-01-02T03:04:05.123456Z")
	const wantPayload = "015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"
	const wantEntry = "1ba8d89504426287e72e93f218be564663fa9c34dde5aa480b815f6fa48019e7"

	payload, err := json.Marshal(map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := hexSHA(payload); got != wantPayload {
		t.Errorf("payload hash %s, want %s", got, wantPayload)
	}

	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	if got := chainHash(GenesisHash, wantPayload, at); got != wantEntry {
		t.Errorf("entry hash %s, want %s", got, wantEntry)
	}
}

func TestGenesisHashIsTheChainRoot(t *testing.T) {
	if len(GenesisHash) != 64 || strings.Trim(GenesisHash, "0") != "" {
		t.Errorf("genesis hash must be 64 zero hex characters, got %q", GenesisHash)
	}
}

func TestChainHashDependsOnEveryInput(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	base := chainHash(GenesisHash, "a", at)
	mutations := map[string]string{
		"previous hash":  chainHash(strings.Repeat("1", 64), "a", at),
		"payload hash":   chainHash(GenesisHash, "b", at),
		"one second":     chainHash(GenesisHash, "a", at.Add(time.Second)),
		"one nanosecond": chainHash(GenesisHash, "a", at.Add(time.Nanosecond)),
	}
	for name, got := range mutations {
		if got == base {
			t.Errorf("changing the %s did not change the entry hash", name)
		}
	}
}

// Postgres TIMESTAMPTZ keeps microsecond precision. A timestamp carrying deeper
// sub-second precision formats into the hash one way and reads back another, so
// the re-verified chain would break against rows the writer just accepted.
func TestRecordedAtRoundTripsAtMicrosecondPrecision(t *testing.T) {
	for _, at := range []time.Time{
		time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 12, 8, 0, 0, 123456000, time.UTC),
		time.Date(2026, 10, 12, 8, 0, 0, 100000000, time.UTC),
		time.Date(2026, 10, 12, 23, 59, 59, 999999000, time.UTC),
	} {
		stamped := at.Truncate(time.Microsecond)
		text := stamped.Format(time.RFC3339Nano)
		back, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			t.Fatalf("parse %s: %v", text, err)
		}
		if chainHash(GenesisHash, "x", stamped) != chainHash(GenesisHash, "x", back) {
			t.Errorf("%s does not survive a write/read round trip", text)
		}
		if strings.Contains(text, "+00:00") {
			t.Errorf("UTC stamps must serialise as Z, got %s", text)
		}
	}
}

func TestUntruncatedNanosecondsWouldBreakVerification(t *testing.T) {
	// Documents why AppendAudit truncates: nanoseconds beyond the sixth digit are
	// silently dropped by Postgres, so the stored row hashes differently.
	at := time.Date(2026, 10, 12, 8, 0, 0, 123456789, time.UTC)
	if chainHash(GenesisHash, "x", at) == chainHash(GenesisHash, "x", at.Truncate(time.Microsecond)) {
		t.Error("expected the sub-microsecond digits to change the hash")
	}
}
