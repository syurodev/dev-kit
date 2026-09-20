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
	TableSyncAccountBinding = "sync_account_binding"
	TableSyncEntityVersions = "sync_entity_versions"
	TableSyncAppliedOps     = "sync_applied_operations"
	TableSyncIdentity       = "sync_identity"
	TableSyncLocalEntities  = "sync_local_entity_versions"
	TableSyncLocalMutations = "sync_local_mutations"
	TableSyncConflicts      = "sync_conflicts"
	TableEnvTags            = "env_tags"
	TableAgentSessions      = "agent_sessions"
	TableAgentTurns         = "agent_turns"
	// TableAgentTranscript holds what was said in a session. Like sessions and
	// turns it is device-local and outside vault_items: sync has no path here,
	// which is deliberate — a transcript is operational, project data lives in
	// Tasks instead.
	TableAgentTranscript = "agent_transcript"
	// TableAgentCheckpoints indexes the file snapshots taken around agent
	// turns. The snapshots themselves live in a shadow git repository under the
	// data directory; a row is what makes one reachable, and deleting the row
	// is what lets maintenance collect it. Device-local like the transcript.
	TableAgentCheckpoints = "agent_checkpoints"

	// TableAccountSecrets holds scoped account credentials: the ones belonging
	// to exactly one connection. It is NOT vault_items, and that is the whole
	// design — everything in vault_items is sync-bound by construction, and a
	// database password is only useful on a machine that can reach the
	// database. Same reason db_query_history has a table of its own.
	TableAccountSecrets = "account_secrets"
	TableAppPreferences = "app_preferences"
	TableUIState        = "ui_state"
	// TableDBQueryHistory is deliberately outside vault_items: sync has no
	// path to this table, which is what keeps query history on one device.
	TableDBQueryHistory = "db_query_history"
	// TableDBSchemaCache holds table/column names only — structure, not data —
	// so it lives in plaintext outside the vault, like note_links and env_tags.
	TableDBSchemaCache = "db_schema_cache"

	// TableNoteLinks indexes note-to-note references. IDs are not content, so
	// like ui_state and env_tags this sits in plaintext outside the vault.
	TableNoteLinks = "note_links"

	TableAuditEvents = "audit_events"
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
			for _, col := range []struct {
				name       string
				definition string
			}{
				{"syncing_since", "TEXT NOT NULL DEFAULT ''"},
				{"account_id", "TEXT NOT NULL DEFAULT ''"},
				{"device_id", "TEXT NOT NULL DEFAULT ''"},
				{"protocol_version", "INTEGER NOT NULL DEFAULT 0"},
				{"operation_id", "TEXT NOT NULL DEFAULT ''"},
				{"entity_version", "INTEGER NOT NULL DEFAULT 0"},
				{"key_id", "TEXT NOT NULL DEFAULT ''"},
			} {
				if err := addColumnIfNotExists(ctx, db, TableSyncOutbox, col.name, col.definition); err != nil {
					return err
				}
			}
			return nil
		},
	}

	m9 := Migration{
		Version: 9,
		Name:    "sync account binding and remote entity versions",
		Up: func(ctx context.Context) error {
			// The account binding is intentionally separate from sync_identity:
			// device identity is local, while account identity is supplied only by
			// an authenticated remote caller. A fixed primary key permits at most
			// one account binding for this local database.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY CHECK (id = 'local'),
					account_id TEXT NOT NULL UNIQUE,
					bound_at TEXT NOT NULL
				)`, TableSyncAccountBinding,
			)); err != nil {
				return err
			}

			// This current-state ledger survives deletes and enforces monotonic
			// entity versions. vault_items rows are not backfilled because the app
			// is greenfield.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					account_id TEXT NOT NULL,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					entity_version INTEGER NOT NULL CHECK (entity_version > 0),
					operation_id TEXT NOT NULL,
					operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'delete')),
					tombstone INTEGER NOT NULL CHECK (tombstone IN (0, 1)),
					updated_at TEXT NOT NULL,
					PRIMARY KEY (account_id, record_id),
					FOREIGN KEY (account_id) REFERENCES %s(account_id) ON DELETE RESTRICT
				)`, TableSyncEntityVersions, TableSyncAccountBinding,
			)); err != nil {
				return err
			}

			// Applied operation IDs are append-only. Keeping the full immutable
			// identity prevents an old operation ID from becoming reusable after
			// the per-entity ledger advances to a newer version. No record content
			// or content-derived digest is stored here.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					account_id TEXT NOT NULL,
					operation_id TEXT NOT NULL,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					entity_version INTEGER NOT NULL CHECK (entity_version > 0),
					operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'delete')),
					applied_at TEXT NOT NULL,
					PRIMARY KEY (account_id, operation_id),
					FOREIGN KEY (account_id) REFERENCES %s(account_id) ON DELETE RESTRICT
				)`, TableSyncAppliedOps, TableSyncAccountBinding,
			))
			return err
		},
	}

	m10 := Migration{
		Version: 10,
		Name:    "local sync mutation journal",
		Up: func(ctx context.Context) error {
			// Local versions do not carry an account ID. This lets local-first
			// writes remain durable before the first authenticated sync session.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					record_id TEXT PRIMARY KEY,
					record_type TEXT NOT NULL,
					entity_version INTEGER NOT NULL CHECK (entity_version > 0),
					operation_id TEXT NOT NULL UNIQUE,
					operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'delete')),
					tombstone INTEGER NOT NULL CHECK (tombstone IN (0, 1)),
					updated_at TEXT NOT NULL
				)`, TableSyncLocalEntities,
			)); err != nil {
				return err
			}

			// Snapshot contains the same local metadata already present in
			// vault_items and an encrypted inner payload. It is never a wire DTO;
			// SyncNow encrypts it into the authenticated outbox after connect.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					sequence INTEGER PRIMARY KEY AUTOINCREMENT,
					operation_id TEXT NOT NULL UNIQUE,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					entity_version INTEGER NOT NULL CHECK (entity_version > 0),
					operation TEXT NOT NULL CHECK (operation IN ('create', 'update', 'delete')),
					snapshot BLOB NOT NULL,
					key_version INTEGER NOT NULL CHECK (key_version > 0),
					key_id TEXT NOT NULL,
					created_at TEXT NOT NULL,
					UNIQUE (record_id, entity_version)
				)`, TableSyncLocalMutations,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				"CREATE INDEX IF NOT EXISTS idx_%s_record_version ON %s (record_id, entity_version)",
				TableSyncLocalMutations, TableSyncLocalMutations,
			))
			return err
		},
	}

	m11 := Migration{
		Version: 11,
		Name:    "sync conflict theirs snapshots",
		Up: func(ctx context.Context) error {
			// Per-record conflict side ("theirs"): the authenticated remote
			// snapshot captured when a pull carries the divergent op for a record
			// that lost a same-version push. No FK (app-layer integrity, matching
			// the SaaS design); theirs_snapshot is the same encrypted record JSON
			// stored elsewhere, never a plaintext secret. Unique per record.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					account_id TEXT NOT NULL,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					remote_operation_id TEXT NOT NULL,
					remote_operation TEXT NOT NULL CHECK (remote_operation IN ('create', 'update', 'delete')),
					remote_entity_version INTEGER NOT NULL CHECK (remote_entity_version > 0),
					theirs_snapshot BLOB NOT NULL,
					local_change_key TEXT NOT NULL,
					detected_at TEXT NOT NULL,
					PRIMARY KEY (account_id, record_id)
				)`, TableSyncConflicts,
			))
			return err
		},
	}

	m12 := Migration{
		Version: 12,
		Name:    "create env_tags with builtin seed",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL UNIQUE,
					requires_confirmation INTEGER NOT NULL,
					is_builtin INTEGER NOT NULL,
					locked INTEGER NOT NULL
				)`, TableEnvTags,
			)); err != nil {
				return err
			}
			// Builtin seed: dev/stg start unlocked (editable), prod is always
			// locked with requires_confirmation=1 — no UI, bridge, or MCP path may
			// change that (enforced again in internal/environments.Service).
			seed := []struct {
				id                   string
				name                 string
				requiresConfirmation bool
				locked               bool
			}{
				{fmt.Sprintf("%024x", 1), "dev", false, false},
				{fmt.Sprintf("%024x", 2), "stg", false, false},
				{fmt.Sprintf("%024x", 3), "prod", true, true},
			}
			for _, tag := range seed {
				requiresConfirmation, locked := 0, 0
				if tag.requiresConfirmation {
					requiresConfirmation = 1
				}
				if tag.locked {
					locked = 1
				}
				if _, err := db.ExecContext(ctx, fmt.Sprintf(
					"INSERT OR IGNORE INTO %s (id, name, requires_confirmation, is_builtin, locked) VALUES (?, ?, ?, ?, ?)",
					TableEnvTags,
				), tag.id, tag.name, requiresConfirmation, 1, locked); err != nil {
					return err
				}
			}
			return nil
		},
	}

	m13 := Migration{
		Version: 13,
		Name:    "create agent hub sessions and turns",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					agent_type TEXT NOT NULL,
					project_path TEXT NOT NULL,
					status TEXT NOT NULL,
					parent_session_id TEXT,
					depth INTEGER NOT NULL DEFAULT 0,
					title TEXT,
					acp_session_id TEXT,
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`, TableAgentSessions,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					stop_reason TEXT,
					usage_json TEXT,
					raw_meta TEXT,
					created_at TEXT NOT NULL,
					FOREIGN KEY(session_id) REFERENCES %s(id)
				)`, TableAgentTurns, TableAgentSessions,
			))
			return err
		},
	}

	m14 := Migration{
		Version: 14,
		Name:    "create app preferences",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL
				)`, TableAppPreferences,
			))
			return err
		},
	}

	m15 := Migration{
		Version: 15,
		Name:    "sync conflict delete snapshots and outbox lineage index",
		Up: func(ctx context.Context) (err error) {
			// SQLite DDL is transactional, so the rebuild runs in one inner
			// transaction (the runner itself must not wrap Up). theirs_snapshot
			// becomes nullable: a captured remote delete is "theirs" with no
			// record body — the remote_operation column already distinguishes
			// it. The outbox index serves the per-record lineage probes in
			// Pending and the apply-guard.
			tx, txErr := db.BeginTx(ctx, nil)
			if txErr != nil {
				return txErr
			}
			defer func() {
				if err != nil {
					_ = tx.Rollback()
				}
			}()
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE %s (
					account_id TEXT NOT NULL,
					record_id TEXT NOT NULL,
					record_type TEXT NOT NULL,
					remote_operation_id TEXT NOT NULL,
					remote_operation TEXT NOT NULL CHECK (remote_operation IN ('create', 'update', 'delete')),
					remote_entity_version INTEGER NOT NULL CHECK (remote_entity_version > 0),
					theirs_snapshot BLOB,
					local_change_key TEXT NOT NULL,
					detected_at TEXT NOT NULL,
					PRIMARY KEY (account_id, record_id)
				)`, TableSyncConflicts+"_rebuild",
			)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(
				`INSERT INTO %s_rebuild (
					account_id, record_id, record_type, remote_operation_id, remote_operation,
					remote_entity_version, theirs_snapshot, local_change_key, detected_at
				) SELECT account_id, record_id, record_type, remote_operation_id, remote_operation,
					remote_entity_version, theirs_snapshot, local_change_key, detected_at FROM %s`,
				TableSyncConflicts, TableSyncConflicts,
			)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", TableSyncConflicts)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(
				"ALTER TABLE %s_rebuild RENAME TO %s", TableSyncConflicts, TableSyncConflicts,
			)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS ix_sync_outbox_lineage ON %s (account_id, record_id, entity_version, status)`,
				TableSyncOutbox,
			)); err != nil {
				return err
			}
			return tx.Commit()
		},
	}

	m16 := Migration{
		Version: 16,
		Name:    "create ui_state",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL
				)`, TableUIState,
			))
			return err
		},
	}

	m17 := Migration{
		Version: 17,
		Name:    "create db_query_history",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					profile_id TEXT NOT NULL,
					sql_envelope BLOB NOT NULL,
					created_at TEXT NOT NULL
				)`, TableDBQueryHistory,
			)); err != nil {
				return err
			}
			// Prune and listing both read newest-first within one profile.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS ix_db_query_history_profile ON %s (profile_id, created_at DESC)`,
				TableDBQueryHistory,
			))
			return err
		},
	}

	m18 := Migration{
		Version: 18,
		Name:    "create db_schema_cache",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					profile_id TEXT PRIMARY KEY,
					schema_json TEXT NOT NULL,
					indexed_at TEXT NOT NULL
				)`, TableDBSchemaCache,
			))
			return err
		},
	}

	// Link targets are references, not content, so the index lives in
	// plaintext outside the vault — the same call already made for ui_state
	// and env_tags. Keeping it there is what lets ListBacklinks be one query
	// instead of decrypting every note's body to look for mentions.
	m19 := Migration{
		Version: 19,
		Name:    "create note_links",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					from_note_id TEXT NOT NULL,
					to_note_id TEXT NOT NULL,
					updated_at TEXT NOT NULL,
					PRIMARY KEY (from_note_id, to_note_id)
				)`, TableNoteLinks,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_note_links_to ON %s (to_note_id)`,
				TableNoteLinks,
			))
			return err
		},
	}

	m20 := Migration{
		Version: 20,
		Name:    "create audit_events",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					at TEXT NOT NULL,
					caller TEXT NOT NULL,
					subject TEXT NOT NULL,
					capability TEXT NOT NULL,
					scope TEXT NOT NULL,
					outcome TEXT NOT NULL,
					metadata TEXT NOT NULL
				)`, TableAuditEvents,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_audit_events_at ON %s (at)`,
				TableAuditEvents,
			))
			return err
		},
	}

	m21 := Migration{
		Version: 21,
		Name:    "create agent hub transcript",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					seq INTEGER NOT NULL,
					kind TEXT NOT NULL,
					tool_call_id TEXT NOT NULL DEFAULT '',
					text TEXT NOT NULL DEFAULT '',
					payload TEXT,
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL,
					FOREIGN KEY(session_id) REFERENCES %s(id)
				)`, TableAgentTranscript, TableAgentSessions,
			)); err != nil {
				return err
			}
			// Reads are always "the whole transcript of one session, in order",
			// and the recorder looks up the last row of a session on every
			// chunk — both are this index.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_transcript_session_seq ON %s (session_id, seq)`,
				TableAgentTranscript,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_agent_transcript_tool_call ON %s (session_id, tool_call_id)`,
				TableAgentTranscript,
			))
			return err
		},
	}

	m22 := Migration{
		Version: 22,
		Name:    "agent hub checkpoints",
		Up: func(ctx context.Context) error {
			// A turn row is written when the turn completes, long after the prompt
			// that started it; the prompt's message id is what ties the two, and
			// the transcript row, and the checkpoint, together.
			if err := addColumnIfNotExists(ctx, db, TableAgentTurns, "prompt_message_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					project_key TEXT NOT NULL,
					prompt_message_id TEXT NOT NULL DEFAULT '',
					kind TEXT NOT NULL,
					before_tree TEXT NOT NULL DEFAULT '',
					after_tree TEXT NOT NULL DEFAULT '',
					status TEXT NOT NULL,
					outcome TEXT NOT NULL DEFAULT '',
					changed TEXT,
					skipped TEXT,
					agent_report TEXT,
					overlap TEXT,
					detail TEXT,
					created_at TEXT NOT NULL,
					finished_at TEXT,
					FOREIGN KEY(session_id) REFERENCES %s(id)
				)`, TableAgentCheckpoints, TableAgentSessions,
			)); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_agent_checkpoints_session_created ON %s (session_id, created_at)`,
				TableAgentCheckpoints,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_agent_checkpoints_session_prompt ON %s (session_id, prompt_message_id)`,
				TableAgentCheckpoints,
			))
			return err
		},
	}

	m23 := Migration{
		Version: 23,
		Name:    "scoped account secrets",
		Up: func(ctx context.Context) error {
			// scope_owner is the profile that owns this credential, and it is
			// the link instead of the profile storing an account id: an id
			// pointing at a row that does not sync dangles on the next device,
			// and writing a replacement back would have two devices overwrite
			// each other forever.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					scope_owner TEXT NOT NULL,
					provider TEXT NOT NULL,
					kind TEXT NOT NULL,
					label TEXT NOT NULL DEFAULT '',
					username TEXT NOT NULL DEFAULT '',
					envelope TEXT NOT NULL,
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`, TableAccountSecrets,
			)); err != nil {
				return err
			}
			// One credential per owning profile: the lookup is always "the
			// secret for this profile", and two would make that ambiguous.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_account_secrets_owner ON %s (scope_owner)`,
				TableAccountSecrets,
			))
			return err
		},
	}

	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6, m7, m8, m9, m10, m11, m12, m13, m14, m15, m16, m17, m18, m19, m20, m21, m22, m23})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func addColumnIfNotExists(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}
