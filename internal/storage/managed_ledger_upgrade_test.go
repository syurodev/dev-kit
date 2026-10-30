package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestManagedLedgerUpgradeFrom48PreservesExistingData(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Build the exact previous schema by removing the new, still-empty tables.
	// Keep a pre-existing key/value row to detect destructive upgrade behavior.
	for _, statement := range []string{
		`DROP TABLE agent_work_artifacts`,
		`DROP TABLE agent_work_packages`,
		`DROP TABLE agent_work_leases`,
		`DROP TABLE agent_work_attempts`,
		`DROP TABLE agent_work_items`,
		`DROP TABLE agent_work_groups`,
		`DELETE FROM schema_migrations WHERE version>=49`,
		`INSERT INTO ui_state (key,value) VALUES ('upgrade-sentinel','{"retained":true}')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migration retry: %v", err)
	}
	var retained string
	if err := db.QueryRowContext(ctx, `SELECT value FROM ui_state WHERE key='upgrade-sentinel'`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != `{"retained":true}` {
		t.Fatal("upgrade changed existing data")
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 57 {
		t.Fatalf("schema version %d, want 57", version)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_work_groups','agent_work_items','agent_work_attempts')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 3 {
		t.Fatalf("managed tables = %d", tables)
	}
}

func TestManagedLedgerMigration50QuarantinesLegacyRowsWithoutLosingAttempt(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "legacy49.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`,
		`CREATE TABLE agent_accounts(id TEXT PRIMARY KEY,home_dir TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE agent_turns(id TEXT PRIMARY KEY,session_id TEXT)`,
		`CREATE TABLE agent_transcript(id TEXT PRIMARY KEY,session_id TEXT,kind TEXT,created_at TEXT)`,
		`INSERT INTO schema_migrations VALUES(49,'agent managed work ledger','2026-10-25T00:00:00Z')`,
		`CREATE TABLE agent_work_groups(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,board_id TEXT NOT NULL,parent_task_id TEXT NOT NULL,coordinator_id TEXT NOT NULL,enabled INTEGER NOT NULL,paused INTEGER NOT NULL,policy_revision INTEGER NOT NULL,revision INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE agent_work_items(id TEXT PRIMARY KEY,group_id TEXT NOT NULL,workspace_id TEXT NOT NULL,board_id TEXT NOT NULL,parent_task_id TEXT NOT NULL,task_id TEXT NOT NULL,task_revision INTEGER NOT NULL,requirements_revision TEXT NOT NULL,role TEXT NOT NULL,state TEXT NOT NULL,current_attempt_id TEXT,revision INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE agent_work_attempts(id TEXT PRIMARY KEY,work_item_id TEXT NOT NULL,group_id TEXT NOT NULL,workspace_id TEXT NOT NULL,board_id TEXT NOT NULL,parent_task_id TEXT NOT NULL,session_id TEXT NOT NULL,role TEXT NOT NULL,task_revision INTEGER NOT NULL,requirements_revision TEXT NOT NULL,generation INTEGER NOT NULL,revision INTEGER NOT NULL,state TEXT NOT NULL,dispatch_state TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`INSERT INTO agent_work_groups VALUES('legacy-g','ws','board','parent','session-real',1,0,4,7,'2026-10-24T00:00:00Z','2026-10-24T01:00:00Z')`,
		`INSERT INTO agent_work_items VALUES('legacy-i','legacy-g','ws','board','parent','task',9,'sha256:v2:legacy','implementer','dispatching','legacy-a',3,'2026-10-24T00:00:00Z','2026-10-24T01:00:00Z')`,
		`INSERT INTO agent_work_attempts VALUES('legacy-a','legacy-i','legacy-g','ws','board','parent','session-real','implementer',9,'sha256:v2:legacy',1,2,'running','running','2026-10-24T00:30:00Z','2026-10-24T01:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	l := NewManagedLedger(db)
	g, err := l.GetWorkGroup(ctx, "legacy-g")
	if err != nil {
		t.Fatal(err)
	}
	if g.Enabled || !g.Paused || g.SchemaVersion != 1 || g.PolicyRevision != 4 || g.Revision != 7 {
		t.Fatalf("legacy group was not safely quarantined/preserved: %+v", g)
	}
	i, err := l.GetWorkItem(ctx, "legacy-i")
	if err != nil {
		t.Fatal(err)
	}
	if i.ID != "legacy-i" || i.TaskID != "task" || i.TaskRevision != 9 || i.RequirementsRevision != "sha256:v2:legacy" || i.CurrentAttemptID != "legacy-a" || i.SchemaVersion != 1 {
		t.Fatalf("legacy item changed: %+v", i)
	}
	a, err := l.ListAttempts(ctx, "legacy-i", "", 10)
	if err != nil || len(a.Items) != 1 || a.Items[0].ID != "legacy-a" || a.Items[0].State != "running" || a.Items[0].SchemaVersion != 1 {
		t.Fatalf("legacy attempt=%+v err=%v", a, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_work_groups SET schema_version=99 WHERE id='legacy-g'`); err != nil {
		t.Fatal(err)
	}
	if _, err = l.GetWorkGroup(ctx, "legacy-g"); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("future group schema read=%v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_work_groups SET schema_version=1 WHERE id='legacy-g'; UPDATE agent_work_items SET schema_version=99 WHERE id='legacy-i'`); err != nil {
		t.Fatal(err)
	}
	if _, err = l.GetWorkItem(ctx, "legacy-i"); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("future item schema read=%v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_work_items SET schema_version=1 WHERE id='legacy-i'; UPDATE agent_work_attempts SET schema_version=99 WHERE id='legacy-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err = l.ListAttempts(ctx, "legacy-i", "", 10); !errors.Is(err, ErrManagedInvalid) {
		t.Fatalf("future attempt schema read=%v", err)
	}
}

func TestManagedOwnershipUpgradeFrom51PreservesUsageAttribution(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "upgrade51.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE agent_work_artifacts`, `DROP TABLE agent_work_packages`, `DROP TABLE agent_work_leases`,
		`DELETE FROM schema_migrations WHERE version>=52`,
		`INSERT INTO agent_tool_usage_runs(id,started_at,batch_seq,dropped) VALUES('existing-run','2026-10-26T00:00:00Z',7,3)`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var batch, dropped, version int
	if err = db.QueryRowContext(ctx, `SELECT batch_seq,dropped FROM agent_tool_usage_runs WHERE id='existing-run'`).Scan(&batch, &dropped); err != nil {
		t.Fatal(err)
	}
	if batch != 7 || dropped != 3 {
		t.Fatalf("usage attribution changed: %d %d", batch, dropped)
	}
	if err = db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 57 {
		t.Fatalf("schema=%d, err=%v", version, err)
	}
}
