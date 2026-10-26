package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// ManagedPackagesMigration adds immutable encrypted context-package and
// artifact rows. Bodies are ciphertext envelopes only; scope and provenance
// metadata remain queryable without opening the vault.
func ManagedPackagesMigration(db *sql.DB) Migration {
	return Migration{
		Version: 53,
		Name:    "managed immutable context packages and artifacts",
		Up: func(ctx context.Context) error {
			return migrateManagedPackages(ctx, db)
		},
	}
}

func migrateManagedPackages(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("managed package migration requires database")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agent_work_packages (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			board_id TEXT NOT NULL,
			parent_task_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			work_item_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL UNIQUE,
			generation INTEGER NOT NULL CHECK(generation > 0),
			item_revision INTEGER NOT NULL CHECK(item_revision > 0),
			attempt_revision INTEGER NOT NULL CHECK(attempt_revision > 0),
			requirements_revision TEXT NOT NULL CHECK(length(requirements_revision) > 0),
			role TEXT NOT NULL CHECK(role IN ('coordinator','implementer','reviewer','integrator')),
			schema_version INTEGER NOT NULL CHECK(schema_version > 0),
			body_hash TEXT NOT NULL CHECK(length(body_hash) = 71 AND substr(body_hash,1,7) = 'sha256:'),
			body_size INTEGER NOT NULL CHECK(body_size > 0 AND body_size <= 65536),
			sealed_body BLOB NOT NULL CHECK(length(sealed_body) > 0 AND length(sealed_body) <= 1500000),
			created_at TEXT NOT NULL,
			UNIQUE(id, workspace_id, group_id, work_item_id, attempt_id),
			FOREIGN KEY(group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_groups(id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_items(id, group_id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(attempt_id, work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_attempts(id, work_item_id, group_id, workspace_id, board_id, parent_task_id)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_work_artifacts (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			board_id TEXT NOT NULL,
			parent_task_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			work_item_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK(generation > 0),
			lease_generation INTEGER NOT NULL CHECK(lease_generation > 0),
			item_revision INTEGER NOT NULL CHECK(item_revision > 0),
			attempt_revision INTEGER NOT NULL CHECK(attempt_revision > 0),
			requirements_revision TEXT NOT NULL CHECK(length(requirements_revision) > 0),
			kind TEXT NOT NULL CHECK(kind IN ('file','patch','test_output','image','other')),
			provenance TEXT NOT NULL CHECK(provenance = 'agent_claim'),
			producer_session_id TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('coordinator','implementer','reviewer','integrator')),
			schema_version INTEGER NOT NULL CHECK(schema_version > 0),
			body_hash TEXT NOT NULL CHECK(length(body_hash) = 71 AND substr(body_hash,1,7) = 'sha256:'),
			body_size INTEGER NOT NULL CHECK(body_size > 0 AND body_size <= 1000000),
			sealed_body BLOB NOT NULL CHECK(length(sealed_body) > 0 AND length(sealed_body) <= 1500000),
			created_at TEXT NOT NULL,
			UNIQUE(id, workspace_id, group_id, work_item_id, attempt_id),
			FOREIGN KEY(group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_groups(id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_items(id, group_id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(attempt_id, work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_attempts(id, work_item_id, group_id, workspace_id, board_id, parent_task_id)
		)`,
		`CREATE INDEX IF NOT EXISTS agent_work_artifacts_item_page_idx ON agent_work_artifacts(work_item_id, id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
