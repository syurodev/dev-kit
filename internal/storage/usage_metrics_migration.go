package storage

import (
	"context"
	"database/sql"
)

func migrateUsageMetrics(ctx context.Context, db *sql.DB) error {
	for _, c := range []struct{ table, name, definition string }{
		{"agent_accounts", "usage_generation", "TEXT"},
		{"agent_turns", "account_id", "TEXT"}, {"agent_turns", "account_generation", "TEXT"},
		{"agent_turns", "started_at", "TEXT"}, {"agent_turns", "usage_workspace_id", "TEXT"},
		{"agent_turns", "usage_context_version", "INTEGER"},
		{"agent_turns", "harness_profile_revision", "INTEGER"}, {"agent_turns", "harness_override_revision", "INTEGER"},
	} {
		if err := addColumnIfNotExists(ctx, db, c.table, c.name, c.definition); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_accounts SET usage_generation=CASE WHEN home_dir='' THEN 'builtin' ELSE lower(hex(randomblob(16))) END WHERE usage_generation IS NULL`); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS agent_tool_usage_daily (
 day TEXT NOT NULL, vendor TEXT NOT NULL, workspace_key TEXT NOT NULL, tool TEXT NOT NULL,
 profile_key TEXT NOT NULL, override_key TEXT NOT NULL, version TEXT NOT NULL, cut_mode TEXT NOT NULL,
 profile_revision INTEGER, override_revision INTEGER,
 calls INTEGER NOT NULL, results INTEGER NOT NULL, text_calls INTEGER NOT NULL, non_text_calls INTEGER NOT NULL,
 tool_errors INTEGER NOT NULL, handler_errors INTEGER NOT NULL, exempt_calls INTEGER NOT NULL, cuts INTEGER NOT NULL,
 before_bytes INTEGER NOT NULL, after_bytes INTEGER NOT NULL, max_before INTEGER NOT NULL, max_after INTEGER NOT NULL,
 PRIMARY KEY(day,vendor,workspace_key,tool,profile_key,override_key,version,cut_mode),
 CHECK((profile_revision IS NULL AND profile_key='none') OR (profile_revision IS NOT NULL AND profile_key='v:'||profile_revision)),
 CHECK((override_revision IS NULL AND override_key='none') OR (override_revision IS NOT NULL AND override_key='v:'||override_revision)))`,
		`CREATE TABLE IF NOT EXISTS agent_tool_usage_runs (
 id TEXT PRIMARY KEY, started_at TEXT NOT NULL, last_flushed_at TEXT, closed_at TEXT,
 batch_seq INTEGER NOT NULL DEFAULT 0, dropped INTEGER NOT NULL DEFAULT 0, write_failures INTEGER NOT NULL DEFAULT 0,
 unclean INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS agent_turns_usage_start_idx ON agent_turns(session_id,started_at,id)`,
		`CREATE INDEX IF NOT EXISTS agent_transcript_usage_notice_idx ON agent_transcript(kind,created_at,id)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
