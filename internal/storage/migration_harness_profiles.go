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
