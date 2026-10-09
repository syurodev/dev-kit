// Package docwatch tells an editor when a file it has open changed on disk.
//
// It watches the *directories* of the documents it was given — not the files,
// because an editor or `git checkout` saves by writing a temp file and renaming
// it over the target, which replaces the inode a file watch was attached to —
// and only those directories. A workspace of ten folders and 100k files costs
// as many watches as there are directories holding an open document, capped at
// MaxDirs, not as many as the tree has directories. (The index watcher in
// internal/codegraph watches every directory of a project because re-indexing
// has to notice any source change; an editor only has to notice its own files.)
//
// It is a hint, not a source of truth. An event only names a document; the
// caller asks the filesystem what is there now and decides. That is what makes
// the awkward cases uniform: rename, remove-then-create, a burst of writes, a
// watch that could not be set, an overflowed kernel queue (Event.Resync) and a
// window that was in the background while the files changed all end in "look
// again", never in a guess made from an event kind.
//
// Leaf package: standard library and fsnotify only.
package docwatch

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Doc is one open document: an opaque key chosen by the caller and the absolute
// path of the file.
type Doc struct {
	Key  string
	Path string
}

// Event is what the watcher reports. Keys are the documents that may have
// changed, deduplicated and sorted. Resync means events were lost (the kernel
// queue overflowed, or the watcher failed): every document should be looked at
// again.
type Event struct {
	Keys   []string
	Resync bool
}

// Options tune a Watcher. The zero value is the production configuration.
type Options struct {
	// MaxDirs caps the directories watched (default 256).
	MaxDirs int
	// Debounce is the quiet period before an event is delivered (default
	// 120ms): a save is several filesystem events and wants one report.
	Debounce time.Duration
	// MaxWait caps how long a continuous stream of events can postpone the
	// report (default 1s).
	MaxWait time.Duration
	// Rewatch is how often a directory that disappeared is looked for again
	// (default 500ms, up to rewatchTries times). Negative turns the retry off:
	// Set still repairs the watch.
	Rewatch time.Duration
}

const (
	defaultMaxDirs  = 256
	defaultDebounce = 120 * time.Millisecond
	defaultMaxWait  = time.Second
	defaultRewatch  = 500 * time.Millisecond
	// rewatchTries bounds the retries: a directory gone for good is the
	// documents' business (they report as missing), not a timer that runs forever.
	rewatchTries = 40
)

// Watcher watches the directories of a set of documents.
type Watcher struct {
	emit func(Event)
	opts Options
	fsw  *fsnotify.Watcher

	mu sync.Mutex
	// byDir maps a watched directory to the documents in it, by file name.
	byDir map[string]map[string][]string
	// pending are the keys waiting out the debounce.
	pending map[string]struct{}
	// lost are directories that were watched and went away (deleted, renamed).
	// The kernel drops the watch with them, so they are watched again when they
	// come back — by the retry below or by the next Set.
	lost        map[string]map[string][]string
	rewatchLeft int
	rewatchTmr  *time.Timer
	resync      bool
	timer       *time.Timer
	firstSeen   time.Time
	closed      bool

	done chan struct{}
}

// New starts a Watcher. emit is called from the watcher's own goroutine, never
// while holding its lock, and must not block for long.
func New(emit func(Event), opts Options) (*Watcher, error) {
	if opts.MaxDirs <= 0 {
		opts.MaxDirs = defaultMaxDirs
	}
	if opts.Debounce <= 0 {
		opts.Debounce = defaultDebounce
	}
	if opts.MaxWait <= 0 {
		opts.MaxWait = defaultMaxWait
	}
	if opts.Rewatch == 0 {
		opts.Rewatch = defaultRewatch
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		emit:    emit,
		opts:    opts,
		fsw:     fsw,
		byDir:   map[string]map[string][]string{},
		lost:    map[string]map[string][]string{},
		pending: map[string]struct{}{},
		done:    make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

// Set replaces the watched documents with docs. It adds watches for new
// directories and releases the ones no document needs any more, so calling it
// with the editor's current open set on every change is the whole protocol.
//
// unwatched are the keys that could not be watched — their directory is gone,
// is unreadable, hit the watch ceiling or the system's inotify limit. The
// caller should fall back to looking at those when the window regains focus.
func (w *Watcher) Set(docs []Doc) (unwatched []string) {
	want := map[string]map[string][]string{}
	for _, d := range docs {
		dir, base := filepath.Split(filepath.Clean(d.Path))
		dir = filepath.Clean(dir)
		if want[dir] == nil {
			want[dir] = map[string][]string{}
		}
		want[dir][base] = append(want[dir][base], d.Key)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return keysOf(docs)
	}
	for dir := range w.byDir {
		if _, keep := want[dir]; !keep {
			_ = w.fsw.Remove(dir)
			delete(w.byDir, dir)
		}
	}
	for dir := range w.lost {
		delete(w.lost, dir) // what is wanted now is decided below
	}
	dirs := make([]string, 0, len(want))
	for dir := range want {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs) // a stable choice of which directories fit under the ceiling
	for _, dir := range dirs {
		if _, have := w.byDir[dir]; have {
			// Trust the watch only while the directory is the one it was set on: a
			// directory deleted and created again has a new inode and no watch.
			if !w.alive(dir) {
				_ = w.fsw.Remove(dir)
				delete(w.byDir, dir)
			} else {
				w.byDir[dir] = want[dir]
				continue
			}
		}
		if len(w.byDir) >= w.opts.MaxDirs || w.fsw.Add(dir) != nil {
			for _, keys := range want[dir] {
				unwatched = append(unwatched, keys...)
			}
			continue
		}
		w.byDir[dir] = want[dir]
	}
	sort.Strings(unwatched)
	return unwatched
}

// Dirs is how many directories are being watched.
func (w *Watcher) Dirs() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.byDir)
}

// Close stops the watcher and releases every watch. Safe to call twice.
func (w *Watcher) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.rewatchTmr != nil {
		w.rewatchTmr.Stop()
	}
	w.mu.Unlock()
	_ = w.fsw.Close()
	<-w.done
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// An overflowed queue or a failing watcher means events were lost.
			_ = err
			w.mark(nil, true)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	path := filepath.Clean(ev.Name)
	dir, base := filepath.Dir(path), filepath.Base(path)
	w.mu.Lock()
	var keys []string
	if files, ok := w.byDir[dir]; ok {
		keys = append(keys, files[base]...)
	}
	// The watched directory itself going away (or being renamed) invalidates
	// every document in it.
	if files, ok := w.byDir[path]; ok && ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		for _, ks := range files {
			keys = append(keys, ks...)
		}
		// Its kernel watch is gone with it. Keep the documents so the directory
		// can be watched again when it comes back.
		_ = w.fsw.Remove(path)
		delete(w.byDir, path)
		w.lost[path] = files
		w.armRewatch()
	}
	w.mu.Unlock()
	if len(keys) > 0 {
		w.mark(keys, false)
	}
}

// alive reports whether dir is still the directory the watch was added to. fsnotify
// forgets a watch when its directory is removed, so a directory that exists but is
// not in the watch list was replaced.
func (w *Watcher) alive(dir string) bool {
	for _, p := range w.fsw.WatchList() {
		if filepath.Clean(p) == dir {
			_, err := os.Stat(dir)
			return err == nil
		}
	}
	return false
}

// armRewatch schedules a look for lost directories. Holds w.mu.
func (w *Watcher) armRewatch() {
	if w.opts.Rewatch < 0 || w.closed || w.rewatchTmr != nil {
		return
	}
	w.rewatchLeft = rewatchTries
	w.rewatchTmr = time.AfterFunc(w.opts.Rewatch, w.rewatch)
}

// rewatch watches lost directories that exist again, and reports their
// documents: they may hold other content than when the watch was lost.
func (w *Watcher) rewatch() {
	w.mu.Lock()
	if w.closed {
		w.rewatchTmr = nil
		w.mu.Unlock()
		return
	}
	var back []string
	for dir, files := range w.lost {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if len(w.byDir) >= w.opts.MaxDirs || w.fsw.Add(dir) != nil {
			continue
		}
		w.byDir[dir] = files
		delete(w.lost, dir)
		for _, ks := range files {
			back = append(back, ks...)
		}
	}
	w.rewatchLeft--
	if len(w.lost) > 0 && w.rewatchLeft > 0 {
		w.rewatchTmr = time.AfterFunc(w.opts.Rewatch, w.rewatch)
	} else {
		w.rewatchTmr = nil
	}
	w.mu.Unlock()
	if len(back) > 0 {
		w.mark(back, false)
	}
}

// mark records keys (or a resync) and arms the debounce timer.
func (w *Watcher) mark(keys []string, resync bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	for _, k := range keys {
		w.pending[k] = struct{}{}
	}
	if resync {
		w.resync = true
	}
	now := time.Now()
	if w.timer == nil {
		w.firstSeen = now
		w.timer = time.AfterFunc(w.opts.Debounce, w.flush)
		return
	}
	// A burst keeps pushing the report back, but not past MaxWait.
	wait := w.opts.Debounce
	if left := w.opts.MaxWait - now.Sub(w.firstSeen); left < wait {
		wait = left
	}
	if wait < 0 {
		wait = 0
	}
	w.timer.Reset(wait)
}

func (w *Watcher) flush() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	ev := Event{Resync: w.resync}
	for k := range w.pending {
		ev.Keys = append(ev.Keys, k)
	}
	sort.Strings(ev.Keys)
	w.pending = map[string]struct{}{}
	w.resync = false
	w.timer = nil
	w.mu.Unlock()
	if len(ev.Keys) > 0 || ev.Resync {
		w.emit(ev)
	}
}

func keysOf(docs []Doc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Key)
	}
	sort.Strings(out)
	return out
}
