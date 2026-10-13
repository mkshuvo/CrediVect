package storage

import (
	"context"
	"encoding/json"

	"credivect/core/internal/domain"
)

// InsertStreamEvent lands one consumed Kafka message in the read model. The
// (partition, offset) key is the natural id of a Kafka record, so a redelivery
// after a consumer restart resolves to the same row instead of double counting.
func (r *Repository) InsertStreamEvent(ctx context.Context, e domain.StreamEvent) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO application_events (kafka_offset, kafka_partition, event_type, application_id, actor,
		                                occurred_at, ledger_entry_hash, payload, consumed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (kafka_partition, kafka_offset) DO NOTHING`,
		e.Offset, e.Partition, e.EventType, e.ApplicationID, e.Actor,
		e.OccurredAt, e.LedgerHash, payload, e.ConsumedAt)
	return err
}

func (r *Repository) ListStreamEvents(ctx context.Context, applicationID string, limit int) ([]domain.StreamEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kafka_offset, kafka_partition, event_type, application_id::text, actor, occurred_at,
		       ledger_entry_hash, payload, consumed_at
		FROM application_events
		WHERE ($1 = '' OR application_id::text = $1)
		ORDER BY occurred_at DESC, kafka_partition ASC, kafka_offset DESC
		LIMIT $2`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.StreamEvent
	for rows.Next() {
		var e domain.StreamEvent
		var raw []byte
		if err := rows.Scan(&e.Offset, &e.Partition, &e.EventType, &e.ApplicationID, &e.Actor,
			&e.OccurredAt, &e.LedgerHash, &raw, &e.ConsumedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountStreamEventsByType is the consumer's own progress report: it answers
// "did the stream actually reach somewhere" without a Kafka client.
func (r *Repository) CountStreamEventsByType(ctx context.Context) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT event_type, count(*) FROM application_events GROUP BY event_type ORDER BY event_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int64{}
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		counts[kind] = n
	}
	return counts, rows.Err()
}
