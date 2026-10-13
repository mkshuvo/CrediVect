package domain

import "time"

// StreamEvent is one application lifecycle event as it landed in the read model.
// It carries the Kafka coordinates alongside the payload so a consumer's
// progress can be traced back to the partition it came from.
//
// This is the projection, not the record of authority: audit_ledger stays the
// tamper-evident chain, and every row here names the ledger hash it was derived
// from so the two can be reconciled.
type StreamEvent struct {
	Offset        int64          `json:"offset"`
	Partition     int32          `json:"partition"`
	EventType     string         `json:"event_type"`
	ApplicationID string         `json:"application_id"`
	Actor         string         `json:"actor"`
	OccurredAt    time.Time      `json:"occurred_at"`
	LedgerHash    string         `json:"ledger_entry_hash,omitempty"`
	Payload       map[string]any `json:"payload"`
	ConsumedAt    time.Time      `json:"consumed_at"`
}
