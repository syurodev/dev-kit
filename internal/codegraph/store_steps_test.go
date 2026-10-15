package codegraph

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// A call site's step survives the store, in source order, with its frames.
func TestStoreKeepsCallSiteSteps(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{method("a"), method("b"), method("c")},
		CallSites: []CallSite{
			{From: "a", To: "", Line: 9, Col: 5, Unresolved: "library", Text: "log.warn(x)", CalleeKey: "org.slf4j.Logger#warn",
				Receiver: "field", ResultUse: "discarded"},
			{From: "a", To: "b", Line: 9, Col: 1, Text: "b()", CalleeKey: "acme#b", Receiver: "this", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "8:3", Kind: "catch", Text: "catch E", Exits: true}}, Target: "b"},
			{From: "b", To: "c", Line: 2, Role: RoleOverride},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sites, err := store.GetStepSites(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 3 || sites[0].To != "b" || sites[0].Target != "b" || sites[1].CalleeKey != "org.slf4j.Logger#warn" || sites[2].Role != RoleOverride {
		t.Fatalf("sites = %+v", sites)
	}
	if f := sites[0].Frames; len(f) != 1 || f[0] != (StepFrame{ID: "8:3", Kind: "catch", Text: "catch E", Exits: true}) {
		t.Fatalf("frames = %+v", f)
	}
	stats, err := store.CallStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Methods != 3 || stats.Callers["org.slf4j.Logger#warn"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// An index written before steps opens and reads its call sites with no step.
func TestStoreAddsStepColumnsToAnOldIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE call_sites (from_id TEXT NOT NULL, to_id TEXT NOT NULL, line INTEGER NOT NULL, unresolved TEXT NOT NULL);
INSERT INTO call_sites VALUES ('a', 'b', 3, '');
CREATE TABLE kafka_accesses (from_id TEXT NOT NULL, topic TEXT NOT NULL, role TEXT NOT NULL, client TEXT NOT NULL,
	group_id TEXT NOT NULL, is_pattern INTEGER NOT NULL, file TEXT NOT NULL, line INTEGER NOT NULL);
INSERT INTO kafka_accesses VALUES ('a', 'orders', 'produce', 'kafka-template', '', 0, 'a.java', 3);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := newSQLiteStore(path)
	if err != nil {
		t.Fatalf("open old index: %v", err)
	}
	defer store.Close()
	sites, err := store.GetStepSites(t.Context(), []string{"a"})
	if err != nil || len(sites) != 1 || sites[0].Text != "" || sites[0].Frames != nil || sites[0].Target != "" {
		t.Fatalf("sites = %+v, err = %v", sites, err)
	}
	effects, err := store.ListEffectsFrom(t.Context(), []string{"a"})
	if err != nil || len(effects) != 1 || effects[0].Line != 3 || effects[0].Col != 0 {
		t.Fatalf("effects = %+v, err = %v", effects, err)
	}
}

// What extractors saw in a method comes back keyed by the line it is on.
func TestStoreListsEffectsAndFieldWriters(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes:         []Node{method("a"), method("b")},
		KafkaAccesses: []KafkaAccess{{From: "a", Topic: "orders", Role: KafkaProduce, Client: KafkaTemplate, File: "a.java", Line: 4, Col: 9}},
		RedisAccesses: []RedisAccess{{From: "a", Key: "k:{id}", Access: "write", Command: "set", File: "a.java", Line: 5, Col: 33}},
		TableAccesses: []TableAccess{{From: "b", Table: "orders", Access: "read", Via: "jdbc", File: "b.java", Line: 2, Col: 7}},
		OutboundCalls: []OutboundCall{{From: "b", File: "b.java", Line: 3, Col: 12, Client: "rest-template", HTTPMethod: "GET", URL: "/x"}},
		FieldAccesses: []FieldAccess{{ClassID: "C", NodeID: "b", FieldName: "state", Kind: "write", Line: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := store.ListEffectsFrom(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []StepEffectAt{
		{From: "a", Line: 4, Col: 9, Effect: StepEffect{Kind: "kafka", Role: "produce", Target: "orders"}},
		{From: "a", Line: 5, Col: 33, Effect: StepEffect{Kind: "redis", Role: "write", Target: "k:{id}"}},
		{From: "b", Line: 2, Col: 7, Effect: StepEffect{Kind: "db", Role: "read", Target: "orders"}},
		{From: "b", Line: 3, Col: 12, Effect: StepEffect{Kind: "http", Role: "call", Target: "GET /x"}},
	}
	if !reflect.DeepEqual(effects, want) {
		t.Fatalf("effects = %+v", effects)
	}
	writers, err := store.FieldWriters(ctx, []string{"a", "b"})
	if err != nil || !writers["b"] || writers["a"] {
		t.Fatalf("writers = %v, err = %v", writers, err)
	}
}

// CallStats is read once per index, and a re-index is the only thing that
// changes call_sites, so it is cached until the next one.
func TestStoreCallStatsFollowsAReindex(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	index := func(callers ...string) {
		t.Helper()
		result := AnalysisResult{Nodes: []Node{method("x")}}
		for _, c := range callers {
			result.Nodes = append(result.Nodes, method(c))
			result.CallSites = append(result.CallSites, CallSite{From: c, Line: 2, Unresolved: "library", CalleeKey: "acme.Audit#record"})
		}
		if err := store.ReplaceIndex(ctx, result); err != nil {
			t.Fatal(err)
		}
	}
	index("a")
	if stats, err := store.CallStats(ctx); err != nil || stats.Callers["acme.Audit#record"] != 1 || stats.Methods != 2 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
	index("a", "b", "c")
	if stats, err := store.CallStats(ctx); err != nil || stats.Callers["acme.Audit#record"] != 3 || stats.Methods != 4 {
		t.Fatalf("after a re-index, stats = %+v, err = %v", stats, err)
	}
}

// The step lookups read these tables by caller; without an index each one
// is a scan of the whole table, per batch.
func TestStoreIndexesTheStepLookups(t *testing.T) {
	store := newTestStore(t)
	for _, name := range []string{"idx_kafka_accesses_from", "idx_redis_accesses_from", "idx_table_accesses_from", "idx_field_accesses_node"} {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil || n != 1 {
			t.Fatalf("index %s: n = %d, err = %v", name, n, err)
		}
	}
}

// Two opens of one old index can both see a column missing and both add it.
// The second ALTER fails with "duplicate column name"; that is the column
// being there, which is what the migration wanted.
func TestAddColumnToleratesAColumnAddedConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE edges (from_id TEXT NOT NULL, to_id TEXT NOT NULL, kind TEXT NOT NULL, guard TEXT NOT NULL DEFAULT '');
CREATE TABLE call_sites (from_id TEXT NOT NULL, to_id TEXT NOT NULL, line INTEGER NOT NULL, unresolved TEXT NOT NULL,
	col INTEGER NOT NULL DEFAULT 0);`); err != nil {
		t.Fatal(err)
	}
	// What the other open did between this one's check and its ALTER.
	if err := addColumn(db, "edges", "guard", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("guard added twice: %v", err)
	}
	if err := addColumn(db, "call_sites", "col", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		t.Fatalf("col added twice: %v", err)
	}
	// Any other failure is still one.
	if err := addColumn(db, "no_such_table", "col", "INTEGER NOT NULL DEFAULT 0"); err == nil {
		t.Fatal("ALTER of a missing table succeeded")
	}
}
