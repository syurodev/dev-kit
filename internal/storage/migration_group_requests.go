package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// GroupRequestsMigration adds the ledger of a child session's requests for more
// tool groups and the per-session group locks.
//
// A request row holds ids, group names, a route, a state and a revision. Its
// reason is the child's own text, kept short and only ever shown labelled as
// untrusted; the audit trail never receives it. A lock is a (session, group)
// pair the person wrote on purpose: it forbids every agent-origin path from
// adding that group to that session's override. Both tables go with the session
// (ON DELETE CASCADE), like the override they relate to.
func GroupRequestsMigration(db *sql.DB) Migration {
	return Migration{
		Version: 57,
		Name:    "child tool-group requests and group locks",
		Up: func(ctx context.Context) error {
			if db == nil {
				return fmt.Errorf("group requests migration requires database")
			}
			for _, statement := range []string{
				`CREATE TABLE IF NOT EXISTS agent_group_requests (
					id TEXT PRIMARY KEY,
					child_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
					parent_id TEXT NOT NULL CHECK(length(parent_id) > 0),
					groups TEXT NOT NULL CHECK(length(groups) BETWEEN 2 AND 1024),
					reason TEXT NOT NULL CHECK(length(reason) <= 2000),
					route TEXT NOT NULL CHECK(route IN ('parent','user')),
					state TEXT NOT NULL CHECK(state IN ('pending_parent','pending_user','approved','denied','expired','invalidated','superseded')),
					revision INTEGER NOT NULL CHECK(revision > 0),
					resolver TEXT NOT NULL DEFAULT '' CHECK(length(resolver) <= 200),
					outcome TEXT NOT NULL DEFAULT '' CHECK(length(outcome) <= 100),
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL,
					resolved_at TEXT NOT NULL DEFAULT '',
					CHECK((route = 'parent') OR (state <> 'pending_parent')),
					CHECK((state IN ('pending_parent','pending_user')) = (resolved_at = ''))
				)`,
				`CREATE INDEX IF NOT EXISTS agent_group_requests_child_idx ON agent_group_requests(child_id, state)`,
				`CREATE INDEX IF NOT EXISTS agent_group_requests_parent_idx ON agent_group_requests(parent_id, state)`,
				`CREATE TABLE IF NOT EXISTS agent_session_group_locks (
					session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
					group_name TEXT NOT NULL CHECK(length(group_name) BETWEEN 1 AND 128),
					created_at TEXT NOT NULL,
					PRIMARY KEY(session_id, group_name)
				)`,
			} {
				if _, err := db.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
