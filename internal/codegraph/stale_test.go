package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// namedAnalyzer is an analyzer that can say what it is — the property a
// toolchain's analyzer has in production — optionally held until released.
type namedAnalyzer struct {
	id      string
	gate    chan struct{} // nil: answers at once
	fail    error
	started chan struct{}
}

func (a *namedAnalyzer) AnalyzerIdentity() string { return a.id }

func (a *namedAnalyzer) Analyze(ctx context.Context, _ string) (AnalysisResult, error) {
	if a.started != nil {
		select {
		case a.started <- struct{}{}:
		default:
		}
	}
	if a.gate != nil {
		select {
		case <-a.gate:
		case <-ctx.Done():
			return AnalysisResult{}, ctx.Err()
		}
	}
	if a.fail != nil {
		return AnalysisResult{}, a.fail
	}
	return sampleResult(), nil
}

// indexProject writes a first index for root at dbPath with analyzer, the
// way an earlier session of the app would have, and closes it.
func indexProject(t *testing.T, dbPath, root string, analyzer Analyzer) {
	t.Helper()
	first := NewService(NewNilStore(), analyzer)
	if err := first.OpenProjectAt(t.Context(), dbPath, root); err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

// reopen opens root again with a refresh that is held until the returned
// release is called.
func reopen(t *testing.T, dbPath, root, id string) (*Service, *namedAnalyzer) {
	t.Helper()
	held := &namedAnalyzer{id: id, gate: make(chan struct{}), started: make(chan struct{}, 1)}
	svc := NewService(NewNilStore(), held)
	t.Cleanup(func() {
		_ = svc.Shutdown()
	})
	if err := svc.OpenProjectAt(t.Context(), dbPath, root); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the reopen's refresh never started")
	}
	return svc, held
}

func TestReopenServesTheStoredIndexWhileItRefreshes(t *testing.T) {
	dbPath, root := filepath.Join(t.TempDir(), "index.db"), t.TempDir()
	indexProject(t, dbPath, root, &namedAnalyzer{id: "java|jar-1|build-1"})

	svc, held := reopen(t, dbPath, root, "java|jar-1|build-1")
	endpoints, err := svc.ListEndpoints(t.Context())
	if err != nil {
		t.Fatalf("query during the refresh: %v — the stored index was written by this same analyzer and should be served", err)
	}
	if len(endpoints) == 0 {
		t.Fatal("served an empty index")
	}
	if info, _ := svc.IndexInfo(t.Context()); !info.Stale || !info.Indexing {
		t.Fatalf("IndexInfo = %+v, want stale and indexing while the refresh runs", info)
	}

	close(held.gate)
	if _, err := svc.WaitForIndexReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info, _ := svc.IndexInfo(t.Context()); info.Stale {
		t.Fatal("still stale after the refresh committed")
	}
}

// A different analyzer — a rebuilt jar, a new DevKit — may index the same
// files differently, so its predecessor's index waits for the refresh as
// every reopen did before.
func TestReopenWithAnotherAnalyzerWaitsForTheRefresh(t *testing.T) {
	dbPath, root := filepath.Join(t.TempDir(), "index.db"), t.TempDir()
	indexProject(t, dbPath, root, &namedAnalyzer{id: "java|jar-1|build-1"})

	svc, held := reopen(t, dbPath, root, "java|jar-2|build-1")
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("err = %v, want ErrIndexNotReady until the new analyzer's refresh commits", err)
	}
	close(held.gate)
	if _, err := svc.WaitForIndexReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListEndpoints(t.Context()); err != nil {
		t.Fatalf("after the refresh: %v", err)
	}
}

// An analyzer that cannot name itself keeps the old rule, and so does an
// index written before the stamp existed.
func TestReopenWithAnUnnamedAnalyzerWaitsForTheRefresh(t *testing.T) {
	dbPath, root := filepath.Join(t.TempDir(), "index.db"), t.TempDir()
	indexProject(t, dbPath, root, fakeAnalyzer{result: sampleResult()}) // stamps ""

	svc, held := reopen(t, dbPath, root, "")
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("err = %v, want ErrIndexNotReady", err)
	}
	close(held.gate)
}

// A failed refresh is still a failure: the stored index stops being served.
func TestAFailedRefreshStopsServingTheStoredIndex(t *testing.T) {
	dbPath, root := filepath.Join(t.TempDir(), "index.db"), t.TempDir()
	indexProject(t, dbPath, root, &namedAnalyzer{id: "lua|build-1"})

	svc, held := reopen(t, dbPath, root, "lua|build-1")
	held.fail = errors.New("analyzer crashed")
	close(held.gate)
	if _, err := svc.WaitForIndexReady(t.Context()); err == nil {
		t.Fatal("want the refresh's failure")
	}
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("err = %v, want ErrIndexNotReady after a failed refresh", err)
	}
}

func TestStoreRoundTripsTheIndexedAnalyzer(t *testing.T) {
	store, err := newSQLiteStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got, err := store.IndexedAnalyzer(t.Context()); err != nil || got != "" {
		t.Fatalf("fresh store = %q, %v; want empty", got, err)
	}
	for _, id := range []string{"java|a", "java|b"} {
		if err := store.SetIndexedAnalyzer(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		if got, _ := store.IndexedAnalyzer(t.Context()); got != id {
			t.Fatalf("IndexedAnalyzer = %q, want %q", got, id)
		}
	}
}

func TestToolchainIdentityFollowsItsArtifact(t *testing.T) {
	if buildIdentity() == "" {
		t.Skip("this test binary cannot name its own build")
	}
	if id := LuaToolchain().identity(); id == "" {
		t.Fatal("a native analyzer is the DevKit build and should always have an identity")
	}

	dir := t.TempDir()
	tc := Toolchain{Language: "java", Artifact: filepath.Join(dir, "analyzer.jar")}
	if id := tc.identity(); id != "" {
		t.Fatalf("identity with no artifact = %q, want empty", id)
	}
	if err := os.WriteFile(tc.Artifact, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tc.identity()
	if err := os.WriteFile(tc.Artifact, []byte("version two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := tc.identity(); before == "" || after == before {
		t.Fatalf("identity before %q, after a rebuild %q — want two different, non-empty", before, after)
	}
	if got := analyzerIdentity(tc.Analyzer()); got != tc.identity() {
		t.Fatalf("the toolchain's analyzer names itself %q, want %q", got, tc.identity())
	}
}
