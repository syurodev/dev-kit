package codegraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T, limit int, analyzer Analyzer) (*Registry, *Service) {
	t.Helper()
	focus := NewService(NewNilStore(), analyzer)
	focus.SetConfigDir(t.TempDir())
	r := NewRegistry(focus, limit)
	r.analyzer = analyzer
	t.Cleanup(func() {
		_ = r.Shutdown()
		_ = focus.Close()
	})
	return r, focus
}

func TestRegistryOpensResidentServicesOnTheSharedHost(t *testing.T) {
	r, focus := newTestRegistry(t, 4, fakeAnalyzer{result: sampleResult()})
	a, b := t.TempDir(), t.TempDir()

	svcA, err := r.Open(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	svcB, err := r.Open(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	if svcA == svcB || svcA == focus {
		t.Fatal("each folder must get its own Service, apart from the focused one")
	}
	if svcA.Host() != focus.Host() || svcB.Host() != focus.Host() {
		t.Fatal("resident services must share the focused Service's Host")
	}
	if again, _ := r.Open(t.Context(), a+"/"); again != svcA {
		t.Fatal("the same folder (spelled differently) opened a second Service")
	}
	if root := svcA.ProjectRoot(); root != ProjectKey(a) {
		t.Fatalf("resident root = %q, want %q", root, a)
	}
	if got := r.Resident(); len(got) != 2 || got[1] != ProjectKey(a) {
		t.Fatalf("resident = %v, want b then a (a used last)", got)
	}
}

// Past the limit the least recently used resident is shut down — its store
// closed — and reported. The focused Service is never a candidate.
func TestRegistryEvictsTheLeastRecentlyUsed(t *testing.T) {
	r, focus := newTestRegistry(t, 2, fakeAnalyzer{result: sampleResult()})
	var evicted []string
	r.OnEvicted = func(p string) { evicted = append(evicted, p) }
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()

	if _, err := r.Open(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	svcB, err := r.Open(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	r.Service(a) // a is now the most recently used
	if _, err := r.Open(t.Context(), c); err != nil {
		t.Fatal(err)
	}

	if len(evicted) != 1 || evicted[0] != ProjectKey(b) {
		t.Fatalf("evicted = %v, want [b]", evicted)
	}
	if _, ok := r.Service(b); ok {
		t.Fatal("an evicted service is still reachable")
	}
	svcB.mu.RLock()
	_, released := svcB.store.(nilStore)
	svcB.mu.RUnlock()
	if !released {
		t.Fatal("the evicted service still holds its index store; Shutdown did not run")
	}
	if got, ok := r.Service(""); !ok || got != focus {
		t.Fatal("the focused Service must always be there")
	}
}

func TestRegistryAnswersForTheFocusedFolderWithTheFocusedService(t *testing.T) {
	r, focus := newTestRegistry(t, 2, fakeAnalyzer{result: sampleResult()})
	root := t.TempDir()
	if err := focus.OpenProjectDir(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	got, err := r.Open(t.Context(), root)
	if err != nil || got != focus {
		t.Fatalf("Open(focused folder) = %v, %v; want the focused Service", got, err)
	}
	if len(r.Resident()) != 0 {
		t.Fatal("the focused folder took a resident slot")
	}
}

// A failed open frees its slot and leaves no Service behind.
func TestRegistryFailedOpenLeavesNothing(t *testing.T) {
	r, _ := newTestRegistry(t, 2, fakeAnalyzer{err: errors.New("boom")})
	if _, err := r.Open(t.Context(), t.TempDir()); err == nil {
		t.Fatal("want the analyzer's error")
	}
	if got := r.Resident(); len(got) != 0 {
		t.Fatalf("resident after a failed open = %v, want none", got)
	}
}

func TestRegistryWithNoRoomOnlyHasTheFocusedService(t *testing.T) {
	r, _ := newTestRegistry(t, 0, fakeAnalyzer{result: sampleResult()})
	if _, err := r.Open(t.Context(), t.TempDir()); !errors.Is(err, ErrNoResidentSlots) {
		t.Fatalf("err = %v, want ErrNoResidentSlots", err)
	}
}

func TestRegistryReportsResidentReindexesByFolder(t *testing.T) {
	r, _ := newTestRegistry(t, 2, fakeAnalyzer{result: sampleResult()})
	var mu sync.Mutex
	var got []string
	r.OnIndexUpdated = func(p string, _ int) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}
	dir := t.TempDir()
	// The open's own first index write is an update like a re-index's.
	if _, err := r.Open(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no index update was reported")
	}
	for _, p := range got {
		if p != ProjectKey(dir) {
			t.Fatalf("index update named %q, want %s", p, dir)
		}
	}
}

// gatedAnalyzer holds every analysis until released and records how many
// ran at once.
type gatedAnalyzer struct {
	release chan struct{}
	running atomic.Int32
	peak    atomic.Int32
}

func (b *gatedAnalyzer) Analyze(ctx context.Context, _ string) (AnalysisResult, error) {
	n := b.running.Add(1)
	defer b.running.Add(-1)
	for {
		p := b.peak.Load()
		if n <= p || b.peak.CompareAndSwap(p, n) {
			break
		}
	}
	select {
	case <-b.release:
		return sampleResult(), nil
	case <-ctx.Done():
		return AnalysisResult{}, ctx.Err()
	}
}

// The job limit is the Host's, so it holds across resident services: three
// opens at once with a limit of one analyze one at a time.
func TestAnalysesShareTheHostJobLimit(t *testing.T) {
	analyzer := &gatedAnalyzer{release: make(chan struct{})}
	r, focus := newTestRegistry(t, 4, analyzer)
	focus.Host().SetJobLimit(1)

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		dir := t.TempDir()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Open(t.Context(), dir)
			errs <- err
		}()
	}
	deadline := time.After(5 * time.Second)
	for released := 0; released < 3; released++ {
		for analyzer.running.Load() == 0 {
			select {
			case <-deadline:
				t.Fatal("no analysis started")
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		analyzer.release <- struct{}{}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if p := analyzer.peak.Load(); p != 1 {
		t.Fatalf("%d analyses ran at once, want 1", p)
	}
}

func TestAcquireJobGivesUpWithItsContext(t *testing.T) {
	host := NewHost()
	host.SetJobLimit(1)
	release, err := host.acquireJob(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := host.acquireJob(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
}
