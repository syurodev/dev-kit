package database

import (
	"context"
	"os"
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
	if result.RowCount != 1 || result.Rows[0][0] != "1" || result.Rows[0][1] != "hi" {
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
	// Warm up pool with a writeable query
	if _, err := svc.Query(ctx, summary.ID, "create temporary table if not exists test_warm (id int); insert into test_warm values (1);", 0); err != nil {
		t.Fatalf("Warmup write failed: %v", err)
	}

	// Toggle read_only: true on the warm pool
	if _, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{
		Env:      "dev",
		ReadOnly: true,
	}); err != nil {
		t.Fatalf("UpdateProfileOptions failed: %v", err)
	}

	// Subsequent write must be rejected by PostgreSQL in read-only safe-mode
	_, err = svc.Query(ctx, summary.ID, "insert into test_warm values (2);", 0)
	if err == nil {
		t.Fatal("expected write to be rejected on safe-mode read_only profile, but succeeded")
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
