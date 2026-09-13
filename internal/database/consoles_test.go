package database

import (
	"os"
	"path/filepath"
	"runtime"
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

/*
Changing the folder takes the consoles with it.

Leaving them behind is the version that generates the bug report: the
setting changes, the next launch reads an empty folder, and every console
the user had is gone as far as the window is concerned.
*/
func TestConsolesFollowTheFolderTheyAreMovedTo(t *testing.T) {
	home, elsewhere := t.TempDir(), filepath.Join(t.TempDir(), "sql")
	store := NewConsoleStore(home)
	if err := store.Save(Console{ID: "c1", ProfileID: "p1", SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}

	if err := store.SetDir(elsewhere); err != nil {
		t.Fatalf("SetDir: %v", err)
	}

	if store.Dir() != elsewhere {
		t.Fatalf("Dir = %q", store.Dir())
	}
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].SQL != "SELECT 1" || list[0].ProfileID != "p1" {
		t.Fatalf("the console did not come along: %+v", list)
	}
	if left, _ := os.ReadDir(home); len(left) != 0 {
		t.Fatalf("a copy was left behind: %v", left)
	}
}

// Nothing here is worth overwriting somebody's file for.
func TestMovingNeverOverwritesAFileAlreadyThere(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	store := NewConsoleStore(home)
	if err := store.Save(Console{ID: "c1", SQL: "SELECT moving"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "c1.sql"), []byte("SELECT theirs"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.SetDir(elsewhere); err != nil {
		t.Fatalf("SetDir: %v", err)
	}

	kept, _ := os.ReadFile(filepath.Join(elsewhere, "c1.sql"))
	if string(kept) != "SELECT theirs" {
		t.Errorf("the file that was already there was overwritten: %q", kept)
	}
	// And the one that could not move is still where it was, not deleted.
	if _, err := os.Stat(filepath.Join(home, "c1.sql")); err != nil {
		t.Errorf("the console that did not move was removed anyway: %v", err)
	}
}

// The setting only changes if the move did, so the folder shown is always
// the folder the consoles are in.
func TestAFolderThatCannotBeUsedLeavesTheSettingAlone(t *testing.T) {
	home := t.TempDir()
	store := NewConsoleStore(home)
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"relative/path", file} {
		if err := store.SetDir(dir); err == nil {
			t.Errorf("SetDir(%q) was allowed", dir)
		}
		if store.Dir() != home {
			t.Fatalf("the setting moved anyway: %q", store.Dir())
		}
	}
}

func TestEmptyMeansBackToTheDefault(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	store := NewConsoleStore(home)
	if err := store.SetDir(elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDir(""); err != nil {
		t.Fatalf("SetDir(\"\"): %v", err)
	}
	if store.Dir() != home {
		t.Fatalf("Dir = %q, want the default %q", store.Dir(), home)
	}
}

// #50. A console folder can be a project's folder, and its own .sql files are
// not consoles.
func TestMovingTakesOnlyConsoles(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	store := NewConsoleStore(home)
	if err := store.Save(Console{ID: "c1", SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "migrations.sql"), []byte("CREATE TABLE t ()"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.SetDir(elsewhere); err != nil {
		t.Fatalf("SetDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "migrations.sql")); err != nil {
		t.Errorf("a file that is not a console was moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "migrations.sql")); err == nil {
		t.Error("a file that is not a console arrived in the new folder")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "c1.sql")); err != nil {
		t.Errorf("the console did not move: %v", err)
	}
}

// #50. A move that fails partway leaves every console where the setting says
// they are.
func TestAFailedMoveLeavesEveryConsoleWhereItWas(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file its owner cannot read")
	}
	home, elsewhere := t.TempDir(), t.TempDir()
	store := NewConsoleStore(home)
	for _, id := range []string{"c1", "c2"} {
		if err := store.Save(Console{ID: id, SQL: "SELECT " + id}); err != nil {
			t.Fatal(err)
		}
	}
	unreadable := filepath.Join(home, "c2.sql")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	if err := store.SetDir(elsewhere); err == nil {
		t.Fatal("SetDir succeeded with a console it could not read")
	}
	if store.Dir() != home {
		t.Fatalf("Dir = %q after a failed move", store.Dir())
	}
	if _, err := os.Stat(filepath.Join(home, "c1.sql")); err != nil {
		t.Errorf("c1 left the folder the setting still names: %v", err)
	}
	if left, _ := os.ReadDir(elsewhere); len(left) != 0 {
		t.Errorf("a half-finished move left copies behind: %v", left)
	}
}
