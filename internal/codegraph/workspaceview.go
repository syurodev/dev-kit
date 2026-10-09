package codegraph

import (
	"context"
	"errors"
	"os"
)

// What a workspace panel asks of a folder: its entry points and the outline of
// one file. Both can be answered from the index on disk without opening a
// Service — no watcher, no resident slot, no analyzer — so a workspace of
// twenty folders is listed without twenty projects being opened.
// A folder that is open (focused or resident) answers for itself, because its
// index is fresher than the file and knows when it is being refreshed.

// View states of a folder's index.
const (
	ViewIndexed     = SnapshotIndexed    // an index is being served
	ViewIndexing    = SnapshotIndexing   // open, and no index can be served yet
	ViewNotIndexed  = SnapshotNotIndexed // nothing on disk for this folder
	ViewUnsupported = "unsupported"      // no analyzer reads this folder
	ViewFailed      = "failed"           // the last run failed; Error says why
)

// EntryPointsView is one folder's entry points and how current they are.
type EntryPointsView struct {
	Path      string       `json:"path"`
	State     string       `json:"state"`
	Entries   []EntryPoint `json:"entries"`
	IndexedAt int64        `json:"indexedAt,omitempty"`
	// Stale: served from the stored index while a refresh runs.
	Stale bool   `json:"stale,omitempty"`
	Error string `json:"error,omitempty"`
	// MissingToolchain: the failure is a missing analyzer, not the code.
	MissingToolchain bool `json:"missingToolchain,omitempty"`
}

func emptyEntryView(path, state string) EntryPointsView {
	return EntryPointsView{Path: path, State: state, Entries: []EntryPoint{}}
}

// ReadStoredEntryPoints reads a folder's entry points from the index on disk.
func ReadStoredEntryPoints(ctx context.Context, configDir, projectPath string) (EntryPointsView, error) {
	view := emptyEntryView(projectPath, ViewNotIndexed)
	store, ok := openStoredIndex(ctx, configDir, projectPath)
	if !ok {
		return view, nil
	}
	defer store.Close()
	entries, err := store.ListEntryPoints(ctx)
	if err != nil {
		return EntryPointsView{}, err
	}
	entries = completeEntryPoints(entries)
	sortEntryPoints(entries)
	view.State = ViewIndexed
	view.Entries = entries
	view.IndexedAt, _ = store.IndexedAt(ctx)
	return view, nil
}

// openStoredIndex opens the on-disk index of exactly projectPath, or reports
// that there is none (missing file, or a file describing another project).
func openStoredIndex(ctx context.Context, configDir, projectPath string) (*sqliteStore, bool) {
	if configDir == "" {
		return nil, false
	}
	dbPath := ProjectDBPath(configDir, projectPath)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, false
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return nil, false
	}
	if !storedIndexMatches(ctx, store, projectPath) {
		_ = store.Close()
		return nil, false
	}
	return store, true
}

func (r *Registry) configDir() string {
	if r.focus == nil {
		return ""
	}
	return r.focus.Host().ConfigDir()
}

// EntryPointsOf is the entry points of the folder at servicePath: from its
// Service when one is open, else from the index on disk. It never opens a
// Service.
func (r *Registry) EntryPointsOf(ctx context.Context, servicePath string) (EntryPointsView, error) {
	if svc, ok := r.Service(servicePath); ok && svc != nil && servicePath != "" {
		view := emptyEntryView(servicePath, ViewIndexed)
		entries, err := svc.ListEntryPoints(ctx)
		switch {
		case err == nil:
			view.Entries = entries
			if info, ierr := svc.IndexInfo(ctx); ierr == nil {
				view.IndexedAt, view.Stale = info.IndexedAt, info.Stale
			}
			return view, nil
		case errors.Is(err, ErrUnsupportedProject):
			view.State = ViewUnsupported
			return view, nil
		case errors.Is(err, ErrIndexNotReady):
			info, ierr := svc.IndexInfo(ctx)
			if ierr == nil && info.Error != "" && !info.Indexing {
				view.State, view.Error = ViewFailed, info.Error
				return view, nil
			}
			view.State = ViewIndexing
			return view, nil
		case errors.Is(err, ErrNoActiveProject):
			// Registered but not holding a project (closing): read the disk.
		default:
			return EntryPointsView{}, err
		}
	}
	return ReadStoredEntryPoints(ctx, r.configDir(), servicePath)
}

// OutlineSymbol is one declaration in a file, with those it contains.
type OutlineSymbol struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine"`
	// Children are the declarations inside this one, in source order. Siblings
	// start at increasing lines, so a position is found by binary search.
	Children []OutlineSymbol `json:"children,omitempty"`
}

// FileOutline is a file's declarations as the index last saw it. It describes
// the SAVED file as of IndexedAt, never an unsaved buffer.
type FileOutline struct {
	Path      string          `json:"path"`
	State     string          `json:"state"`
	Symbols   []OutlineSymbol `json:"symbols"`
	IndexedAt int64           `json:"indexedAt,omitempty"`
}

// BuildOutline nests nodes by the line ranges they cover. nodes must be ordered
// by start line, outermost first (Store.ListNodesInFile); a node whose range is
// not inside the one before it — including one that only partly overlaps — is
// not its child.
func BuildOutline(nodes []Node) []OutlineSymbol {
	type built struct {
		sym      OutlineSymbol
		children []*built
	}
	var top, chain []*built
	for _, n := range nodes {
		if n.Line <= 0 {
			continue
		}
		end := n.EndLine
		if end < n.Line {
			end = n.Line
		}
		b := &built{sym: OutlineSymbol{ID: n.ID, Label: n.Label, Kind: n.Kind, Line: n.Line, EndLine: end}}
		for len(chain) > 0 {
			p := chain[len(chain)-1].sym
			if p.Line <= b.sym.Line && b.sym.EndLine <= p.EndLine {
				break
			}
			chain = chain[:len(chain)-1]
		}
		if len(chain) == 0 {
			top = append(top, b)
		} else {
			parent := chain[len(chain)-1]
			parent.children = append(parent.children, b)
		}
		chain = append(chain, b)
	}
	var flatten func(bs []*built) []OutlineSymbol
	flatten = func(bs []*built) []OutlineSymbol {
		out := make([]OutlineSymbol, 0, len(bs))
		for _, b := range bs {
			s := b.sym
			s.Children = flatten(b.children)
			if len(s.Children) == 0 {
				s.Children = nil
			}
			out = append(out, s)
		}
		return out
	}
	return flatten(top)
}

func outlineFrom(ctx context.Context, store Store, relPath string) (FileOutline, error) {
	nodes, err := store.ListNodesInFile(ctx, relPath)
	if err != nil {
		return FileOutline{}, err
	}
	out := FileOutline{Path: relPath, State: ViewIndexed, Symbols: BuildOutline(nodes)}
	out.IndexedAt, _ = store.IndexedAt(ctx)
	return out, nil
}

// OutlineOf is the outline of one file of the folder at servicePath: from its
// Service when open, else from the index on disk. relPath is relative to the
// folder, slash-separated.
func (r *Registry) OutlineOf(ctx context.Context, servicePath, relPath string) (FileOutline, error) {
	rel, err := projectRelative(servicePath, relPath)
	if err != nil {
		return FileOutline{}, err
	}
	notIndexed := FileOutline{Path: rel, State: ViewNotIndexed, Symbols: []OutlineSymbol{}}
	if svc, ok := r.Service(servicePath); ok && svc != nil && servicePath != "" {
		store, release, err := svc.activeStore()
		switch {
		case err == nil:
			defer release()
			return outlineFrom(ctx, store, rel)
		case errors.Is(err, ErrUnsupportedProject):
			notIndexed.State = ViewUnsupported
			return notIndexed, nil
		case errors.Is(err, ErrIndexNotReady):
			notIndexed.State = ViewIndexing
			return notIndexed, nil
		case errors.Is(err, ErrNoActiveProject):
		default:
			return FileOutline{}, err
		}
	}
	store, ok := openStoredIndex(ctx, r.configDir(), servicePath)
	if !ok {
		return notIndexed, nil
	}
	defer store.Close()
	return outlineFrom(ctx, store, rel)
}
