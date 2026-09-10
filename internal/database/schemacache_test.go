package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/storage"
)

func newSchemaCacheDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestDDLHeuristicTriggersOnlyOnSchemaChanges(t *testing.T) {
	ddl := []string{
		"CREATE TABLE t (id int)",
		"  alter table orders add column x int",
		"DROP INDEX ix",
		"\n-- add the column\nALTER TABLE orders ADD COLUMN y int",
		"TRUNCATE TABLE audit_log",
	}
	for _, sql := range ddl {
		if !looksLikeDDL(sql) {
			t.Fatalf("missed DDL: %q", sql)
		}
	}
	notDDL := []string{
		"SELECT 1",
		"UPDATE orders SET status = 'x'",
		"SELECT 'CREATE TABLE' AS label",
		"INSERT INTO t VALUES (1)",
		"",
	}
	for _, sql := range notDDL {
		if looksLikeDDL(sql) {
			t.Fatalf("re-index would fire for: %q", sql)
		}
	}
}

func TestSchemaCacheRoundTripsTheEditorShape(t *testing.T) {
	repo := NewMemorySchemaCache()
	// Three levels — schema, table, columns — because that is what
	// @codemirror/lang-sql already understands; nothing is transformed on the
	// way to the editor.
	snapshot := SchemaSnapshot{Namespace: SchemaNamespace{
		"public": {"orders": {"id", "total_cents"}, "customers": {"id", "email"}},
	}}
	if err := repo.Put("profile-1", snapshot); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, found, err := repo.Get("profile-1")
	if err != nil || !found {
		t.Fatalf("Get: err=%v found=%v", err, found)
	}
	if len(got.Namespace["public"]["orders"]) != 2 {
		t.Fatalf("namespace lost its shape: %#v", got.Namespace)
	}
	if err := repo.Delete("profile-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := repo.Get("profile-1"); found {
		t.Fatal("cache survived deletion")
	}
}

func TestSchemaColumnsProjectTheEditorShape(t *testing.T) {
	columns := SchemaColumns{
		"public": {
			"orders": {
				{Name: "id", DataType: "bigint", Nullable: false, Position: 1},
				{Name: "total_cents", DataType: "integer", Nullable: true, Position: 2},
			},
		},
	}
	namespace := columns.Namespace()
	got := namespace["public"]["orders"]
	if len(got) != 2 || got[0] != "id" || got[1] != "total_cents" {
		t.Fatalf("names lost their order or content: %#v", got)
	}
}

// A row written before the cache carried types holds bare strings where a
// column object belongs. That has to read as a miss, not an error: re-indexing
// is the recovery, and an error would take the whole rail down instead.
func TestSchemaCacheReportsAPreTypesRowAsAMiss(t *testing.T) {
	db := newSchemaCacheDB(t)
	repo := NewSQLSchemaCache(db)

	legacy := `{"public":{"orders":["id","total_cents"]}}`
	if _, err := db.Exec(
		`INSERT INTO db_schema_cache (profile_id, schema_json, indexed_at) VALUES (?, ?, ?)`,
		"profile-1", legacy, "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	snapshot, found, err := repo.Get("profile-1")
	if err != nil {
		t.Fatalf("a pre-types row must not error: %v", err)
	}
	if found {
		t.Fatalf("a pre-types row must read as a miss, got %#v", snapshot)
	}
}

func TestSQLSchemaCacheRoundTripsColumnTypes(t *testing.T) {
	repo := NewSQLSchemaCache(newSchemaCacheDB(t))
	columns := SchemaColumns{
		"public": {"orders": {{Name: "id", DataType: "bigint", Nullable: false, Position: 1}}},
	}
	if err := repo.Put("profile-1", SchemaSnapshot{
		Namespace: columns.Namespace(),
		Columns:   columns,
		IndexedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, found, err := repo.Get("profile-1")
	if err != nil || !found {
		t.Fatalf("Get: err=%v found=%v", err, found)
	}
	if got.Columns["public"]["orders"][0].DataType != "bigint" {
		t.Fatalf("type did not survive the cache: %#v", got.Columns)
	}
	// Derived on read, so the editor's shape needs no separate storage.
	if got.Namespace["public"]["orders"][0] != "id" {
		t.Fatalf("namespace was not projected: %#v", got.Namespace)
	}
}
