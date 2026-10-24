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
	TableVaultKeyring         = "vault_keyring"
	TableNotes                = "notes"
	TableConnectionProfiles   = "connection_profiles"
	TableSSHProfiles          = "ssh_profiles"
	TableVaultItems           = "vault_items"
	TableSyncOutbox           = "sync_outbox"
	TableSyncCursors          = "sync_cursors"
	TableSyncAccountBinding   = "sync_account_binding"
	TableSyncEntityVersions   = "sync_entity_versions"
	TableSyncAppliedOps       = "sync_applied_operations"
	TableSyncIdentity         = "sync_identity"
	TableSyncLocalEntities    = "sync_local_entity_versions"
	TableSyncLocalMutations   = "sync_local_mutations"
	TableSyncConflicts        = "sync_conflicts"
	TableEnvTags              = "env_tags"
	TableAgentSessions        = "agent_sessions"
	TableAgentAccounts        = "agent_accounts"
	TableAgentPendingRequests = "agent_pending_requests"
	TableAgentDocuments       = "agent_documents"
	// TableSessionChangeRequests ties a pull/merge request to an Agent Hub
	// session by hand or by pr.create. Local like the session; not synced.
	TableSessionChangeRequests = "session_change_requests"
	TableAgentTurns            = "agent_turns"
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

	// TableTaskVerifyRuns is the queue of close-a-task verify runs. It is
	// derived/operational — it says what this machine is doing, not what the
	// project is — so it never syncs. It is durable because a run interrupted
	// by a quit must come back as a queued run rather than as a guess about
	// what its exit code would have been.
	TableTaskVerifyRuns = "task_verify_runs"

	// TableTaskBoardBindings binds boards to workspaces on this machine. It is
	// derived/operational — it says how this machine views the project, not what
	// the project is — so it never syncs. A board can sync to another machine
	// without its binding; when it arrives, it lands in the "unbound" group until
	// the user gathers it into a workspace.
	//
	// workspace_id is nullable: a row with NULL means this machine has
	// considered the board and left it unbound on purpose — a person unbound
	// it, or its workspace was deleted — recorded separately from no row at
	// all, which means the board was never considered here. That distinction
	// is what lets a startup migration bind boards it has never seen without
	// undoing an unbind a person already made.
	TableTaskBoardBindings = "task_board_bindings"

	// TableAccountHostChoices remembers which account was picked for a host on
	// an environment, so the question is asked once per pair rather than every
	// time. Local, not a vault item: it is a convenience that can be answered
	// again in one click, and making it sync would buy a record type, a schema
	// and a conflict rule for a preference whose worst failure is one dialog.
	TableAccountHostChoices = "account_host_choices"

	// TableAccountRemoteChoices remembers which account a git remote's owner
	// was answered with, per host: acme-corp on github.com is the company
	// account, everything else there is the personal one. It cannot be
	// TableAccountHostChoices because that key is (host, env), and two
	// accounts on one host split by who owns the repository, not by
	// environment. Local for the same reason that table is: a preference whose
	// worst failure is one dialog does not earn a sync record type.
	TableAccountRemoteChoices = "account_remote_choices"

	// TableAccountIdentities holds the name and email an account commits as.
	// Local: it is a property of how this machine writes commits, it is not a
	// secret, and putting it in a shared account's metadata would make an older
	// DevKit on another device refuse the whole record (that metadata is
	// decoded field by field). Losing a row costs one form to fill in again.
	TableAccountIdentities = "account_identities"

	// TableAccountLinks says which API token goes with which SSH key account:
	// one login the person keeps as two credentials. Local, like identities: it
	// is how this machine groups its accounts, not a secret, and a shared
	// account's metadata is decoded field by field by every DevKit on every
	// device, so a new field there would make an older one refuse the record.
	TableAccountLinks = "account_links"

	// TableAPIRequestHistory is what this machine sent and what came back. It
	// never syncs — it says what this device did, not what the project is —
	// and it is the same call db_query_history already made. The URL is
	// SEALED rather than stored in plaintext: an interpolated URL can carry a
	// secret variable, and a local file is still at rest.
	TableAPIRequestHistory = "api_request_history"

	// TableCodexHookEvents is what an external Codex CLI session reported
	// through its lifecycle hooks (internal/codexhook). Local-only, like
	// api_request_history: it says what this machine observed, not what the
	// project is. Prompt text and tool input/output are SEALED — a prompt can
	// as easily carry a secret as a URL query string can, and DevKit did not
	// write either.
	TableCodexHookEvents = "codex_hook_events"

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

	// TableWorkspaces and TableWorkspaceServices are the multi-repo registry
	// (docs/dev-kit/workspace.mdx): a name and a root, and the (name, path)
	// pairs auto-discovery resolved under it. Machine-local like env_tags —
	// a path is only ever correct on the machine that resolved it.
	TableWorkspaces        = "workspaces"
	TableWorkspaceServices = "workspace_services"
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

	m24 := Migration{
		Version: 24,
		Name:    "task verify runs",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					task_id TEXT NOT NULL,
					board_id TEXT NOT NULL,
					project_path TEXT NOT NULL,
					target_status TEXT NOT NULL,
					command TEXT NOT NULL,
					timeout_sec INTEGER NOT NULL DEFAULT 0,
					requested_by TEXT NOT NULL,
					requested_at TEXT NOT NULL,
					started_at TEXT,
					finished_at TEXT,
					state TEXT NOT NULL,
					exit_code INTEGER NOT NULL DEFAULT 0,
					output TEXT NOT NULL DEFAULT ''
				)`, TableTaskVerifyRuns,
			)); err != nil {
				return err
			}
			// The drain asks "what is queued for this project", and the queue
			// is per project because the work tree is what two runs contend
			// for — not the board, and not the task.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_task_verify_runs_project_state ON %s (project_path, state, requested_at)`,
				TableTaskVerifyRuns,
			))
			return err
		},
	}

	m25 := Migration{
		Version: 25,
		Name:    "account host choices",
		Up: func(ctx context.Context) error {
			// Keyed by the pair, because the same host on a different
			// environment is a different question: the personal GitHub account
			// on dev and the company one on prod is the ordinary case, not an
			// edge one.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					host TEXT NOT NULL,
					env TEXT NOT NULL,
					account_id TEXT NOT NULL,
					chosen_at TEXT NOT NULL,
					PRIMARY KEY (host, env)
				)`, TableAccountHostChoices,
			))
			return err
		},
	}

	m26 := Migration{
		Version: 26,
		Name:    "api request history",
		Up: func(ctx context.Context) error {
			// No body and no headers, deliberately: a response body can be
			// megabytes and either side can hold a token, while re-sending is
			// one click. What is here is what a person scans a list for.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					request_id TEXT NOT NULL,
					environment_id TEXT NOT NULL DEFAULT '',
					method TEXT NOT NULL,
					url_envelope TEXT NOT NULL,
					status INTEGER NOT NULL DEFAULT 0,
					duration_ms INTEGER NOT NULL DEFAULT 0,
					bytes INTEGER NOT NULL DEFAULT 0,
					failure TEXT NOT NULL DEFAULT '',
					sent_at TEXT NOT NULL
				)`, TableAPIRequestHistory,
			)); err != nil {
				return err
			}
			// The list is always "this request, newest first", and the trim
			// that keeps it bounded asks the same question.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_api_request_history_request ON %s (request_id, sent_at DESC)`,
				TableAPIRequestHistory,
			))
			return err
		},
	}

	m27 := Migration{
		Version: 27,
		Name:    "codex hook events",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					event TEXT NOT NULL,
					cwd TEXT NOT NULL DEFAULT '',
					turn_id TEXT NOT NULL DEFAULT '',
					model TEXT NOT NULL DEFAULT '',
					permission_mode TEXT NOT NULL DEFAULT '',
					tool_name TEXT NOT NULL DEFAULT '',
					summary TEXT NOT NULL DEFAULT '',
					detail_envelope TEXT NOT NULL DEFAULT '',
					occurred_at TEXT NOT NULL
				)`, TableCodexHookEvents,
			)); err != nil {
				return err
			}
			// The transcript for one session, and the "which sessions exist"
			// list, both read newest-first off this same pair.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_codex_hook_events_session ON %s (session_id, occurred_at)`,
				TableCodexHookEvents,
			))
			return err
		},
	}

	m28 := Migration{
		Version: 28,
		Name:    "create workspaces and workspace_services",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					root TEXT NOT NULL
				)`, TableWorkspaces,
			)); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					workspace_id TEXT NOT NULL,
					name TEXT NOT NULL,
					path TEXT NOT NULL,
					name_source TEXT NOT NULL
				)`, TableWorkspaceServices,
			)); err != nil {
				return err
			}
			// Listing a workspace's services is the read this table exists for.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_workspace_services_workspace ON %s (workspace_id)`,
				TableWorkspaceServices,
			))
			return err
		},
	}

	m29 := Migration{
		Version: 29,
		Name:    "agent_sessions extra_rw_paths",
		Up: func(ctx context.Context) error {
			// The jail widening a session was created with (a workspace's
			// other services), as a JSON array. Resume and rewind rebuild
			// the jail from the row; without this they narrowed it back to
			// project_path. "" = no grant, which is also what every row
			// written before this column reads as.
			return addColumnIfNotExists(ctx, db, TableAgentSessions, "extra_rw_paths", "TEXT NOT NULL DEFAULT ''")
		},
	}

	m30 := Migration{
		Version: 30,
		Name:    "workspace_services foreign key and unique path",
		Up: func(ctx context.Context) error {
			// m28 created workspace_services with neither: nothing tied a
			// service to its workspace, and nothing stopped one path being
			// registered twice. Rescan needs the second — it matches a
			// rediscovered service to the stored row by path, so a manual
			// rename survives — and SQLite can only add either by rebuilding
			// the table. Duplicate paths collapse to one row (the copy keeps
			// the first by rowid); orphans whose workspace is gone are dropped.
			// One transaction of its own (DDL included — SQLite allows it), so
			// an interrupted run leaves the old table, never half of each.
			stmts := []string{
				fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s_new (
					id TEXT PRIMARY KEY,
					workspace_id TEXT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
					name TEXT NOT NULL,
					path TEXT NOT NULL,
					name_source TEXT NOT NULL,
					UNIQUE (workspace_id, path)
				)`, TableWorkspaceServices, TableWorkspaces),
				fmt.Sprintf(`INSERT OR IGNORE INTO %[1]s_new (id, workspace_id, name, path, name_source)
					SELECT id, workspace_id, name, path, name_source FROM %[1]s
					WHERE workspace_id IN (SELECT id FROM %[2]s)
					ORDER BY rowid`, TableWorkspaceServices, TableWorkspaces),
				fmt.Sprintf(`DROP TABLE %s`, TableWorkspaceServices),
				fmt.Sprintf(`ALTER TABLE %[1]s_new RENAME TO %[1]s`, TableWorkspaceServices),
				fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_workspace_services_workspace ON %s (workspace_id)`, TableWorkspaceServices),
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			for _, stmt := range stmts {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return tx.Commit()
		},
	}

	m31 := Migration{
		Version: 31,
		Name:    "workspace_services index outcome",
		Up: func(ctx context.Context) error {
			// Only the outcomes that must survive a restart: a service whose
			// index failed or that no analyzer claims is not queued again on
			// every start (docs/dev-kit/workspace.mdx § "Trạng thái index").
			// pending/indexing are this process's state, and indexed_at
			// lives in the index itself.
			for _, col := range []struct{ name, def string }{
				{"index_state", "TEXT NOT NULL DEFAULT ''"},
				{"index_error", "TEXT NOT NULL DEFAULT ''"},
				{"index_missing_toolchain", "INTEGER NOT NULL DEFAULT 0"},
			} {
				if err := addColumnIfNotExists(ctx, db, TableWorkspaceServices, col.name, col.def); err != nil {
					return err
				}
			}
			return nil
		},
	}

	m32 := Migration{
		Version: 32,
		Name:    "task board bindings",
		Up: func(ctx context.Context) error {
			// Board-to-workspace bindings are machine-local: a binding represents
			// how this machine views the board's place in the project, not a fact
			// about the project itself. workspace_id is nullable, and NULL is not
			// "no opinion" — the row's mere presence records that this machine has
			// already considered the board, bound or deliberately not, which is
			// what tells a legacy-board migration apart from a person's own
			// unbind. When a workspace is deleted its bindings are kept and set to
			// NULL (ON DELETE SET NULL), not dropped: dropping the row would erase
			// that "already considered" fact along with the workspace. See
			// docs/dev-kit/tasks.mdx § "Tích hợp Workspace".
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					board_id     TEXT PRIMARY KEY,
					workspace_id TEXT NULL REFERENCES %s(id) ON DELETE SET NULL,
					bound_at     TEXT NOT NULL
				)`, TableTaskBoardBindings, TableWorkspaces,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_task_board_bindings_workspace ON %s (workspace_id)`,
				TableTaskBoardBindings,
			))
			return err
		},
	}

	m33 := Migration{
		Version: 33,
		Name:    "agent sessions workspace identity",
		Up: func(ctx context.Context) error {
			// Existing sessions cannot be assigned safely when their folder is
			// in multiple workspaces. Keep those unbound for an explicit choice.
			if err := addColumnIfNotExists(ctx, db, TableAgentSessions, "workspace_id", "TEXT NULL REFERENCES workspaces(id) ON DELETE SET NULL"); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `UPDATE agent_sessions SET workspace_id = (
				SELECT MIN(ws.workspace_id) FROM workspace_services ws
				WHERE agent_sessions.project_path = ws.path
				   OR substr(agent_sessions.project_path, 1, length(ws.path) + 1) = ws.path || '/'
				HAVING COUNT(DISTINCT ws.workspace_id) = 1
			) WHERE workspace_id IS NULL AND parent_session_id IS NULL`); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, `WITH RECURSIVE lineage(id, root_id) AS (
				SELECT id, id FROM agent_sessions WHERE parent_session_id IS NULL
				UNION ALL
				SELECT child.id, lineage.root_id FROM agent_sessions child
				JOIN lineage ON child.parent_session_id = lineage.id
			) UPDATE agent_sessions SET workspace_id = (
				SELECT root.workspace_id FROM lineage
				JOIN agent_sessions root ON root.id = lineage.root_id
				WHERE lineage.id = agent_sessions.id
			) WHERE parent_session_id IS NOT NULL`)
			return err
		},
	}

	m34 := Migration{
		Version: 34,
		Name:    "workspace instructions",
		Up: func(ctx context.Context) error {
			return addColumnIfNotExists(ctx, db, "workspaces", "instructions", "TEXT NOT NULL DEFAULT ''")
		},
	}

	// Shared memory stays local, alongside workspace instructions. Every change
	// retains its full immutable snapshot, including source attribution.
	m35 := Migration{
		Version: 35,
		Name:    "shared workspace memory and revisions",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS workspace_memory (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0),
 deleted INTEGER NOT NULL DEFAULT 0,
 title TEXT NOT NULL,
 summary TEXT NOT NULL,
 body TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 data TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS workspace_memory_workspace_updated ON workspace_memory(workspace_id, deleted, updated_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS workspace_memory_revisions (
 entry_id TEXT NOT NULL REFERENCES workspace_memory(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0),
 operation TEXT NOT NULL,
 data TEXT NOT NULL,
 PRIMARY KEY(entry_id, revision)
);`)
			return err
		},
	}

	m36 := Migration{
		Version: 36,
		Name:    "indexed workspace memory and summary read models",
		Up:      func(ctx context.Context) error { return migrateWorkspaceMemoryIndex(ctx, db) },
	}

	m37 := Migration{
		Version: 37,
		Name:    "account remote choices",
		Up: func(ctx context.Context) error {
			// host and owner are stored lower-cased, as the lookup normalises
			// them: GitHub and GitLab namespaces are case-insensitive.
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					host TEXT NOT NULL,
					owner TEXT NOT NULL,
					account_id TEXT NOT NULL,
					chosen_at TEXT NOT NULL,
					PRIMARY KEY (host, owner)
				)`, TableAccountRemoteChoices,
			))
			return err
		},
	}

	m38 := Migration{
		Version: 38,
		Name:    "ssh key accounts",
		Up: func(ctx context.Context) error {
			// A machine-local account (an SSH key that does not sync) keeps what
			// its provider said about it here: shared accounts have a vault item
			// metadata for that, and this table has had no place for it.
			for column, definition := range map[string]string{
				"subject":        "TEXT NOT NULL DEFAULT ''",
				"verified_state": "TEXT NOT NULL DEFAULT ''",
				"verified_at":    "TEXT NOT NULL DEFAULT ''",
			} {
				if err := addColumnIfNotExists(ctx, db, TableAccountSecrets, column, definition); err != nil {
					return err
				}
			}
			// A remembered answer is per transport as well as per (host, owner):
			// the token that authenticates acme-corp over HTTPS and the SSH key
			// that authenticates it over SSH are different accounts, and one
			// must not read the other's answer as stale and delete it. The key
			// changes, so the table is rebuilt; answers given so far were all
			// for HTTPS.
			for _, statement := range []string{
				fmt.Sprintf(`CREATE TABLE %[1]s_new (
					host TEXT NOT NULL,
					owner TEXT NOT NULL,
					via TEXT NOT NULL DEFAULT 'https',
					account_id TEXT NOT NULL,
					chosen_at TEXT NOT NULL,
					PRIMARY KEY (host, owner, via)
				)`, TableAccountRemoteChoices),
				fmt.Sprintf(`INSERT INTO %[1]s_new (host, owner, via, account_id, chosen_at)
					SELECT host, owner, 'https', account_id, chosen_at FROM %[1]s`, TableAccountRemoteChoices),
				fmt.Sprintf(`DROP TABLE %s`, TableAccountRemoteChoices),
				fmt.Sprintf(`ALTER TABLE %[1]s_new RENAME TO %[1]s`, TableAccountRemoteChoices),
			} {
				if _, err := db.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			return nil
		},
	}

	m39 := Migration{
		Version: 39,
		Name:    "account commit identities",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					account_id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					email TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`, TableAccountIdentities,
			))
			return err
		},
	}

	m40 := Migration{
		Version: 40,
		Name:    "agent session documents",
		Up: func(ctx context.Context) error {
			// A PDF attached to a session: described here, kept as a file in the
			// data directory. (session_id, id) is the key — a handle like "doc1"
			// means something only inside its session — and (session_id, sha256)
			// is unique so the same file attached twice is one document.
			// No ON DELETE CASCADE, like the other agent tables: the store deletes
			// children first, in one transaction.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					session_id TEXT NOT NULL,
					id TEXT NOT NULL,
					ordinal INTEGER NOT NULL,
					kind TEXT NOT NULL,
					name TEXT NOT NULL,
					mime TEXT NOT NULL,
					size INTEGER NOT NULL,
					sha256 TEXT NOT NULL,
					pages INTEGER NOT NULL,
					text_layer TEXT NOT NULL,
					chrome TEXT NOT NULL DEFAULT '',
					created_at TEXT NOT NULL,
					PRIMARY KEY (session_id, id),
					UNIQUE (session_id, sha256),
					FOREIGN KEY(session_id) REFERENCES %s(id)
				)`, TableAgentDocuments, TableAgentSessions,
			)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS idx_agent_documents_session ON %s (session_id, ordinal)`, TableAgentDocuments,
			))
			return err
		},
	}

	// m41: the line the sidebar shows under a session's title — where it got
	// to, written when a turn ends (agenthub.PreviewFromMessage/FromError).
	m41 := Migration{
		Version: 41,
		Name:    "agent_sessions preview",
		Up: func(ctx context.Context) error {
			return addColumnIfNotExists(ctx, db, TableAgentSessions, "preview", "TEXT NOT NULL DEFAULT ''")
		},
	}

	// m42: pull/merge requests attached to a session by hand (a pasted address,
	// a fork) or opened by the pr.create tool. A branch's own request is found
	// from the branch and needs no row.
	m42 := Migration{
		Version: 42,
		Name:    "session change requests",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					session_id TEXT NOT NULL,
					host TEXT NOT NULL,
					path TEXT NOT NULL,
					number INTEGER NOT NULL,
					folder TEXT NOT NULL DEFAULT '',
					url TEXT NOT NULL DEFAULT '',
					source TEXT NOT NULL,
					created_at TEXT NOT NULL,
					PRIMARY KEY (session_id, host, path, number)
				)`, TableSessionChangeRequests,
			))
			return err
		},
	}

	// m43: why a session's last turn did not complete, as JSON (agenthub.SessionStop),
	// empty when it did. Read by a parent agent before it resumes or messages a child.
	m43 := Migration{
		Version: 43,
		Name:    "agent_sessions stop",
		Up: func(ctx context.Context) error {
			return addColumnIfNotExists(ctx, db, TableAgentSessions, "stop", "TEXT NOT NULL DEFAULT ''")
		},
	}

	// m44: the API token that goes with an SSH key account (account_links).
	m44 := Migration{
		Version: 44,
		Name:    "account key-token links",
		Up: func(ctx context.Context) error {
			_, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					account_id TEXT PRIMARY KEY,
					token_id TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`, TableAccountLinks,
			))
			return err
		},
	}

	// m45: the vendor accounts a person signed in to Agent Hub (agenthub.AccountRecord).
	// Local, never synced, no credential: a vendor CLI keeps its own login in home_dir.
	// agent_sessions.agent_account_id is the account a session's process runs as
	// ('' = the built-in account).
	m45 := Migration{
		Version: 45,
		Name:    "agent accounts",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					vendor TEXT NOT NULL,
					label TEXT NOT NULL,
					home_dir TEXT NOT NULL DEFAULT '',
					signed_in INTEGER NOT NULL DEFAULT 0,
					shared INTEGER NOT NULL DEFAULT 0,
					exhausted_until INTEGER NOT NULL DEFAULT 0,
					usage TEXT NOT NULL DEFAULT '',
					usage_source TEXT NOT NULL DEFAULT '',
					observed_at INTEGER NOT NULL DEFAULT 0,
					created_at TEXT NOT NULL
				)`, TableAgentAccounts,
			)); err != nil {
				return err
			}
			return addColumnIfNotExists(ctx, db, TableAgentSessions, "agent_account_id", "TEXT NOT NULL DEFAULT ''")
		},
	}

	// m46: an account whose reopen failed (or any other reason that is not "it reached
	// a limit") is kept out of selection for a while: agent_accounts.cooldown_until,
	// Unix seconds. exhausted_until keeps meaning "a limit was reached".
	m46 := Migration{
		Version: 46,
		Name:    "agent accounts cooldown",
		Up: func(ctx context.Context) error {
			return addColumnIfNotExists(ctx, db, TableAgentAccounts, "cooldown_until", "INTEGER NOT NULL DEFAULT 0")
		},
	}

	// m47: durable Agent Hub peer delivery and guard state. The outbox is local
	// application data and mirrors peer text already stored in the transcript.
	m47 := Migration{
		Version: 47,
		Name:    "agent peer outbox",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_peer_outbox (
				message_id TEXT PRIMARY KEY,
				from_session TEXT NOT NULL DEFAULT '',
				to_session TEXT NOT NULL,
				from_workspace TEXT NOT NULL DEFAULT '',
				to_workspace TEXT NOT NULL DEFAULT '',
				relation TEXT NOT NULL DEFAULT '',
				source_json TEXT NOT NULL,
				body TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				state TEXT NOT NULL,
				code TEXT NOT NULL DEFAULT '',
				hop INTEGER NOT NULL DEFAULT 1,
				exchange_limit INTEGER NOT NULL DEFAULT 6,
				sibling INTEGER NOT NULL DEFAULT 0,
				cross_workspace_approved INTEGER NOT NULL DEFAULT 0
			)`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS agent_peer_outbox_target_state ON agent_peer_outbox(to_session, state, created_at)`); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_peer_guard_state (
				state_key TEXT PRIMARY KEY,
				state_json TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`)
			return err
		},
	}

	// m48: only metadata for Agent Hub permission and elicitation waits. The
	// request payload is intentionally absent: it can contain questions, secrets,
	// or authorization details and cannot be answered after a restart.
	m48 := Migration{
		Version: 48,
		Name:    "agent pending request recovery",
		Up: func(ctx context.Context) error {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				session_id TEXT NOT NULL,
				kind TEXT NOT NULL CHECK (kind IN ('permission', 'elicitation')),
				created_at TEXT NOT NULL,
				expires_at TEXT,
				resolution TEXT NOT NULL DEFAULT 'pending',
				reason TEXT NOT NULL DEFAULT '',
				resolved_at TEXT
			)`, TableAgentPendingRequests)); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS agent_pending_requests_session_idx ON %s(session_id, resolution)`, TableAgentPendingRequests))
			return err
		},
	}

	runner, err := NewMigrationRunner(&sqlMigrationStore{db: db}, []Migration{m1, m2, m3, m4, m5, m6, m7, m8, m9, m10, m11, m12, m13, m14, m15, m16, m17, m18, m19, m20, m21, m22, m23, m24, m25, m26, m27, m28, m29, m30, m31, m32, m33, m34, m35, m36, m37, m38, m39, m40, m41, m42, m43, m44, m45, m46, m47, m48})
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
