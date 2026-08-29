package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// SchemaNamespace is the shape @codemirror/lang-sql already understands:
// schema → table → column names. Three levels map 1:1 onto
// ListSchemas → ListTables → ListColumns, so nothing is transformed on the way
// out to the editor.
type SchemaNamespace map[string]map[string][]string

// SchemaSnapshot is a cached index plus when it was taken. The timestamp is
// shown to the user: a stale suggestion nobody knows is stale is worse than no
// suggestion.
type SchemaSnapshot struct {
	Namespace SchemaNamespace `json:"namespace"`
	IndexedAt time.Time       `json:"indexed_at"`
}

// SchemaCacheRepository stores one snapshot per profile. Table and column names
// are structure, not data, so this lives in plaintext outside the vault.
type SchemaCacheRepository interface {
	Get(profileID string) (SchemaSnapshot, bool, error)
	Put(profileID string, snapshot SchemaSnapshot) error
	Delete(profileID string) error
}

type SQLSchemaCache struct{ db *sql.DB }

func NewSQLSchemaCache(db *sql.DB) *SQLSchemaCache { return &SQLSchemaCache{db: db} }

func (r *SQLSchemaCache) Get(profileID string) (SchemaSnapshot, bool, error) {
	var raw, indexedAt string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT schema_json, indexed_at FROM db_schema_cache WHERE profile_id = ?", profileID).
		Scan(&raw, &indexedAt)
	if err == sql.ErrNoRows {
		return SchemaSnapshot{}, false, nil
	}
	if err != nil {
		return SchemaSnapshot{}, false, err
	}
	var namespace SchemaNamespace
	if err := json.Unmarshal([]byte(raw), &namespace); err != nil {
		return SchemaSnapshot{}, false, err
	}
	when, _ := time.Parse(time.RFC3339Nano, indexedAt)
	return SchemaSnapshot{Namespace: namespace, IndexedAt: when}, true, nil
}

func (r *SQLSchemaCache) Put(profileID string, snapshot SchemaSnapshot) error {
	raw, err := json.Marshal(snapshot.Namespace)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO db_schema_cache (profile_id, schema_json, indexed_at) VALUES (?, ?, ?)
		 ON CONFLICT(profile_id) DO UPDATE SET schema_json = excluded.schema_json, indexed_at = excluded.indexed_at`,
		profileID, string(raw), snapshot.IndexedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (r *SQLSchemaCache) Delete(profileID string) error {
	_, err := r.db.ExecContext(context.Background(),
		"DELETE FROM db_schema_cache WHERE profile_id = ?", profileID)
	return err
}

type MemorySchemaCache struct {
	mu        sync.Mutex
	snapshots map[string]SchemaSnapshot
}

func NewMemorySchemaCache() *MemorySchemaCache {
	return &MemorySchemaCache{snapshots: make(map[string]SchemaSnapshot)}
}

func (r *MemorySchemaCache) Get(profileID string) (SchemaSnapshot, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, found := r.snapshots[profileID]
	return snapshot, found, nil
}

func (r *MemorySchemaCache) Put(profileID string, snapshot SchemaSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots[profileID] = snapshot
	return nil
}

func (r *MemorySchemaCache) Delete(profileID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.snapshots, profileID)
	return nil
}

// looksLikeDDL is a deliberate heuristic, not a parser — the same best-effort
// level as the inferred-relationship guess in the ERD. It only decides whether
// to re-index in the background; a miss costs a stale suggestion, never
// correctness.
func looksLikeDDL(sql string) bool {
	trimmed := strings.TrimLeft(sql, " \t\r\n(")
	for len(trimmed) > 2 && strings.HasPrefix(trimmed, "--") {
		if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
			trimmed = strings.TrimLeft(trimmed[idx+1:], " \t\r\n(")
			continue
		}
		break
	}
	upper := strings.ToUpper(trimmed)
	for _, keyword := range []string{"CREATE ", "ALTER ", "DROP ", "TRUNCATE ", "COMMENT ON "} {
		if strings.HasPrefix(upper, keyword) {
			return true
		}
	}
	return false
}
