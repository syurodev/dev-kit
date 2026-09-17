// internal/checkpoint/snapshot.go
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Taking a snapshot.
//
// The jail only lets an agent write inside the project, so a tree of the
// project at the boundary of a turn catches everything it did — including the
// writes Claude's own checkpoints miss, which is every file touched by a Bash
// script or an MCP server.
//
// Cost is what makes this viable: `ls-files` reports only what changed since
// the last snapshot, so after the first one (0.86s on a 1262-file repository)
// each turn boundary is around 0.04s.

// Slot names the two trees of a checkpoint.
const (
	SlotBefore = "before"
	SlotAfter  = "after"
)

// EmptyTree is git's fixed hash for a tree with nothing in it. It stands in for
// "there was no snapshot before this one" so a first checkpoint can be diffed
// like any other.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// SkipReason says why a path is in no tree, which is what stops a restore from
// ever deciding it should be deleted or rewritten.
type SkipReason string

const (
	// SkipTooLarge: bigger than Limits.MaxFileBytes.
	SkipTooLarge SkipReason = "too_large"
	// SkipNestedRepo: a directory with its own .git. Its contents belong to
	// that repository's history, not to ours.
	SkipNestedRepo SkipReason = "nested_repo"
	// SkipExcluded: matched the deny list even though something tracked it.
	SkipExcluded SkipReason = "excluded"
)

// Skip is one path left out of a tree, with the reason to show and to protect
// it by later.
type Skip struct {
	Path   string     `json:"path"`
	Reason SkipReason `json:"reason"`
}

// Snapshot is one tree of the project.
type Snapshot struct {
	Tree    string        `json:"tree"`
	Files   int           `json:"files"`
	Skipped []Skip        `json:"skipped,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// denyComponents are directories never snapshotted, whatever the project's
// .gitignore says — requirement (3) of the plan. A project whose .gitignore
// forgot node_modules, or un-ignores a file inside it, must still not put a
// quarter of a million dependency files through update-index every turn.
//
// Deliberately absent: dist, build, target, vendor. Go's vendor/ is source, and
// a project that commits its build output is entitled to check-pointed build
// output; .gitignore plus the file ceiling handle those.
var denyComponents = map[string]bool{
	".git":             true,
	"node_modules":     true,
	"bower_components": true,
	".pnpm-store":      true,
	".venv":            true,
	"venv":             true,
	"__pycache__":      true,
	".gradle":          true,
	".next":            true,
	".nuxt":            true,
	".turbo":           true,
	".cache":           true,
}

// denyNames are files never snapshotted, matched on the last component.
var denyNames = map[string]bool{".DS_Store": true}

// denyPatterns are the same list in gitignore syntax, handed to `ls-files -x`.
//
// Passing them to git as well as filtering in Go is not belt and braces: -x
// patterns are the highest-priority exclude source git has (they beat a `!`
// negation in .gitignore), and, more importantly, an excluded directory is
// never descended into, which is where the time saving comes from. The Go
// filter is what covers the case git will not — a denied path that somehow
// reached the index, which -x does not apply to because it only filters
// untracked files.
func denyPatterns(extra []string) []string {
	out := make([]string, 0, len(denyComponents)+len(denyNames)+len(extra))
	names := make([]string, 0, len(denyComponents))
	for name := range denyComponents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, name+"/")
	}
	files := make([]string, 0, len(denyNames))
	for name := range denyNames {
		files = append(files, name)
	}
	sort.Strings(files)
	out = append(out, files...)
	for _, pattern := range extra {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}

// Denied reports whether a project-relative path is one the engine refuses to
// snapshot. Exported because a restore has to ask the same question.
func Denied(rel string) bool {
	if rel == "" {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, part := range parts {
		if denyComponents[part] {
			return true
		}
		if i == len(parts)-1 && denyNames[part] {
			return true
		}
	}
	return false
}

var checkpointIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Snapshot writes the project's current state as a tree and points
// refs/devkit/cp/<id>/<slot> at it.
//
// The ref exists only so gc has a reason to keep the tree; the tree hash is
// what gets stored and what every other operation takes.
func (r *Repo) Snapshot(ctx context.Context, id, slot string) (Snapshot, error) {
	if !checkpointIDPattern.MatchString(id) {
		return Snapshot{}, fmt.Errorf("checkpoint: bad checkpoint id %q", id)
	}
	if slot != SlotBefore && slot != SlotAfter {
		return Snapshot{}, fmt.Errorf("checkpoint: bad slot %q", slot)
	}
	var out Snapshot
	err := r.runLocked(ctx, func() error {
		started := time.Now()
		snap, err := r.snapshotLocked(ctx)
		if err != nil {
			return err
		}
		if _, err := r.git(ctx, nil, "update-ref", RefName(id, slot), snap.Tree); err != nil {
			return err
		}
		snap.Elapsed = time.Since(started)
		out = snap
		return nil
	})
	return out, err
}

// RefName is where a checkpoint's tree is anchored.
func RefName(id, slot string) string { return "refs/devkit/cp/" + id + "/" + slot }

func (r *Repo) snapshotLocked(ctx context.Context) (Snapshot, error) {
	limits := r.engine.Limits()
	args := []string{"ls-files", "-z", "-m", "-o", "-d", "--exclude-standard"}
	for _, pattern := range denyPatterns(limits.ExtraExcludes) {
		args = append(args, "-x", pattern)
	}
	raw, err := r.git(ctx, nil, args...)
	if err != nil {
		return Snapshot{}, err
	}
	candidates := dedupeStrings(splitNUL(raw))
	if len(candidates) > limits.MaxFiles {
		return Snapshot{}, fmt.Errorf("%w: %d changed files exceeds the %d file ceiling",
			ErrProjectTooLarge, len(candidates), limits.MaxFiles)
	}

	var (
		gone    []string
		keep    []string
		drop    []string
		skipped []Skip
	)
	for _, rel := range candidates {
		// A nested repository is reported by ls-files as the directory itself,
		// with a trailing slash, because git refuses to descend into it.
		if strings.HasSuffix(rel, "/") {
			skipped = append(skipped, Skip{Path: strings.TrimSuffix(rel, "/"), Reason: SkipNestedRepo})
			continue
		}
		if Denied(rel) {
			skipped = append(skipped, Skip{Path: rel, Reason: SkipExcluded})
			drop = append(drop, rel)
			continue
		}
		info, err := os.Lstat(filepath.Join(r.work, filepath.FromSlash(rel)))
		switch {
		case notPresent(err):
			// Deleted since the last snapshot. Removals go in their own pass,
			// before the additions — see the comment at the update-index calls.
			gone = append(gone, rel)
		case err != nil:
			return Snapshot{}, err
		case info.IsDir():
			// The index holds a blob here and the disk now holds a directory:
			// a file was replaced by a folder. The entry has to go, or adding
			// anything under it collides. The folder's own contents arrive as
			// their own untracked paths, so nothing is lost by dropping this
			// one — a genuinely nested repository is the trailing-slash case
			// handled above.
			drop = append(drop, rel)
		case info.Mode()&os.ModeSymlink != 0:
			// A symlink's content is its target, which is never large.
			keep = append(keep, rel)
		case info.Size() > limits.MaxFileBytes:
			// Forced out of the index rather than merely not added: a tracked
			// file that grew past the ceiling would otherwise stay in every
			// later tree at its last small version, and a restore would put
			// that stale content back.
			skipped = append(skipped, Skip{Path: rel, Reason: SkipTooLarge})
			drop = append(drop, rel)
		default:
			keep = append(keep, rel)
		}
	}

	// Removals first, additions second, and the order is load-bearing: when a
	// directory becomes a symlink (or the reverse), the index holds `pkg/file`
	// while the disk now holds `pkg`, and git refuses a path that "appears as
	// both a file and as a directory". Dropping the stale entries in their own
	// pass clears the collision before anything is added.
	if len(gone)+len(drop) > 0 {
		if _, err := r.git(ctx, joinNUL(append(gone, drop...)), "update-index", "-z", "--force-remove", "--stdin"); err != nil {
			return Snapshot{}, err
		}
	}
	if len(keep) > 0 {
		if _, err := r.git(ctx, joinNUL(keep), "update-index", "-z", "--add", "--remove", "--stdin"); err != nil {
			return Snapshot{}, err
		}
	}
	tree, err := r.git(ctx, nil, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	count, err := r.countIndex(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return Snapshot{Tree: strings.TrimSpace(string(tree)), Files: count, Skipped: skipped}, nil
}

func (r *Repo) countIndex(ctx context.Context) (int, error) {
	raw, err := r.git(ctx, nil, "ls-files", "-z", "-c")
	if err != nil {
		return 0, err
	}
	return len(splitNUL(raw)), nil
}

// DeleteRefs drops a checkpoint's anchors. The trees stay until gc runs.
func (r *Repo) DeleteRefs(ctx context.Context, ids ...string) error {
	return r.runLocked(ctx, func() error {
		for _, id := range ids {
			for _, slot := range []string{SlotBefore, SlotAfter} {
				// -d without an old value succeeds whether or not the ref is
				// there, which is what a delete of an unknown id should do.
				if _, err := r.git(ctx, nil, "update-ref", "-d", RefName(id, slot)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// notPresent reports whether a stat error means "there is no file here".
//
// ENOENT is the obvious one. ENOTDIR is the case that actually bites: when a
// directory is replaced by a file or a symlink to one, every path that used to
// live under it fails with ENOTDIR rather than ENOENT, and treating that as an
// unexpected error fails the whole snapshot — so the turn loses its checkpoint
// because somebody replaced a directory.
func notPresent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func splitNUL(raw []byte) []string {
	var out []string
	for _, part := range strings.Split(string(raw), "\x00") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func joinNUL(paths []string) []byte {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return []byte(b.String())
}
