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

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}
