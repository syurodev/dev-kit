package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

/*
The two guards a user is told they have, tried the way they fail.

Stop (#40): a cancelled SELECT that goes away proves little — a driver that only
hangs up also ends a sleep sooner or later. A cancelled UPDATE is the case that
matters: if the server was never told, the statement runs on after the socket
closes and commits. So the rows are read back, afterwards, on another
connection.

Read-only (#43): the flag is a session setting, and a session setting is
something the session can change. Each test changes it first and then writes.

	DEVKIT_PG_TEST=1 DEVKIT_MYSQL_TEST=1 go test ./internal/database/ -run 'Guard' -v
*/

func readOnly(cfg *ConnConfig) { cfg.ReadOnly = true }

// tryExec runs one statement to the end and returns what the server said.
func tryExec(conn interface {
	Query(context.Context, string, ...any) (Rows, error)
}, sql string) error {
	rows, err := conn.Query(context.Background(), sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// A table of 30 zeroes, dropped afterwards.
func guardTable(t *testing.T, setup Conn, name, ddl, insert string) {
	t.Helper()
	ctx := context.Background()
	exec(t, ctx, setup, "DROP TABLE IF EXISTS "+name)
	exec(t, ctx, setup, ddl)
	exec(t, ctx, setup, insert)
	t.Cleanup(func() { exec(t, ctx, setup, "DROP TABLE IF EXISTS "+name) })
}

func thirtyZeroes() string {
	values := make([]string, 30)
	for i := range values {
		values[i] = fmt.Sprintf("(%d, 0)", i+1)
	}
	return strings.Join(values, ", ")
}

// assertUntouched waits for the server to say the statement is gone, then reads
// the rows. Waiting on the server rather than a clock: an UPDATE that is still
// running has not committed yet, so reading early would pass for the wrong
// reason.
func assertUntouched(t *testing.T, probe Conn, runningSQL, table string) {
	t.Helper()
	assertNoneRunning(t, probe, runningSQL)
	if got := queryRow(t, probe, "SELECT COALESCE(SUM(v), 0) FROM "+table); got[0] != "0" {
		t.Fatalf("SUM(v) = %s after Stop: the UPDATE kept running on the server and committed", got[0])
	}
}

func TestGuardPostgresStopCancelsAWrite(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL guard tests")
	}
	setup := dialPostgres(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_guard_stop",
		"CREATE TABLE devkit_guard_stop (id int PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO devkit_guard_stop VALUES "+thirtyZeroes())

	conn := dialPostgres(t)
	defer conn.Close()
	// CPU, not pg_sleep: a sleeping backend notices its client hang up, a
	// working one does not until it next writes to the socket — which for an
	// UPDATE is after it has committed. The subquery is correlated so it runs
	// for every row.
	cancelWhileRunning(t, conn, `UPDATE devkit_guard_stop SET v = v + 1
		WHERE (SELECT count(*) FROM generate_series(1, 1500000 + id)) > 0`)
	assertUntouched(t, setup, `SELECT count(*) FROM pg_stat_activity
		WHERE state = 'active' AND query ILIKE '%devkit_guard_stop%' AND pid <> pg_backend_pid()`,
		"devkit_guard_stop")
}

func TestGuardMySQLStopCancelsAWrite(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL guard tests")
	}
	setup := dialMySQL(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_guard_stop",
		"CREATE TABLE devkit_guard_stop (id int PRIMARY KEY, v int NOT NULL) ENGINE=InnoDB",
		"INSERT INTO devkit_guard_stop (id, v) VALUES "+thirtyZeroes())

	conn := dialMySQL(t)
	defer conn.Close()
	cancelWhileRunning(t, conn, "UPDATE devkit_guard_stop SET v = v + 1 WHERE SLEEP(0.1) = 0")
	assertUntouched(t, setup, `SELECT count(*) FROM information_schema.processlist
		WHERE info LIKE '%devkit_guard_stop%' AND id <> connection_id()`,
		"devkit_guard_stop")
}

func TestGuardPostgresReadOnlySurvivesSET(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL guard tests")
	}
	setup := dialPostgres(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_guard_ro",
		"CREATE TABLE devkit_guard_ro (id int PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO devkit_guard_ro VALUES (1, 0)")

	ro := dialPostgres(t, readOnly)
	defer ro.Close()

	// Auto-commit: the setting persists on the pooled connection, and the
	// next statement is served by that same idle connection.
	_ = tryExec(ro, "SET default_transaction_read_only = off")
	if err := tryExec(ro, "UPDATE devkit_guard_ro SET v = 1"); err == nil {
		t.Error("UPDATE succeeded on a read-only profile after SET default_transaction_read_only = off")
	}
	// One statement that lifts READ ONLY and then writes, if it can.
	if err := tryExec(ro, `DO $$ BEGIN
		SET TRANSACTION READ WRITE;
		UPDATE devkit_guard_ro SET v = 3;
	END $$`); err == nil {
		t.Error("a DO block lifted READ ONLY and wrote on a read-only profile")
	}

	// Manual commit: SET TRANSACTION READ WRITE is legal as the first
	// statement of a transaction, which is exactly where the user's first
	// statement lands.
	tx, err := ro.(Transactor).Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = tryExec(tx, "SET TRANSACTION READ WRITE")
	if err := tryExec(tx, "UPDATE devkit_guard_ro SET v = 2"); err == nil {
		_ = tx.Commit(context.Background())
		t.Error("UPDATE succeeded in a read-only profile's transaction after SET TRANSACTION READ WRITE")
	} else {
		_ = tx.Rollback(context.Background())
	}

	if got := queryRow(t, setup, "SELECT v FROM devkit_guard_ro"); got[0] != "0" {
		t.Fatalf("v = %s: a read-only profile wrote", got[0])
	}
}

func TestGuardMySQLReadOnlySurvivesSET(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL guard tests")
	}
	setup := dialMySQL(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_guard_ro",
		"CREATE TABLE devkit_guard_ro (id int PRIMARY KEY, v int NOT NULL) ENGINE=InnoDB",
		"INSERT INTO devkit_guard_ro (id, v) VALUES (1, 0)")

	ro := dialMySQL(t, readOnly)
	defer ro.Close()

	_ = tryExec(ro, "SET SESSION transaction_read_only = 0")
	if err := tryExec(ro, "UPDATE devkit_guard_ro SET v = 1"); err == nil {
		t.Error("UPDATE succeeded on a read-only profile after SET SESSION transaction_read_only = 0")
	}

	tx, err := ro.(Transactor).Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = tryExec(tx, "SET SESSION transaction_read_only = 0")
	if err := tryExec(tx, "UPDATE devkit_guard_ro SET v = 2"); err == nil {
		_ = tx.Commit(context.Background())
		t.Error("UPDATE succeeded in a read-only profile's transaction after SET SESSION transaction_read_only = 0")
	} else {
		_ = tx.Rollback(context.Background())
	}

	if got := queryRow(t, setup, "SELECT v FROM devkit_guard_ro"); got[0] != "0" {
		t.Fatalf("v = %s: a read-only profile wrote", got[0])
	}
}
