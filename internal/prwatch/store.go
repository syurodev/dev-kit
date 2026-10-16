package prwatch

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Attachment ties a change request to a session by hand: pasted as a URL
// (a fork, a request opened elsewhere) or opened by the pr.create tool, which
// knows the number before the branch lookup could.
type Attachment struct {
	SessionID string    `json:"sessionId"`
	Folder    string    `json:"folder"`
	Host      string    `json:"host"`
	Path      string    `json:"path"`
	Number    int       `json:"number"`
	URL       string    `json:"url"`
	Source    string    `json:"source"` // "url" or "created"
	CreatedAt time.Time `json:"createdAt"`
}

// Store keeps attachments.
type Store interface {
	List(ctx context.Context, sessionID string) ([]Attachment, error)
	Put(ctx context.Context, a Attachment) error
	Delete(ctx context.Context, sessionID, host, path string, number int) error
}

// MemoryStore is the Store with no database behind it.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[string]Attachment
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{rows: map[string]Attachment{}} }

func key(sessionID, host, path string, number int) string {
	return sessionID + "|" + host + "|" + path + "|" + strconv.Itoa(number)
}

func (m *MemoryStore) List(_ context.Context, sessionID string) ([]Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Attachment
	for _, a := range m.rows {
		if a.SessionID == sessionID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryStore) Put(_ context.Context, a Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key(a.SessionID, a.Host, a.Path, a.Number)] = a
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, sessionID, host, path string, number int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, key(sessionID, host, path, number))
	return nil
}

// SQLStore keeps attachments in a local table (not synced: a session lives on
// one machine).
type SQLStore struct {
	db    *sql.DB
	table string
}

func NewSQLStore(db *sql.DB, table string) *SQLStore { return &SQLStore{db: db, table: table} }

func (s *SQLStore) List(ctx context.Context, sessionID string) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT folder, host, path, number, url, source, created_at FROM "+s.table+" WHERE session_id = ? ORDER BY created_at, number", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a := Attachment{SessionID: sessionID}
		var created string
		if err := rows.Scan(&a.Folder, &a.Host, &a.Path, &a.Number, &a.URL, &a.Source, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLStore) Put(ctx context.Context, a Attachment) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO `+s.table+` (session_id, host, path, number, folder, url, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id, host, path, number) DO UPDATE SET folder = excluded.folder, url = excluded.url`,
		a.SessionID, a.Host, a.Path, a.Number, a.Folder, a.URL, a.Source, a.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLStore) Delete(ctx context.Context, sessionID, host, path string, number int) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM "+s.table+" WHERE session_id = ? AND host = ? AND path = ? AND number = ?", sessionID, host, path, number)
	return err
}
