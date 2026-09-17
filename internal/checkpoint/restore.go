// internal/checkpoint/restore.go
package checkpoint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Putting files back.
//
// A restore is the one operation that destroys work, so it is built around one
// rule: it only touches a path whose current bytes are the bytes it expects.
// "Expected" is the tree written the last time DevKit looked, so anything the
// user did in their editor afterwards shows up as a conflict rather than as a
// silent overwrite.
//
// The writes go through `checkout-index` against a throwaway index, never
// against the shadow repository's real index — a restore must not change what
// the next snapshot compares against.

// ActionKind is what a restore would do to one path.
type ActionKind string

const (
	ActionWrite  ActionKind = "write"
	ActionDelete ActionKind = "delete"
)

// ConflictReason says why a path was left out of a plan.
type ConflictReason string

const (
	// ConflictChangedSince: the file on disk is not what the checkpoint that
	// last touched it recorded. Overridable.
	ConflictChangedSince ConflictReason = "changed_since"
	// ConflictProtected: the path was skipped by a snapshot (too large, nested
	// repository, denied), so no tree knows its real content. Never overridable.
	ConflictProtected ConflictReason = "protected"
	// ConflictUnsafePath: a directory on the way to the file is a symlink or
	// not a directory at all. Never overridable — following it would write
	// somewhere the user did not point us.
	ConflictUnsafePath ConflictReason = "unsafe_path"
)

// Overridable reports whether the user may force past this conflict.
func (c ConflictReason) Overridable() bool { return c == ConflictChangedSince }

// Action is one path a restore would change.
type Action struct {
	Path string     `json:"path"`
	Kind ActionKind `json:"kind"`
	// Mode is the git mode the path will have afterwards; empty for a delete.
	Mode string `json:"mode,omitempty"`
}

// Conflict is one path a restore will not change, and why.
type Conflict struct {
	Path   string         `json:"path"`
	Reason ConflictReason `json:"reason"`
	// Kind is what the restore would have done had there been no conflict, so
	// the dialog can say "would have deleted" rather than only naming the path.
	Kind ActionKind `json:"kind"`
}

// RestorePlan is the read-only answer to "what would this restore do".
type RestorePlan struct {
	// Target is the tree the selected paths end up matching.
	Target string `json:"target"`
	// Expected is the tree the plan was checked against; Apply re-checks it, so
	// a plan that went stale between the dialog and the button is caught.
	Expected  string     `json:"expected"`
	Actions   []Action   `json:"actions"`
	Conflicts []Conflict `json:"conflicts"`
}

// Empty reports whether the plan would change nothing.
func (p RestorePlan) Empty() bool { return len(p.Actions) == 0 }

// PlanRestore works out what it would take to make the project's files match
// target, for the paths that differ between expected and target.
//
// protected is the union of every path any relevant snapshot skipped. Those are
// refused outright: the trees do not hold their content, so "restoring" one
// would either delete a file nobody checkpointed or write a stale version of it.
func (r *Repo) PlanRestore(ctx context.Context, target, expected string, protected []string) (RestorePlan, error) {
	target, expected = orEmptyTree(target), orEmptyTree(expected)
	plan := RestorePlan{Target: target, Expected: expected}
	protected = append([]string(nil), protected...)

	wanted, err := r.Diff(ctx, expected, target)
	if err != nil {
		return RestorePlan{}, err
	}
	if len(wanted) == 0 {
		return plan, nil
	}

	// One snapshot of the project as it is now, compared against the same
	// "expected" tree: every path in here has moved under us since.
	var drifted map[string]bool
	err = r.runLocked(func() error {
		current, err := r.snapshotLocked(ctx)
		if err != nil {
			return err
		}
		for _, skip := range current.Skipped {
			protected = append(protected, skip.Path)
		}
		changes, err := r.diffLocked(ctx, expected, current.Tree)
		if err != nil {
			return err
		}
		drifted = make(map[string]bool, len(changes))
		for _, change := range changes {
			drifted[change.Path] = true
		}
		return nil
	})
	if err != nil {
		return RestorePlan{}, err
	}

	protectedSet := make(map[string]bool, len(protected))
	for _, path := range protected {
		protectedSet[path] = true
	}

	for _, change := range wanted {
		kind := ActionWrite
		mode := change.Mode
		if change.Kind == ChangeDeleted {
			kind, mode = ActionDelete, ""
		}
		switch {
		case protectedSet[change.Path] || Denied(change.Path):
			plan.Conflicts = append(plan.Conflicts, Conflict{Path: change.Path, Reason: ConflictProtected, Kind: kind})
		case r.leadingPathUnsafe(change.Path):
			plan.Conflicts = append(plan.Conflicts, Conflict{Path: change.Path, Reason: ConflictUnsafePath, Kind: kind})
		case drifted[change.Path]:
			plan.Conflicts = append(plan.Conflicts, Conflict{Path: change.Path, Reason: ConflictChangedSince, Kind: kind})
		default:
			plan.Actions = append(plan.Actions, Action{Path: change.Path, Kind: kind, Mode: mode})
		}
	}
	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].Path < plan.Actions[j].Path })
	sort.Slice(plan.Conflicts, func(i, j int) bool { return plan.Conflicts[i].Path < plan.Conflicts[j].Path })
	return plan, nil
}

// ErrPlanStale is returned when the project moved between planning a restore
// and applying it. The dialog the user was reading described a different world,
// so nothing is written.
var ErrPlanStale = errors.New("checkpoint: the restore plan is out of date")

// Applied reports what a restore actually did.
type Applied struct {
	Written []string `json:"written"`
	Deleted []string `json:"deleted"`
}

// ApplyRestore carries out a plan.
//
// overwrite names paths from plan.Conflicts the user chose to force. Only
// ConflictChangedSince can be forced: the other two reasons are not the user's
// to waive, because neither the content nor the destination is known to be safe.
func (r *Repo) ApplyRestore(ctx context.Context, plan RestorePlan, overwrite []string) (Applied, error) {
	forced := make(map[string]bool, len(overwrite))
	for _, path := range overwrite {
		forced[path] = true
	}
	actions := append([]Action(nil), plan.Actions...)
	for _, conflict := range plan.Conflicts {
		if forced[conflict.Path] && conflict.Reason.Overridable() {
			actions = append(actions, Action{Path: conflict.Path, Kind: conflict.Kind})
		}
	}
	if len(actions) == 0 {
		return Applied{}, nil
	}

	var applied Applied
	err := r.runLocked(func() error {
		// The plan was made without the lock, and a dialog sits between the two
		// calls. Anything that moved in between invalidates the answer the user
		// was shown, except on the paths they explicitly chose to overwrite —
		// those were already known to have moved.
		current, err := r.snapshotLocked(ctx)
		if err != nil {
			return err
		}
		drift, err := r.diffLocked(ctx, plan.Expected, current.Tree)
		if err != nil {
			return err
		}
		drifted := make(map[string]bool, len(drift))
		for _, change := range drift {
			drifted[change.Path] = true
		}

		var writes, deletes []string
		for _, action := range actions {
			// Re-checked under the lock: a symlink could have appeared on the
			// way to the button.
			if Denied(action.Path) || r.leadingPathUnsafe(action.Path) {
				return fmt.Errorf("checkpoint: refusing to touch %s", action.Path)
			}
			if drifted[action.Path] && !forced[action.Path] {
				return fmt.Errorf("%w: %s changed since the plan was made", ErrPlanStale, action.Path)
			}
			if action.Kind == ActionDelete {
				deletes = append(deletes, action.Path)
			} else {
				writes = append(writes, action.Path)
			}
		}
		if len(writes) > 0 {
			if err := r.checkoutPaths(ctx, plan.Target, writes); err != nil {
				return err
			}
			applied.Written = writes
		}
		for _, path := range deletes {
			if err := r.deletePath(path); err != nil {
				return err
			}
			applied.Deleted = append(applied.Deleted, path)
		}
		r.pruneEmptyDirs(deletes)
		return nil
	})
	if err != nil {
		return applied, err
	}
	return applied, nil
}

// checkoutPaths writes paths out of a tree using a scratch index.
//
// GIT_INDEX_FILE points somewhere disposable so the shadow repository's own
// index — the baseline the next snapshot diffs against — is untouched by a
// restore.
func (r *Repo) checkoutPaths(ctx context.Context, tree string, paths []string) error {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	indexPath := filepath.Join(r.gitDir, "devkit-restore-index-"+hex.EncodeToString(suffix))
	defer os.Remove(indexPath)

	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := r.gitWithEnv(ctx, env, nil, "read-tree", tree); err != nil {
		return err
	}
	// -f overwrites what is there; without it checkout-index skips every path
	// that already exists, which is most of a restore.
	_, err := r.gitWithEnv(ctx, env, joinNUL(paths), "checkout-index", "--stdin", "-z", "-f")
	return err
}

// leadingPathUnsafe reports whether anything between the project root and the
// file is not a real directory.
//
// git will not follow a symlinked directory out of the work tree — it replaces
// the symlink with a real directory instead — but "did not escape" is a lower
// bar than this feature wants: a user who replaced a directory with a symlink
// made a change, and a restore that silently unmakes it is the overwrite this
// whole file exists to prevent.
func (r *Repo) leadingPathUnsafe(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.HasPrefix(rel, "/") || rel != filepath.ToSlash(filepath.Clean(rel)) {
		return true
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	current := r.work
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if notPresent(err) {
			// A directory a write will create. Nothing to follow, nothing unsafe.
			return false
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// deletePath removes one file, having already established that its leading path
// is real. A directory where the tree expected a file is left alone: it is not
// the file we checkpointed, and removing a tree of unknown contents is not
// something a restore gets to do.
func (r *Repo) deletePath(rel string) error {
	full := filepath.Join(r.work, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if notPresent(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	return os.Remove(full)
}

// pruneEmptyDirs removes directories this restore emptied, and only those: the
// candidates are the parents of the files it deleted, and os.Remove refuses a
// directory with anything left in it — including an ignored or skipped file,
// which is exactly the outcome wanted.
func (r *Repo) pruneEmptyDirs(deleted []string) {
	seen := map[string]bool{}
	var candidates []string
	for _, rel := range deleted {
		dir := filepath.Dir(filepath.FromSlash(rel))
		for dir != "." && dir != string(filepath.Separator) {
			if !seen[dir] {
				seen[dir] = true
				candidates = append(candidates, dir)
			}
			dir = filepath.Dir(dir)
		}
	}
	// Deepest first, so a parent becomes removable once its child is gone.
	sort.Slice(candidates, func(i, j int) bool {
		return strings.Count(candidates[i], string(filepath.Separator)) >
			strings.Count(candidates[j], string(filepath.Separator))
	})
	for _, dir := range candidates {
		if r.leadingPathUnsafe(filepath.ToSlash(filepath.Join(dir, "x"))) {
			continue
		}
		os.Remove(filepath.Join(r.work, dir)) // fails, by design, unless empty
	}
}
