package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// Table names for the key/value entity stores. They are constants (never user
// input) so they are safe to interpolate into DDL/DML.
const (
	TableVaultKeyring       = "vault_keyring"
	TableNotes              = "notes"
	TableConnectionProfiles = "connection_profiles"
	TableSSHProfiles        = "ssh_profiles"
	TableVaultItems         = "vault_items"
	TableSyncOutbox         = "sync_outbox"
	TableSyncCursors        = "sync_cursors"
)

var chmodFile = os.Chmod

// Open opens (creating if needed) a SQLite database using the pure-Go driver
// (no cgo) and applies sensible pragmas.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// modernc's driver serializes writes; a single connection avoids
	// "database is locked" under concurrent bridge calls.
	db.SetMaxOpenConns(1)
	// Keep the local database to a single file so journal sidecars cannot
	// bypass the database file's restrictive 0600 permission.
	if _, err := db.Exec("PRAGMA journal_mode=DELETE; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	if err := chmodFile(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict SQLite database permissions: %w", err)
	}
	return db, nil
}

// KV is a minimal id -> JSON blob store over one table. Entity repositories wrap
// it and (un)marshal their own record types. Sensitive fields inside the blob
// are already encrypted envelopes.
// ponytail: record-as-JSON in a `data` column; promote fields to real
// columns/indexes when queries need them.
type KV struct {
	db    *sql.DB
	table string
}

func NewKV(db *sql.DB, table string) *KV {
	return &KV{db: db, table: table}
}

func (k *KV) List(ctx context.Context) ([][]byte, error) {
	rows, err := k.db.QueryContext(ctx, fmt.Sprintf("SELECT data FROM %s", k.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]byte
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, rows.Err()
}

// Get returns the blob for id, or (nil, false, nil) if it does not exist.
func (k *KV) Get(ctx context.Context, id string) ([]byte, bool, error) {
	var data []byte
	err := k.db.QueryRowContext(ctx, fmt.Sprintf("SELECT data FROM %s WHERE id = ?", k.table), id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (k *KV) Put(ctx context.Context, id string, data []byte) error {
	_, err := k.db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, data) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data",
		k.table,
	), id, data)
	return err
}

func (k *KV) Delete(ctx context.Context, id string) error {
	_, err := k.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", k.table), id)
	return err
}

// sqlMigrationStore implements MigrationStore backed by a schema_migrations table.
type sqlMigrationStore struct {
	db *sql.DB
}

func (s *sqlMigrationStore) CurrentVersion(ctx context.Context) (int, error) {
	if _, err := s.db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)",
	); err != nil {
		return 0, err
	}
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

func (s *sqlMigrationStore) ApplyMigration(ctx context.Context, migration Migration) error {
	// No wrapping transaction: with a single pooled connection, holding a tx
	// while migration.Up runs its own statements on the same pool deadlocks.
	// Migrations are idempotent (CREATE TABLE IF NOT EXISTS), so applying the
	// change then recording the version is safe if interrupted.
	if err := migration.Up(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, datetime('now'))",
		migration.Version, migration.Name,
	)
	return err
}

// Migrate creates the entity tables (idempotent, forward-only) via the runner.
func Migrate(ctx context.Context, db *sql.DB) error {
	kvTable := func(name string) Migration {
		return Migration{
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(ctx, fmt.Sprintf(
					"CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, data TEXT NOT NULL)", name,
				))
				return err
			},
		}
	}

	m1 := kvTable(TableVaultKeyring)
	m1.Version, m1.Name = 1, "create vault_keyring"
	m2 := kvTable(TableNotes)
	m2.Version, m2.Name = 2, "create notes"
	m3 := kvTable(TableConnectionProfiles)
	m3.Version, m3.Name = 3, "create connection_profiles"
	m4 := kvTable(TableSSHProfiles)
	m4.Version, m4.Name = 4, "create ssh_profiles"

	m5 := Migration{
		Version: 5,
		Name:    "create vault_items",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, type TEXT NOT NULL, data TEXT NOT NULL)",
				TableVaultItems,
			))
			return err
		},
	}

	m6 := Migration{
		Version: 6,
		Name:    "drop legacy item tables",
		Up: func(ctx context.Context) error {
			for _, table := range []string{TableNotes, TableConnectionProfiles, TableSSHProfiles} {
				if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
					return err
				}
			}
			return nil
		},
	}

	m7 := Migration{
		Version: 7,
		Name:    "create local sync persistence",
		Up: func(ctx context.Context) error {
			// This is local queue state only. The payload is opaque ciphertext;
			// no vault_items plaintext is copied or backfilled into this schema.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					operation TEXT NOT NULL,
					payload BLOB NOT NULL,
					key_version INTEGER NOT NULL,
					status TEXT NOT NULL,
					attempts INTEGER NOT NULL DEFAULT 0,
					next_attempt_at TEXT NOT NULL,
					last_error TEXT NOT NULL DEFAULT '',
					idempotency_key TEXT NOT NULL UNIQUE,
					conflict_json TEXT,
					created_at TEXT NOT NULL
				)`, TableSyncOutbox,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					name TEXT PRIMARY KEY,
					cursor TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`, TableSyncCursors,
			))
			return err
		},
	}

	m8 := Migration{
		Version: 8,
		Name:    "sync identity and outbox lease columns",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					account_id TEXT NOT NULL,
					device_id TEXT NOT NULL,
					created_at TEXT NOT NULL
				)`, TableSyncIdentity,
			)); err != nil {
				return err
			}
			for _, stmt := range []string{
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN syncing_since TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN account_id TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN device_id TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN protocol_version INTEGER NOT NULL DEFAULT 0", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN operation_id TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN entity_version INTEGER NOT NULL DEFAULT 0", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN key_id TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN content_digest TEXT NOT NULL DEFAULT ''", TableSyncOutbox),
			} {
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	}

	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6, m7, m8})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}
