// internal/checkpoint/diff.go
package checkpoint

import (
	"fmt"
	"strconv"
	"strings"

	"context"
)

// Comparing two trees.
//
// Everything the feature shows and every decision it makes is a diff between
// two trees: what a turn changed (before → after), what changed outside a turn
// (previous after → this before), and what a restore would have to do
// (expected → target).

// ChangeKind is what happened to one path between two trees.
type ChangeKind string

const (
	ChangeAdded      ChangeKind = "added"
	ChangeModified   ChangeKind = "modified"
	ChangeDeleted    ChangeKind = "deleted"
	ChangeTypeChange ChangeKind = "typechange"
)

// Change is one path's difference between two trees.
type Change struct {
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
	// Added and Removed are line counts, or -1 each when git called the file
	// binary.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	// Mode is the path's git mode in the "to" tree (100644, 100755, 120000), or
	// its mode in the "from" tree when it was deleted. It is what tells the UI
	// a symlink from a file and what a restore puts back.
	Mode string `json:"mode"`
}

// Binary reports whether git declined to count lines for this path.
func (c Change) Binary() bool { return c.Added < 0 }

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
	err := r.runLocked(func() error {
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
	changes, order, err := parseRaw(rawOut)
	if err != nil {
		return nil, err
	}
	statOut, err := r.git(ctx, nil, "diff-tree", "-r", "-z", "--no-renames", "--numstat", from, to)
	if err != nil {
		return nil, err
	}
	if err := applyNumstat(statOut, changes); err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(order))
	for _, path := range order {
		out = append(out, *changes[path])
	}
	return out, nil
}

// parseRaw reads `diff-tree -r -z --raw`, whose records are
// ":<srcmode> <dstmode> <srcsha> <dstsha> <status>\0<path>\0".
func parseRaw(raw []byte) (map[string]*Change, []string, error) {
	fields := strings.Split(string(raw), "\x00")
	changes := map[string]*Change{}
	var order []string
	for i := 0; i+1 < len(fields); i += 2 {
		meta := fields[i]
		path := fields[i+1]
		if meta == "" || path == "" {
			continue
		}
		parts := strings.Fields(strings.TrimPrefix(meta, ":"))
		if len(parts) < 5 {
			return nil, nil, fmt.Errorf("checkpoint: unreadable diff record %q", meta)
		}
		srcMode, dstMode, status := parts[0], parts[1], parts[4]
		change := &Change{Path: path, Mode: dstMode, Added: -1, Removed: -1}
		switch status[0] {
		case 'A':
			change.Kind = ChangeAdded
		case 'D':
			change.Kind = ChangeDeleted
			change.Mode = srcMode
		case 'T':
			change.Kind = ChangeTypeChange
		default:
			change.Kind = ChangeModified
		}
		changes[path] = change
		order = append(order, path)
	}
	return changes, order, nil
}

// applyNumstat reads `diff-tree -r -z --numstat`, whose records are
// "<added>\t<removed>\t<path>\0" once renames are off. "-" in either count is
// git saying the file is binary.
func applyNumstat(raw []byte, changes map[string]*Change) error {
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\t", 3)
		if len(parts) != 3 {
			return fmt.Errorf("checkpoint: unreadable numstat record %q", record)
		}
		change, ok := changes[parts[2]]
		if !ok {
			continue
		}
		if parts[0] == "-" || parts[1] == "-" {
			continue // stays at the -1/-1 the raw pass set: binary
		}
		added, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("checkpoint: unreadable numstat record %q", record)
		}
		removed, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("checkpoint: unreadable numstat record %q", record)
		}
		change.Added, change.Removed = added, removed
	}
	return nil
}

// FileContent returns one path's bytes out of a tree, for the diff drawer.
func (r *Repo) FileContent(ctx context.Context, tree, path string) ([]byte, error) {
	tree = orEmptyTree(tree)
	var out []byte
	err := r.runLocked(func() error {
		var err error
		out, err = r.git(ctx, nil, "cat-file", "blob", tree+":"+path)
		return err
	})
	return out, err
}
