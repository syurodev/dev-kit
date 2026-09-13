package database

import (
	"context"
	"os"
	"testing"
	"time"
)

/*
How much of a result the service holds, against a real server (#46).

	DEVKIT_PG_TEST=1 go test ./internal/database/ -run Result -v
*/

func livePostgresProfile(t *testing.T) (*Service, string) {
	t.Helper()
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "results-live",
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
	return svc, summary.ID
}

// Ten rows asked for out of thirty million. The set-returning function is in
// the select list, where it streams, rather than in FROM, where Postgres
// materialises every row before sending the first.
func TestResultTruncatedIsNotDrained(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL results test")
	}
	svc, id := livePostgresProfile(t)

	started := time.Now()
	result, err := svc.Query(context.Background(), id, "SELECT generate_series(1, 30000000)", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !result.Truncated || result.RowCount != 10 {
		t.Fatalf("rows = %d, truncated = %v", result.RowCount, result.Truncated)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %v: the other 29,999,990 rows were read and thrown away", elapsed)
	}

	// And the connection is fine afterwards.
	if _, err := svc.Query(context.Background(), id, "SELECT 1", 0); err != nil {
		t.Fatalf("Query after truncation: %v", err)
	}
}

func TestResultIsCappedByWhatItHolds(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL results test")
	}
	svc, id := livePostgresProfile(t)

	// 200 rows of one MiB each, against a 64 MiB cap: cut at a row, never
	// inside one.
	result, err := svc.Query(context.Background(), id,
		"SELECT repeat('x', 1048576) FROM generate_series(1, 200)", 1000)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !result.Truncated || result.RowCount != 64 {
		t.Fatalf("rows = %d, truncated = %v; want 64 whole rows", result.RowCount, result.Truncated)
	}
	for i, row := range result.Rows {
		if len(*row[0]) != 1048576 {
			t.Fatalf("row %d holds %d bytes: a cell was cut", i, len(*row[0]))
		}
	}
}
