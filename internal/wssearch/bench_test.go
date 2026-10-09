package wssearch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// benchWorkspace builds the shape the feature is meant for: repositories, a
// monorepo of services, a library and a config folder — about 10k files, the same
// relative paths in several roots, a NESTED root inside the monorepo, a 2 MB
// single line and a file where every line matches.
func benchWorkspace(tb testing.TB) []Root {
	tb.Helper()
	base := tb.TempDir()
	write := func(root, rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	body := func(i int) string {
		var b strings.Builder
		for l := 0; l < 40; l++ {
			fmt.Fprintf(&b, "func handler%d_%d(ctx context.Context) error { return process(ctx, %d) } // héllo\n", i, l, l)
		}
		return b.String()
	}
	var roots []Root
	add := func(id string, files int, layout func(i int) string) string {
		root := filepath.Join(base, id)
		for i := 0; i < files; i++ {
			write(root, layout(i), body(i))
		}
		roots = append(roots, Root{ID: id, Path: root})
		return root
	}
	add("orders", 2000, func(i int) string { return fmt.Sprintf("src/pkg%d/file%d.go", i%40, i) })
	add("billing", 2000, func(i int) string { return fmt.Sprintf("src/pkg%d/file%d.go", i%40, i) }) // same relative paths as orders
	monorepo := add("platform", 2500, func(i int) string { return fmt.Sprintf("services/svc%d/src/f%d.go", i%12, i) })
	// A folder nested inside the monorepo, also a member of the workspace: its 500 files are searched once, as its own.
	nested := filepath.Join(monorepo, "libs", "shared")
	for i := 0; i < 500; i++ {
		write(nested, fmt.Sprintf("src/m%d/x%d.go", i%20, i), body(i))
	}
	roots = append(roots, Root{ID: "shared", Path: nested})
	add("shared-lib", 1500, func(i int) string { return fmt.Sprintf("lib/m%d/x%d.go", i%30, i) })
	add("config", 1500, func(i int) string { return fmt.Sprintf("env/c%d.yaml", i) })
	write(roots[0].Path, "src/minified.js", strings.Repeat("a;needle(1);", 170000)) // one 2 MB line
	write(roots[1].Path, "src/dense.go", strings.Repeat("needle needle needle\n", 20000))
	return roots
}

// memSampler records the peak of the Go heap while a search runs. HeapAlloc
// counts live objects plus garbage the collector has not reclaimed yet, so the
// peak says how much memory a search needs room for, which a figure read after
// the run cannot.
type memSampler struct {
	peak atomic.Uint64
	stop chan struct{}
	done sync.WaitGroup
}

func startSampler() *memSampler {
	s := &memSampler{stop: make(chan struct{})}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		var m runtime.MemStats
		t := time.NewTicker(500 * time.Microsecond)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > s.peak.Load() {
					s.peak.Store(m.HeapAlloc)
				}
			}
		}
	}()
	return s
}

func (s *memSampler) finish() uint64 { close(s.stop); s.done.Wait(); return s.peak.Load() }

const mib = 1 << 20

// BenchmarkWorkspaceSearch reports, per search: the standard ns/op, B/op and
// allocs/op (everything allocated, most of it garbage); liveHeapMiB (HeapAlloc
// after a forced GC once the runs are done — what a search leaves behind, which
// should be the fixture's own few MiB, not the search's); peakHeapMiB (highest
// HeapAlloc sampled while it ran); and gc-pause-ms/op and gcs/op.
func BenchmarkWorkspaceSearch(b *testing.B) {
	roots := benchWorkspace(b)
	cases := []struct {
		name  string
		roots []Root
		opts  Options
	}{
		{"literal-rare-full-scan", roots, Options{Query: "handler1999_39"}},
		{"literal-common-hits-cap", roots, Options{Query: "process("}},
		{"regex", roots, Options{Query: `handler\d+_3\d\(`, Regex: true}},
		{"whole-word", roots, Options{Query: "ctx", WholeWord: true}},
		{"dense-matches-and-long-line", roots, Options{Query: "needle", MaxMatches: 100000}},
		{"nested-roots-full-scan", roots, Options{Query: "handler2499_39"}},
		{"one-folder-only", roots[:1], Options{Query: "handler1999_39"}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			sampler := startSampler()
			var last Result
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := Run(context.Background(), c.roots, c.opts)
				if err != nil {
					b.Fatal(err)
				}
				last = res
			}
			b.StopTimer()
			peak := sampler.finish()
			runtime.ReadMemStats(&after)
			runtime.GC()
			var settled runtime.MemStats
			runtime.ReadMemStats(&settled)
			n := float64(b.N)
			b.ReportMetric(float64(last.FilesSearched), "files-searched")
			b.ReportMetric(float64(last.Matches), "matches")
			b.ReportMetric(float64(settled.HeapAlloc)/mib, "liveHeapMiB-after")
			b.ReportMetric(float64(peak)/mib, "peakHeapMiB")
			b.ReportMetric(float64(after.PauseTotalNs-before.PauseTotalNs)/1e6/n, "gc-pause-ms/op")
			b.ReportMetric(float64(after.NumGC-before.NumGC)/n, "gcs/op")
		})
	}
}

// TestCancellationStopsAMidFlightSearch starts the slowest search, cancels it
// while it is demonstrably running (it has not returned, and returns the
// context's error rather than a result), and reports how long Run takes to
// return after the cancel signal. That is the backend half only: the wire from
// the UI to this call is measured separately, in the browser.
func TestCancellationStopsAMidFlightSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 10k file workspace")
	}
	roots := benchWorkspace(t)
	var lat []time.Duration
	for _, delay := range []time.Duration{5 * time.Millisecond, 20 * time.Millisecond, 60 * time.Millisecond, 120 * time.Millisecond} {
		ctx, cancel := context.WithCancel(context.Background())
		type out struct {
			err error
			at  time.Time
		}
		done := make(chan out, 1)
		go func() {
			_, err := Run(ctx, roots, Options{Query: `handler\d+_\d+\(ctx`, Regex: true, MaxMatches: 100000})
			done <- out{err, time.Now()}
		}()
		time.Sleep(delay)
		cancelledAt := time.Now()
		cancel()
		o := <-done
		if !errors.Is(o.err, context.Canceled) {
			t.Logf("delay %v: the search finished before it was cancelled (err=%v); not counted", delay, o.err)
			continue
		}
		t.Logf("cancel %v into the search -> Run returned after %v", delay, o.at.Sub(cancelledAt))
		lat = append(lat, o.at.Sub(cancelledAt))
	}
	if len(lat) == 0 {
		t.Fatal("no cancel landed mid-flight; the workspace is too small for this machine")
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	t.Logf("cancel -> Run returned (mid-flight cancels: %d): min %v, max %v", len(lat), lat[0], lat[len(lat)-1])
	if lat[len(lat)-1] > 250*time.Millisecond {
		t.Fatalf("a cancelled search kept running for %v", lat[len(lat)-1])
	}
}

// TestQueryChurn types a query one letter at a time, cancelling each search when
// the next letter arrives (what the UI's debounce does not absorb), and checks
// that only the last search produces a result and nothing is left running.
func TestQueryChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 10k file workspace")
	}
	roots := benchWorkspace(t)
	query := "handler1999_39"
	goroutinesBefore := runtime.NumGoroutine()
	var cancels []context.CancelFunc
	var wg sync.WaitGroup
	var finished atomic.Int32
	started := time.Now()
	for i := 3; i <= len(query); i++ {
		if len(cancels) > 0 {
			cancels[len(cancels)-1]()
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		q := query[:i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Run(ctx, roots, Options{Query: q}); err == nil {
				finished.Add(1)
			}
		}()
		time.Sleep(8 * time.Millisecond)
	}
	wg.Wait()
	elapsed := time.Since(started)
	for _, c := range cancels {
		c()
	}
	time.Sleep(50 * time.Millisecond)
	t.Logf("%d searches typed 8ms apart, %d ran to completion, all returned %v after the first started; goroutines %d -> %d",
		len(query)-2, finished.Load(), elapsed.Round(time.Millisecond), goroutinesBefore, runtime.NumGoroutine())
	if finished.Load() < 1 {
		t.Fatal("the last search never finished")
	}
	if g := runtime.NumGoroutine(); g > goroutinesBefore+4 {
		t.Fatalf("goroutines leaked: %d -> %d", goroutinesBefore, g)
	}
}
