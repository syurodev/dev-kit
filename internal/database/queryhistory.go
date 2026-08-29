package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"devkit/internal/crypto"
)

// HistoryKeyID is the payload identity for a sealed history statement. It is
// distinct from the saved-query key id on purpose: an envelope written here
// must not open as a saved query, and vice versa.
const HistoryKeyID = "db-query-body"

// HistoryRecordType is the AAD record type for history envelopes. It is NOT a
// vault item type — history deliberately lives outside vault_items, because
// everything in that table is sync-bound by construction and history must stay
// on the device that produced it.
const HistoryRecordType = "db-query-history"

// HistoryLimit is how many statements are kept per profile.
const HistoryLimit = 50

// HistoryRow is one stored statement. SQL is never stored or passed in
// plaintext — Envelope is the sealed statement.
type HistoryRow struct {
	ID        string
	ProfileID string
	Envelope  []byte
	CreatedAt time.Time
}

// QueryHistoryRepository persists per-profile statement history locally.
type QueryHistoryRepository interface {
	Insert(row HistoryRow) error
	ListByProfile(profileID string, limit int) ([]HistoryRow, error)
	PruneProfile(profileID string, keep int) error
	DeleteProfile(profileID string) error
}

// Sealer is the narrow view of the vault the history log needs.
type Sealer interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}

func historyAD(id string) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: HistoryRecordType, RecordID: id, KeyID: HistoryKeyID}
}

// ---- SQL ------------------------------------------------------------------

type SQLQueryHistory struct {
	db *sql.DB
}

func NewSQLQueryHistory(db *sql.DB) *SQLQueryHistory { return &SQLQueryHistory{db: db} }

func (r *SQLQueryHistory) Insert(row HistoryRow) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO db_query_history (id, profile_id, sql_envelope, created_at) VALUES (?, ?, ?, ?)`,
		row.ID, row.ProfileID, row.Envelope, row.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (r *SQLQueryHistory) ListByProfile(profileID string, limit int) ([]HistoryRow, error) {
	rows, err := r.db.QueryContext(context.Background(),
		`SELECT id, profile_id, sql_envelope, created_at FROM db_query_history
		 WHERE profile_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryRow
	for rows.Next() {
		var row HistoryRow
		var created string
		if err := rows.Scan(&row.ID, &row.ProfileID, &row.Envelope, &created); err != nil {
			return nil, err
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *SQLQueryHistory) PruneProfile(profileID string, keep int) error {
	_, err := r.db.ExecContext(context.Background(),
		`DELETE FROM db_query_history
		 WHERE profile_id = ? AND id NOT IN (
		   SELECT id FROM db_query_history WHERE profile_id = ?
		   ORDER BY created_at DESC, id DESC LIMIT ?
		 )`, profileID, profileID, keep)
	return err
}

func (r *SQLQueryHistory) DeleteProfile(profileID string) error {
	_, err := r.db.ExecContext(context.Background(),
		`DELETE FROM db_query_history WHERE profile_id = ?`, profileID)
	return err
}

// ---- Memory ---------------------------------------------------------------

type MemoryQueryHistory struct {
	mu   sync.Mutex
	rows []HistoryRow
}

func NewMemoryQueryHistory() *MemoryQueryHistory { return &MemoryQueryHistory{} }

func (r *MemoryQueryHistory) Insert(row HistoryRow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, row)
	return nil
}

func (r *MemoryQueryHistory) forProfile(profileID string) []HistoryRow {
	var out []HistoryRow
	for _, row := range r.rows {
		if row.ProfileID == profileID {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (r *MemoryQueryHistory) ListByProfile(profileID string, limit int) ([]HistoryRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.forProfile(profileID)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryQueryHistory) PruneProfile(profileID string, keep int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ordered := r.forProfile(profileID)
	if len(ordered) <= keep {
		return nil
	}
	survivors := make(map[string]bool, keep)
	for _, row := range ordered[:keep] {
		survivors[row.ID] = true
	}
	kept := r.rows[:0]
	for _, row := range r.rows {
		if row.ProfileID != profileID || survivors[row.ID] {
			kept = append(kept, row)
		}
	}
	r.rows = kept
	return nil
}

func (r *MemoryQueryHistory) DeleteProfile(profileID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.rows[:0]
	for _, row := range r.rows {
		if row.ProfileID != profileID {
			kept = append(kept, row)
		}
	}
	r.rows = kept
	return nil
}

func decodeEnvelope(raw []byte) (crypto.Envelope, error) {
	var envelope crypto.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return crypto.Envelope{}, err
	}
	return envelope, nil
}

// associatedDataFor builds an AAD for an arbitrary record type. Tests use it to
// prove a history envelope cannot be opened as something else.
func associatedDataFor(recordType, id, keyID string) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: recordType, RecordID: id, KeyID: keyID}
}
