package workspacememory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"devkit/internal/storage"
	"devkit/internal/workspacememory"
)

var ctx = context.Background()

func setup(t *testing.T) (*workspacememory.Service, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devkit.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err = db.Exec(`INSERT INTO workspaces(id,name,root)VALUES(?,?,?)`, id, id, "/"+id); err != nil {
			t.Fatal(err)
		}
	}
	return workspacememory.NewService(workspacememory.NewSQLRepository(db)), db, path
}
func input() workspacememory.Input {
	return workspacememory.Input{Kind: "decision", Title: "Use shared memory", Summary: "Keep workspace knowledge in DevKit", Body: "Detailed justification"}
}
func TestHistoryDeleteRestoreScopeAndPersistence(t *testing.T) {
	s, db, path := setup(t)
	source := workspacememory.Source{Caller: "agent", SessionID: "claude-session", AgentType: "claude"}
	e, err := s.Create(ctx, "a", input(), source)
	if err != nil {
		t.Fatal(err)
	}
	if e.Revision != 1 || e.Source != source {
		t.Fatal(e)
	}
	if _, err = s.Get(ctx, "b", e.ID); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatalf("cross scope get: %v", err)
	}
	if _, err = s.Update(ctx, "b", e.ID, 1, input(), source); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatalf("cross scope update: %v", err)
	}
	if _, err = s.History(ctx, "b", e.ID, 20, 0); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatalf("cross scope history: %v", err)
	}
	next := input()
	next.Body = "Changed by Codex"
	codex := workspacememory.Source{Caller: "agent", SessionID: "codex-session", AgentType: "codex"}
	e, err = s.Update(ctx, "a", e.ID, 1, next, codex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "a", e.ID, 1, input(), source); !errors.Is(err, workspacememory.ErrConflict) {
		t.Fatal(err)
	}
	e, err = s.Delete(ctx, "a", e.ID, 2, codex)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Deleted || e.Revision != 3 {
		t.Fatal(e)
	}
	page, err := s.List(ctx, "a", "", 20, 0)
	if err != nil || len(page.Entries) != 0 {
		t.Fatal(page, err)
	}
	e, err = s.Restore(ctx, "a", e.ID, 3, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	if e.Deleted || e.Revision != 4 || e.Body != input().Body {
		t.Fatal(e)
	}
	history, err := s.History(ctx, "a", e.ID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Operation != "restore" || history[1].Operation != "delete" || history[2].Entry.Source != codex || history[3].Entry.Source != source {
		t.Fatal(history)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = storage.Migrate(ctx, reopened); err != nil {
		t.Fatal(err)
	}
	other := workspacememory.NewService(workspacememory.NewSQLRepository(reopened))
	stored, err := other.Get(ctx, "a", e.ID)
	if err != nil || stored != e {
		t.Fatal(stored, err)
	}
	if _, err = reopened.Exec(`DELETE FROM workspaces WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workspace_memory", "workspace_memory_revisions"} {
		var n int
		if err = reopened.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}
func TestConcurrentChangesKeepBothHistoryAndConflict(t *testing.T) {
	s, _, _ := setup(t)
	e, err := s.Create(ctx, "a", input(), workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, body := range []string{"first", "second"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			in := input()
			in.Body = body
			_, err := s.Update(ctx, "a", e.ID, 1, in, workspacememory.Source{SessionID: body})
			errs <- err
		}(body)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, workspacememory.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflicts)
	}
	history, err := s.History(ctx, "a", e.ID, 20, 0)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	if history[1].Entry.Body != input().Body {
		t.Fatal(history)
	}
}
func TestValidationPaginationAndLiteralSearch(t *testing.T) {
	s, _, _ := setup(t)
	for _, modify := range []func(*workspacememory.Input){func(i *workspacememory.Input) { i.Kind = "other" }, func(i *workspacememory.Input) { i.Title = " " }, func(i *workspacememory.Input) { i.Summary = "" }, func(i *workspacememory.Input) { i.Body = "" }, func(i *workspacememory.Input) { i.Title = strings.Repeat("a", 257) }, func(i *workspacememory.Input) { i.Summary = strings.Repeat("a", 1025) }, func(i *workspacememory.Input) { i.Body = strings.Repeat("a", 32769) }, func(i *workspacememory.Input) { i.Body = string([]byte{255}) }} {
		in := input()
		modify(&in)
		if _, err := s.Create(ctx, "a", in, workspacememory.Source{}); !errors.Is(err, workspacememory.ErrInvalidInput) {
			t.Fatal(in.Kind, err)
		}
	}
	if _, err := s.Create(ctx, "missing", input(), workspacememory.Source{}); !errors.Is(err, workspacememory.ErrWorkspaceNotFound) {
		t.Fatal(err)
	}
	e, err := s.Create(ctx, "a", input(), workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "a", e.ID, 0, input(), workspacememory.Source{}); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = s.Delete(ctx, "a", e.ID, 0, workspacememory.Source{}); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = s.Restore(ctx, "a", e.ID, 1, 0, workspacememory.Source{}); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	for _, kind := range []string{"convention", "knowledge", "handoff"} {
		in := input()
		in.Kind = kind
		if _, err = s.Create(ctx, "a", in, workspacememory.Source{}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List(ctx, "a", "", 2, 0)
	if err != nil || len(page.Entries) != 2 || !page.HasMore {
		t.Fatal(page, err)
	}
	page, err = s.List(ctx, "a", "", 2, 2)
	if err != nil || len(page.Entries) != 2 || page.HasMore {
		t.Fatal(page, err)
	}
	for _, q := range []string{"%", "_"} {
		page, err = s.List(ctx, "a", q, 20, 0)
		if err != nil || len(page.Entries) != 0 {
			t.Fatal(page, err)
		}
	}
	page, err = s.List(ctx, "a", "JUSTIFICATION", 20, 0)
	if err != nil || len(page.Entries) != 4 {
		t.Fatal(page, err)
	}
	for _, bounds := range [][2]int{{101, 0}, {-1, 0}, {20, -1}, {20, 1000001}} {
		if _, err = s.List(ctx, "a", "", bounds[0], bounds[1]); !errors.Is(err, workspacememory.ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	if _, err = s.List(ctx, "a", strings.Repeat("a", 257), 20, 0); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
}
func TestContextBudgetAndNoBodies(t *testing.T) {
	s, _, _ := setup(t)
	for range 110 {
		in := input()
		in.Summary = strings.Repeat("界", 341)
		in.Title = strings.Repeat("界", 85)
		in.Body = "PRIVATE_BODY_CONTENT"
		if _, err := s.Create(ctx, "a", in, workspacememory.Source{}); err != nil {
			t.Fatal(err)
		}
	}
	text, err := s.Context(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > workspacememory.MaxContextBytes || !utf8.ValidString(text) || strings.Contains(text, "PRIVATE_BODY_CONTENT") {
		t.Fatal(len(text), text)
	}
	var data struct {
		Entries []map[string]any `json:"entries"`
		Omitted bool             `json:"omitted"`
	}
	if err = json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatal(err)
	}
	if !data.Omitted || len(data.Entries) == 0 {
		t.Fatal(data)
	}
	for _, entry := range data.Entries {
		if len(entry) != 5 || entry["id"] == "" || entry["revision"] == nil {
			t.Fatal(entry)
		}
	}
	empty, err := s.Context(ctx, "b")
	if err != nil || empty != `{"entries":[],"omitted":false}` {
		t.Fatal(empty, err)
	}
}

func TestRestoreOldRevisionAndHistoryPagination(t *testing.T) {
	s, _, _ := setup(t)
	e, err := s.Create(ctx, "a", input(), workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	for range 105 {
		next := input()
		next.Body = "new body"
		e, err = s.Update(ctx, "a", e.ID, e.Revision, next, workspacememory.Source{})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.History(ctx, "a", e.ID, 100, 0)
	if err != nil || len(first) != 100 || first[0].Entry.Revision != 106 || first[99].Entry.Revision != 7 {
		t.Fatal(len(first), err)
	}
	second, err := s.History(ctx, "a", e.ID, 100, 100)
	if err != nil || len(second) != 6 || second[5].Entry.Revision != 1 {
		t.Fatal(second, err)
	}
	e, err = s.Restore(ctx, "a", e.ID, 106, 1, workspacememory.Source{})
	if err != nil || e.Revision != 107 || e.Body != input().Body {
		t.Fatal(e, err)
	}
	e, err = s.Delete(ctx, "a", e.ID, 107, workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Restore(ctx, "a", e.ID, 108, 108, workspacememory.Source{}); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = s.Restore(ctx, "a", e.ID, 108, 109, workspacememory.Source{}); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestFailedSnapshotRollsBackMutationAndSanitizesError(t *testing.T) {
	s, db, _ := setup(t)
	e, err := s.Create(ctx, "a", input(), workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_snapshot BEFORE INSERT ON workspace_memory_revisions BEGIN SELECT RAISE(ABORT,'PRIVATE DATABASE DIAGNOSTIC'); END;`); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Body = "must not survive"
	if _, err = s.Update(ctx, "a", e.ID, 1, in, workspacememory.Source{}); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "a", e.ID)
	if err != nil || got != e {
		t.Fatal(got, err)
	}
	history, err := s.History(ctx, "a", e.ID, 20, 0)
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
}
