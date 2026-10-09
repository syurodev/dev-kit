package docwatch

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type sink struct {
	mu     sync.Mutex
	events []Event
	ch     chan struct{}
}

func newSink() *sink { return &sink{ch: make(chan struct{}, 64)} }

func (s *sink) emit(e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	s.ch <- struct{}{}
}

func (s *sink) wait(t *testing.T) Event {
	t.Helper()
	select {
	case <-s.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[len(s.events)-1]
}

func (s *sink) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-s.ch:
		t.Fatal("unexpected event")
	case <-time.After(d):
	}
}

func fast() Options { return Options{Debounce: 20 * time.Millisecond, MaxWait: 200 * time.Millisecond} }

func TestWriteAndAtomicRenameAndDeleteAreReportedByKey(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newSink()
	w, err := New(s.emit, fast())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if un := w.Set([]Doc{{"A", a}, {"B", b}}); len(un) != 0 {
		t.Fatalf("unwatched = %v", un)
	}

	if err := os.WriteFile(a, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"A"}) {
		t.Fatalf("write: %+v", ev)
	}

	// An atomic save: temp file renamed over the target. A watch on the file
	// itself would have been left on the replaced inode.
	tmp := filepath.Join(dir, ".b.txt.tmp")
	if err := os.WriteFile(tmp, []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, b); err != nil {
		t.Fatal(err)
	}
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"B"}) {
		t.Fatalf("rename-over: %+v", ev)
	}

	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"A"}) {
		t.Fatalf("remove: %+v", ev)
	}
}

func TestOtherFilesInTheDirectoryAreIgnoredAndBurstsCoalesce(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(a, []byte("1"), 0o644)
	s := newSink()
	w, _ := New(s.emit, fast())
	defer w.Close()
	w.Set([]Doc{{"A", a}})

	_ = os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("x"), 0o644)
	s.quiet(t, 150*time.Millisecond)

	for i := 0; i < 20; i++ {
		_ = os.WriteFile(a, []byte{byte(i)}, 0o644)
	}
	s.wait(t)
	s.quiet(t, 150*time.Millisecond) // one report for the whole burst
}

func TestSetReleasesDirectoriesAndRespectsTheCeiling(t *testing.T) {
	root := t.TempDir()
	var docs []Doc
	for i := 0; i < 5; i++ {
		d := filepath.Join(root, string(rune('a'+i)))
		_ = os.MkdirAll(d, 0o755)
		p := filepath.Join(d, "f.txt")
		_ = os.WriteFile(p, nil, 0o644)
		docs = append(docs, Doc{Key: string(rune('A' + i)), Path: p})
	}
	opts := fast()
	opts.MaxDirs = 3
	w, _ := New(newSink().emit, opts)
	defer w.Close()

	un := w.Set(docs)
	if w.Dirs() != 3 || !reflect.DeepEqual(un, []string{"D", "E"}) {
		t.Fatalf("dirs = %d, unwatched = %v; want 3 watched and D,E left to the focus fallback", w.Dirs(), un)
	}
	if un := w.Set(docs[:1]); len(un) != 0 || w.Dirs() != 1 {
		t.Fatalf("after shrinking: dirs = %d, unwatched = %v", w.Dirs(), un)
	}
	if un := w.Set(nil); len(un) != 0 || w.Dirs() != 0 {
		t.Fatalf("after clearing: dirs = %d", w.Dirs())
	}
}

func TestMissingDirectoryIsReportedUnwatched(t *testing.T) {
	w, _ := New(newSink().emit, fast())
	defer w.Close()
	un := w.Set([]Doc{{"X", filepath.Join(t.TempDir(), "gone", "f.txt")}})
	if !reflect.DeepEqual(un, []string{"X"}) {
		t.Fatalf("unwatched = %v", un)
	}
}

func TestRemovingTheWatchedDirectoryReportsItsDocuments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, nil, 0o644)
	s := newSink()
	w, _ := New(s.emit, fast())
	defer w.Close()
	w.Set([]Doc{{"F", p}})
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"F"}) {
		t.Fatalf("event = %+v", ev)
	}
}

func TestCloseIsIdempotentAndStopsDelivery(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, nil, 0o644)
	s := newSink()
	w, _ := New(s.emit, fast())
	w.Set([]Doc{{"F", p}})
	w.Close()
	w.Close()
	_ = os.WriteFile(p, []byte("x"), 0o644)
	s.quiet(t, 100*time.Millisecond)
	if un := w.Set([]Doc{{"F", p}}); len(un) != 1 {
		t.Fatalf("a closed watcher watches nothing: %v", un)
	}
}

// A directory that is deleted and created again (a branch switch, a clean
// rebuild) takes its kernel watch with it. The documents in it must be heard
// again once it is back — without anyone having to reopen them.
func TestADirectoryRemovedAndCreatedAgainIsWatchedAgain(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("one"), 0o644)
	s := newSink()
	w, _ := New(s.emit, Options{Debounce: 20 * time.Millisecond, MaxWait: 200 * time.Millisecond, Rewatch: 30 * time.Millisecond})
	defer w.Close()
	w.Set([]Doc{{"F", p}})

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	s.wait(t) // the removal itself
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(p, []byte("two"), 0o644)
	// The file is back with other content: that is reported when the directory is watched again.
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"F"}) {
		t.Fatalf("after the directory came back: %+v", ev)
	}
	// And later writes are heard as before.
	time.Sleep(100 * time.Millisecond)
	for len(s.ch) > 0 {
		<-s.ch
	}
	_ = os.WriteFile(p, []byte("three"), 0o644)
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"F"}) {
		t.Fatalf("a write after the directory was watched again: %+v", ev)
	}
}

// Set called again with the same documents must not trust a watch the kernel
// dropped: the frontend calls it whenever tabs change.
func TestSetRepairsAWatchLostWhenTheDirectoryWasReplaced(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("one"), 0o644)
	s := newSink()
	// No automatic retry: only Set may repair it.
	w, _ := New(s.emit, Options{Debounce: 20 * time.Millisecond, MaxWait: 200 * time.Millisecond, Rewatch: -1})
	defer w.Close()
	w.Set([]Doc{{"F", p}})
	_ = os.RemoveAll(dir)
	s.wait(t)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(p, []byte("two"), 0o644)
	s.quiet(t, 150*time.Millisecond)

	if un := w.Set([]Doc{{"F", p}}); len(un) != 0 {
		t.Fatalf("unwatched = %v", un)
	}
	_ = os.WriteFile(p, []byte("three"), 0o644)
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"F"}) {
		t.Fatalf("event = %+v", ev)
	}
}

// One physical file can be open under two folder identities (an outer folder and
// a folder nested in it). A change names both documents; neither is dropped.
func TestOnePathOpenUnderTwoKeysReportsBoth(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(p, []byte("a"), 0o644)
	s := newSink()
	w, _ := New(s.emit, fast())
	defer w.Close()
	w.Set([]Doc{{"outer:libs/f.txt", p}, {"inner:f.txt", p}})
	_ = os.WriteFile(p, []byte("b"), 0o644)
	if ev := s.wait(t); !reflect.DeepEqual(ev.Keys, []string{"inner:f.txt", "outer:libs/f.txt"}) {
		t.Fatalf("event = %+v", ev)
	}
}
