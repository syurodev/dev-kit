package database

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

/*
Does cancelling a call actually stop the statement on the server?

The console's Stop button cancels the Wails call, which cancels the context the
bridge handed the driver. Whether that reaches the SERVER — rather than only
ending the UI's interest in the answer — is a property of the driver and cannot
be asserted against a fake: pgx has to send a cancel request on a second
connection, and the MySQL driver has to issue its own KILL.

The proof is not that Query returns. A driver that abandoned the connection
would also return. It is that the sleeping backend is gone from the server's own
view of what is running, asked over a DIFFERENT connection afterwards.

	DEVKIT_PG_TEST=1 DEVKIT_MYSQL_TEST=1 go test ./internal/database/ -run Cancel -v
*/

const sleepSeconds = 20

func TestPostgresCancelStopsTheStatement(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL cancel test")
	}
	conn := dialPostgres(t)
	defer conn.Close()

	cancelWhileRunning(t, conn, "SELECT pg_sleep(20)")

	// pg_stat_activity is the server's own answer, read on a fresh connection.
	probe := dialPostgres(t)
	defer probe.Close()
	assertNoneRunning(t, probe, `
		SELECT count(*) FROM pg_stat_activity
		WHERE query ILIKE '%pg_sleep%' AND pid <> pg_backend_pid()`)
}

func TestMySQLCancelStopsTheStatement(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL cancel test")
	}
	conn := dialMySQL(t)
	defer conn.Close()

	cancelWhileRunning(t, conn, "SELECT SLEEP(20)")

	probe := dialMySQL(t)
	defer probe.Close()
	assertNoneRunning(t, probe, `
		SELECT count(*) FROM information_schema.processlist
		WHERE info LIKE '%SLEEP%' AND id <> connection_id()`)
}

// Runs sql, cancels it after a moment, and fails if the call did not come back
// promptly — a driver that ignored the cancellation would sit out the sleep.
func cancelWhileRunning(t *testing.T, conn Conn, sql string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// Drained, not just opened. pgx's Query returns a lazy Rows as soon as
		// it has the field descriptions — before the statement has produced
		// anything — so a test that only looked at Query's error saw nil in
		// half a millisecond and learned nothing. The service drains into a
		// QueryResult, and the drain is where the sleep is felt.
		rows, err := conn.Query(ctx, sql)
		if err == nil {
			for rows.Next() {
			}
			err = rows.Err()
			rows.Close()
		}
		done <- err
	}()

	// Long enough for the statement to be on the server, far short of the sleep.
	time.Sleep(500 * time.Millisecond)
	started := time.Now()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Query returned no error after its context was cancelled")
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("Query took %v to return after cancel; it waited out the sleep", elapsed)
		}
	case <-time.After(sleepSeconds * time.Second):
		t.Fatal("Query never returned after its context was cancelled")
	}
}

func assertNoneRunning(t *testing.T, probe Conn, countSQL string) {
	t.Helper()
	// The server needs a moment to reap the backend the cancel killed.
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		rows, err := probe.Query(context.Background(), countSQL)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		last = ""
		if rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				t.Fatalf("probe values: %v", err)
			}
			if len(values) > 0 && values[0] != nil {
				last = *values[0]
			}
		}
		rows.Close()
		if last == "0" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("the statement is still running on the server (count=%q): cancel stopped the wait, not the query", last)
}

// opts adjust the config before dialling — a read-only profile, say.
func dialPostgres(t *testing.T, opts ...func(*ConnConfig)) Conn {
	t.Helper()
	cfg := ConnConfig{
		Engine:   EnginePostgres,
		Host:     envOr("DEVKIT_PG_HOST", "127.0.0.1"),
		Port:     5432,
		Database: envOr("DEVKIT_PG_DB", "devkitdb"),
		Username: envOr("DEVKIT_PG_USER", "devkit"),
		Password: envOr("DEVKIT_PG_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	conn, err := postgresDriver{}.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial postgres: %v", err)
	}
	return conn
}

func dialMySQL(t *testing.T, opts ...func(*ConnConfig)) Conn {
	t.Helper()
	port, _ := strconv.Atoi(envOr("DEVKIT_MYSQL_PORT", "3307"))
	cfg := ConnConfig{
		Engine:   EngineMySQL,
		Host:     envOr("DEVKIT_MYSQL_HOST", "127.0.0.1"),
		Port:     port,
		Database: envOr("DEVKIT_MYSQL_DB", "devkitdb"),
		Username: envOr("DEVKIT_MYSQL_USER", "devkit"),
		Password: envOr("DEVKIT_MYSQL_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	conn, err := mysqlDriver{engine: EngineMySQL}.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial mysql: %v", err)
	}
	return conn
}

// A statement the engine refuses must come back marked, or the bridge will
// redact it to "Connection failed." and the console will blame the connection
// for a typo. Live, because the whole point is what a real driver returns.
func TestPostgresSyntaxErrorIsAStatementError(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL statement-error test")
	}
	conn := dialPostgres(t)
	defer conn.Close()

	rows, err := conn.Query(context.Background(), `SELECT * FROM definitely_not_a_table`)
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Fatal("the engine accepted a table that does not exist")
	}

	marked := NewStatementError(err)
	var statement *StatementError
	if !errors.As(marked, &statement) {
		t.Fatalf("not marked as a statement error: %v", marked)
	}
	if !strings.Contains(statement.Error(), "definitely_not_a_table") {
		t.Fatalf("the engine's sentence did not survive: %q", statement.Error())
	}
}

// The exact statement from the report: MySQL rejects an unknown column, and
// the console has to end up holding that sentence rather than four words
// about a connection that is perfectly fine.
func TestMySQLUnknownColumnReachesTheConsole(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL statement-error test")
	}
	conn := dialMySQL(t)
	defer conn.Close()

	rows, err := conn.Query(context.Background(), "SELECT hello")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Fatal("the engine accepted a column that does not exist")
	}

	var statement *StatementError
	if !errors.As(NewStatementError(err), &statement) {
		t.Fatal("not marked as a statement error")
	}
	if !strings.Contains(statement.Error(), "Unknown column 'hello'") {
		t.Fatalf("the engine's sentence did not survive: %q", statement.Error())
	}
}
