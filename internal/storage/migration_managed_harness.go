package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// ManagedHarnessMigration adds the append-only sidecar that binds a managed
// attempt to the harness configuration it was meant to run with (intended)
// and the one its child ran with (actual). It holds ids, revisions, digests and
// group names only: no model, effort, account, command, path, URL, argument or
// environment column exists, and the immutable package row is not touched.
func ManagedHarnessMigration(db *sql.DB) Migration {
	return Migration{
		Version: 56,
		Name:    "managed harness snapshot bindings",
		Up: func(ctx context.Context) error {
			return migrateManagedHarness(ctx, db)
		},
	}
}

func migrateManagedHarness(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("managed harness migration requires database")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agent_work_harness_bindings (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			board_id TEXT NOT NULL,
			parent_task_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			work_item_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			package_id TEXT NOT NULL,
			phase TEXT NOT NULL CHECK(phase IN ('intended','actual')),
			binding_schema INTEGER NOT NULL CHECK(binding_schema > 0),
			source_schema INTEGER NOT NULL CHECK(source_schema > 0),
			role TEXT NOT NULL CHECK(role IN ('coordinator','implementer','reviewer','integrator')),
			session_id TEXT CHECK(session_id IS NULL OR length(session_id) > 0),
			vendor TEXT NOT NULL CHECK(length(vendor) > 0),
			harness_workspace_id TEXT NOT NULL CHECK(length(harness_workspace_id) > 0),
			vendor_revision INTEGER NOT NULL CHECK(vendor_revision >= 0),
			workspace_revision INTEGER CHECK(workspace_revision IS NULL OR workspace_revision > 0),
			session_revision INTEGER CHECK(session_revision IS NULL OR session_revision > 0),
			fingerprint TEXT NOT NULL CHECK(length(fingerprint) BETWEEN 1 AND 200),
			executable_digest TEXT NOT NULL CHECK(length(executable_digest) BETWEEN 1 AND 200),
			facts_complete INTEGER NOT NULL CHECK(facts_complete IN (0,1)),
			is_default INTEGER NOT NULL CHECK(is_default IN (0,1)),
			required_groups TEXT NOT NULL CHECK(length(required_groups) <= 1024),
			created_at TEXT NOT NULL,
			CHECK((phase = 'actual') = (session_id IS NOT NULL)),
			UNIQUE(attempt_id, phase),
			FOREIGN KEY(group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_groups(id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_items(id, group_id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(attempt_id, work_item_id, group_id, workspace_id, board_id, parent_task_id)
				REFERENCES agent_work_attempts(id, work_item_id, group_id, workspace_id, board_id, parent_task_id),
			FOREIGN KEY(package_id, workspace_id, group_id, work_item_id, attempt_id)
				REFERENCES agent_work_packages(id, workspace_id, group_id, work_item_id, attempt_id)
		)`,
		`CREATE TRIGGER IF NOT EXISTS agent_work_harness_bindings_no_update
			BEFORE UPDATE ON agent_work_harness_bindings
			BEGIN SELECT RAISE(ABORT, 'harness binding is immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
