package storage

import (
	"context"
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
	for _, table := range []string{TableSyncOutbox, TableSyncCursors} {
		if !tableExists(table) {
			t.Fatalf("expected %s table to exist", table)
		}
	}

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 7 {
		t.Fatalf("migration version = %d, want 7", version)
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
	} {
		if _, ok := outboxColumns[name]; !ok {
			t.Fatalf("sync_outbox missing column %q", name)
		}
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
