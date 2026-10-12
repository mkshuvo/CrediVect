package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"credivect/core/internal/domain"

	"github.com/jackc/pgx/v5"
)

// GenesisHash is the fixed predecessor of the first ledger entry. It is a
// constant, not per-process state, so the chain survives restarts and can be
// re-verified from scratch at any time.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// auditLockKey serialises appends so two concurrent requests can never both
// read the same head and fork the chain.
const auditLockKey = int64(0x43524544) // "CRED"

// AppendAudit writes a tamper-evident ledger entry:
//
//	entry_hash = SHA256(previous_entry_hash || payload_sha256 || recorded_at RFC3339Nano)
//
// Only the hash of the payload is stored; the metadata column carries the
// non-PII event summary so the audit trail itself never leaks borrower data.
func (r *Repository) AppendAudit(ctx context.Context, appID, eventType, actor string, metadata map[string]any) (domain.AuditLedgerEntry, error) {
	payload, err := jsonMap(metadata)
	if err != nil {
		return domain.AuditLedgerEntry{}, fmt.Errorf("encode audit metadata: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.AuditLedgerEntry{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditLockKey); err != nil {
		return domain.AuditLedgerEntry{}, fmt.Errorf("acquire ledger lock: %w", err)
	}

	var prev string
	err = tx.QueryRow(ctx, `
		SELECT entry_hash FROM audit_ledger ORDER BY recorded_at DESC, id DESC LIMIT 1`).Scan(&prev)
	if errors.Is(err, pgx.ErrNoRows) {
		prev = GenesisHash
		err = nil
	}
	if err != nil {
		return domain.AuditLedgerEntry{}, fmt.Errorf("read ledger head: %w", err)
	}

	entry := domain.AuditLedgerEntry{
		ID:                domain.NewUUIDv7(),
		ApplicationID:     appID,
		EventType:         eventType,
		Actor:             actor,
		PayloadSHA256:     hexSHA([]byte(payload)),
		PreviousEntryHash: prev,
		Metadata:          payload,
		RecordedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
	entry.EntryHash = chainHash(entry.PreviousEntryHash, entry.PayloadSHA256, entry.RecordedAt)

	// Platform-wide events such as a drift measurement have no application, and
	// the column is a nullable foreign key, not an empty string.
	var appArg any
	if appID != "" {
		appArg = appID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_ledger (id, application_id, event_type, actor, payload_sha256,
		                          previous_entry_hash, entry_hash, metadata, recorded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`,
		entry.ID, appArg, entry.EventType, entry.Actor, entry.PayloadSHA256,
		entry.PreviousEntryHash, entry.EntryHash, entry.Metadata, entry.RecordedAt); err != nil {
		return domain.AuditLedgerEntry{}, fmt.Errorf("append ledger entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AuditLedgerEntry{}, err
	}
	return entry, nil
}

func (r *Repository) ListAudit(ctx context.Context, limit int, applicationID string) ([]domain.AuditLedgerEntry, error) {
	args := []any{limit}
	where := ""
	if applicationID != "" {
		where = "WHERE application_id = $2"
		args = append(args, applicationID)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, COALESCE(application_id::text, ''), event_type, actor, payload_sha256,
		       previous_entry_hash, entry_hash, metadata::text, recorded_at
		FROM audit_ledger %s ORDER BY recorded_at DESC, id DESC LIMIT $1`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.AuditLedgerEntry{}
	for rows.Next() {
		var e domain.AuditLedgerEntry
		if err := rows.Scan(&e.ID, &e.ApplicationID, &e.EventType, &e.Actor, &e.PayloadSHA256,
			&e.PreviousEntryHash, &e.EntryHash, &e.Metadata, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyChain recomputes every entry hash from the genesis constant forward and
// reports the first divergence. A broken chain means the ledger table was
// edited outside the application, which is exactly what a bank examiner asks.
func (r *Repository) VerifyChain(ctx context.Context) (domain.ChainVerification, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, COALESCE(application_id::text, ''), event_type, actor, payload_sha256,
		       previous_entry_hash, entry_hash, metadata::text, recorded_at
		FROM audit_ledger ORDER BY recorded_at ASC, id ASC`)
	if err != nil {
		return domain.ChainVerification{}, err
	}
	defer rows.Close()

	result := domain.ChainVerification{Verified: true, Recomputed: GenesisHash}
	for rows.Next() {
		var e domain.AuditLedgerEntry
		if err := rows.Scan(&e.ID, &e.ApplicationID, &e.EventType, &e.Actor, &e.PayloadSHA256,
			&e.PreviousEntryHash, &e.EntryHash, &e.Metadata, &e.RecordedAt); err != nil {
			return result, err
		}
		result.EntriesChecked++
		expected := chainHash(result.Recomputed, e.PayloadSHA256, e.RecordedAt)
		if e.PreviousEntryHash != result.Recomputed || e.EntryHash != expected {
			result.Verified = false
			result.BrokenAtEntryID = e.ID
			result.BrokenAtRecordedAt = e.RecordedAt
			result.Reason = fmt.Sprintf("entry hash mismatch at ledger row %s", e.ID)
			return result, nil
		}
		result.Recomputed = e.EntryHash
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.HeadHash = result.Recomputed
	return result, nil
}

func hexSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// chainHash is the single definition of the ledger link, shared by the writer
// and the verifier so the two can never drift apart:
//
//	SHA256(previous_entry_hash || payload_sha256 || recorded_at RFC3339Nano)
func chainHash(prevHash, payloadSHA256 string, recordedAt time.Time) string {
	return hexSHA([]byte(prevHash + payloadSHA256 + recordedAt.Format(time.RFC3339Nano)))
}
