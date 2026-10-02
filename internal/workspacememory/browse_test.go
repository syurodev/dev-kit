package workspacememory_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devkit/internal/workspacememory"
)

func TestIndexedSearchUnicodeLiteralWordsAndWorkspaceIsolation(t *testing.T) {
	s, db, _ := setup(t)
	first := workspacememory.Input{Kind: "knowledge", Title: "Kiến trúc bộ nhớ", Summary: "Quyết định dùng chung workspace", Body: "Mô hình lưu trữ Việt Nam development flow"}
	a, err := s.Create(ctx, "a", first, workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	onlyB := first
	onlyB.Title = "isolated needle"
	if _, err = s.Create(ctx, "b", onlyB, workspacememory.Source{}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		query string
		want  int
	}{
		{"KIEN TRUC", 1}, {"kiến trúc", 1}, {"bo nho", 1}, {"viet nam", 1}, {"quyet dung", 1}, {"develop flo", 1},
		{"\"kien\" + \"truc\"", 1}, {"title:kien", 0}, {"needle", 0}, {"development OR needle", 0}, {"% _ \" + * :", 0}, {"absent", 0}, {"velopment", 0}, {"\u0301", 0},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			p, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Query: tc.query})
			if err != nil || len(p.Entries) != tc.want {
				t.Fatalf("query %q entries=%d err=%v", tc.query, len(p.Entries), err)
			}
		})
	}
	updated := first
	updated.Title = "replacement"
	updated.Summary = "updated"
	updated.Body = "New unique searchable content"
	a, err = s.Update(ctx, "a", a.ID, 1, updated, workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "kien"})
	if err != nil || len(p.Entries) != 0 {
		t.Fatal(p, err)
	}
	p, err = s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "unique"})
	if err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
	a, err = s.Delete(ctx, "a", a.ID, 2, workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "unique"})
	if err != nil || len(p.Entries) != 0 {
		t.Fatal(p, err)
	}
	p, err = s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "unique", Deleted: true})
	if err != nil || len(p.Entries) != 1 || !p.Entries[0].Deleted {
		t.Fatal(p, err)
	}
	if _, err = s.Restore(ctx, "a", a.ID, 3, 1, workspacememory.Source{}); err != nil {
		t.Fatal(err)
	}
	p, err = s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "kien truc"})
	if err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
	if _, err = db.Exec(`DELETE FROM workspaces WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM workspace_memory_fts WHERE workspace_memory_fts MATCH 'workspace_key:w61'`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	p, err = s.Browse(ctx, "b", workspacememory.BrowseInput{Query: "needle"})
	if err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
}
func TestBrowseKeysetCursorBindingAndLatePages(t *testing.T) {
	s, db, _ := setup(t)
	entries := make([]workspacememory.Entry, 0)
	for i := range 53 {
		in := input()
		if i%2 == 0 {
			in.Kind = "convention"
		}
		e, err := s.Create(ctx, "a", in, workspacememory.Source{})
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	// Equal timestamp ties must use the entry ID to progress without duplicates.
	if _, err := db.Exec(`UPDATE workspace_memory SET updated_at='2026-10-01T00:00:00.000000000Z', summary_data=json_set(summary_data,'$.updatedAt','2026-10-01T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	first, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Limit: 7})
	if err != nil || len(first.Entries) != 7 || !first.HasMore || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	for _, in := range []workspacememory.BrowseInput{{Limit: 7, Cursor: first.NextCursor, Kind: "decision"}, {Limit: 7, Cursor: first.NextCursor, Query: "shared"}, {Limit: 7, Cursor: first.NextCursor, Deleted: true}, {Limit: 7, Cursor: "garbage"}, {Limit: 7, Cursor: strings.Repeat("a", 2049)}, {Kind: "invalid"}, {Limit: 101}} {
		if _, err = s.Browse(ctx, "a", in); !errors.Is(err, workspacememory.ErrInvalidInput) {
			t.Fatal(in, err)
		}
	}
	if _, err = s.Browse(ctx, "b", workspacememory.BrowseInput{Cursor: first.NextCursor}); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	// A newer insert between pages should not shift the boundary and repeat rows.
	if _, err = s.Create(ctx, "a", input(), workspacememory.Source{}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range first.Entries {
		seen[e.ID] = true
	}
	cursor := first.NextCursor
	for cursor != "" {
		p, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Entries {
			if seen[e.ID] {
				t.Fatalf("duplicate %s", e.ID)
			}
			seen[e.ID] = true
		}
		if p.HasMore && p.NextCursor == "" {
			t.Fatal("missing next cursor")
		}
		cursor = p.NextCursor
	}
	if len(seen) != 53 {
		t.Fatal(len(seen))
	}
	page, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Kind: "convention", Limit: 100})
	if err != nil || len(page.Entries) != 27 || page.HasMore {
		t.Fatal(page, err)
	}
	for _, entry := range page.Entries {
		if entry.Kind != "convention" {
			t.Fatal(entry)
		}
	}
}
func TestHistoryPageMetadataAndReadRevision(t *testing.T) {
	s, _, _ := setup(t)
	e, err := s.Create(ctx, "a", input(), workspacememory.Source{SessionID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	for range 24 {
		e, err = s.Update(ctx, "a", e.ID, e.Revision, input(), workspacememory.Source{SessionID: "updater"})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.HistoryPage(ctx, "a", e.ID, "", 7)
	if err != nil || len(first.Entries) != 7 || !first.HasMore || first.Entries[0].Revision != 25 {
		t.Fatal(first, err)
	}
	text, _ := json.Marshal(first)
	if strings.Contains(string(text), "Detailed justification") || strings.Contains(string(text), `"body"`) {
		t.Fatal(string(text))
	}
	if _, err = s.HistoryPage(ctx, "b", e.ID, "", 7); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.HistoryPage(ctx, "b", e.ID, first.NextCursor, 7); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	other, err := s.Create(ctx, "a", input(), workspacememory.Source{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.HistoryPage(ctx, "a", other.ID, first.NextCursor, 7); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = s.HistoryPage(ctx, "a", e.ID, "bad", 7); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	revision, err := s.ReadRevision(ctx, "a", e.ID, 1)
	if err != nil || revision.Entry.Body != input().Body || revision.Entry.Source.SessionID != "creator" {
		t.Fatal(revision, err)
	}
	if _, err = s.ReadRevision(ctx, "b", e.ID, 1); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.ReadRevision(ctx, "a", e.ID, 0); !errors.Is(err, workspacememory.ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = s.ReadRevision(ctx, "a", e.ID, 26); !errors.Is(err, workspacememory.ErrNotFound) {
		t.Fatal(err)
	}
	count := len(first.Entries)
	cursor := first.NextCursor
	last := first.Entries[len(first.Entries)-1].Revision
	for cursor != "" {
		p, err := s.HistoryPage(ctx, "a", e.ID, cursor, 7)
		if err != nil {
			t.Fatal(err)
		}
		for _, rev := range p.Entries {
			if rev.Revision >= last {
				t.Fatal(rev, last)
			}
			last = rev.Revision
			count++
		}
		cursor = p.NextCursor
	}
	if count != 25 || last != 1 {
		t.Fatal(count, last)
	}
}

func TestDenseSearchKindDeletedCursorAndScope(t *testing.T) {
	s, _, _ := setup(t)
	for i := range 160 {
		in := input()
		in.Title = "Common searchable word"
		if i%3 == 0 {
			in.Kind = "convention"
		}
		e, err := s.Create(ctx, "a", in, workspacememory.Source{})
		if err != nil {
			t.Fatal(err)
		}
		if i%4 == 0 {
			if _, err = s.Delete(ctx, "a", e.ID, 1, workspacememory.Source{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 20 {
		in := input()
		in.Title = "Common searchable word"
		if _, err := s.Create(ctx, "b", in, workspacememory.Source{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		kind    string
		deleted bool
		want    int
	}{{"convention", false, 40}, {"decision", true, 26}} {
		cursor := ""
		seen := map[string]bool{}
		for {
			p, err := s.Browse(ctx, "a", workspacememory.BrowseInput{Query: "common", Kind: tc.kind, Deleted: tc.deleted, Cursor: cursor, Limit: 13})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range p.Entries {
				if e.WorkspaceID != "a" || e.Kind != tc.kind || e.Deleted != tc.deleted || seen[e.ID] {
					t.Fatal(e)
				}
				seen[e.ID] = true
			}
			if !p.HasMore {
				break
			}
			cursor = p.NextCursor
		}
		if len(seen) != tc.want {
			t.Fatal(tc, len(seen))
		}
	}
	page, err := s.Browse(ctx, "b", workspacememory.BrowseInput{Query: "common", Limit: 100})
	if err != nil || len(page.Entries) != 20 {
		t.Fatal(page, err)
	}
	for _, e := range page.Entries {
		if e.WorkspaceID != "b" {
			t.Fatal(e)
		}
	}
}
