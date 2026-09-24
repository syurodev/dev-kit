package codegraph

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// byPathAnalyzer fails for the folders it is told to, succeeds for the rest.
type byPathAnalyzer struct {
	fail map[string]error
}

func (a byPathAnalyzer) Analyze(_ context.Context, path string) (AnalysisResult, error) {
	if err, ok := a.fail[filepath.Clean(path)]; ok {
		return AnalysisResult{}, err
	}
	return sampleResult(), nil
}

// statusLog collects OnIndexStatus reports and waits for a final one per path.
type statusLog struct {
	mu   sync.Mutex
	all  []ServiceIndexStatus
	done map[string]ServiceIndexStatus
	ch   chan struct{}
}

func newStatusLog(r *Registry) *statusLog {
	l := &statusLog{done: map[string]ServiceIndexStatus{}, ch: make(chan struct{}, 64)}
	r.OnIndexStatus = func(st ServiceIndexStatus) {
		l.mu.Lock()
		l.all = append(l.all, st)
		if st.State != IndexPending && st.State != IndexIndexing {
			l.done[st.Path] = st
		}
		l.mu.Unlock()
		l.ch <- struct{}{}
	}
	return l
}

func (l *statusLog) wait(t *testing.T, paths ...string) map[string]ServiceIndexStatus {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		l.mu.Lock()
		all := true
		for _, p := range paths {
			if _, ok := l.done[ProjectKey(p)]; !ok {
				all = false
			}
		}
		out := map[string]ServiceIndexStatus{}
		for k, v := range l.done {
			out[k] = v
		}
		l.mu.Unlock()
		if all {
			return out
		}
		select {
		case <-l.ch:
		case <-deadline:
			t.Fatalf("timed out waiting for %v; saw %+v", paths, l.all)
		}
	}
}

// Queued folders are indexed in the background, one at a time, onto disk —
// and never become resident: the LRU is for what the user opened.
func TestEnqueueIndexesOnceWithoutTakingResidentSlots(t *testing.T) {
	r, _ := newTestRegistry(t, 1, byPathAnalyzer{})
	log := newStatusLog(r)
	a, b := t.TempDir(), t.TempDir()

	r.Enqueue(a, b)
	got := log.wait(t, a, b)

	for _, p := range []string{a, b} {
		st := got[ProjectKey(p)]
		if st.State != IndexIndexed || st.IndexedAt == 0 {
			t.Fatalf("%s: %+v, want indexed with a time", p, st)
		}
		if disk := r.IndexStatus(p); disk.State != IndexIndexed {
			t.Fatalf("%s: IndexStatus = %+v, want indexed from disk", p, disk)
		}
	}
	if n := len(r.Resident()); n != 0 {
		t.Fatalf("resident = %d, want the queue to leave no resident service", n)
	}
	// Every folder went pending → indexing → indexed, in that order.
	log.mu.Lock()
	defer log.mu.Unlock()
	seen := map[string][]IndexState{}
	for _, st := range log.all {
		seen[st.Path] = append(seen[st.Path], st.State)
	}
	for _, p := range []string{a, b} {
		if fmt.Sprint(seen[ProjectKey(p)]) != fmt.Sprint([]IndexState{IndexPending, IndexIndexing, IndexIndexed}) {
			t.Fatalf("%s transitions = %v", p, seen[ProjectKey(p)])
		}
	}
}

// A missing toolchain is told apart from a real failure, and a folder no
// analyzer claims is unsupported rather than failed.
func TestEnqueueNamesHowARunFailed(t *testing.T) {
	broken, noJDK, other := t.TempDir(), t.TempDir(), t.TempDir()
	r, _ := newTestRegistry(t, 1, byPathAnalyzer{fail: map[string]error{
		broken: fmt.Errorf("parse: boom"),
		noJDK:  fmt.Errorf("%w: java not found", ErrToolchainIncomplete),
		other:  ErrUnsupportedProject,
	}})
	log := newStatusLog(r)

	r.Enqueue(broken, noJDK, other)
	got := log.wait(t, broken, noJDK, other)

	if st := got[broken]; st.State != IndexFailed || st.MissingToolchain || st.Error == "" {
		t.Fatalf("broken = %+v, want failed with a reason", st)
	}
	if st := got[noJDK]; st.State != IndexFailed || !st.MissingToolchain {
		t.Fatalf("noJDK = %+v, want failed for a missing toolchain", st)
	}
	if st := got[other]; st.State != IndexUnsupported {
		t.Fatalf("other = %+v, want unsupported", st)
	}
	if st := r.IndexStatus(broken); st.State != IndexFailed {
		t.Fatalf("IndexStatus(broken) = %+v, want the queue's failure", st)
	}
}

// A folder open as a service keeps its own index; the queue does not open
// it a second time.
func TestEnqueueSkipsAFolderAlreadyOpen(t *testing.T) {
	r, _ := newTestRegistry(t, 2, byPathAnalyzer{})
	a := t.TempDir()
	resident, err := r.Open(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	log := newStatusLog(r)

	r.Enqueue(a)
	got := log.wait(t, a)
	if got[ProjectKey(a)].State != IndexIndexed {
		t.Fatalf("status = %+v", got[ProjectKey(a)])
	}
	if svc, _ := r.Service(a); svc != resident {
		t.Fatal("the open service must be left alone")
	}
}

func TestStoredIndexIsFalseWithoutAFile(t *testing.T) {
	if _, ok := StoredIndex(t.TempDir(), t.TempDir()); ok {
		t.Fatal("no index file, no stored index")
	}
	if _, ok := StoredIndex("", "/x"); ok {
		t.Fatal("no config dir, no stored index")
	}
}
