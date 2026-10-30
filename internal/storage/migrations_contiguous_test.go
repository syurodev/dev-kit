package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// Migrations are numbered 1..N with no gap and no repeat: a gap means one was lost
// or renumbered after it shipped, which is how two branches end up disagreeing about
// what a version means. (A duplicate is refused earlier, by the runner.)
func TestMigrationVersionsAreContiguous(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := 1
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != want {
			t.Fatalf("migration versions are not contiguous: found %d where %d was expected", v, want)
		}
		want++
	}
	if want == 1 {
		t.Fatal("no migrations applied")
	}
}
