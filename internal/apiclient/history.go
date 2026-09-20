package apiclient

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"devkit/internal/crypto"
)

// HistoryKeyID and HistoryRecordType are the payload identity of a sealed
// history URL. Distinct from every other key id here, so an envelope written
// as history cannot open as a request — and HistoryRecordType is NOT a vault
// item type, because history stays on the device that produced it.
const (
	HistoryKeyID      = "api-history-url"
	HistoryRecordType = "api-request-history"
)

// HistoryLimit is how many sends are kept per request, matching the figure
// db_query_history already settled on.
const HistoryLimit = 50

// Sealer is the vault's sealing surface, declared locally like everything else
// this package needs from outside.
type Sealer interface {
	SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, ad crypto.AssociatedData) ([]byte, error)
}

// HistoryEntry is one send. There is no body and no headers on purpose: either
// side can carry a token, a body can be megabytes, and re-sending is one
// click — so what would be bought by keeping them is small and what is risked
// is not.
type HistoryEntry struct {
	ID            string `json:"id"`
	RequestID     string `json:"requestId"`
	EnvironmentID string `json:"environmentId"`
	Method        string `json:"method"`
	// URL is the interpolated URL, sealed at rest and opened for this list.
	// Empty when the vault cannot open it, which is a locked vault rather
	// than a broken row.
	URL        string `json:"url"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"durationMs"`
	Bytes      int    `json:"bytes"`
	// Failure is why nothing came back: a DNS failure, a refused confirmation,
	// a timeout. Empty when there is a status.
	Failure string    `json:"failure"`
	SentAt  time.Time `json:"sentAt"`
}

type historyStore struct {
	db     *sql.DB
	table  string
	sealer Sealer
}

// SetHistory wires the local table sends are recorded in.
//
// Without it Send still works and records nothing — history is a convenience,
// and a module that refused to send because it could not write a log would be
// trading the thing for the record of the thing.
func (s *Service) SetHistory(db *sql.DB, table string, sealer Sealer) {
	if db != nil && table != "" && sealer != nil {
		s.history = &historyStore{db: db, table: table, sealer: sealer}
	}
}

func historyID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func (h *historyStore) insert(entry HistoryEntry) error {
	id := historyID()
	ad := crypto.AssociatedData{RecordType: HistoryRecordType, RecordID: id, KeyID: HistoryKeyID}
	envelope, err := h.sealer.SealV2(ad, []byte(entry.URL))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := h.db.ExecContext(context.Background(),
		`INSERT INTO `+h.table+` (id, request_id, environment_id, method, url_envelope,
			status, duration_ms, bytes, failure, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, entry.RequestID, entry.EnvironmentID, entry.Method, string(raw),
		entry.Status, entry.DurationMS, entry.Bytes, entry.Failure,
		entry.SentAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	// Trim here rather than on a schedule: the row that pushes past the limit
	// is the only moment the limit can be exceeded.
	_, err = h.db.ExecContext(context.Background(),
		`DELETE FROM `+h.table+` WHERE request_id = ? AND id NOT IN (
			SELECT id FROM `+h.table+` WHERE request_id = ? ORDER BY sent_at DESC LIMIT ?
		)`, entry.RequestID, entry.RequestID, HistoryLimit)
	return err
}

func (h *historyStore) list(requestID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > HistoryLimit {
		limit = HistoryLimit
	}
	rows, err := h.db.QueryContext(context.Background(),
		`SELECT id, request_id, environment_id, method, url_envelope, status,
			duration_ms, bytes, failure, sent_at
		 FROM `+h.table+` WHERE request_id = ? ORDER BY sent_at DESC LIMIT ?`, requestID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var entry HistoryEntry
		var raw, sentAt string
		if err := rows.Scan(&entry.ID, &entry.RequestID, &entry.EnvironmentID, &entry.Method,
			&raw, &entry.Status, &entry.DurationMS, &entry.Bytes, &entry.Failure, &sentAt); err != nil {
			return nil, err
		}
		entry.SentAt, _ = time.Parse(time.RFC3339Nano, sentAt)
		var envelope crypto.Envelope
		if json.Unmarshal([]byte(raw), &envelope) == nil {
			ad := crypto.AssociatedData{RecordType: HistoryRecordType, RecordID: entry.ID, KeyID: HistoryKeyID}
			// A URL that will not open is a locked vault, not a broken row:
			// the rest of the line is still worth showing.
			if plaintext, openErr := h.sealer.OpenWithAD(envelope, ad); openErr == nil {
				entry.URL = string(plaintext)
			}
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// History lists what was sent for one request, newest first.
func (s *Service) History(requestID string, limit int) ([]HistoryEntry, error) {
	if s.history == nil || requestID == "" {
		return nil, nil
	}
	return s.history.list(requestID, limit)
}

// ClearHistory forgets one request's sends, or all of them when id is empty.
func (s *Service) ClearHistory(requestID string) error {
	if s.history == nil {
		return nil
	}
	if requestID == "" {
		_, err := s.history.db.ExecContext(context.Background(), `DELETE FROM `+s.history.table)
		return err
	}
	_, err := s.history.db.ExecContext(context.Background(),
		`DELETE FROM `+s.history.table+` WHERE request_id = ?`, requestID)
	return err
}

// record writes one send. A failure to record is dropped: the send already
// happened, and reporting the log's problem as the request's would be a lie
// about what went out.
func (s *Service) record(entry HistoryEntry) {
	if s.history == nil || entry.RequestID == "" {
		return
	}
	_ = s.history.insert(entry)
}
