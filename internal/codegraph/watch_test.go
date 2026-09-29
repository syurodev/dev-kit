package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// watchedService opens dir with a toolchain registered, so the watcher judges
// changes by that toolchain's source extension and build files.
func watchedService(t *testing.T, dir string, analyzer Analyzer) *Service {
	t.Helper()
	svc := newTestService(t, analyzer)
	svc.debounceInterval = 20 * time.Millisecond
	svc.SetToolchains([]Toolchain{{Language: "java", SourceExt: ".java", Markers: []string{"pom.xml", "build.gradle*"}}})
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

func TestWatchFilterJudgesByTheToolchains(t *testing.T) {
	relevant := watchFilter([]Toolchain{{SourceExt: ".java", Markers: []string{"pom.xml", "build.gradle.kts"}}})
	for name, want := range map[string]bool{
		"/p/src/Foo.java":      true,
		"/p/pom.xml":           true,
		"/p/build.gradle.kts":  true,
		"/p/README.md":         false,
		"/p/.Foo.java.123.tmp": false,
		"/p/app.log":           false,
		"/p/.DS_Store":         false,
	} {
		if got := relevant(name); got != want {
			t.Errorf("relevant(%s) = %v, want %v", name, got, want)
		}
	}
	if !watchFilter(nil)("/p/anything.txt") {
		t.Error("with no toolchain registered every change must count")
	}
}

func TestIrrelevantChangesNeitherReindexNorBlockQueries(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	svc := watchedService(t, dir, countingAnalyzer{result: sampleResult(), calls: &calls})
	before := calls.Load()

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		// Queries stay available throughout: a log file is not the index.
		if _, err := svc.ListEntryPoints(t.Context()); err != nil {
			t.Fatalf("a log write made the index unready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != before {
		t.Fatalf("irrelevant writes re-indexed %d times", calls.Load()-before)
	}
}

func TestWritesInsideIgnoredDirectoriesDoNotReindex(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	svc := watchedService(t, dir, countingAnalyzer{result: sampleResult(), calls: &calls})
	_ = svc
	before := calls.Load()

	// A build creating target/ and writing generated sources into it.
	gen := filepath.Join(dir, "target", "generated")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, "Gen.java"), []byte("class Gen {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if calls.Load() != before {
		t.Fatalf("writes under target/ re-indexed %d times", calls.Load()-before)
	}
}

// A steady stream of relevant writes used to reset the debounce forever, and
// each one also discarded the run in flight: the index never settled.
func TestAContinuousStreamOfChangesStillReindexes(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	svc.debounceInterval = 100 * time.Millisecond
	svc.maxDebounceWait = 200 * time.Millisecond
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	before := calls.Load()

	stop := time.After(700 * time.Millisecond)
	ticker := time.NewTicker(20 * time.Millisecond) // well inside the debounce interval
	defer ticker.Stop()
	for i := 0; ; i++ {
		select {
		case <-stop:
			if calls.Load() == before {
				t.Fatal("a write every 20ms kept postponing the re-index past maxDebounceWait")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := svc.WaitForIndexReady(ctx); err != nil {
				t.Fatalf("the index did not settle once the writes stopped: %v", err)
			}
			return
		case <-ticker.C:
			if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte{byte(i)}, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// A change arriving while a re-index runs queues another run after it rather
// than cancelling it — otherwise a re-index slower than the edit rate never
// finishes.
func TestAChangeDuringAReindexQueuesInsteadOfCancelling(t *testing.T) {
	dir := t.TempDir()
	var calls, cancelled atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	svc := newTestService(t, funcAnalyzer{analyzeFunc: func(ctx context.Context, _ string) (AnalysisResult, error) {
		if calls.Add(1) == 1 {
			return sampleResult(), nil // the open itself
		}
		entered <- struct{}{}
		select {
		case <-release:
			return sampleResult(), nil
		case <-ctx.Done():
			cancelled.Add(1)
			return AnalysisResult{}, ctx.Err()
		}
	}})
	svc.debounceInterval = 20 * time.Millisecond
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	if err := os.WriteFile(filepath.Join(dir, "A.java"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	<-entered // first re-index running
	if err := os.WriteFile(filepath.Join(dir, "B.java"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // the second change's debounce fires meanwhile
	if cancelled.Load() != 0 {
		t.Fatal("a newer change cancelled the running re-index")
	}
	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := svc.WaitForIndexReady(ctx); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("analyzer ran %d times, want open + running re-index + the queued one", got)
	}
}
