package app

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"

	"devkit/internal/codegraph"
	"devkit/internal/docwatch"
	"devkit/internal/workspace"
	"devkit/internal/wssearch"
)

// WorkspaceCode is what Code Graph asks of a workspace's folders.
//
// Every method names its folder by (workspaceID, folderID) — the workspace and
// the registry row — and this is the only place those become a path. The
// frontend never sends a path to read or write, so a buffer can only be saved
// into a folder that is, at the moment of the save, a member of the workspace
// the buffer was opened under: a folder removed from the workspace, or a stale
// ID, is ErrFolderNotInWorkspace and writes nothing.
//
// Nothing here consults the "focused" Code Graph project. A request carries its
// owner, so concurrent requests for different folders cannot race over which
// project is active, and one folder that cannot be analysed (no analyzer, a
// broken toolchain, an index still building) never stops another's files being
// read.
type WorkspaceCode struct {
	c *Container

	// Reveal shows a resolved file in the file manager; tests replace it.
	Reveal func(path string) error

	mu       sync.Mutex
	searches map[string]*searchRun
	watch    *docwatch.Watcher
}

// searchRun is a registered search. A pointer, so "my own entry" can be told
// from a newer search's by identity (funcs cannot be compared).
type searchRun struct{ cancel context.CancelFunc }

// NewWorkspaceCode builds the service over a container. It reads the
// container's Workspace on every call: main.go replaces the in-memory registry
// with the SQL-backed one after construction.
func NewWorkspaceCode(c *Container) *WorkspaceCode {
	return &WorkspaceCode{c: c, Reveal: RevealInFileManager, searches: map[string]*searchRun{}}
}

// ErrFolderNotInWorkspace: the folder is not (or is no longer) one of the
// workspace's.
var ErrFolderNotInWorkspace = workspace.ErrServiceNotFound

// Folder resolves a folder of a workspace.
func (w *WorkspaceCode) Folder(workspaceID, folderID string) (workspace.WorkspaceService, error) {
	if w.c.Workspace == nil {
		return workspace.WorkspaceService{}, ErrFolderNotInWorkspace
	}
	services, err := w.c.Workspace.ListServices(workspaceID)
	if err != nil {
		return workspace.WorkspaceService{}, err
	}
	for _, svc := range services {
		if svc.ID == folderID {
			return svc, nil
		}
	}
	return workspace.WorkspaceService{}, ErrFolderNotInWorkspace
}

// FolderState is the sidebar's facts about a folder that need the machine, not
// the registry: whether it is there, and what git says.
type FolderState struct {
	FolderID   string `json:"folderId"`
	Accessible bool   `json:"accessible"`
	Error      string `json:"error,omitempty"`
	IsRepo     bool   `json:"isRepo"`
	Branch     string `json:"branch"`
	// Changed is the number of changed files; -1 when git did not answer in time.
	Changed int `json:"changed"`
}

// FolderStates reads every folder of the workspace, a few at a time. A folder
// that cannot be read is reported, not an error of the call.
func (w *WorkspaceCode) FolderStates(ctx context.Context, workspaceID string) ([]FolderState, error) {
	services, err := w.c.Workspace.ListServices(workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]FolderState, len(services))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, svc := range services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st := FolderState{FolderID: svc.ID, Accessible: true}
			info, statErr := os.Stat(svc.Path)
			switch {
			case statErr != nil:
				st.Accessible = false
				switch {
				case errors.Is(statErr, os.ErrNotExist):
					st.Error = "folder not found"
				case errors.Is(statErr, os.ErrPermission):
					st.Error = "permission denied"
				default:
					st.Error = "folder unavailable"
				}
			case !info.IsDir():
				st.Accessible = false
				st.Error = "not a directory"
			default:
				git := codegraph.GitInfoAt(ctx, svc.Path)
				st.IsRepo, st.Branch, st.Changed = git.IsRepo, git.Branch, git.Changed
			}
			out[i] = st
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FileList is a folder's file tree.
type FileList struct {
	Entries   []codegraph.FileEntry `json:"entries"`
	Truncated bool                  `json:"truncated"`
}

func (w *WorkspaceCode) ListFiles(ctx context.Context, workspaceID, folderID string) (FileList, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return FileList{}, err
	}
	entries, truncated, err := codegraph.ListFilesAt(ctx, svc.Path)
	if err != nil {
		return FileList{}, err
	}
	if entries == nil {
		entries = []codegraph.FileEntry{}
	}
	return FileList{Entries: entries, Truncated: truncated}, nil
}

func (w *WorkspaceCode) ReadFile(workspaceID, folderID, rel string) (codegraph.FileContent, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return codegraph.FileContent{}, err
	}
	return codegraph.ReadFileAt(svc.Path, rel)
}

// RevealFile shows a file of a folder in the operating system's file manager.
// The path is resolved here, inside the folder, so the window never names one.
func (w *WorkspaceCode) RevealFile(workspaceID, folderID, rel string) error {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return err
	}
	path, err := codegraph.ResolveExistingFileAt(svc.Path, rel)
	if err != nil {
		return err
	}
	return w.Reveal(path)
}

func (w *WorkspaceCode) StatFile(workspaceID, folderID, rel string) (codegraph.FileStatus, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return codegraph.FileStatus{}, err
	}
	return codegraph.StatFileAt(svc.Path, rel)
}

// WriteFile saves a buffer into its owner folder if the file on disk still has
// expectedRevision (codegraph.WriteFileAt says what that guarantees). The
// returned content is empty; the revision is what the caller keeps.
func (w *WorkspaceCode) WriteFile(workspaceID, folderID, rel, content, expectedRevision string) (codegraph.FileContent, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return codegraph.FileContent{}, err
	}
	if _, err := os.Stat(svc.Path); err != nil {
		// The folder is registered but gone from the disk: writing would
		// recreate its root as a bare directory. Refuse, keep the edits.
		return codegraph.FileContent{}, err
	}
	return codegraph.WriteFileAt(svc.Path, rel, content, expectedRevision)
}

func (w *WorkspaceCode) GitStatus(ctx context.Context, workspaceID, folderID string) (map[string]string, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return nil, err
	}
	return codegraph.GitStatusAt(ctx, svc.Path)
}

func (w *WorkspaceCode) registry() (*codegraph.Registry, error) {
	if w.c.CodeGraphRegistry == nil {
		return nil, codegraph.ErrNoActiveProject
	}
	return w.c.CodeGraphRegistry, nil
}

// EntryPoints is a folder's entry points with the index state that explains an
// empty list: not indexed yet, indexing, unsupported, failed.
func (w *WorkspaceCode) EntryPoints(ctx context.Context, workspaceID, folderID string) (codegraph.EntryPointsView, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return codegraph.EntryPointsView{}, err
	}
	reg, err := w.registry()
	if err != nil {
		return codegraph.EntryPointsView{}, err
	}
	view, err := reg.EntryPointsOf(ctx, svc.Path)
	if err != nil {
		return codegraph.EntryPointsView{}, err
	}
	return mergeIndexStatus(view, ServiceIndexStatus(w.c, svc)), nil
}

// mergeIndexStatus lays the queue's and the registry's account of a folder over
// what its index file says. The index answers "what is known"; the status
// answers "what is happening to it": a queued or running folder is being
// (re)indexed, a failed one carries why — and whatever index it still has is
// kept and marked, not hidden.
func mergeIndexStatus(view codegraph.EntryPointsView, st codegraph.ServiceIndexStatus) codegraph.EntryPointsView {
	switch st.State {
	case codegraph.IndexPending, codegraph.IndexIndexing:
		if view.State == codegraph.ViewNotIndexed {
			view.State = codegraph.ViewIndexing
		} else if view.State == codegraph.ViewIndexed {
			view.Stale = true
		}
	case codegraph.IndexFailed:
		view.Error, view.MissingToolchain = st.Error, st.MissingToolchain
		if view.State == codegraph.ViewNotIndexed || view.State == codegraph.ViewIndexing {
			view.State = codegraph.ViewFailed
		}
	case codegraph.IndexUnsupported:
		view.State = codegraph.ViewUnsupported
		view.Error = st.Error
		view.Entries = []codegraph.EntryPoint{}
	}
	return view
}

// Outline is the declarations of one file as the index last saw them.
func (w *WorkspaceCode) Outline(ctx context.Context, workspaceID, folderID, rel string) (codegraph.FileOutline, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return codegraph.FileOutline{}, err
	}
	reg, err := w.registry()
	if err != nil {
		return codegraph.FileOutline{}, err
	}
	return reg.OutlineOf(ctx, svc.Path, rel)
}

// FolderSymbols is the symbols matching a query in one folder.
type FolderSymbols struct {
	FolderID string                  `json:"folderId"`
	State    string                  `json:"state"`
	Matches  []codegraph.SymbolMatch `json:"matches"`
}

// SearchSymbols looks for classes and methods in the indexes of the chosen
// folders (all when folderIDs is empty), without opening any project: each
// folder's index file is read, or its open Service asked. Folders are listed in
// the workspace's order; one that cannot answer says why instead of failing the
// rest.
func (w *WorkspaceCode) SearchSymbols(ctx context.Context, workspaceID string, folderIDs []string, query string) ([]FolderSymbols, error) {
	reg, err := w.registry()
	if err != nil {
		return nil, err
	}
	services, err := w.c.Workspace.ListServices(workspaceID)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range folderIDs {
		wanted[id] = true
	}
	var out []FolderSymbols
	for _, svc := range services {
		if len(wanted) > 0 && !wanted[svc.ID] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		view, err := reg.SymbolsOf(ctx, svc.Path, query)
		if err != nil {
			out = append(out, FolderSymbols{FolderID: svc.ID, State: codegraph.ViewFailed, Matches: []codegraph.SymbolMatch{}})
			continue
		}
		out = append(out, FolderSymbols{FolderID: svc.ID, State: view.State, Matches: view.Matches})
	}
	return out, nil
}

// SearchRequest is a find-in-workspace request. FolderIDs empty means every
// folder of the workspace. RequestID names the search so it can be cancelled.
type SearchRequest struct {
	RequestID     string   `json:"requestId"`
	WorkspaceID   string   `json:"workspaceId"`
	FolderIDs     []string `json:"folderIds"`
	Query         string   `json:"query"`
	Regex         bool     `json:"regex"`
	CaseSensitive bool     `json:"caseSensitive"`
	WholeWord     bool     `json:"wholeWord"`
	Include       []string `json:"include"`
	Exclude       []string `json:"exclude"`
	MaxMatches    int      `json:"maxMatches"`
}

// Search runs a search over the chosen folders, in the workspace's folder order
// (primary first, then by name — the Explorer's order), and returns hits
// grouped by folder. A newer search with the same RequestID cancels the older
// one; CancelSearch cancels by ID.
func (w *WorkspaceCode) Search(ctx context.Context, req SearchRequest) (wssearch.Result, error) {
	services, err := w.c.Workspace.ListServices(req.WorkspaceID)
	if err != nil {
		return wssearch.Result{}, err
	}
	if len(services) == 0 {
		return wssearch.Result{Files: []wssearch.FileResult{}, Folders: []wssearch.FolderStatus{}}, nil
	}
	wanted := map[string]bool{}
	for _, id := range req.FolderIDs {
		wanted[id] = true
	}
	var roots []wssearch.Root
	for _, svc := range services {
		if len(wanted) == 0 || wanted[svc.ID] {
			roots = append(roots, wssearch.Root{ID: svc.ID, Path: svc.Path})
			delete(wanted, svc.ID)
		}
	}
	if len(wanted) > 0 {
		return wssearch.Result{}, ErrFolderNotInWorkspace
	}

	ctx, cancel := context.WithCancel(ctx)
	run := &searchRun{cancel: cancel}
	if req.RequestID != "" {
		w.mu.Lock()
		if prev, ok := w.searches[req.RequestID]; ok {
			prev.cancel()
		}
		w.searches[req.RequestID] = run
		w.mu.Unlock()
	}
	defer func() {
		cancel()
		if req.RequestID != "" {
			w.mu.Lock()
			// Only forget our own entry: a newer search may have replaced it.
			if w.searches[req.RequestID] == run {
				delete(w.searches, req.RequestID)
			}
			w.mu.Unlock()
		}
	}()
	return wssearch.Run(ctx, roots, wssearch.Options{
		Query:         req.Query,
		Regex:         req.Regex,
		CaseSensitive: req.CaseSensitive,
		WholeWord:     req.WholeWord,
		Include:       req.Include,
		Exclude:       req.Exclude,
		MaxMatches:    req.MaxMatches,
	})
}

// CancelSearch stops the search with this ID, if it is still running.
func (w *WorkspaceCode) CancelSearch(requestID string) {
	w.mu.Lock()
	run := w.searches[requestID]
	delete(w.searches, requestID)
	w.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// Service is the Code Graph Service of a folder, opened if need be: the folder's
// own plan, comments and graph queries live in its index. Unlike the file and
// view methods above, this one may start an analysis, so it is for the things a
// person explicitly asked of that folder (its plan, a flow).
func (w *WorkspaceCode) Service(ctx context.Context, workspaceID, folderID string) (*codegraph.Service, error) {
	svc, err := w.Folder(workspaceID, folderID)
	if err != nil {
		return nil, err
	}
	reg, err := w.registry()
	if err != nil {
		return nil, err
	}
	return reg.Open(ctx, svc.Path)
}

// SetPrimary changes the workspace's primary folder: see workspace.SetPrimary.
func (w *WorkspaceCode) SetPrimary(workspaceID, folderID string) (workspace.Workspace, error) {
	return w.c.Workspace.SetPrimary(workspaceID, folderID)
}

// Reindex queues the given folders (every folder when folderIDs is empty) to be
// analysed again, or withdraws them from the queue when cancel is set. It
// returns at once; progress arrives as codegraph:indexStatus events.
func (w *WorkspaceCode) Reindex(workspaceID string, folderIDs []string, cancel bool) error {
	reg, err := w.registry()
	if err != nil {
		return err
	}
	services, err := w.c.Workspace.ListServices(workspaceID)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, id := range folderIDs {
		wanted[id] = true
	}
	var paths []string
	for _, svc := range services {
		if len(wanted) == 0 || wanted[svc.ID] {
			paths = append(paths, svc.Path)
			delete(wanted, svc.ID)
		}
	}
	if len(wanted) > 0 {
		return ErrFolderNotInWorkspace
	}
	if cancel {
		reg.CancelIndex(paths...)
	} else {
		reg.Reindex(paths...)
	}
	return nil
}

// WatchedDocument names an open document.
type WatchedDocument struct {
	WorkspaceID string `json:"workspaceId"`
	FolderID    string `json:"folderId"`
	Path        string `json:"path"`
}

// DocumentKey is the key a document is known by in the watcher's events and in
// the frontend: "workspaceId:folderId:path".
func DocumentKey(d WatchedDocument) string {
	return d.WorkspaceID + ":" + d.FolderID + ":" + d.Path
}

const maxWatchedDocuments = 512

// StartWatcher creates the document watcher. emit receives the keys of
// documents that may have changed on disk and whether events were lost
// (resync). Called once from main.go.
func (w *WorkspaceCode) StartWatcher(emit func(keys []string, resync bool)) error {
	watcher, err := docwatch.New(func(ev docwatch.Event) { emit(ev.Keys, ev.Resync) }, docwatch.Options{})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.watch = watcher
	w.mu.Unlock()
	return nil
}

// Close stops the watcher.
func (w *WorkspaceCode) Close() {
	w.mu.Lock()
	watcher := w.watch
	w.watch = nil
	w.mu.Unlock()
	if watcher != nil {
		watcher.Close()
	}
}

// WatchDocuments replaces the set of watched documents with docs and returns
// the keys that could not be watched (their directory is gone, or the watch
// ceiling or the system's limit was reached). Those the frontend checks when
// the window regains focus. Documents whose folder is no longer in the
// workspace are unwatched too.
func (w *WorkspaceCode) WatchDocuments(docs []WatchedDocument) []string {
	w.mu.Lock()
	watcher := w.watch
	w.mu.Unlock()

	var unwatched []string
	if len(docs) > maxWatchedDocuments {
		for _, d := range docs[maxWatchedDocuments:] {
			unwatched = append(unwatched, DocumentKey(d))
		}
		docs = docs[:maxWatchedDocuments]
	}
	var set []docwatch.Doc
	for _, d := range docs {
		svc, err := w.Folder(d.WorkspaceID, d.FolderID)
		if err != nil {
			unwatched = append(unwatched, DocumentKey(d))
			continue
		}
		abs, err := codegraph.ResolveInRoot(svc.Path, d.Path)
		if err != nil {
			unwatched = append(unwatched, DocumentKey(d))
			continue
		}
		set = append(set, docwatch.Doc{Key: DocumentKey(d), Path: abs})
	}
	if watcher == nil {
		for _, d := range set {
			unwatched = append(unwatched, d.Key)
		}
	} else {
		unwatched = append(unwatched, watcher.Set(set)...)
	}
	sort.Strings(unwatched)
	return unwatched
}
