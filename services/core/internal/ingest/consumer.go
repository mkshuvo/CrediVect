package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"credivect/core/internal/domain"
	"credivect/core/internal/events"
	"credivect/core/internal/storage"

	"github.com/segmentio/kafka-go"
)

// Consumer reads the application event stream and lands each message in the
// queryable read model. It is the destination the producer was always aimed at:
// until now the topic had writers and no reader, so nothing proved the events
// were well-formed or that a decision actually reached the stream.
type Consumer struct {
	reader *kafka.Reader
	repo   *storage.Repository
	topic  string
}

func New(brokers []string, topic, groupID string, repo *storage.Repository) (*Consumer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no kafka brokers configured")
	}
	return &Consumer{
		topic: topic,
		repo:  repo,
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   topic,
			// Offsets are committed by the consumer after the row is written, so a
			// crash replays the message rather than dropping it; the (partition,
			// offset) primary key makes that replay idempotent.
			MinBytes:    1,
			MaxBytes:    10 << 20,
			MaxWait:     500 * time.Millisecond,
			StartOffset: kafka.FirstOffset,
		}),
	}, nil
}

// streamEvent is the whole mapping from a wire message to a stored row, kept
// separate so the field-for-field copy is testable without a broker. Every field
// here is part of the audit trail: a silently dropped ledger hash or a local-time
// timestamp would corrupt the read model without failing anything.
func streamEvent(msg kafka.Message, e events.Event, consumedAt time.Time) domain.StreamEvent {
	return domain.StreamEvent{
		Offset:        msg.Offset,
		Partition:     int32(msg.Partition),
		EventType:     e.EventType,
		ApplicationID: e.ApplicationID,
		Actor:         e.Actor,
		OccurredAt:    e.OccurredAt.UTC(),
		LedgerHash:    e.LedgerHash,
		Payload:       e.Payload,
		ConsumedAt:    consumedAt.UTC(),
	}
}

// Run consumes until the context is cancelled. A malformed or unstorable
// message is logged and skipped rather than stalling the partition forever,
// because the ledger already holds the authoritative copy of the same fact.
func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Warn("kafka fetch failed", "topic", c.topic, "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
			continue
		}

		var e events.Event
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			slog.Error("discarding undecodable event", "partition", msg.Partition,
				"offset", msg.Offset, "error", err)
			_ = c.reader.CommitMessages(ctx, msg)
			continue
		}

		row := streamEvent(msg, e, time.Now().UTC())
		if err := c.repo.InsertStreamEvent(ctx, row); err != nil {
			slog.Error("could not store event", "event_type", e.EventType,
				"application_id", e.ApplicationID, "error", err)
			continue
		}
		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			slog.Warn("offset commit failed; the message will be re-consumed", "error", err)
		}
		slog.Info("event consumed", "event_type", e.EventType, "application_id", e.ApplicationID,
			"partition", msg.Partition, "offset", msg.Offset)
	}
	return c.reader.Close()
}
