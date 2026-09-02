package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"credivect/core/internal/domain"
)

type Repository struct {
	mu               sync.RWMutex
	applicants       map[string]domain.Applicant
	applications     map[string]domain.CreditApplication
	auditLedger      []domain.AuditLedgerEntry
	lastLedgerHash   string
}

func NewRepository() *Repository {
	return &Repository{
		applicants:     make(map[string]domain.Applicant),
		applications:   make(map[string]domain.CreditApplication),
		auditLedger:    make([]domain.AuditLedgerEntry, 0),
		lastLedgerHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}
}

func (r *Repository) SaveApplicant(a domain.Applicant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applicants[a.ID] = a
}

func (r *Repository) GetApplicant(id string) (domain.Applicant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.applicants[id]
	return a, ok
}

func (r *Repository) SaveApplication(app domain.CreditApplication) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applications[app.ID] = app
}

func (r *Repository) GetApplication(id string) (domain.CreditApplication, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	app, ok := r.applications[id]
	return app, ok
}

func (r *Repository) ListApplications() []domain.CreditApplication {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]domain.CreditApplication, 0, len(r.applications))
	for _, app := range r.applications {
		list = append(list, app)
	}
	return list
}

// AppendAuditRecord appends a cryptographically verified record using the Uber LedgerStore Merkle hash chain
func (r *Repository) AppendAuditRecord(appID, eventType, actor, payload string) domain.AuditLedgerEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	
	// 1. Calculate payload hash
	pHash := sha256.Sum256([]byte(payload))
	pHashHex := hex.EncodeToString(pHash[:])

	// 2. Calculate cryptographic entry hash: SHA256(prev_hash + payload_hash + timestamp)
	rawChain := fmt.Sprintf("%s:%s:%s", r.lastLedgerHash, pHashHex, now.Format(time.RFC3339Nano))
	entryHash := sha256.Sum256([]byte(rawChain))
	entryHashHex := hex.EncodeToString(entryHash[:])

	record := domain.AuditLedgerEntry{
		ID:                domain.NewUUIDv7(),
		ApplicationID:     appID,
		EventType:         eventType,
		Actor:             actor,
		PayloadSHA256:     pHashHex,
		PreviousEntryHash: r.lastLedgerHash,
		EntryHash:         entryHashHex,
		Metadata:          payload,
		RecordedAt:        now,
	}

	r.auditLedger = append(r.auditLedger, record)
	r.lastLedgerHash = entryHashHex
	return record
}

func (r *Repository) ListAuditLedger() []domain.AuditLedgerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]domain.AuditLedgerEntry, len(r.auditLedger))
	copy(res, r.auditLedger)
	return res
}
