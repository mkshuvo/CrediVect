package ingest

import (
	"encoding/json"
	"testing"
	"time"

	"credivect/core/internal/events"

	"github.com/segmentio/kafka-go"
)

func TestNewRejectsEmptyBrokerList(t *testing.T) {
	if _, err := New(nil, "topic", "group", nil); err == nil {
		t.Fatal("expected an error with no brokers configured")
	}
}

// The wire format is defined by internal/events, not by this package, so the
// test pins the names the producer actually emits. A tag renamed on either side
// would otherwise decode into a zero value and land an empty row.
func TestStreamEventMapsEveryProducedField(t *testing.T) {
	wire := `{"event_type":"APPLICATION_SCORED","application_id":"01a1369a-ef64-7ac7-8f40-e1a030b62505",` +
		`"actor":"SYSTEM","occurred_at":"2026-10-13T18:51:28.165906Z",` +
		`"payload":{"risk_tier":"PRIME","scorecard_points":687},` +
		`"ledger_entry_hash":"350fb2e406ed5565ee621887e039187de5d04c482a7929eea0ea63f01a61927c"}`

	var e events.Event
	if err := json.Unmarshal([]byte(wire), &e); err != nil {
		t.Fatalf("producer payload did not decode: %v", err)
	}
	if e.ApplicationID == "" || e.EventType == "" || e.LedgerHash == "" {
		t.Fatalf("field name drift: %+v", e)
	}

	// A broker message carrying a non-UTC occurred_at must still be stored as UTC.
	east := time.FixedZone("UTC+6", 6*60*60)
	e.OccurredAt = time.Date(2026, 10, 13, 2, 51, 28, 165906000, east)
	consumed := time.Date(2026, 10, 13, 19, 0, 0, 0, time.UTC)

	row := streamEvent(kafka.Message{Partition: 3, Offset: 42}, e, consumed)

	if row.Partition != 3 || row.Offset != 42 {
		t.Errorf("position lost: partition=%d offset=%d", row.Partition, row.Offset)
	}
	if row.EventType != "APPLICATION_SCORED" || row.Actor != "SYSTEM" {
		t.Errorf("event identity lost: %+v", row)
	}
	if row.LedgerHash[:12] != "350fb2e406ed" {
		t.Errorf("ledger hash not carried through: %q", row.LedgerHash)
	}
	if row.Payload["risk_tier"] != "PRIME" || row.Payload["scorecard_points"] != float64(687) {
		t.Errorf("payload lost: %+v", row.Payload)
	}
	if row.OccurredAt.Location() != time.UTC || !row.OccurredAt.Equal(e.OccurredAt) {
		t.Errorf("occurred_at not normalised to UTC: %s", row.OccurredAt)
	}
	if row.ConsumedAt.Location() != time.UTC {
		t.Errorf("consumed_at not UTC: %s", row.ConsumedAt)
	}
}
