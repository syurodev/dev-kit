// internal/codegraph/rootfiles.go
package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"devkit/internal/gitparse"
)

// Root-scoped file operations.
//
// Everything in this file takes the folder it works on as an argument. None of
// it reads a Service, so none of it depends on which project the app has
// "focused", on whether an index exists, or on an analyzer being installed
// (docs/design/codegraph-workspace.md § 4). A workspace folder that cannot be
// analysed — unsupported language, broken toolchain, still indexing — is still
// a folder whose files can be listed, read, edited and saved.

// ErrFileConflict is WriteFileAt refusing to overwrite a file that is not the
// one the caller read: the bytes on disk have another revision than the one
// the buffer is based on (or the file appeared / disappeared). The caller's
// edits are untouched; the fix is to compare, reload or choose to overwrite
// knowingly — never to retry the same call.
var ErrFileConflict = errors.New("codegraph: the file changed on disk")

// AbsentRevision is the expected revision of a file that must not exist yet.
// WriteFileAt given it creates the file, and conflicts if one is there.
const AbsentRevision = "absent"

// AnyRevision skips the revision check. Only for a caller that has
// deliberately chosen to overwrite what is on disk ("Overwrite" after a
// conflict was shown) — never a default.
const AnyRevision = "*"

// FileContent is a text file as the editor opens it.
type FileContent struct {
	Content string `json:"content"`
	// Revision identifies exactly these bytes (see RevisionOf).
	Revision string `json:"revision"`
	Size     int64  `json:"size"`
	// ModifiedAt is the file's mtime, Unix milliseconds. Informational: the
	// revision, not the clock, decides whether a file changed.
	ModifiedAt int64 `json:"modifiedAt"`
}

// FileStatus is what is on disk at a path, without its text.
type FileStatus struct {
	Exists bool `json:"exists"`
	// Revision is the revision of the bytes on disk; "" when the file does
	// not exist or was too large to hash.
	Revision   string `json:"revision"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modifiedAt"`
	// Unreadable says why the editor cannot open it as text, when it cannot:
	// "binary", "too_large" or "not_regular". The revision of a binary file
	// is still the hash of its bytes.
	Unreadable string `json:"unreadable,omitempty"`
}

// RevisionOf is the identity of a file's bytes: the first 128 bits of SHA-256,
// hex. Content-derived on purpose. An mtime or inode comparison misses a
// same-second rewrite and false-alarms on `touch`; a hash only changes when
// the bytes do, which is the question "did someone else edit this?".
func RevisionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// ListFilesAt walks root and returns its files and directories as a flat,
// path-sorted list, skipping the shared ignored directories and stopping at
// maxListFiles. truncated says the list was cut. Symlinked entries are listed
// and not followed.
func ListFilesAt(ctx context.Context, root string) (entries []FileEntry, truncated bool, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			// An unreadable subdirectory is a hole in the tree, not a reason
			// to list nothing.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() && ignoredDir(d.Name()) {
			return filepath.SkipDir
		}
		if len(entries) >= maxListFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), IsDir: d.IsDir()})
		return nil
	})
	if walkErr != nil {
		return nil, false, fmt.Errorf("codegraph: list files: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, truncated, nil
}

// ReadFileAt reads a text file under root, with the revision of the bytes read.
func ReadFileAt(root, relativePath string) (FileContent, error) {
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return FileContent{}, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return FileContent{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return FileContent{}, fmt.Errorf("codegraph: read file: %s is not a regular file", relativePath)
	}
	if info.Size() > maxReadFileBytes {
		return FileContent{}, fmt.Errorf("%w: %d bytes, the limit is %d", ErrFileTooLarge, info.Size(), maxReadFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReadFileBytes+1))
	if err != nil {
		return FileContent{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	if len(data) > maxReadFileBytes {
		return FileContent{}, fmt.Errorf("%w: the limit is %d bytes", ErrFileTooLarge, maxReadFileBytes)
	}
	if !utf8.Valid(data) {
		return FileContent{}, ErrBinaryFile
	}
	return FileContent{
		Content:    string(data),
		Revision:   RevisionOf(data),
		Size:       int64(len(data)),
		ModifiedAt: info.ModTime().UnixMilli(),
	}, nil
}

// StatFileAt reports what is on disk at a path. A missing file is a normal
// answer (Exists false), not an error: the editor asks this to reconcile its
// buffer after a file watcher event, and "it was deleted" is one of the
// answers.
func StatFileAt(root, relativePath string) (FileStatus, error) {
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return FileStatus{}, err
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return FileStatus{}, nil
	}
	if err != nil {
		return FileStatus{}, fmt.Errorf("codegraph: stat file: %w", err)
	}
	status := FileStatus{Exists: true, Size: info.Size(), ModifiedAt: info.ModTime().UnixMilli()}
	if !info.Mode().IsRegular() {
		status.Unreadable = "not_regular"
		return status, nil
	}
	if info.Size() > maxReadFileBytes {
		status.Unreadable = "too_large"
		return status, nil
	}
	data, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return FileStatus{}, nil
	}
	if err != nil {
		return FileStatus{}, fmt.Errorf("codegraph: stat file: %w", err)
	}
	status.Size = int64(len(data))
	status.Revision = RevisionOf(data)
	if !utf8.Valid(data) {
		status.Unreadable = "binary"
	}
	return status, nil
}

// writeLocks serialises WriteFileAt calls on one file inside this process, so
// two saves of the same buffer (a double Cmd-S, or two windows) cannot both
// pass the revision check against the same disk state and then rename over
// each other. Striped by a hash of the resolved path: a fixed number of
// mutexes, however many files are ever saved.
var writeLocks [64]sync.Mutex

func writeLockFor(path string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	return &writeLocks[h.Sum32()%uint32(len(writeLocks))]
}

// WriteFileAt saves content to relativePath under root if — and only if — the
// file on disk still has expectedRevision (or AbsentRevision for a new file,
// or AnyRevision to overwrite knowingly). It returns the revision of the bytes
// it wrote, so the caller can tell its own save from someone else's when the
// file watcher reports the change.
//
// What this guarantees, precisely: no save from this process lands on a file
// that had a different revision when the save began, and saves of one file
// from this process are serialised. What it does not: the filesystem has no
// compare-and-swap, so another program writing in the window between the
// check and the rename is overwritten without notice. The window is
// microseconds, and the temp-file-and-rename keeps the file whole either way;
// it is a detection, not isolation.
//
// The write is atomic (temp file in the same directory, renamed over the
// target) and keeps the existing file's permissions. A failed write leaves the
// original untouched.
func WriteFileAt(root, relativePath, content, expectedRevision string) (FileContent, error) {
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return FileContent{}, err
	}
	lock := writeLockFor(resolved)
	lock.Lock()
	defer lock.Unlock()

	if expectedRevision != AnyRevision {
		current, err := currentRevision(resolved)
		if err != nil {
			return FileContent{}, fmt.Errorf("codegraph: write file: %w", err)
		}
		want := expectedRevision
		if want == "" {
			// A caller that names no revision gets the strict reading: it
			// believes the file is new.
			want = AbsentRevision
		}
		if current != want {
			return FileContent{}, fmt.Errorf("%w: expected %s, found %s", ErrFileConflict, want, current)
		}
	}
	data := []byte(content)
	// resolvePath has already kept the path inside root, symlinks included, so
	// creating the missing directories of a new file cannot leave it.
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return FileContent{}, fmt.Errorf("codegraph: write file: %w", err)
	}
	if err := writeFileAtomic(resolved, data); err != nil {
		return FileContent{}, fmt.Errorf("codegraph: write file: %w", err)
	}
	info, statErr := os.Stat(resolved)
	out := FileContent{Revision: RevisionOf(data), Size: int64(len(data))}
	if statErr == nil {
		out.ModifiedAt = info.ModTime().UnixMilli()
	}
	return out, nil
}

// currentRevision is the revision on disk at path, AbsentRevision when there is
// no file. A file past the read ceiling hashes as its size and mtime: it can
// never be a buffer's base, so it can only ever be a conflict.
func currentRevision(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return AbsentRevision, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxReadFileBytes {
		return fmt.Sprintf("big-%d-%d", info.Size(), info.ModTime().UnixNano()), nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return AbsentRevision, nil
	}
	if err != nil {
		return "", err
	}
	return RevisionOf(data), nil
}

// gitTimeout bounds the git subprocesses of the folder-level readers. A work
// tree on a network mount that stopped answering must not hold up a sidebar
// that is asking about ten folders.
const gitTimeout = 8 * time.Second

// GitStatusAt maps each changed file under root, by path relative to root with
// forward slashes, to a one-letter state: M modified, A added (staged or
// untracked), D deleted, R renamed. Outside a work tree, or without git, it is
// an empty map: this decorates a tree and must never break it.
func GitStatusAt(ctx context.Context, root string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	prefixOut, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return map[string]string{}, nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return map[string]string{}, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return gitparse.Porcelain(out, strings.TrimSpace(string(prefixOut))), nil
}

// FolderGit is what the sidebar says about a folder's repository.
type FolderGit struct {
	IsRepo bool `json:"isRepo"`
	// Branch is the checked-out branch, "HEAD" for a detached head, "" when the
	// folder is not a work tree.
	Branch string `json:"branch"`
	// Changed counts files git reports as changed; -1 when it could not be
	// read in time.
	Changed int `json:"changed"`
}

// GitInfoAt reads a folder's branch and change count.
func GitInfoAt(ctx context.Context, root string) FolderGit {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		// A repository with no commit yet fails rev-parse HEAD; symbolic-ref
		// still names its branch.
		sym, symErr := exec.CommandContext(ctx, "git", "-C", root, "symbolic-ref", "--short", "-q", "HEAD").Output()
		if symErr != nil {
			return FolderGit{Changed: 0}
		}
		out = sym
	}
	info := FolderGit{IsRepo: true, Branch: strings.TrimSpace(string(out)), Changed: -1}
	if changes, err := GitStatusAt(ctx, root); err == nil {
		info.Changed = len(changes)
	}
	return info
}

// ResolveInRoot is the absolute, symlink-resolved path of relativePath under
// root, or ErrPathOutsideProject when it would leave root. For callers that
// need the real path of a document (a file watcher) and must apply the same
// containment rule the readers and writers do.
func ResolveInRoot(root, relativePath string) (string, error) {
	return resolvePath(root, relativePath)
}
