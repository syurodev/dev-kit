// Package gitparse reads git's machine-readable output.
//
// It is a leaf: it must not import any devkit/internal package. Parsing what
// git printed does not depend on which git directory printed it, which is the
// whole reason this is separate — internal/checkpoint runs git against a
// shadow repository and internal/git runs it against the project's own, and
// both need the same three parsers. Before this package existed the porcelain
// reader lived in internal/codegraph and the diff readers in
// internal/checkpoint, and a third caller would have meant a third copy.
package gitparse

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
)

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

// Raw reads `diff-tree -r -z --raw`, whose records are
// ":<srcmode> <dstmode> <srcsha> <dstsha> <status>\0<path>\0". It returns the
// changes by path and the order git listed them in.
func Raw(raw []byte) (map[string]*Change, []string, error) {
	fields := strings.Split(string(raw), "\x00")
	changes := map[string]*Change{}
	var order []string
	for i := 0; i+1 < len(fields); i += 2 {
		meta := fields[i]
		p := fields[i+1]
		if meta == "" || p == "" {
			continue
		}
		parts := strings.Fields(strings.TrimPrefix(meta, ":"))
		if len(parts) < 5 {
			return nil, nil, fmt.Errorf("gitparse: unreadable diff record %q", meta)
		}
		srcMode, dstMode, status := parts[0], parts[1], parts[4]
		change := &Change{Path: p, Mode: dstMode, Added: -1, Removed: -1}
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
		changes[p] = change
		order = append(order, p)
	}
	return changes, order, nil
}

// Numstat reads `diff-tree -r -z --numstat`, whose records are
// "<added>\t<removed>\t<path>\0" once renames are off. "-" in either count is
// git saying the file is binary, and such a path keeps the -1/-1 Raw set.
func Numstat(raw []byte, changes map[string]*Change) error {
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\t", 3)
		if len(parts) != 3 {
			return fmt.Errorf("gitparse: unreadable numstat record %q", record)
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
			return fmt.Errorf("gitparse: unreadable numstat record %q", record)
		}
		removed, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("gitparse: unreadable numstat record %q", record)
		}
		change.Added, change.Removed = added, removed
	}
	return nil
}

// Porcelain reads `git status --porcelain=v1 -z`. Paths come relative to the
// repository root; prefix (from rev-parse --show-prefix) is where the project
// sits inside it, and is cut off so the keys are relative to the project.
//
// Each value is a one-letter state: M modified, A added (staged or untracked),
// D deleted, R renamed.
func Porcelain(out []byte, prefix string) map[string]string {
	changes := map[string]string{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		entry := string(fields[i])
		if len(entry) < 4 {
			continue
		}
		xy, file := entry[:2], entry[3:]
		// A rename or copy carries its source as the next field.
		if xy[0] == 'R' || xy[0] == 'C' {
			i++
		}
		if prefix != "" {
			if !strings.HasPrefix(file, prefix) {
				continue
			}
			file = strings.TrimPrefix(file, prefix)
		}
		changes[path.Clean(file)] = State(xy)
	}
	return changes
}

// State collapses a porcelain XY pair to the one letter the file tree draws.
func State(xy string) string {
	switch {
	case xy == "??" || strings.ContainsRune(xy, 'A'):
		return "A"
	case strings.ContainsRune(xy, 'D'):
		return "D"
	case strings.ContainsRune(xy, 'R'):
		return "R"
	default:
		return "M"
	}
}
