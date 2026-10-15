package codegraph

import (
	"database/sql"
	"path/filepath"
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
				Frames: []StepFrame{{ID: "8:3", Kind: "catch", Text: "catch E", Exits: true}}},
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
	if len(sites) != 3 || sites[0].To != "b" || sites[1].CalleeKey != "org.slf4j.Logger#warn" || sites[2].Role != RoleOverride {
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
INSERT INTO call_sites VALUES ('a', 'b', 3, '');`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := newSQLiteStore(path)
	if err != nil {
		t.Fatalf("open old index: %v", err)
	}
	defer store.Close()
	sites, err := store.GetStepSites(t.Context(), []string{"a"})
	if err != nil || len(sites) != 1 || sites[0].Text != "" || sites[0].Frames != nil {
		t.Fatalf("sites = %+v, err = %v", sites, err)
	}
}

// What extractors saw in a method comes back keyed by the line it is on.
func TestStoreListsEffectsAndFieldWriters(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes:         []Node{method("a"), method("b")},
		KafkaAccesses: []KafkaAccess{{From: "a", Topic: "orders", Role: KafkaProduce, Client: KafkaTemplate, File: "a.java", Line: 4}},
		RedisAccesses: []RedisAccess{{From: "a", Key: "k:{id}", Access: "write", Command: "set", File: "a.java", Line: 5}},
		FieldAccesses: []FieldAccess{{ClassID: "C", NodeID: "b", FieldName: "state", Kind: "write", Line: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := store.ListEffectsFrom(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 2 || effects[0].Line != 4 || effects[0].Effect != (StepEffect{Kind: "kafka", Role: "produce", Target: "orders"}) ||
		effects[1].Effect != (StepEffect{Kind: "redis", Role: "write", Target: "k:{id}"}) {
		t.Fatalf("effects = %+v", effects)
	}
	writers, err := store.FieldWriters(ctx, []string{"a", "b"})
	if err != nil || !writers["b"] || writers["a"] {
		t.Fatalf("writers = %v, err = %v", writers, err)
	}
}
