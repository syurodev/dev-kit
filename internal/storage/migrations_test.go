package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

type testMigrationStore struct {
	version int
}

func newTestMigrationStore(version int) *testMigrationStore {
	return &testMigrationStore{version: version}
}

func (s *testMigrationStore) CurrentVersion(ctx context.Context) (int, error) {
	return s.version, nil
}

func (s *testMigrationStore) ApplyMigration(ctx context.Context, migration Migration) error {
	if migration.Up == nil {
		return errors.New("migration Up is nil")
	}
	if err := migration.Up(ctx); err != nil {
		return err
	}
	s.version = migration.Version
	return nil
}

func TestMigrationRunnerRunsPendingMigrationsInOrder(t *testing.T) {
	ctx := context.Background()
	var applied []int

	store := newTestMigrationStore(1)
	runner, err := NewMigrationRunner(store, []Migration{
		{
			Version: 1,
			Name:    "already applied",
			Up: func(ctx context.Context) error {
				applied = append(applied, 1)
				return nil
			},
		},
		{
			Version: 2,
			Name:    "second",
			Up: func(ctx context.Context) error {
				applied = append(applied, 2)
				return nil
			},
		},
		{
			Version: 3,
			Name:    "third",
			Up: func(ctx context.Context) error {
				applied = append(applied, 3)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewMigrationRunner returned error: %v", err)
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(applied) != 2 || applied[0] != 2 || applied[1] != 3 {
		t.Fatalf("applied = %v, want [2 3]", applied)
	}

	version, err := store.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion returned error: %v", err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}
}

func TestNewMigrationRunnerRejectsDuplicateVersions(t *testing.T) {
	store := newTestMigrationStore(0)

	_, err := NewMigrationRunner(store, []Migration{
		{Version: 2, Name: "second"},
		{Version: 2, Name: "second duplicate"},
	})
	if err == nil {
		t.Fatal("NewMigrationRunner error = nil, want duplicate version error")
	}
}

func TestNewMigrationRunnerRejectsNilStore(t *testing.T) {
	_, err := NewMigrationRunner(nil, nil)
	if err == nil {
		t.Fatal("NewMigrationRunner error = nil, want nil store error")
	}
}

func TestNewMigrationRunnerRejectsMigrationWithNilUp(t *testing.T) {
	store := newTestMigrationStore(0)

	_, err := NewMigrationRunner(store, []Migration{
		{Version: 1, Name: "missing up"},
	})
	if err == nil {
		t.Fatal("NewMigrationRunner error = nil, want nil Up error")
	}
}

func TestTestMigrationStoreApplyMigrationDoesNotAdvanceVersionOnFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestMigrationStore(4)
	wantErr := errors.New("boom")

	err := store.ApplyMigration(ctx, Migration{
		Version: 5,
		Name:    "fails",
		Up: func(ctx context.Context) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyMigration error = %v, want %v", err, wantErr)
	}

	version, err := store.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion returned error: %v", err)
	}
	if version != 4 {
		t.Fatalf("version = %d, want 4", version)
	}
}

func TestMigrateCreatesVaultItemsTable(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tableExists := func(name string) bool {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", name,
		).Scan(&got)
		return err == nil && got == name
	}

	if !tableExists(TableVaultItems) {
		t.Fatalf("expected %s table to exist", TableVaultItems)
	}
	for _, table := range []string{
		TableSyncOutbox, TableSyncCursors, TableSyncIdentity,
		TableSyncAccountBinding, TableSyncEntityVersions, TableSyncAppliedOps,
		TableSyncLocalEntities, TableSyncLocalMutations, TableSyncConflicts,
		TableAppPreferences, TableUIState, TableDBQueryHistory, TableDBSchemaCache,
		TableAuditEvents, TableAgentCheckpoints, TableAccountSecrets,
		TableTaskVerifyRuns, TableAccountHostChoices, TableAPIRequestHistory, TableCodexHookEvents,
		TableTaskBoardBindings,
	} {
		if !tableExists(table) {
			t.Fatalf("expected %s table to exist", table)
		}
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 34 {
		t.Fatalf("migration version = %d, want 34", version)
	}

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateCreatesAgentHubTables(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{TableAgentSessions, TableAgentTurns} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}

	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil || v < 13 {
		t.Fatalf("version=%d err=%v", v, err)
	}

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateCreatesSyncSchemaWithoutVaultItemBackfill(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	columns := func(table string) map[string]struct {
		typeName string
		notNull  int
	} {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatalf("table info %s: %v", table, err)
		}
		defer rows.Close()
		got := make(map[string]struct {
			typeName string
			notNull  int
		})
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, typeName string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey); err != nil {
				t.Fatalf("scan table info %s: %v", table, err)
			}
			got[name] = struct {
				typeName string
				notNull  int
			}{typeName: typeName, notNull: notNull}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read table info %s: %v", table, err)
		}
		return got
	}

	outboxColumns := columns(TableSyncOutbox)
	for _, name := range []string{
		"id", "record_id", "record_type", "operation", "payload", "key_version", "status",
		"attempts", "next_attempt_at", "last_error", "idempotency_key", "conflict_json", "created_at",
		"syncing_since", "account_id", "device_id", "protocol_version", "operation_id",
		"entity_version", "key_id",
	} {
		if _, ok := outboxColumns[name]; !ok {
			t.Fatalf("sync_outbox missing column %q", name)
		}
	}
	if _, exists := outboxColumns["content_digest"]; exists {
		t.Fatal("sync_outbox must not persist a plaintext-derived content digest")
	}
	if outboxColumns["payload"].typeName != "BLOB" {
		t.Fatalf("payload type = %q, want BLOB", outboxColumns["payload"].typeName)
	}
	if outboxColumns["conflict_json"].notNull != 0 {
		t.Fatal("conflict_json must be nullable")
	}

	cursorColumns := columns(TableSyncCursors)
	for _, name := range []string{"name", "cursor", "updated_at"} {
		if _, ok := cursorColumns[name]; !ok {
			t.Fatalf("sync_cursors missing column %q", name)
		}
	}

	bindingColumns := columns(TableSyncAccountBinding)
	for _, name := range []string{"id", "account_id", "bound_at"} {
		if _, ok := bindingColumns[name]; !ok {
			t.Fatalf("sync_account_binding missing column %q", name)
		}
	}

	versionColumns := columns(TableSyncEntityVersions)
	for _, name := range []string{
		"account_id", "record_id", "record_type", "entity_version",
		"operation_id", "operation", "tombstone", "updated_at",
	} {
		if _, ok := versionColumns[name]; !ok {
			t.Fatalf("sync_entity_versions missing column %q", name)
		}
	}

	appliedOperationColumns := columns(TableSyncAppliedOps)
	for _, name := range []string{
		"account_id", "operation_id", "record_id", "record_type",
		"entity_version", "operation", "applied_at",
	} {
		if _, ok := appliedOperationColumns[name]; !ok {
			t.Fatalf("sync_applied_operations missing column %q", name)
		}
	}

	localEntityColumns := columns(TableSyncLocalEntities)
	for _, name := range []string{
		"record_id", "record_type", "entity_version", "operation_id",
		"operation", "tombstone", "updated_at",
	} {
		if _, ok := localEntityColumns[name]; !ok {
			t.Fatalf("sync_local_entity_versions missing column %q", name)
		}
	}

	localMutationColumns := columns(TableSyncLocalMutations)
	for _, name := range []string{
		"sequence", "operation_id", "record_id", "record_type", "entity_version",
		"operation", "snapshot", "key_version", "key_id", "created_at",
	} {
		if _, ok := localMutationColumns[name]; !ok {
			t.Fatalf("sync_local_mutations missing column %q", name)
		}
	}

	var vaultItemCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items").Scan(&vaultItemCount); err != nil {
		t.Fatalf("count vault_items: %v", err)
	}
	if vaultItemCount != 0 {
		t.Fatalf("vault_items row count = %d, want 0", vaultItemCount)
	}

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigration9DoesNotBackfillVaultItems(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{TableSyncAccountBinding, TableSyncEntityVersions, TableSyncAppliedOps} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s row count = %d, want 0", table, count)
		}
	}
}

func TestMigration8AddColumnIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Simulate interrupted m8 after columns were applied but before version insert.
	if _, err := db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = 8"); err != nil {
		t.Fatalf("reset migration version: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("resume m8 migrate: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("third migrate: %v", err)
	}
}

func TestMigrateDropsLegacyItemTables(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tableExists := func(name string) bool {
		var got string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", name,
		).Scan(&got)
		return err == nil && got == name
	}
	for _, legacy := range []string{TableNotes, TableConnectionProfiles, TableSSHProfiles} {
		if tableExists(legacy) {
			t.Fatalf("expected legacy table %q to be dropped", legacy)
		}
	}

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrateCreatesUIStateTable(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := db.Exec("SELECT key, value FROM ui_state LIMIT 0"); err != nil {
		t.Fatalf("expected ui_state table to exist with key, value columns: %v", err)
	}
}

func TestMigration22KeepsTurnsWrittenBeforeIt(t *testing.T) {
	// The column is added to a table that already holds turns. Rolling the
	// version back to 21 and migrating again replays m22 on a database that has
	// half of it — the column but not the table — which is what an interrupted
	// upgrade leaves.
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_sessions (id, agent_type, project_path, status, depth, created_at, updated_at) VALUES ('s1', 'mock', '/p', 'idle', 0, 'x', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_turns (id, session_id, created_at) VALUES ('old-turn', 's1', 'x')`); err != nil {
		t.Fatal(err)
	}
	// The runner resumes from MAX(version), so replaying 22 means clearing 22
	// and everything after it — otherwise the newer row keeps 22 skipped.
	for _, stmt := range []string{`DROP TABLE agent_checkpoints`, `DELETE FROM schema_migrations WHERE version >= 22`} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("replaying m22: %v", err)
	}
	var promptID string
	if err := db.QueryRowContext(ctx, `SELECT prompt_message_id FROM agent_turns WHERE id = 'old-turn'`).Scan(&promptID); err != nil || promptID != "" {
		t.Fatalf("old turn prompt_message_id = %q, %v; want empty", promptID, err)
	}
	if _, err := db.ExecContext(ctx, `SELECT id FROM agent_checkpoints LIMIT 1`); err != nil {
		t.Fatalf("checkpoints table after replay: %v", err)
	}
}

// m30 rebuilds workspace_services from m28's shape: a duplicate path keeps
// its first row, an orphan is dropped, and afterwards the table refuses a
// second copy of a path and follows its workspace on delete.
func TestMigrationRebuildsWorkspaceServicesWithKeys(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		// Back to m28's table, holding what m28 let through.
		`DROP TABLE workspace_services`,
		`CREATE TABLE workspace_services (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL, path TEXT NOT NULL, name_source TEXT NOT NULL)`,
		`INSERT INTO workspaces (id, name, root) VALUES ('w1', 'acme', '/acme')`,
		`INSERT INTO workspace_services VALUES ('s1', 'w1', 'billing', '/acme/billing', 'folder')`,
		`INSERT INTO workspace_services VALUES ('s2', 'w1', 'billing-dup', '/acme/billing', 'folder')`,
		`INSERT INTO workspace_services VALUES ('s3', 'gone', 'orphan', '/elsewhere', 'folder')`,
		`DELETE FROM schema_migrations WHERE version >= 30`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("replaying m30: %v", err)
	}

	var ids []string
	rows, err := db.QueryContext(ctx, `SELECT id FROM workspace_services ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("services after rebuild = %v, want [s1]", ids)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_services VALUES ('s4', 'w1', 'again', '/acme/billing', 'folder')`); err == nil {
		t.Fatal("a second row for the same (workspace, path) was accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_services VALUES ('s5', 'nope', 'x', '/x', 'folder')`); err == nil {
		t.Fatal("a service for a workspace that does not exist was accepted")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = 'w1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_services`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("services after deleting their workspace = %d, %v; want 0 (cascade)", n, err)
	}
}

// TestTaskBoardBindingsNulledOnWorkspaceDelete verifies that deleting a
// workspace leaves its board bindings in place with workspace_id set to NULL
// (ON DELETE SET NULL), rather than removing the row: the row itself is the
// record that this machine has considered the board, and losing it on a
// workspace delete would make that board look never-touched to the next
// migration. The board itself (a vault_item) is unaffected either way.
func TestTaskBoardBindingsNulledOnWorkspaceDelete(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Create a workspace and insert a binding.
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces (id, name, root) VALUES ('ws1', 'test-ws', '/test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO task_board_bindings (board_id, workspace_id, bound_at) VALUES ('board1', 'ws1', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// Verify the binding exists.
	var boardID string
	if err := db.QueryRowContext(ctx, `SELECT board_id FROM task_board_bindings WHERE board_id = 'board1'`).Scan(&boardID); err != nil {
		t.Fatalf("binding not inserted: %v", err)
	}

	// Delete the workspace.
	if _, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = 'ws1'`); err != nil {
		t.Fatal(err)
	}

	// The row survives with a NULL workspace, not gone.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_board_bindings`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("bindings after deleting workspace = %d, %v; want 1 (row kept)", count, err)
	}
	var workspaceID sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT workspace_id FROM task_board_bindings WHERE board_id = 'board1'`).Scan(&workspaceID); err != nil {
		t.Fatalf("read binding after delete: %v", err)
	}
	if workspaceID.Valid {
		t.Fatalf("workspace_id after deleting workspace = %q, want NULL", workspaceID.String)
	}
}

func TestSessionWorkspaceMigrationKeepsAmbiguousRootsUnbound(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w1", "w2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,root) VALUES (?,?,?)`, id, id, "/shared"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO workspace_services(id,workspace_id,name,path,name_source) VALUES (?,?,?,?,?)`, id, id, id, "/shared", "folder"); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, path, parent string }{{"ambiguous", "/shared", ""}, {"unique", "/unique", ""}, {"child", "/unique/sub", "unique"}} {
		var parent any
		if row.parent != "" {
			parent = row.parent
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_sessions(id,agent_type,project_path,status,parent_session_id,depth,created_at,updated_at) VALUES (?,'mock',?,'idle',?,0,'2026-01-01','2026-01-01')`, row.id, row.path, parent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_services(id,workspace_id,name,path,name_source) VALUES ('unique-svc','w1','unique','/unique','folder')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version >= 33`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ id, workspace string }{{"ambiguous", ""}, {"unique", "w1"}, {"child", "w1"}} {
		var got sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT workspace_id FROM agent_sessions WHERE id = ?`, want.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.String != want.workspace {
			t.Errorf("%s workspace = %q, want %q", want.id, got.String, want.workspace)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = 'w1'`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unique", "child"} {
		var got sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT workspace_id FROM agent_sessions WHERE id = ?`, id).Scan(&got); err != nil || got.Valid {
			t.Fatalf("%s after workspace delete = %v, %v; want NULL", id, got, err)
		}
	}
}

// Existing workspaces acquire empty instructions without losing their folders;
// text written afterwards survives reopening and repeating migrations.
func TestWorkspaceInstructionsMigrationAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "devkit.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE workspaces DROP COLUMN instructions`,
		`DELETE FROM schema_migrations WHERE version >= 34`,
		`INSERT INTO workspaces(id,name,root) VALUES ('existing','Existing','/root')`,
		`INSERT INTO workspace_services(id,workspace_id,name,path,name_source) VALUES ('svc','existing','service','/root','folder')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var instructions, root string
	if err := db.QueryRowContext(ctx, `SELECT instructions, root FROM workspaces WHERE id='existing'`).Scan(&instructions, &root); err != nil {
		t.Fatal(err)
	}
	if instructions != "" || root != "/root" {
		t.Fatalf("migrated = %q, %q", instructions, root)
	}
	var folders int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_services WHERE workspace_id='existing'`).Scan(&folders); err != nil || folders != 1 {
		t.Fatalf("folders = %d, %v", folders, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE workspaces SET instructions=? WHERE id='existing'`, "Chỉ dùng DevKit.\nKeep this text."); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT instructions FROM workspaces WHERE id='existing'`).Scan(&instructions); err != nil {
		t.Fatal(err)
	}
	if instructions != "Chỉ dùng DevKit.\nKeep this text." {
		t.Fatalf("reopened instructions = %q", instructions)
	}
}
