package database

import "testing"

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
