package database

import (
	"context"
	"os"
	"testing"
)

/*
What a Postgres cell reads as, against a real server (#37).

The values go on to be cursor literals, UPDATE keys and "filter by value"
clauses, so the bar is the server's own text — the string it prints and the
string it parses back. Decoded-and-%v'd Go values were not that for any type
pgx decodes into a struct or a byte array.

	DEVKIT_PG_TEST=1 go test ./internal/database/ -run Values -v
*/

func TestPostgresValuesAreTheServersText(t *testing.T) {
	if os.Getenv("DEVKIT_PG_TEST") != "1" {
		t.Skip("set DEVKIT_PG_TEST=1 to run the PostgreSQL values test")
	}
	conn := dialPostgres(t)
	defer conn.Close()

	want := []string{
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"12345.67",
		`{"a": 12345678901234567890, "b": 1}`,
		`{"b":1}`,
		"2024-01-02 03:04:05.123456",
		"2024-01-02",
		"1 day 02:00:00",
		"true",
		"false",
		"{1,2}",
		`\x78`,
		"NULL",
	}
	got := queryRow(t, conn, `SELECT
		'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid,
		12345.67::numeric,
		'{"b": 1, "a": 12345678901234567890}'::jsonb,
		'{"b":1}'::json,
		'2024-01-02 03:04:05.123456'::timestamp,
		'2024-01-02'::date,
		'1 day 2 hours'::interval,
		true,
		false,
		ARRAY[1, 2],
		'x'::bytea,
		NULL`)
	assertRow(t, got, want)

	// Asking for text is a query option riding in the args; a statement with
	// real parameters must still count them right.
	assertRow(t, queryRow(t, conn, `SELECT $1::int + 1`, 41), []string{"42"})

	// And a manual transaction reads through its own Query, not the pool's.
	tx, err := conn.(*postgresConn).Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(context.Background(), `SELECT 1.50::numeric`)
	if err != nil {
		t.Fatalf("tx Query: %v", err)
	}
	assertRow(t, drainOne(t, rows), []string{"1.50"})
}

func queryRow(t *testing.T, conn Conn, sql string, args ...any) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return drainOne(t, rows)
}

func drainOne(t *testing.T, rows Rows) []string {
	t.Helper()
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	cells, err := rows.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	// NULL spelled out, so a want list can say which cells should be one.
	values := make([]string, len(cells))
	for i, cell := range cells {
		values[i] = "NULL"
		if cell != nil {
			values[i] = *cell
		}
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("drain: %v", err)
	}
	return values
}

func assertRow(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d values %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, got[i], want[i])
		}
	}
}
