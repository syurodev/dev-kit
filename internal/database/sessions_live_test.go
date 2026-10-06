package database

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

/*
A console's session, against real servers (#48): what one run leaves is there
for the next run in the same console, no other console sees it, Stop ends the
statement and not the session, and closing the session ends it.

	DEVKIT_PG_TEST=1 DEVKIT_MYSQL_TEST=1 go test ./internal/database/ -run Session -v
*/

func liveMySQLProfile(t *testing.T) (*Service, string) {
	t.Helper()
	svc := newService(t)
	port, _ := strconv.Atoi(envOr("DEVKIT_MYSQL_PORT", "3307"))
	summary, err := svc.CreateProfile(CreateProfileInput{
		Engine:   EngineMySQL,
		Name:     "sessions-live",
		Host:     envOr("DEVKIT_MYSQL_HOST", "127.0.0.1"),
		Port:     port,
		Database: envOr("DEVKIT_MYSQL_DB", "devkitdb"),
		Username: envOr("DEVKIT_MYSQL_USER", "devkit"),
		Password: envOr("DEVKIT_MYSQL_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	return svc, summary.ID
}

// stopAfter cancels a run a moment in, and fails if the call did not come back
// promptly.
func stopAfter(t *testing.T, run func(ctx context.Context)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	run(ctx)
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Stop took %v to come back", elapsed)
	}
}

func cell(t *testing.T, result QueryResult) string {
	t.Helper()
	if len(result.Rows) == 0 || len(result.Rows[0]) == 0 {
		t.Fatalf("no value in %+v", result)
	}
	if result.Rows[0][0] == nil {
		return "NULL"
	}
	return *result.Rows[0][0]
}

func TestSessionPostgresKeepsWhatARunLeft(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL session test")
	}
	svc, id := livePostgresProfile(t)
	ctx := context.Background()
	const table = "SELECT count(*) FROM devkit_session_t"

	if _, err := svc.QueryInSession(ctx, id, "console-a", "CREATE TEMP TABLE devkit_session_t (x int)", 0); err != nil {
		t.Fatalf("CREATE TEMP TABLE: %v", err)
	}
	if _, err := svc.QueryInSession(ctx, id, "console-a", table, 0); err != nil {
		t.Fatalf("the next run in the same console lost its temporary table: %v", err)
	}
	if _, err := svc.QueryInSession(ctx, id, "console-b", table, 0); err == nil {
		t.Fatal("another console saw this console's temporary table")
	}

	stopAfter(t, func(stop context.Context) {
		if _, err := svc.QueryInSession(stop, id, "console-a", "SELECT pg_sleep(5)", 0); err == nil {
			t.Fatal("the sleep was not stopped")
		}
	})
	if _, err := svc.QueryInSession(ctx, id, "console-a", table, 0); err != nil {
		t.Fatalf("Stop took the session with it: %v", err)
	}

	svc.CloseSession(id, "console-a")
	if _, err := svc.QueryInSession(ctx, id, "console-a", table, 0); err == nil {
		t.Fatal("a closed session kept its temporary table")
	}
}

func TestSessionMySQLKeepsWhatARunLeft(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL session test")
	}
	svc, id := liveMySQLProfile(t)
	ctx := context.Background()
	read := func(session string) string {
		result, err := svc.QueryInSession(ctx, id, session, "SELECT @devkit_v", 0)
		if err != nil {
			t.Fatalf("SELECT @devkit_v: %v", err)
		}
		return cell(t, result)
	}

	if _, err := svc.QueryInSession(ctx, id, "console-a", "SET @devkit_v = 42", 0); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if got := read("console-a"); got != "42" {
		t.Fatalf("the next run in the same console read @devkit_v = %s", got)
	}
	if got := read("console-b"); got != "NULL" {
		t.Fatalf("another console read @devkit_v = %s", got)
	}

	stopAfter(t, func(stop context.Context) {
		_, _ = svc.QueryInSession(stop, id, "console-a", "SELECT SLEEP(5)", 0)
	})
	if got := read("console-a"); got != "42" {
		t.Fatalf("Stop took the session with it: @devkit_v = %s", got)
	}

	svc.CloseSession(id, "console-a")
	if got := read("console-a"); got != "NULL" {
		t.Fatalf("a closed session kept @devkit_v = %s", got)
	}
}
