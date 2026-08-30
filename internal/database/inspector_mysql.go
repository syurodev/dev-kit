package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var _ SessionInspector = (*mysqlConn)(nil)

func (c *mysqlConn) ListActivity(ctx context.Context) ([]SessionRow, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, coalesce(command, ''), coalesce(time, 0), coalesce(user, ''),
		       coalesce(host, ''), coalesce(state, ''), coalesce(info, '')
		FROM information_schema.processlist
		WHERE db = DATABASE()
		ORDER BY time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var (
			id      int64
			command string
			seconds int64
			user    string
			host    string
			state   sql.NullString
			info    sql.NullString
		)
		if err := rows.Scan(&id, &command, &seconds, &user, &host, &state, &info); err != nil {
			return nil, err
		}
		out = append(out, SessionRow{
			PID:      fmt.Sprintf("%d", id),
			State:    strings.ToLower(command),
			Duration: fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60),
			User:     user,
			Client:   host,
			// MySQL has no pg_blocking_pids equivalent. v1 leaves BlockedBy
			// empty rather than guessing from INNODB_LOCK_WAITS, which differs
			// by version and is easy to get wrong.
			WaitEvent: state.String,
			Query:     info.String,
		})
	}
	return out, rows.Err()
}

func (c *mysqlConn) CancelQuery(ctx context.Context, pid string) error {
	_, err := c.db.ExecContext(ctx, fmt.Sprintf("KILL QUERY %s", quoteConnectionID(pid)))
	return err
}

func (c *mysqlConn) TerminateSession(ctx context.Context, pid string) error {
	_, err := c.db.ExecContext(ctx, fmt.Sprintf("KILL %s", quoteConnectionID(pid)))
	return err
}

// quoteConnectionID guards the one place an identifier has to be interpolated:
// MySQL's KILL takes no placeholders. Anything that is not a plain integer is
// rejected rather than escaped.
func quoteConnectionID(pid string) string {
	for _, r := range pid {
		if r < '0' || r > '9' {
			return "0"
		}
	}
	if pid == "" {
		return "0"
	}
	return pid
}
