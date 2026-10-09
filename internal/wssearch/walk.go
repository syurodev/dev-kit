package wssearch

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Source says how a folder's files were listed.
const (
	// SourceGit: `git ls-files --cached --others --exclude-standard`, so the
	// files are exactly the ones the repository tracks plus the untracked ones
	// it does not ignore. .gitignore is honoured as git itself reads it
	// (nested ignores, global excludes, negations), and a directory that merely
	// happens to be called `build` or `bin` is searched when it holds tracked
	// source.
	SourceGit = "git"
	// SourceWalk: a plain directory walk, for a folder that is not a work tree.
	// There is no .gitignore to ask, so version-control metadata and
	// dependency/cache directories are skipped by name (walkSkip).
	SourceWalk = "walk"
)

// walkSkip is what a folder with no repository skips by name: VCS metadata and
// directories that only ever hold fetched or generated dependencies. It is
// deliberately short — `build`, `bin`, `out`, `vendor` and `dist` are source
// directories in plenty of projects, and without a .gitignore saying
// otherwise the search cannot know.
var walkSkip = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, ".gradle": true, "__pycache__": true,
	".venv": true, "venv": true,
}

const listTimeout = 30 * time.Second

// listFiles returns the files of root as slash-separated relative paths, sorted
// bytewise, minus skip subtrees (nested folders of the same search, which are
// listed under their own root) and minus what the filter excludes.
func listFiles(ctx context.Context, root string, skip []string, filter pathFilter) (files []string, source string, err error) {
	if listed, ok := listWithGit(ctx, root); ok {
		source = SourceGit
		files = listed
	} else {
		source = SourceWalk
		files, err = listWithWalk(ctx, root)
		if err != nil {
			return nil, source, err
		}
	}
	out := files[:0]
	for _, f := range files {
		if underAny(f, skip) {
			continue
		}
		if !filter.empty() && !filter.allows(f) {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out, source, nil
}

func underAny(rel string, subtrees []string) bool {
	for _, s := range subtrees {
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

func listWithGit(ctx context.Context, root string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	// Structured argv: root is a path argument, never interpolated into a shell.
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	seen := make(map[string]struct{}, 1024)
	var files []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		rel := filepath.ToSlash(string(raw))
		if _, dup := seen[rel]; dup {
			continue
		}
		seen[rel] = struct{}{}
		files = append(files, rel)
	}
	return files, true
}

func listWithWalk(ctx context.Context, root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if path != root && walkSkip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// realPath resolves symlinks so two spellings of one folder compare equal.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(p)
}

// nestedSubtrees returns, for each root, the relative paths of the other roots
// of the same search that lie inside it. The outer root skips them: they are
// listed once, under the inner root, which is the more specific owner — so a
// workspace holding a monorepo and one of its packages as separate folders
// does not search that package twice, and every match says which folder it
// belongs to.
//
// A root identical to an earlier one (two spellings of one folder) is reported
// in dup so it is not searched at all.
func nestedSubtrees(roots []string) (skip [][]string, dup map[int]int) {
	real := make([]string, len(roots))
	for i, r := range roots {
		real[i] = realPath(r)
	}
	skip = make([][]string, len(roots))
	dup = map[int]int{}
	for i := range roots {
		for j := range roots {
			if i == j {
				continue
			}
			if real[i] == real[j] {
				if j < i {
					dup[i] = j
				}
				continue
			}
			prefix := real[i] + string(filepath.Separator)
			if strings.HasPrefix(real[j], prefix) {
				rel, err := filepath.Rel(real[i], real[j])
				if err == nil {
					skip[i] = append(skip[i], filepath.ToSlash(rel))
				}
			}
		}
	}
	return skip, dup
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
