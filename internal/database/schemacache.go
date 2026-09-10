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

// SchemaColumns is the same three levels carrying the whole column, and it is
// the one the index actually builds: ListColumns already returns the type and
// the nullability, and keeping only the names meant the sidebar had to ask for
// them again, one table at a time, on every expand.
type SchemaColumns map[string]map[string][]ColumnInfo

// Namespace projects the names back out for @codemirror/lang-sql, which wants
// nothing else. Derived rather than stored beside SchemaColumns so the two can
// never disagree about which columns a table has.
func (c SchemaColumns) Namespace() SchemaNamespace {
	namespace := make(SchemaNamespace, len(c))
	for schema, tables := range c {
		entry := make(map[string][]string, len(tables))
		for table, columns := range tables {
			names := make([]string, 0, len(columns))
			for _, column := range columns {
				names = append(names, column.Name)
			}
			entry[table] = names
		}
		namespace[schema] = entry
	}
	return namespace
}

// SchemaSnapshot is a cached index plus when it was taken. The timestamp is
// shown to the user: a stale suggestion nobody knows is stale is worse than no
// suggestion.
//
// Namespace is derived from Columns on the way out. It stays on the struct
// because the SQL editor's completion source consumes exactly that shape.
type SchemaSnapshot struct {
	Namespace SchemaNamespace `json:"namespace"`
	Columns   SchemaColumns   `json:"columns"`
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
	var columns SchemaColumns
	if err := json.Unmarshal([]byte(raw), &columns); err != nil {
		// A row written before the cache carried types holds bare name strings
		// where a column object is expected. Reported as a miss rather than an
		// error: re-indexing is exactly the recovery, and it is one the caller
		// already knows how to do.
		return SchemaSnapshot{}, false, nil
	}
	when, _ := time.Parse(time.RFC3339Nano, indexedAt)
	return SchemaSnapshot{
		Namespace: columns.Namespace(),
		Columns:   columns,
		IndexedAt: when,
	}, true, nil
}

func (r *SQLSchemaCache) Put(profileID string, snapshot SchemaSnapshot) error {
	raw, err := json.Marshal(snapshot.Columns)
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
