package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"credivect/core/internal/domain"

	"github.com/segmentio/kafka-go"
)

// Publisher appends application lifecycle events to the Kafka topic. The
// ledger is the tamper-evident record; this stream is the live projection that
// downstream consumers (queue dashboards, monitoring) read.
type Publisher struct {
	writer *kafka.Writer
	topic  string
}

type Event struct {
	EventType     string         `json:"event_type"`
	ApplicationID string         `json:"application_id"`
	Actor         string         `json:"actor"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Payload       map[string]any `json:"payload"`
	LedgerHash    string         `json:"ledger_entry_hash"`
}

func NewPublisher(brokers []string, topic string) (*Publisher, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no kafka brokers configured")
	}
	p := &Publisher{
		topic: topic,
		writer: &kafka.Writer{
			Addr:                   kafka.TCP(brokers...),
			Topic:                  topic,
			Balancer:               &kafka.Hash{},
			BatchTimeout:           50 * time.Millisecond,
			RequiredAcks:           kafka.RequireAll,
			AllowAutoTopicCreation: true,
			WriteTimeout:           5 * time.Second,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.ping(ctx, brokers[0]); err != nil {
		_ = p.writer.Close()
		return nil, err
	}
	return p, nil
}

func (p *Publisher) ping(ctx context.Context, broker string) error {
	dialer := kafka.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("dial kafka broker %s: %w", broker, err)
	}
	defer conn.Close()
	if _, err := conn.Controller(); err != nil {
		return fmt.Errorf("kafka controller on %s: %w", broker, err)
	}
	return nil
}

func (p *Publisher) Close() error { return p.writer.Close() }

func (p *Publisher) Publish(ctx context.Context, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:     []byte(e.ApplicationID),
		Value:   body,
		Headers: []kafka.Header{{Key: "event_type", Value: []byte(e.EventType)}},
	}
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("publish %s to %s: %w", e.EventType, p.topic, err)
	}
	return nil
}

// FromAudit derives the stream event from the ledger entry that was just
// committed, so the projection always carries the hash examiners can verify.
func FromAudit(entry domain.AuditLedgerEntry, payload map[string]any) Event {
	return Event{
		EventType:     entry.EventType,
		ApplicationID: entry.ApplicationID,
		Actor:         entry.Actor,
		OccurredAt:    entry.RecordedAt,
		Payload:       payload,
		LedgerHash:    entry.EntryHash,
	}
}
