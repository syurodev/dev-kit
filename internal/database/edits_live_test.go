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

// Metadata must retain the boolean alias's width: data_type alone says tinyint.
func TestMySQLTypedEditMetadataAndValues(t *testing.T) {
	if os.Getenv("DEVKIT_MYSQL_TEST") != "1" {
		t.Skip("set DEVKIT_MYSQL_TEST=1 to run the typed edits test")
	}
	ctx := context.Background()
	conn := dialMySQL(t)
	t.Cleanup(conn.Close)
	guardTable(t, conn, "devkit_typed_edits",
		"CREATE TABLE devkit_typed_edits (id int PRIMARY KEY, active BOOLEAN NOT NULL, due DATE, happened DATETIME(6), duration TIME(6), amount DECIMAL(30,10)) ENGINE=InnoDB",
		"INSERT INTO devkit_typed_edits VALUES (1, FALSE, NULL, NULL, NULL, NULL)")
	columns, err := conn.ListColumns(ctx, envOr("DEVKIT_MYSQL_DB", "devkitdb"), "devkit_typed_edits")
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 6 || columns[1].DataType != "tinyint(1)" || columns[1].Nullable {
		t.Fatalf("boolean metadata lost: %+v", columns)
	}
	rows, err := conn.Query(ctx, "UPDATE devkit_typed_edits SET active=TRUE, due='2024-02-29', happened='2026-10-05 12:30:01.123456', duration='-838:59:59.123456', amount='12345678901234567890.1234567890' WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	rows, err = conn.Query(ctx, "SELECT active, due, happened, duration, amount FROM devkit_typed_edits WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no updated row")
	}
	values, err := rows.Values()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "2024-02-29", "2026-10-05 12:30:01.123456", "-838:59:59.123456", "12345678901234567890.1234567890"}
	for i, v := range values {
		if v == nil || *v != want[i] {
			t.Fatalf("column %d: got %v, want %s", i, v, want[i])
		}
	}
}
