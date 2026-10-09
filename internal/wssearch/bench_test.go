package wssearch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// benchWorkspace builds the shape the feature is meant for: three repositories,
// a monorepo of services, a library and a config folder — about 10k files, the
// same relative paths in several roots, a few very long lines and one file that
// is mostly matches.
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
	add := func(id string, files int, layout func(i int) string) {
		root := filepath.Join(base, id)
		for i := 0; i < files; i++ {
			write(root, layout(i), body(i))
		}
		roots = append(roots, Root{ID: id, Path: root})
	}
	add("orders", 2000, func(i int) string { return fmt.Sprintf("src/pkg%d/file%d.go", i%40, i) })
	add("billing", 2000, func(i int) string { return fmt.Sprintf("src/pkg%d/file%d.go", i%40, i) }) // same relative paths as orders
	add("platform", 3000, func(i int) string { return fmt.Sprintf("services/svc%d/src/f%d.go", i%12, i) })
	add("shared-lib", 1500, func(i int) string { return fmt.Sprintf("lib/m%d/x%d.go", i%30, i) })
	add("config", 1500, func(i int) string { return fmt.Sprintf("env/c%d.yaml", i) })
	// A 2 MB single line and a file where every line matches.
	write(roots[0].Path, "src/minified.js", strings.Repeat("a;needle(1);", 170000))
	write(roots[1].Path, "src/dense.go", strings.Repeat("needle needle needle\n", 20000))
	return roots
}

func BenchmarkWorkspaceSearch(b *testing.B) {
	roots := benchWorkspace(b)
	cases := map[string]Options{
		"literal-common": {Query: "process("},
		"literal-rare":   {Query: "handler1999_39"},
		"regex":          {Query: `handler\d+_3\d\(`, Regex: true},
		"whole-word":     {Query: "ctx", WholeWord: true},
		"many-matches":   {Query: "needle", MaxMatches: 100000},
	}
	for name, o := range cases {
		b.Run(name, func(b *testing.B) {
			var ms runtime.MemStats
			var last Result
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				res, err := Run(context.Background(), roots, o)
				if err != nil {
					b.Fatal(err)
				}
				last = res
			}
			runtime.ReadMemStats(&ms)
			b.ReportMetric(float64(last.FilesSearched), "files")
			b.ReportMetric(float64(last.Matches), "matches")
			b.ReportMetric(float64(ms.HeapSys)/(1<<20), "heapSysMiB")
		})
	}
}

// TestCancelLatency measures how long a cancelled search keeps running.
func TestCancelLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 10k file workspace")
	}
	roots := benchWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan time.Duration, 1)
	var cancelledAt time.Time
	go func() {
		_, _ = Run(ctx, roots, Options{Query: `handler\d+_\d+\(ctx`, Regex: true})
		done <- time.Since(cancelledAt)
	}()
	time.Sleep(15 * time.Millisecond)
	cancelledAt = time.Now()
	cancel()
	select {
	case d := <-done:
		t.Logf("cancel -> Run returned in %v", d)
		if d > 500*time.Millisecond {
			t.Fatalf("a cancelled search kept running for %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("search did not stop")
	}
}
