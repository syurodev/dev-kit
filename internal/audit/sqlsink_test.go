package audit_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/audit"
	"devkit/internal/storage"
)

func openAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + storage.TableAuditEvents).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSQLSinkWritesEveryEvent(t *testing.T) {
	db := openAuditDB(t)
	sink := audit.NewSQLSink(db, storage.TableAuditEvents, nil)
	const n = 5000 // several times the queue size: backpressure path must not drop
	for i := 0; i < n; i++ {
		sink.Record(audit.Event{Caller: "agent:x", SubjectID: "builtin.test", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed,
			Metadata: map[string]any{"password": "secret", "i": i}})
	}
	sink.Close()
	if got := countRows(t, db); got != n {
		t.Fatalf("rows = %d, want %d (events must never be dropped)", got, n)
	}
	var meta string
	if err := db.QueryRow(`SELECT metadata FROM ` + storage.TableAuditEvents + ` LIMIT 1`).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if meta == "" || containsSecret(meta) {
		t.Fatalf("metadata must be redacted JSON, got %q", meta)
	}
}

func containsSecret(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if s[i:i+6] == "secret" {
			return true
		}
	}
	return false
}

func TestSQLSinkFallsBackWhenWriteFails(t *testing.T) {
	db := openAuditDB(t)
	mem := audit.NewMemorySink()
	sink := audit.NewSQLSink(db, "no_such_table", mem)
	sink.Record(audit.Event{Caller: "ui", SubjectID: "builtin.test", Capability: "c", Scope: "s", Outcome: audit.OutcomeDenied})
	sink.Close()
	if got := len(mem.Events()); got != 1 {
		t.Fatalf("fallback events = %d, want 1", got)
	}
}

func TestSQLSinkPruneRemovesOldRows(t *testing.T) {
	db := openAuditDB(t)
	sink := audit.NewSQLSink(db, storage.TableAuditEvents, nil)
	sink.Record(audit.Event{At: time.Now().UTC().Add(-40 * 24 * time.Hour), Caller: "ui", SubjectID: "old", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	sink.Record(audit.Event{Caller: "ui", SubjectID: "new", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	sink.Close()
	if err := sink.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db); got != 1 {
		t.Fatalf("rows after prune = %d, want 1", got)
	}
}

func TestTeeFansOut(t *testing.T) {
	a, b := audit.NewMemorySink(), audit.NewMemorySink()
	audit.NewTee(a, b).Record(audit.Event{Caller: "ui", SubjectID: "x", Capability: "c", Scope: "s", Outcome: audit.OutcomeAllowed})
	if len(a.Events()) != 1 || len(b.Events()) != 1 {
		t.Fatalf("tee did not reach both sinks: %d/%d", len(a.Events()), len(b.Events()))
	}
}
