package database

import (
	"context"
	"os"
	"strings"
	"testing"
)

/*
Reviewed edits, against real servers (#52): all of them, or none.

	DEVKIT_PG_TEST=1 DEVKIT_MYSQL_TEST=1 go test ./internal/database/ -run ApplyEdits -v
*/

func assertApplyEdits(t *testing.T, svc *Service, id string) {
	t.Helper()
	ctx := context.Background()
	read := func() string {
		result, err := svc.Query(ctx, id, "SELECT v FROM devkit_edits ORDER BY id", 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		values := make([]string, len(result.Rows))
		for i, row := range result.Rows {
			values[i] = *row[0]
		}
		return strings.Join(values, ",")
	}

	if err := svc.ApplyEdits(ctx, id, []string{
		"UPDATE devkit_edits SET v = 10 WHERE id = 1",
		"UPDATE devkit_edits SET v = 20 WHERE id = 2",
	}); err != nil {
		t.Fatalf("ApplyEdits: %v", err)
	}
	if got := read(); got != "10,20" {
		t.Fatalf("after applying both: v = %s", got)
	}

	err := svc.ApplyEdits(ctx, id, []string{
		"UPDATE devkit_edits SET v = 99 WHERE id = 1",
		"UPDATE devkit_edits SET v = 99 WHERE id = 999",
	})
	if err == nil || !strings.Contains(err.Error(), "matched no row") {
		t.Fatalf("an edit matching nothing: err = %v", err)
	}
	if got := read(); got != "10,20" {
		t.Fatalf("a failed review left half of it applied: v = %s", got)
	}
}

func TestApplyEditsPostgres(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL edits test")
	}
	setup := dialPostgres(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_edits",
		"CREATE TABLE devkit_edits (id int PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO devkit_edits VALUES (1, 0), (2, 0)")
	svc, id := livePostgresProfile(t)
	assertApplyEdits(t, svc, id)
}

func TestApplyEditsMySQL(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the MySQL edits test")
	}
	setup := dialMySQL(t)
	t.Cleanup(setup.Close)
	guardTable(t, setup, "devkit_edits",
		"CREATE TABLE devkit_edits (id int PRIMARY KEY, v int NOT NULL) ENGINE=InnoDB",
		"INSERT INTO devkit_edits (id, v) VALUES (1, 0), (2, 0)")
	svc, id := liveMySQLProfile(t)
	assertApplyEdits(t, svc, id)
}
