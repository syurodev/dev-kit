// Package wssearch finds text in the files of a workspace's folders.
//
// It is a leaf: standard library only, no knowledge of workspaces, vaults or
// the bridge. The caller says which directories to search and in what order;
// this package lists their files, matches them on a bounded pool of workers
// and returns the hits in an order that depends only on the folders, the paths
// and the positions — never on which worker finished first.
//
// All file I/O and matching happens here, in Go. The frontend sends a query
// and receives matches; it never receives the workspace.
package wssearch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"
)

// Defaults and ceilings. A search is a question with a bounded answer: when it
// would be larger it says so (Result.Truncated) rather than growing.
const (
	DefaultMaxMatches = 2000
	HardMaxMatches    = 10000
	// DefaultMaxFileBytes: a source file larger than this is generated or
	// data, and reading it whole is what a worker pool is meant to avoid.
	DefaultMaxFileBytes = 2 << 20
	// maxMatchesPerFile keeps one file full of hits (a lockfile, a fixture)
	// from using the whole budget; the file reports Truncated.
	maxMatchesPerFile = 500
	// binarySniff is how much of a file is checked for a NUL byte.
	binarySniff = 8192
	// maxCandidates bounds the number of files one search will look at.
	maxCandidates = 200000
)

// Root is one folder of the search. Order matters: results are grouped in the
// order roots are given.
type Root struct {
	ID   string
	Path string
}

// Options is a search request. Query is the only required field.
type Options struct {
	Query         string
	Regex         bool
	CaseSensitive bool
	WholeWord     bool
	// Include and Exclude are path patterns (glob.go). Exclude wins.
	Include []string
	Exclude []string
	// MaxMatches caps the hits returned (default DefaultMaxMatches).
	MaxMatches int
	// MaxFileBytes: files larger than this are skipped, and counted as such.
	MaxFileBytes int64
	// Workers bounds concurrent file reads for the whole search, across all
	// folders (default min(GOMAXPROCS, 8)).
	Workers int
}

// Match is one hit. Line and Column are 1-based; Column and Length count UTF-16
// code units, which is how the editor addresses text. Preview is a clipped
// slice of the line and PreviewStart is the 0-based UTF-16 offset in the line
// where it begins, so a match is highlighted at Column-1-PreviewStart.
type Match struct {
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	Length       int    `json:"length"`
	Preview      string `json:"preview"`
	PreviewStart int    `json:"previewStart"`
}

// FileResult is the hits in one file.
type FileResult struct {
	FolderID string  `json:"folderId"`
	Path     string  `json:"path"`
	Matches  []Match `json:"matches"`
	// Truncated: the file had more hits than the per-file ceiling.
	Truncated bool `json:"truncated,omitempty"`
}

// Folder states.
const (
	FolderOK          = "ok"
	FolderUnavailable = "unavailable" // missing, not a directory, or unreadable
	FolderError       = "error"       // listing failed
	FolderDuplicate   = "duplicate"   // the same directory as an earlier folder
)

// FolderStatus says how one folder took part in the search.
type FolderStatus struct {
	FolderID string `json:"folderId"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	// Source is how its files were listed (SourceGit / SourceWalk).
	Source string `json:"source,omitempty"`
	// Files is how many of its files were searched.
	Files int `json:"files"`
	// DuplicateOf names the folder this one repeats, for FolderDuplicate.
	DuplicateOf string `json:"duplicateOf,omitempty"`
}

// Truncation reasons.
const (
	TruncatedMatches = "matches"
	TruncatedFiles   = "files"
)

// Result is the answer to a search.
type Result struct {
	Files   []FileResult   `json:"files"`
	Folders []FolderStatus `json:"folders"`
	// Matches is the number of hits in Files.
	Matches int `json:"matches"`
	// FilesSearched were read and matched; FilesSkipped were binary, too large,
	// not UTF-8, symbolic links or unreadable.
	FilesSearched int `json:"filesSearched"`
	FilesSkipped  int `json:"filesSkipped"`
	// Truncated is "" for a complete answer, else why it is not.
	Truncated string `json:"truncated,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
}

type candidate struct {
	root int
	rel  string
}

type scanned struct {
	matches   []Match
	truncated bool
	skipped   bool
}

// Run searches roots for opts.Query. It returns early with ctx.Err() when
// cancelled; what had been found is discarded, since a partial answer to a
// request nobody is waiting for is only a way to show stale results.
func Run(ctx context.Context, roots []Root, opts Options) (Result, error) {
	started := time.Now()
	m, err := newMatcher(opts)
	if err != nil {
		return Result{}, err
	}
	maxMatches := opts.MaxMatches
	if maxMatches <= 0 {
		maxMatches = DefaultMaxMatches
	}
	if maxMatches > HardMaxMatches {
		maxMatches = HardMaxMatches
	}
	maxBytes := opts.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers > 8 {
			workers = 8
		}
	}
	filter := newPathFilter(opts.Include, opts.Exclude)

	paths := make([]string, len(roots))
	for i, r := range roots {
		paths[i] = r.Path
	}
	skip, dup := nestedSubtrees(paths)

	result := Result{Folders: make([]FolderStatus, len(roots))}
	var cands []candidate
	for i, r := range roots {
		status := FolderStatus{FolderID: r.ID, State: FolderOK}
		switch {
		case !isDir(r.Path):
			status.State = FolderUnavailable
			status.Error = unavailableReason(r.Path)
		case func() bool { _, d := dup[i]; return d }():
			status.State = FolderDuplicate
			status.DuplicateOf = roots[dup[i]].ID
		default:
			files, source, lerr := listFiles(ctx, r.Path, skip[i], filter)
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			status.Source = source
			if lerr != nil {
				status.State = FolderError
				status.Error = lerr.Error()
				break
			}
			for _, f := range files {
				if len(cands) >= maxCandidates {
					result.Truncated = TruncatedFiles
					break
				}
				cands = append(cands, candidate{root: i, rel: f})
			}
		}
		result.Folders[i] = status
	}

	scan := func(c candidate) scanned {
		return scanFile(filepath.Join(roots[c.root].Path, filepath.FromSlash(c.rel)), roots[c.root].Path, m, maxBytes)
	}
	files, searched, skipped, truncated := runOrdered(ctx, cands, workers, maxMatches, scan, roots)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result.Files = files
	result.FilesSearched = searched
	result.FilesSkipped = skipped
	for _, f := range files {
		result.Matches += len(f.Matches)
	}
	if truncated {
		result.Truncated = TruncatedMatches
	}
	counts := make([]int, len(roots))
	for _, c := range cands {
		counts[c.root]++
	}
	for i := range result.Folders {
		result.Folders[i].Files = counts[i]
	}
	result.ElapsedMs = time.Since(started).Milliseconds()
	return result, nil
}

func unavailableReason(path string) string {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return "not a directory"
	case errors.Is(err, os.ErrNotExist):
		return "folder not found"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	default:
		return "folder unavailable"
	}
}

// runOrdered scans candidates on a bounded pool and consumes the results in
// candidate order, so the output — and which hits are kept when the budget
// runs out — is the same on every run whatever the scheduler does. Workers may
// run at most a window ahead of the consumer, which bounds the results held in
// memory.
func runOrdered(ctx context.Context, cands []candidate, workers, maxMatches int, scan func(candidate) scanned, roots []Root) (files []FileResult, searched, skipped int, truncated bool) {
	n := len(cands)
	if n == 0 {
		return nil, 0, 0, false
	}
	if workers > n {
		workers = n
	}
	window := workers * 4

	var (
		mu       sync.Mutex
		cond     = sync.NewCond(&mu)
		slots    = make([]*scanned, n)
		next     int
		consumed int
		stop     bool
	)
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			stop = true
			cond.Broadcast()
			mu.Unlock()
		case <-ctxDone:
		}
	}()
	defer close(ctxDone)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				for !stop && next < n && next >= consumed+window {
					cond.Wait()
				}
				if stop || next >= n {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()

				res := scan(cands[i])

				mu.Lock()
				slots[i] = &res
				cond.Broadcast()
				mu.Unlock()
			}
		}()
	}

	total := 0
	for i := 0; i < n; i++ {
		mu.Lock()
		for slots[i] == nil && !stop {
			cond.Wait()
		}
		res := slots[i]
		slots[i] = nil
		consumed = i + 1
		cond.Broadcast()
		halted := stop && res == nil
		mu.Unlock()
		if halted {
			break
		}

		if res.skipped {
			skipped++
			continue
		}
		searched++
		if len(res.matches) == 0 {
			continue
		}
		room := maxMatches - total
		ms := res.matches
		over := false
		if len(ms) > room {
			ms = ms[:room]
			over = true
		}
		total += len(ms)
		files = append(files, FileResult{
			FolderID:  roots[cands[i].root].ID,
			Path:      cands[i].rel,
			Matches:   ms,
			Truncated: res.truncated || over,
		})
		if total >= maxMatches && (over || i+1 < n) {
			truncated = over || i+1 < n
			mu.Lock()
			stop = true
			cond.Broadcast()
			mu.Unlock()
			break
		}
	}
	mu.Lock()
	stop = true
	cond.Broadcast()
	mu.Unlock()
	wg.Wait()
	return files, searched, skipped, truncated
}

// scanFile reads one file and returns its hits. A symbolic link is not
// followed — it could lead outside the folder — and a file that is binary, too
// large, not valid UTF-8 or unreadable is skipped rather than failing the
// search.
func scanFile(path, root string, m *matcher, maxBytes int64) scanned {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBytes {
		return scanned{skipped: true}
	}
	f, err := os.Open(path)
	if err != nil {
		return scanned{skipped: true}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return scanned{skipped: true}
	}
	sniff := data
	if len(sniff) > binarySniff {
		sniff = sniff[:binarySniff]
	}
	if bytes.IndexByte(sniff, 0) >= 0 || !utf8.Valid(data) {
		return scanned{skipped: true}
	}
	return scanBytes(data, m)
}

// scanBytes matches every line of data. Lines end at \n, with a preceding \r
// removed (CRLF); a lone \r inside a line is ordinary text. There is no line
// length limit: a minified file's single long line is searched whole.
func scanBytes(data []byte, m *matcher) scanned {
	var out scanned
	if !m.couldMatch(data) {
		return out
	}
	lineNo := 0
	for pos := 0; pos <= len(data); {
		end := bytes.IndexByte(data[pos:], '\n')
		var line []byte
		if end < 0 {
			line = data[pos:]
			end = len(data)
		} else {
			line = data[pos : pos+end]
			end = pos + end
		}
		lineNo++
		next := end + 1
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 {
			cursor := utf16Cursor{line: line}
			full := false
			m.each(line, func(start, stop int) bool {
				if len(out.matches) >= maxMatchesPerFile {
					out.truncated = true
					full = true
					return false
				}
				col := cursor.at(start)
				length := cursor.at(stop) - col
				preview, from := previewOf(line, start, stop)
				// The preview begins a bounded number of bytes before the
				// match, so its offset is the match column less the width of
				// that stretch — never a rescan of the line from its start.
				previewStart := col - utf16Len(line[from:start])
				out.matches = append(out.matches, Match{
					Line:         lineNo,
					Column:       col + 1,
					Length:       length,
					Preview:      preview,
					PreviewStart: previewStart,
				})
				return true
			})
			if full {
				return out
			}
		}
		if next > len(data) {
			break
		}
		pos = next
	}
	return out
}
