// internal/checkpoint/diff.go
package checkpoint

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"devkit/internal/gitparse"
)

// Comparing two trees.
//
// Everything the feature shows and every decision it makes is a diff between
// two trees: what a turn changed (before → after), what changed outside a turn
// (previous after → this before), and what a restore would have to do
// (expected → target).

// Change, ChangeKind and the kind constants are gitparse's: parsing what git
// printed does not depend on which git directory printed it, and internal/git
// needs the same values. Aliased rather than wrapped so every existing
// checkpoint.Change in agenthub and the bridge keeps working unchanged.
type (
	ChangeKind = gitparse.ChangeKind
	Change     = gitparse.Change
)

const (
	ChangeAdded      = gitparse.ChangeAdded
	ChangeModified   = gitparse.ChangeModified
	ChangeDeleted    = gitparse.ChangeDeleted
	ChangeTypeChange = gitparse.ChangeTypeChange
)

// Diff reports how the "to" tree differs from the "from" tree.
//
// An empty tree hash on either side means "nothing", which lets the first
// checkpoint of a project be diffed like every other one.
func (r *Repo) Diff(ctx context.Context, from, to string) ([]Change, error) {
	from, to = orEmptyTree(from), orEmptyTree(to)
	if from == to {
		return nil, nil
	}
	var changes []Change
	err := r.runLocked(ctx, func() error {
		var err error
		changes, err = r.diffLocked(ctx, from, to)
		return err
	})
	return changes, err
}

func orEmptyTree(tree string) string {
	if strings.TrimSpace(tree) == "" {
		return EmptyTree
	}
	return tree
}

func (r *Repo) diffLocked(ctx context.Context, from, to string) ([]Change, error) {
	// Two invocations rather than one: --raw carries the modes and the status
	// letter, --numstat carries the line counts, and git will not emit both
	// from one diff-tree run.
	rawOut, err := r.git(ctx, nil, "diff-tree", "-r", "-z", "--no-renames", "--raw", from, to)
	if err != nil {
		return nil, err
	}
	changes, order, err := gitparse.Raw(rawOut)
	if err != nil {
		return nil, err
	}
	statOut, err := r.git(ctx, nil, "diff-tree", "-r", "-z", "--no-renames", "--numstat", from, to)
	if err != nil {
		return nil, err
	}
	if err := gitparse.Numstat(statOut, changes); err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(order))
	for _, path := range order {
		out = append(out, *changes[path])
	}
	return out, nil
}

// FileContent returns one path's bytes out of a tree, for the diff drawer.
//
// maxBytes is a ceiling, not a truncation: a file over it comes back as
// (nil, true, nil). Half a file is not a diff — showing one would draw a
// difference at the cut that is not in the file — and the size is asked for
// first so a gigabyte is never read into memory to be thrown away. The
// checkpoint ceiling can be raised to 1 GiB, so "the file is small enough to
// snapshot" is not the same question as "small enough to render".
func (r *Repo) FileContent(ctx context.Context, tree, path string, maxBytes int64) (content []byte, tooLarge bool, err error) {
	tree = orEmptyTree(tree)
	spec := tree + ":" + path
	err = r.runLocked(ctx, func() error {
		if maxBytes > 0 {
			raw, err := r.git(ctx, nil, "cat-file", "-s", spec)
			if err != nil {
				return err
			}
			size, convErr := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
			if convErr != nil {
				return fmt.Errorf("checkpoint: unreadable size for %s: %w", path, convErr)
			}
			if size > maxBytes {
				tooLarge = true
				return nil
			}
		}
		out, err := r.git(ctx, nil, "cat-file", "blob", spec)
		if err != nil {
			return err
		}
		content = out
		return nil
	})
	return content, tooLarge, err
}
