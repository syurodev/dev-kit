package storage

import (
	"context"
	"database/sql"
)

// HarnessProfilesMigration stores the workspace harness profile (W) and the
// session override (S). Both hold group names, budgets and server/skill
// definitions as JSON, never credentials or environment values. A deleted
// record stays as a tombstone so its revision keeps increasing.
func HarnessProfilesMigration(db *sql.DB) Migration {
	return Migration{Version: 54, Name: "workspace harness profiles and session overrides", Up: func(ctx context.Context) error {
		for _, q := range []string{
			`CREATE TABLE IF NOT EXISTS workspace_harness_profiles (
 workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
 schema_version INTEGER NOT NULL CHECK(schema_version > 0),
 revision INTEGER NOT NULL CHECK(revision > 0),
 deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
 body TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(deleted = 0 OR body = ''))`,
			// workspace_id is the session's workspace when S was written. It is not
			// a foreign key: a session that loses its workspace must make S
			// unreadable (scope error), not move it to another workspace.
			`CREATE TABLE IF NOT EXISTS session_harness_overrides (
 session_id TEXT PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
 workspace_id TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK(schema_version > 0),
 revision INTEGER NOT NULL CHECK(revision > 0),
 deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
 body TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(deleted = 0 OR body = ''))`,
		} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}}
}

// HarnessAppliedMigration records, per session, which harness configuration its
// runtime was last opened with: a reference (ids, revisions, fingerprint and
// executable digest), never a definition, path, command or environment value.
// It lets a restart refuse to adopt a changed set of executables without being
// able to reconstruct the old configuration.
func HarnessAppliedMigration(db *sql.DB) Migration {
	return Migration{Version: 55, Name: "session harness applied references", Up: func(ctx context.Context) error {
		_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_session_harness_applied (
 session_id TEXT PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
 schema_version INTEGER NOT NULL CHECK(schema_version > 0),
 ref TEXT NOT NULL,
 enforced INTEGER NOT NULL CHECK(enforced IN (0,1)),
 applied_at TEXT NOT NULL)`)
		return err
	}}
}
