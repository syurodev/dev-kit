package storage

import (
	"context"
	"database/sql"
)

// ManagedLeasesMigration is wired as migration 51 by the composition-owned
// migration list. It stores ownership metadata only, never paths or payloads.
func ManagedLeasesMigration(db *sql.DB) Migration {
	return Migration{Version: 51, Name: "managed writer leases", Up: func(ctx context.Context) error {
		if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_work_leases (
			id TEXT PRIMARY KEY,
			schema_version INTEGER NOT NULL CHECK(schema_version > 0),
			workspace_id TEXT NOT NULL,
			board_id TEXT NOT NULL,
			parent_task_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			work_item_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL CHECK(attempt_generation > 0),
			session_id TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK(generation > 0),
			revision INTEGER NOT NULL CHECK(revision > 0),
			heartbeat_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			released_at TEXT,
			release_reason TEXT NOT NULL DEFAULT '',
			UNIQUE(resource_key,generation),
			UNIQUE(attempt_id,generation),
			FOREIGN KEY(work_item_id,group_id,workspace_id,board_id,parent_task_id)
				REFERENCES agent_work_items(id,group_id,workspace_id,board_id,parent_task_id),
			FOREIGN KEY(attempt_id,work_item_id,group_id,workspace_id,board_id,parent_task_id)
				REFERENCES agent_work_attempts(id,work_item_id,group_id,workspace_id,board_id,parent_task_id)
		)`); err != nil {
			return err
		}
		for _, index := range []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS agent_work_leases_active_resource_idx ON agent_work_leases(resource_key) WHERE released_at IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS agent_work_leases_active_session_idx ON agent_work_leases(session_id) WHERE released_at IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS agent_work_leases_active_attempt_idx ON agent_work_leases(attempt_id) WHERE released_at IS NULL`,
			`CREATE UNIQUE INDEX IF NOT EXISTS agent_work_leases_active_task_idx ON agent_work_leases(task_id) WHERE released_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS agent_work_leases_attempt_history_idx ON agent_work_leases(attempt_id,generation)`,
		} {
			if _, err := db.ExecContext(ctx, index); err != nil {
				return err
			}
		}
		return nil
	}}
}
