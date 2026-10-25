package storage

import (
	"context"
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
		`DROP TABLE agent_work_attempts`,
		`DROP TABLE agent_work_items`,
		`DROP TABLE agent_work_groups`,
		`DELETE FROM schema_migrations WHERE version=49`,
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
	if version != 49 {
		t.Fatalf("schema version %d, want 49", version)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_work_groups','agent_work_items','agent_work_attempts')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 3 {
		t.Fatalf("managed tables = %d", tables)
	}
}
