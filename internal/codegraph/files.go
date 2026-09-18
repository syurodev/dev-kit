// internal/codegraph/files.go
package codegraph

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// maxReadFileBytes caps what ReadFile hands across the bridge. The editor
	// is for source files; a 200 MB log or a data dump would be read whole
	// into memory, JSON-encoded, and then choke the webview.
	maxReadFileBytes = 5 << 20
	// maxListFiles caps ListFiles. The ignored directories already remove the
	// usual explosions (node_modules, target); this is the ceiling for a tree
	// that is simply enormous, past which the list is cut rather than the
	// walk allowed to run for minutes.
	maxListFiles = 50000
)

// ListFiles walks the active project's root and returns its files and
// directories as a flat, path-sorted list, skipping the shared ignored
// directories (ignoredDirs) and stopping at maxListFiles entries. Symlinked
// entries are listed but not followed further — WalkDir does not descend into
// a symlink target, matching the same "don't silently trust symlinks" stance
// resolvePath enforces for individual reads/writes.
func (s *Service) ListFiles(ctx context.Context) ([]FileEntry, error) {
	root, err := s.projectFiles()
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
		return nil, fmt.Errorf("codegraph: list files: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func (s *Service) ReadFile(ctx context.Context, relativePath string) (string, error) {
	root, err := s.projectFiles()
	if err != nil {
		return "", err
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return "", err
	}
	return readTextFile(resolved)
}

// readTextFile reads a file for the editor: bounded by maxReadFileBytes and
// refused when it is not UTF-8 text (see ErrBinaryFile).
func readTextFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("codegraph: read file: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > maxReadFileBytes {
		return "", fmt.Errorf("%w: %d bytes, the limit is %d", ErrFileTooLarge, info.Size(), maxReadFileBytes)
	}
	// Bounded even past the Stat: a file can grow between the two calls.
	data, err := io.ReadAll(io.LimitReader(f, maxReadFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("codegraph: read file: %w", err)
	}
	if len(data) > maxReadFileBytes {
		return "", fmt.Errorf("%w: the limit is %d bytes", ErrFileTooLarge, maxReadFileBytes)
	}
	if !utf8.Valid(data) {
		return "", ErrBinaryFile
	}
	return string(data), nil
}

// WriteFile re-validates relativePath against the *current* project root
// (not whatever was active when the file was opened for editing).
//
// expectedRoot closes the other half of that race. The active project can
// change between a UI open and a Ctrl+S save — an agent can switch it over
// MCP while the editor still holds a buffer from the old one — and a relative
// path means something different under the new root: the save would land, as
// a new or overwritten file, in a project the user was not editing. A caller
// that knows which project its buffer came from passes that root, and a
// mismatch is ErrProjectChanged rather than a write. Empty keeps the old
// behaviour, for callers (tests, MCP) with no buffer of their own.
//
// The write is atomic — a temp file in the same directory, renamed over the
// target — so a crash or a full disk mid-save leaves the old file intact
// rather than a truncated one, and the analyzer never reads half a file. The
// existing file's permissions are kept. A symlink inside the project is
// written through: resolvePath has already resolved it (and refused one
// pointing outside), so the rename replaces the target and the link survives.
func (s *Service) WriteFile(ctx context.Context, relativePath, content, expectedRoot string) error {
	root, err := s.projectFiles()
	if err != nil {
		return err
	}
	if expectedRoot != "" && filepath.Clean(expectedRoot) != root {
		return fmt.Errorf("%w: the open project is %s, not %s", ErrProjectChanged, root, filepath.Clean(expectedRoot))
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(resolved, []byte(content)); err != nil {
		return fmt.Errorf("codegraph: write file: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		mode = info.Mode().Perm()
	}
	// The ".tmp" suffix matters: the file watcher ignores it, so the temp file
	// appearing is not mistaken for a source change.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once the rename has happened
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// projectRelative turns a path a caller may have written any of several ways —
// absolute under the root, "./src/Foo.java", "src/../src/Foo.java" — into the
// root-relative, slash-separated form the index stores. The index only ever
// matches that one spelling, so an absolute path used to find no node and
// silently fall back to the raw line range.
func projectRelative(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", ErrPathOutsideProject
		}
		path = rel
	}
	path = filepath.Clean(path)
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideProject
	}
	return filepath.ToSlash(path), nil
}

// GetCodeSnippet returns the real source text for filePath, snapped to the
// enclosing method/class's actual boundaries when the index has one covering
// line — the caller does not need to already know a method's exact line
// range, e.g. clicking anywhere inside a graph node's body should still show
// the whole method. Falls back to the input line/endLine verbatim when
// nothing in the index covers that line (a non-Java file, or an unindexed
// location) — still useful for arbitrary text files' File editing preview.
func (s *Service) GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (CodeSnippet, error) {
	store, root, release, err := s.activeProject()
	if err != nil {
		return CodeSnippet{}, err
	}
	defer release()
	resolved, err := resolvePath(root, filePath)
	if err != nil {
		return CodeSnippet{}, err
	}
	indexPath, err := projectRelative(root, filePath)
	if err != nil {
		return CodeSnippet{}, err
	}

	useLine, useEndLine, fqn := line, endLine, ""
	if node, ok, err := store.FindNodeAtLine(ctx, indexPath, line); err != nil {
		return CodeSnippet{}, err
	} else if ok {
		useLine, useEndLine, fqn = node.Line, node.EndLine, node.Label
	}

	text, err := readTextFile(resolved)
	if err != nil {
		return CodeSnippet{}, err
	}
	lines := strings.Split(text, "\n")
	if useLine < 1 || useEndLine > len(lines) || useLine > useEndLine {
		return CodeSnippet{}, fmt.Errorf("codegraph: line range %d-%d out of bounds for %q (%d lines)", useLine, useEndLine, filePath, len(lines))
	}
	source := strings.Join(lines[useLine-1:useEndLine], "\n")

	return CodeSnippet{
		FilePath: filePath,
		Line:     useLine,
		EndLine:  useEndLine,
		Source:   source,
		Language: languageForPath(filePath),
		FQN:      fqn,
	}, nil
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java":
		return "java"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
