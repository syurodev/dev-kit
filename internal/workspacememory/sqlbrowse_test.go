package workspacememory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/storage"
)

func indexFixture(t testing.TB) (*sql.DB, *Service) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{"a", "b"} {
		if _, err = db.Exec(`INSERT INTO workspaces(id,name,root)VALUES(?,?,?)`, ws, ws, "/"+ws); err != nil {
			t.Fatal(err)
		}
	}
	return db, NewService(NewSQLRepository(db))
}
func TestSummaryReadsDoNotParseBodies(t *testing.T) {
	db, s := indexFixture(t)
	ctx := context.Background()
	e, err := s.Create(ctx, "a", Input{Kind: "knowledge", Title: "title", Summary: "summary", Body: "large body"}, Source{})
	if err != nil {
		t.Fatal(err)
	}
	// The authoritative payload could be unavailable/corrupt; summary reads still
	// use their materialized read models rather than transforming full bodies.
	if _, err = db.Exec(`UPDATE workspace_memory SET data='not JSON' WHERE id=?`, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE workspace_memory_revisions SET data='not JSON' WHERE entry_id=?`, e.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Browse(ctx, "a", BrowseInput{Query: "large"}); err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
	if p, err := s.List(ctx, "a", "", 20, 0); err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
	if text, err := s.Context(ctx, "a"); err != nil || !strings.Contains(text, "summary") {
		t.Fatal(text, err)
	}
	if p, err := s.HistoryPage(ctx, "a", e.ID, "", 20); err != nil || len(p.Entries) != 1 {
		t.Fatal(p, err)
	}
}
func queryPlan(t testing.TB, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plans, "\n")
}
func TestBrowseQueryPlansUseScopedIndexes(t *testing.T) {
	db, _ := indexFixture(t)
	in := BrowseInput{}
	cursor := pageCursor{UpdatedAt: "2026-10-01T00:00:00.000000000Z", ID: "entry"}
	statement, args := summaryQuery("a", in, "", cursor, 20, -1, false)
	plan := queryPlan(t, db, statement, args...)
	if !strings.Contains(plan, "workspace_memory_workspace_updated") || strings.Contains(plan, "SCAN m") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatal(plan)
	}
	t.Log("deep browse plan:\n" + plan)
	in.Kind = "decision"
	expression, _ := searchExpression("a", "rare word")
	statement, args = summaryQuery("a", in, expression, cursor, 20, -1, false)
	plan = queryPlan(t, db, statement, args...)
	if !strings.Contains(plan, "SEARCH m USING INTEGER PRIMARY KEY") || !strings.Contains(plan, "VIRTUAL TABLE INDEX") || strings.Contains(plan, "SCAN m") {
		t.Fatal(plan)
	}
	t.Log("scoped search plan:\n" + plan)
	statement, args = summaryQuery("a", in, expression, cursor, 20, -1, true)
	plan = queryPlan(t, db, statement, args...)
	if !strings.Contains(plan, "workspace_memory_workspace_kind_updated") || !strings.Contains(plan, "VIRTUAL TABLE INDEX") || strings.Contains(plan, "SCAN m") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatal(plan)
	}
	t.Log("dense scoped search plan:\n" + plan)
	plan = queryPlan(t, db, `SELECT summary_data FROM workspace_memory_revisions WHERE entry_id=? AND revision<? ORDER BY revision DESC LIMIT ?`, "entry", 5000, 20)
	if !strings.Contains(plan, "SEARCH workspace_memory_revisions USING INDEX") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatal(plan)
	}
	t.Log("deep history plan:\n" + plan)
}

// BenchmarkMemory50K uses the real file-backed SQLite driver, 50,000 entries with
// ~1 KiB bodies and a 50,000-revision history. Fixture creation is outside timings.
func BenchmarkMemory50K(b *testing.B) {
	db, s := indexFixture(b)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	insert, err := tx.Prepare(`INSERT INTO workspace_memory(id,workspace_id,revision,deleted,title,summary,body,updated_at,data,kind,summary_data)VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	revisions, err := tx.Prepare(`INSERT INTO workspace_memory_revisions(entry_id,revision,operation,data,summary_data)VALUES(?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	const now = "2026-10-01T00:00:00.000000000Z"
	for i := range 50000 {
		ws := "a"
		if i%10 == 9 {
			ws = "b"
		}
		body := "shared search text " + strings.Repeat("payload ", 126)
		if i%10000 == 0 {
			body += " rareword"
		}
		e := Entry{ID: fmt.Sprintf("entry%06d", i), WorkspaceID: ws, Kind: "knowledge", Title: "Shared workspace knowledge", Summary: "A representative memory summary", Body: body, Revision: 1, CreatedAt: now, UpdatedAt: now, Source: Source{Caller: "benchmark"}}
		data, _ := json.Marshal(e)
		summary, _ := json.Marshal(entrySummary(e))
		if _, err = insert.Exec(e.ID, e.WorkspaceID, e.Revision, e.Deleted, e.Title, e.Summary, e.Body, e.UpdatedAt, string(data), e.Kind, string(summary)); err != nil {
			b.Fatal(err)
		}
	}
	for i := int64(1); i <= 50000; i++ {
		e := Entry{ID: "entry000000", WorkspaceID: "a", Kind: "knowledge", Title: "Shared workspace knowledge", Summary: "History summary", Body: strings.Repeat("payload ", 128), Revision: i, CreatedAt: now, UpdatedAt: now}
		data, _ := json.Marshal(e)
		summary, _ := json.Marshal(revisionSummary(e, "update"))
		if _, err = revisions.Exec(e.ID, i, "update", string(data), string(summary)); err != nil {
			b.Fatal(err)
		}
	}
	insert.Close()
	revisions.Close()
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	base := BrowseInput{Limit: 20}
	deep := encodeCursor(pageCursor{Binding: browseBinding("a", base), UpdatedAt: now, ID: "entry005000"})
	deepSearchInput := BrowseInput{Query: "shared", Limit: 20}
	deepSearch := encodeCursor(pageCursor{Binding: browseBinding("a", deepSearchInput), UpdatedAt: now, ID: "entry005000"})
	deepHistory := encodeCursor(pageCursor{Binding: cursorBinding("history", "a", "entry000000"), Revision: 5000})
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"list", func() error { _, err := s.List(ctx, "a", "", 20, 0); return err }},
		{"rare_search", func() error { _, err := s.Browse(ctx, "a", BrowseInput{Query: "rareword", Limit: 20}); return err }},
		{"common_search", func() error { _, err := s.Browse(ctx, "a", BrowseInput{Query: "shared", Limit: 20}); return err }},
		{"deep_cursor", func() error { _, err := s.Browse(ctx, "a", BrowseInput{Cursor: deep, Limit: 20}); return err }},
		{"deep_common_search", func() error {
			_, err := s.Browse(ctx, "a", BrowseInput{Query: "shared", Cursor: deepSearch, Limit: 20})
			return err
		}},
		{"history", func() error { _, err := s.HistoryPage(ctx, "a", "entry000000", "", 20); return err }},
		{"deep_history", func() error { _, err := s.HistoryPage(ctx, "a", "entry000000", deepHistory, 20); return err }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := tc.run(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestInvalidCursorStructure(t *testing.T) {
	binding := cursorBinding("test")
	for _, c := range []pageCursor{
		{Binding: binding, UpdatedAt: "invalid", ID: "entry"},
		{Binding: binding, UpdatedAt: "2026-10-01T00:00:00.1Z", ID: "entry"},
		{Binding: binding, UpdatedAt: "2026-10-01T00:00:00.000000000Z", ID: "entry", Revision: 2},
		{Binding: binding, UpdatedAt: "2026-10-01T00:00:00.000000000Z"},
	} {
		if _, err := decodeCursor(encodeCursor(c), binding, false); err != ErrInvalidInput {
			t.Fatal(c, err)
		}
	}
	for _, c := range []pageCursor{{Binding: binding}, {Binding: binding, Revision: -1}, {Binding: binding, Revision: 1, ID: "entry"}} {
		if _, err := decodeCursor(encodeCursor(c), binding, true); err != ErrInvalidInput {
			t.Fatal(c, err)
		}
	}
}
