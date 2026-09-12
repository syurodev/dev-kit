package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleRoundTripKeepsTheTextAndWhereItBelongs(t *testing.T) {
	store := NewConsoleStore(t.TempDir())
	written := Console{
		ID:        "console-1",
		ProfileID: "profile-7",
		Database:  "analytics",
		SQL:       "SELECT *\nFROM orders\nWHERE id = 1;",
	}
	if err := store.Save(written); err != nil {
		t.Fatalf("Save: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 console, got %d", len(list))
	}
	got := list[0]
	if got.SQL != written.SQL {
		t.Errorf("SQL = %q, want %q", got.SQL, written.SQL)
	}
	if got.ProfileID != "profile-7" || got.Database != "analytics" {
		t.Errorf("metadata did not survive: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt was not stamped")
	}
}

// The header is ours, not the user's. It must never reach the editor — the
// text that comes back is the text that was typed, byte for byte.
func TestConsoleHeaderNeverReachesTheEditor(t *testing.T) {
	dir := t.TempDir()
	store := NewConsoleStore(dir)
	if err := store.Save(Console{ID: "c1", ProfileID: "p1", SQL: "SELECT 1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "c1.sql"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(raw), "-- devkit-console {") {
		t.Fatalf("the file does not say what it is for: %q", raw)
	}
	if !strings.Contains(string(raw), `"profile_id":"p1"`) {
		t.Errorf("the connection is not in the file: %q", raw)
	}

	list, _ := store.List()
	if list[0].SQL != "SELECT 1" {
		t.Errorf("SQL = %q, want the statement alone", list[0].SQL)
	}
}

/*
A console id becomes a file name, and the id comes from the window.

"../../devkit.db" as an id must not be cleaned into a path inside the
directory and written to — an id that needs cleaning came from somewhere it
should not have, and the cleaned path is the bug, not the fix.
*/
func TestConsoleIDCannotLeaveItsDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewConsoleStore(dir)
	for _, id := range []string{"../escape", "a/b", `..\escape`, "", strings.Repeat("x", 65), "c:1"} {
		if err := store.Save(Console{ID: id, SQL: "SELECT 1"}); err == nil {
			t.Errorf("Save(%q) was allowed", id)
		}
		if err := store.Delete(id); err == nil {
			t.Errorf("Delete(%q) was allowed", id)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("a rejected id still wrote something: %v", entries)
	}
}

// Losing the statement because the metadata went bad keeps the wrong half.
func TestConsoleWithAnUnreadableHeaderKeepsItsStatement(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "c2.sql"), []byte("-- devkit-console {oops\nSELECT 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := NewConsoleStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !strings.Contains(list[0].SQL, "SELECT 2") {
		t.Fatalf("the statement was dropped with the header: %+v", list)
	}
}

// A .sql someone dropped in the folder by hand is still a console.
func TestConsoleWithNoHeaderIsStillAConsole(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hand-written.sql"), []byte("SELECT 3"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := NewConsoleStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "hand-written" || list[0].SQL != "SELECT 3" {
		t.Fatalf("got %+v", list)
	}
}

func TestConsoleListBeforeAnythingIsStoredIsEmptyNotAnError(t *testing.T) {
	list, err := NewConsoleStore(filepath.Join(t.TempDir(), "never-created")).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("got %+v", list)
	}
}

func TestConsoleDeleteIsIdempotentAndLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	store := NewConsoleStore(dir)
	if err := store.Save(Console{ID: "c3", SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("c3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete("c3"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("the temporary file outlived the save: %v", entries)
	}
}

// Saving twice replaces, and leaves no .tmp beside the file it replaced.
func TestConsoleSaveReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	store := NewConsoleStore(dir)
	for _, sql := range []string{"SELECT 1", "SELECT 2"} {
		if err := store.Save(Console{ID: "c4", SQL: sql}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("want one file, got %v", entries)
	}
	list, _ := store.List()
	if list[0].SQL != "SELECT 2" {
		t.Errorf("SQL = %q", list[0].SQL)
	}
}
