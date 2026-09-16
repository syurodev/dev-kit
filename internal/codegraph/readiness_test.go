package codegraph

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func assertIndexBlocked(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := svc.WaitForIndexReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait returned before analyzer completed: %v", err)
	}
	for name, query := range map[string]func() error{
		"endpoints":    func() error { _, e := svc.ListEndpoints(t.Context()); return e },
		"symbols":      func() error { _, e := svc.SearchSymbol(t.Context(), "Controller"); return e },
		"blast radius": func() error { _, e := svc.GetBlastRadius(t.Context(), "Foo.java"); return e },
		"trace":        func() error { _, e := svc.TraceDataFlow(t.Context(), "class", "clock"); return e },
		"neighbors":    func() error { _, e := svc.GetNeighbors(t.Context(), "class", 1, NeighborFilter{}); return e },
	} {
		if err := query(); !errors.Is(err, ErrIndexNotReady) {
			t.Fatalf("%s served unready index: %v", name, err)
		}
	}
}

func TestReadinessIncludesColdAnalysis(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	svc := newTestService(t, funcAnalyzer{analyzeFunc: func(context.Context, string) (AnalysisResult, error) {
		close(entered)
		<-release
		return sampleResult(), nil
	}})
	done := make(chan error, 1)
	go func() { done <- svc.OpenProject(t.Context(), "/fake/project") }()
	<-entered
	assertIndexBlocked(t, svc)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v, err := svc.WaitForIndexReady(t.Context()); err != nil || v != 1 {
		t.Fatalf("ready: %d %v", v, err)
	}
}

func TestReadinessIncludesQueuedRefreshBeforeGoroutineStarts(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	run := svc.prepareReindex(t.Context()) // deliberately not started yet
	assertIndexBlocked(t, svc)
	run()
	if v, err := svc.WaitForIndexReady(t.Context()); err != nil || v != 2 {
		t.Fatalf("ready: %d %v", v, err)
	}
}

func TestCachedStartupWaitsAndKeepsRevisionAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	first := reopenService(t, path, fakeAnalyzer{result: sampleResult()})
	if err := first.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	first.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	next := reopenService(t, path, funcAnalyzer{analyzeFunc: func(context.Context, string) (AnalysisResult, error) {
		close(entered)
		<-release
		return AnalysisResult{}, nil
	}})
	if err := next.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	<-entered
	assertIndexBlocked(t, next)
	close(release)
	if v, err := next.WaitForIndexReady(t.Context()); err != nil || v != 2 {
		t.Fatalf("revision reset: %d %v", v, err)
	}
	endpoints, err := next.ListEndpoints(t.Context())
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("stale endpoints: %+v %v", endpoints, err)
	}
}

func TestFailedRefreshIsNotReadyAndDoesNotBumpRevision(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	svc.SetAnalyzer(fakeAnalyzer{err: errors.New("analyzer failed")})
	svc.runReindex(t.Context())
	if _, err := svc.WaitForIndexReady(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("false ready after failure: %v", err)
	}
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("served stale result: %v", err)
	}
	if v, err := svc.store.IndexVersion(t.Context()); err != nil || v != 1 {
		t.Fatalf("failed run changed revision: %d %v", v, err)
	}
	svc.SetAnalyzer(fakeAnalyzer{result: sampleResult()})
	svc.runReindex(t.Context())
	if v, err := svc.WaitForIndexReady(t.Context()); err != nil || v != 2 {
		t.Fatalf("retry: %d %v", v, err)
	}
}

func TestLastProjectRestoresOnNewService(t *testing.T) {
	config, root := t.TempDir(), t.TempDir()
	first := NewService(NewNilStore(), fakeAnalyzer{result: sampleResult()})
	if err := first.RestoreLastProject(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if err := first.OpenProjectAt(t.Context(), ProjectDBPath(config, root), root); err != nil {
		t.Fatal(err)
	}
	first.Close()
	first.store.Close()
	next := NewService(NewNilStore(), fakeAnalyzer{result: sampleResult()})
	t.Cleanup(func() { next.Close(); next.store.Close() })
	if err := next.RestoreLastProject(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if v, err := next.WaitForIndexReady(t.Context()); err != nil || v != 2 {
		t.Fatalf("restored revision: %d %v", v, err)
	}
	if next.ProjectRoot() != root {
		t.Fatalf("restored wrong project: %s", next.ProjectRoot())
	}
}

func TestObsoleteRunCannotPublishOrClearNewerWork(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	old := svc.prepareReindex(t.Context())
	svc.SetAnalyzer(fakeAnalyzer{result: AnalysisResult{}})
	newer := svc.prepareReindex(t.Context())
	old()
	assertIndexBlocked(t, svc)
	newer()
	if version, err := svc.WaitForIndexReady(t.Context()); err != nil || version != 2 {
		t.Fatalf("obsolete run changed revision: %d %v", version, err)
	}
	nodes, err := svc.SearchSymbol(t.Context(), "")
	if err != nil || len(nodes) != 0 {
		t.Fatalf("obsolete graph survived: %+v %v", nodes, err)
	}
}

func TestRejectedIndexDoesNotAdvancePersistentVersion(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceIndex(t.Context(), sampleResult()); err != nil {
		t.Fatal(err)
	}
	bad := AnalysisResult{Edges: []Edge{{From: "missing", To: "also-missing", Kind: "call"}}}
	if err := store.ReplaceIndex(t.Context(), bad); err == nil {
		t.Fatal("invalid index accepted")
	}
	if version, err := store.IndexVersion(t.Context()); err != nil || version != 1 {
		t.Fatalf("failed transaction advanced version: %d %v", version, err)
	}
}
