package database

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestPostgresIntegration exercises TestConnection + Query against a real
// PostgreSQL instance. It is gated behind DEVKIT_PG_TEST=1 so the default
// `go test` run does not require a database.
//
// Defaults match the local dev setup:
//
//	DEVKIT_PG_HOST=127.0.0.1 DEVKIT_PG_PORT=5432 DEVKIT_PG_USER=devkit
//	DEVKIT_PG_PASSWORD=devkitpass DEVKIT_PG_DB=devkitdb
func TestPostgresIntegration(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL integration test")
	}

	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "local",
		Host:     envOr("DEVKIT_PG_HOST", "127.0.0.1"),
		Port:     5432,
		Database: envOr("DEVKIT_PG_DB", "devkitdb"),
		Username: envOr("DEVKIT_PG_USER", "devkit"),
		Password: envOr("DEVKIT_PG_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	ctx := context.Background()
	if err := svc.TestConnection(ctx, summary.ID); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	result, err := svc.Query(ctx, summary.ID, "select 1 as n, 'hi' as greeting", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Columns) != 2 || result.Columns[0] != "n" || result.Columns[1] != "greeting" {
		t.Fatalf("columns = %v", result.Columns)
	}
	if result.RowCount != 1 || *result.Rows[0][0] != "1" || *result.Rows[0][1] != "hi" {
		t.Fatalf("rows = %v", result.Rows)
	}
}

func TestPostgresReadOnlyWarmPoolRejection(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL integration test")
	}

	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "local-warm-ro",
		Host:     envOr("DEVKIT_PG_HOST", "127.0.0.1"),
		Port:     5432,
		Database: envOr("DEVKIT_PG_DB", "devkitdb"),
		Username: envOr("DEVKIT_PG_USER", "devkit"),
		Password: envOr("DEVKIT_PG_PASSWORD", "devkitpass"),
		SSLMode:  "disable",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	ctx := context.Background()

	/*
		The table is real and made outside the profile's pool, on purpose.

		This test used to warm the pool with
		`create temporary table …; insert …;` — two statements in one Query,
		which pgx refuses outright ("cannot insert multiple commands into a
		prepared statement"), so the test failed before it tested anything. And
		behind that was a quieter bug: a TEMPORARY table belongs to one session,
		and a pool hands the next statement to whichever connection is free. The
		insert that was meant to be rejected for being a write could instead be
		rejected for naming a table that session never saw — and the test, which
		only asked for SOME error, would have passed without read-only mode ever
		being exercised.
	*/
	setup := dialPostgres(t)
	t.Cleanup(setup.Close)
	execPostgres(t, setup, "create table if not exists devkit_warm_ro (id int)")
	t.Cleanup(func() { execPostgres(t, setup, "drop table if exists devkit_warm_ro") })

	// Warm the profile's own pool with a write it is still allowed to make.
	if _, err := svc.Query(ctx, summary.ID, "insert into devkit_warm_ro values (1)", 0); err != nil {
		t.Fatalf("Warmup write failed: %v", err)
	}

	// Toggle read_only: true on the warm pool
	if _, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env:      "dev",
		ReadOnly: true,
	}); err != nil {
		t.Fatalf("UpdateProfileOptions failed: %v", err)
	}

	// Rejected, and rejected FOR BEING A WRITE: 25006 is
	// read_only_sql_transaction. Any other error means the statement failed
	// for a reason that says nothing about safe mode.
	_, err = svc.Query(ctx, summary.ID, "insert into devkit_warm_ro values (2)", 0)
	if err == nil {
		t.Fatal("expected write to be rejected on safe-mode read_only profile, but succeeded")
	}
	if !strings.Contains(err.Error(), "25006") {
		t.Fatalf("the write failed, but not as a read-only rejection: %v", err)
	}
}

// execPostgres runs a statement that returns nothing and surfaces its error.
// pgx hands back Rows before the server has answered, so the error only
// arrives once the rows are drained.
func execPostgres(t *testing.T, conn Conn, sql string) {
	t.Helper()
	rows, err := conn.Query(context.Background(), sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	for rows.Next() {
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
