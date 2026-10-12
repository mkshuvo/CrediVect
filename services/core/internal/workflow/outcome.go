package workflow

import (
	"context"
	"fmt"
	"time"

	"credivect/core/internal/domain"
	"credivect/core/internal/events"
)

// OutcomeInput is one adjudication: which status the application lands on, the
// decision row it produces (empty outcome means status change only), and who
// acted.
type OutcomeInput struct {
	ApplicationID      string
	Status             string
	Outcome            string // APPROVED, DECLINED, "" for status only
	ApprovedLimitCents int64
	ApprovedAPRBps     int32
	DecisionType       string // AUTOMATED, MANUAL_OVERRIDE
	UnderwriterID      string
	Notes              string
	ScoringRunID       string
	EventType          string
	Metadata           map[string]any
}

// RecordOutcome writes the status under optimistic concurrency, appends the
// audit entry and publishes the stream event. A version conflict means another
// adjudication already landed, which is a business outcome, not a retry.
func (a *Activities) RecordOutcome(ctx context.Context, in OutcomeInput) (domain.AuditLedgerEntry, error) {
	if in.ApplicationID == "" || in.Status == "" {
		return domain.AuditLedgerEntry{}, fmt.Errorf("outcome requires application id and status")
	}
	app, err := a.repo.GetApplication(ctx, in.ApplicationID)
	if err != nil {
		return domain.AuditLedgerEntry{}, err
	}

	var entry domain.AuditLedgerEntry
	if err := a.repo.UpdateApplicationStatus(ctx, app.ID, domain.ApplicationStatus(in.Status), app.Version, time.Now().UTC()); err != nil {
		return entry, fmt.Errorf("status transition for %s: %w", app.ID, err)
	}

	if in.Outcome != "" {
		if in.ScoringRunID == "" {
			if app.ScoringResult == nil || app.ScoringResult.ID == "" {
				return entry, fmt.Errorf("application %s has no scoring run to attach a decision to", app.ID)
			}
			in.ScoringRunID = app.ScoringResult.ID
		}
		decision := domain.Decision{
			ID:                 domain.NewUUIDv7(),
			ApplicationID:      app.ID,
			ScoringRunID:       in.ScoringRunID,
			Outcome:            in.Outcome,
			ApprovedLimitCents: in.ApprovedLimitCents,
			ApprovedAPRBps:     in.ApprovedAPRBps,
			DecisionType:       in.DecisionType,
			UnderwriterID:      in.UnderwriterID,
			DecisionNotes:      in.Notes,
			DecisionedAt:       time.Now().UTC(),
		}
		if err := a.repo.InsertDecision(ctx, decision); err != nil {
			return entry, fmt.Errorf("insert decision for %s: %w", app.ID, err)
		}
	}

	metadata := map[string]any{"status": in.Status}
	for k, v := range in.Metadata {
		metadata[k] = v
	}
	if in.Notes != "" {
		metadata["notes"] = in.Notes
	}
	actor := "SYSTEM"
	if in.UnderwriterID != "" {
		actor = "UNDERWRITER"
	}
	eventType := in.EventType
	if eventType == "" {
		eventType = "APPLICATION_" + in.Status
	}
	entry, err = a.repo.AppendAudit(ctx, app.ID, eventType, actor, metadata)
	if err != nil {
		return entry, err
	}
	if a.publisher != nil {
		if err := a.publisher.Publish(ctx, events.FromAudit(entry, metadata)); err != nil {
			return entry, err
		}
	}
	return entry, nil
}
