package codegraph

import (
	"errors"
	"fmt"
	"testing"
)

func pagingEntries(n int) []EntryPoint {
	entries := make([]EntryPoint, n)
	for i := range entries {
		kind, service := EntryKindHTTP, "billing"
		if i%3 == 0 {
			kind, service = EntryKindMessage, "orders"
		}
		entries[i] = EntryPoint{
			ID: fmt.Sprintf("ep%03d", i), Verb: "GET", Target: fmt.Sprintf("/v1/r%03d", i),
			Kind: kind, Service: service, Owner: "com.acme.Ctl",
		}
	}
	return entries
}

// Walking the cursors visits every match once, in order, and Total is the
// count of the whole filtered set rather than of the page.
func TestPageEntryPointsWalksEveryRowOnce(t *testing.T) {
	entries := pagingEntries(25)
	q := EntryPointQuery{Limit: 10}
	var seen []string
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("cursor never ended")
		}
		page, err := PageEntryPoints(entries, q)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 25 {
			t.Fatalf("total = %d, want 25", page.Total)
		}
		for _, ep := range page.Result {
			seen = append(seen, ep.ID)
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 25 {
		t.Fatalf("saw %d rows, want 25", len(seen))
	}
	for i, id := range seen {
		if id != entries[i].ID {
			t.Fatalf("row %d = %s, want %s", i, id, entries[i].ID)
		}
	}
}

func TestPageEntryPointsFilters(t *testing.T) {
	entries := pagingEntries(30)
	count := func(q EntryPointQuery) int {
		t.Helper()
		page, err := PageEntryPoints(entries, q)
		if err != nil {
			t.Fatal(err)
		}
		return page.Total
	}
	if got := count(EntryPointQuery{Kind: EntryKindMessage}); got != 10 {
		t.Fatalf("kind = %d, want 10", got)
	}
	if got := count(EntryPointQuery{Service: "billing", Kind: EntryKindHTTP}); got != 20 {
		t.Fatalf("service+kind = %d, want 20", got)
	}
	if got := count(EntryPointQuery{PathPrefix: "/v1/r00"}); got != 10 {
		t.Fatalf("pathPrefix = %d, want 10", got)
	}
	if got := count(EntryPointQuery{Verb: "get"}); got != 30 {
		t.Fatalf("verb is case-insensitive, got %d", got)
	}
	if got := count(EntryPointQuery{Contains: "R029"}); got != 1 {
		t.Fatalf("contains = %d, want 1", got)
	}
	if got := count(EntryPointQuery{Verb: "POST"}); got != 0 {
		t.Fatalf("no match = %d, want 0", got)
	}
}

func TestPageCamelEntriesSearchesRouteAndConsumerMetadata(t *testing.T) {
	entries := []EntryPoint{{ID: "route", Kind: EntryKindMessage, Target: "kafka:events", RouteID: "webhook-dispatch", Topic: "events", ConsumerGroup: "webhook-group"}}
	for _, query := range []string{"webhook-dispatch", "webhook-group", "events"} {
		page, err := PageEntryPoints(entries, EntryPointQuery{Contains: query})
		if err != nil || page.Total != 1 {
			t.Fatalf("%s: %+v %v", query, page, err)
		}
	}
}

func TestPageEntryPointsEmptyResultIsNotNull(t *testing.T) {
	page, err := PageEntryPoints(nil, EntryPointQuery{})
	if err != nil || page.Result == nil || len(page.Result) != 0 || page.NextCursor != "" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestPageEntryPointsLimit(t *testing.T) {
	entries := pagingEntries(DefaultEntryPointPage + 20)
	page, _ := PageEntryPoints(entries, EntryPointQuery{})
	if len(page.Result) != DefaultEntryPointPage {
		t.Fatalf("default page = %d rows", len(page.Result))
	}
	entries = pagingEntries(MaxEntryPointPage + 20)
	page, _ = PageEntryPoints(entries, EntryPointQuery{Limit: 100000})
	if len(page.Result) != MaxEntryPointPage {
		t.Fatalf("limit is capped, got %d rows", len(page.Result))
	}
	if _, err := PageEntryPoints(entries, EntryPointQuery{Limit: -1}); err == nil {
		t.Fatal("a negative limit must be refused")
	}
}

// A cursor belongs to one query, and to a row that still exists.
func TestPageEntryPointsRejectsForeignAndStaleCursors(t *testing.T) {
	entries := pagingEntries(20)
	page, _ := PageEntryPoints(entries, EntryPointQuery{Limit: 5})
	if page.NextCursor == "" {
		t.Fatal("expected a next cursor")
	}
	for name, q := range map[string]EntryPointQuery{
		"other filters": {Kind: EntryKindMessage, Cursor: page.NextCursor},
		"garbage":       {Cursor: "not-a-cursor!"},
		"not json":      {Cursor: "aGVsbG8"},
	} {
		if _, err := PageEntryPoints(entries, q); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("%s: err = %v, want ErrInvalidCursor", name, err)
		}
	}
	// The row the cursor points at vanished (a re-index).
	if _, err := PageEntryPoints(entries[10:], EntryPointQuery{Limit: 5, Cursor: page.NextCursor}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("stale cursor: err = %v, want ErrInvalidCursor", err)
	}
	// Rows added elsewhere do not disturb it.
	more := append([]EntryPoint{{ID: "ep-new", Kind: EntryKindHTTP, Target: "/v0"}}, entries...)
	if _, err := PageEntryPoints(more, EntryPointQuery{Limit: 5, Cursor: page.NextCursor}); err != nil {
		t.Fatalf("cursor after an unrelated insert: %v", err)
	}
}

func symbolStore(t *testing.T) *sqliteStore {
	t.Helper()
	store := newTestStore(t)
	var nodes []Node
	for i := 0; i < 12; i++ {
		nodes = append(nodes, Node{
			ID: fmt.Sprintf("m%02d", i), Label: fmt.Sprintf("com.acme.ScopeService.m%02d", i),
			Kind: "method", Layer: "service", FilePath: "billing/src/ScopeService.java", Line: i + 1, EndLine: i + 1,
		})
	}
	nodes = append(nodes,
		Node{ID: "c1", Label: "com.acme.ScopeService", Kind: "class", Layer: "service", FilePath: "billing/src/ScopeService.java", Line: 1, EndLine: 99},
		Node{ID: "c2", Label: "com.acme.ApiScope", Kind: "class", Layer: "unknown", FilePath: "orders/src/ApiScope.java", Line: 1, EndLine: 9},
		Node{ID: "c3", Label: "com.acme.Scope", Kind: "class", Layer: "unknown", FilePath: "orders/src/Scope.java", Line: 1, EndLine: 9},
		Node{ID: "c4", Label: "com.acme.get_name", Kind: "method", Layer: "unknown", FilePath: "orders/src/Scope.java", Line: 3, EndLine: 4},
	)
	if err := store.ReplaceIndex(t.Context(), AnalysisResult{Nodes: nodes}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSearchSymbolsPagesWithKeyset(t *testing.T) {
	store := symbolStore(t)
	q := SymbolQuery{Query: "ScopeService", Limit: 5}
	var seen []string
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("cursor never ended")
		}
		page, err := store.SearchSymbols(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 13 {
			t.Fatalf("total = %d, want 13", page.Total)
		}
		for _, m := range page.Result {
			seen = append(seen, m.NodeID)
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 13 {
		t.Fatalf("saw %d, want 13: %v", len(seen), seen)
	}
	uniq := map[string]bool{}
	for _, id := range seen {
		uniq[id] = true
	}
	if len(uniq) != 13 {
		t.Fatalf("a row was served twice: %v", seen)
	}
}

func TestSearchSymbolsMatchModesAndFilters(t *testing.T) {
	store := symbolStore(t)
	ids := func(q SymbolQuery) map[string]bool {
		t.Helper()
		page, err := store.SearchSymbols(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range page.Result {
			out[m.NodeID] = true
		}
		return out
	}
	// contains finds ApiScope; prefix and exact do not.
	if got := ids(SymbolQuery{Query: "Scope", Limit: 200}); !got["c2"] {
		t.Fatal("contains must find ApiScope")
	}
	prefix := ids(SymbolQuery{Query: "Scope", Match: MatchPrefix, Limit: 200})
	if prefix["c2"] || !prefix["c1"] || !prefix["c3"] {
		t.Fatalf("prefix = %v", prefix)
	}
	exact := ids(SymbolQuery{Query: "Scope", Match: MatchExact, Limit: 200})
	if len(exact) != 1 || !exact["c3"] {
		t.Fatalf("exact = %v, want only c3", exact)
	}
	if got := ids(SymbolQuery{Query: "com.acme.Scope", Match: MatchExact}); !got["c3"] || len(got) != 1 {
		t.Fatalf("exact on the whole label = %v", got)
	}
	// kind, layer, path prefix.
	if got := ids(SymbolQuery{Query: "Scope", Kind: "class", Limit: 200}); len(got) != 3 {
		t.Fatalf("kind=class = %v", got)
	}
	if got := ids(SymbolQuery{Query: "Scope", Layer: "unknown", Limit: 200}); len(got) != 2 || !got["c2"] || !got["c3"] {
		t.Fatalf("layer=unknown = %v", got)
	}
	if got := ids(SymbolQuery{Query: "Scope", PathPrefix: "orders/", Limit: 200}); len(got) != 2 || !got["c2"] || !got["c3"] {
		t.Fatalf("pathPrefix = %v", got)
	}
	// An empty query with a filter lists by the filter.
	if got := ids(SymbolQuery{Layer: "service", Limit: 200}); len(got) != 13 {
		t.Fatalf("filter only = %d, want 13", len(got))
	}
	// LIKE wildcards stay literal.
	if got := ids(SymbolQuery{Query: "get_name"}); len(got) != 1 || !got["c4"] {
		t.Fatalf("underscore = %v", got)
	}
	if got := ids(SymbolQuery{Query: "%"}); len(got) != 0 {
		t.Fatalf("percent = %v", got)
	}
}

func TestSearchSymbolsRejectsBadInput(t *testing.T) {
	store := symbolStore(t)
	if _, err := store.SearchSymbols(t.Context(), SymbolQuery{Query: "x", Match: "fuzzy"}); err == nil {
		t.Fatal("unknown match mode must be refused")
	}
	page, _ := store.SearchSymbols(t.Context(), SymbolQuery{Query: "Scope", Limit: 2})
	if _, err := store.SearchSymbols(t.Context(), SymbolQuery{Query: "Other", Limit: 2, Cursor: page.NextCursor}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor from another query: %v", err)
	}
}
