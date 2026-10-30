package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GroupRequestIntegrityMigration makes the request ledger's two promises hold in
// SQL, not in a read followed by a write:
//
//   - no two open requests of one child name the same group
//     (agent_group_request_open_groups, one row per open request and group, a
//     UNIQUE (child_id, group_name)); the rows go when the request closes;
//   - a child has at most four open requests, checked by a trigger in the same
//     statement that inserts the fifth.
//
// An open request does not survive an upgrade (none survives a restart either),
// so the open ones are closed here instead of back-filled.
func GroupRequestIntegrityMigration(db *sql.DB) Migration {
	return Migration{
		Version: 58,
		Name:    "group request integrity: one open request per child and group, quota",
		Up: func(ctx context.Context) error {
			if db == nil {
				return fmt.Errorf("group request integrity migration requires database")
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			statements := []struct {
				sql  string
				args []any
			}{
				{`UPDATE agent_group_requests SET state='invalidated', revision=revision+1, resolver='system', outcome='migration', updated_at=?, resolved_at=? WHERE state IN ('pending_parent','pending_user')`, []any{now, now}},
				{`CREATE TABLE IF NOT EXISTS agent_group_request_open_groups (
					request_id TEXT NOT NULL REFERENCES agent_group_requests(id) ON DELETE CASCADE,
					child_id TEXT NOT NULL,
					group_name TEXT NOT NULL CHECK(length(group_name) BETWEEN 1 AND 128),
					PRIMARY KEY(request_id, group_name),
					UNIQUE(child_id, group_name)
				)`, nil},
				{`CREATE TRIGGER IF NOT EXISTS agent_group_requests_close
					AFTER UPDATE OF state ON agent_group_requests
					WHEN NEW.state NOT IN ('pending_parent','pending_user')
					BEGIN DELETE FROM agent_group_request_open_groups WHERE request_id = NEW.id; END`, nil},
				{`CREATE TRIGGER IF NOT EXISTS agent_group_requests_quota
					BEFORE INSERT ON agent_group_requests
					WHEN NEW.state IN ('pending_parent','pending_user')
						AND (SELECT count(*) FROM agent_group_requests WHERE child_id = NEW.child_id AND state IN ('pending_parent','pending_user')) >= 4
					BEGIN SELECT RAISE(ABORT, 'agenthub.groupRequestLimit'); END`, nil},
			}
			for _, st := range statements {
				if _, err := db.ExecContext(ctx, st.sql, st.args...); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
