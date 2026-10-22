package agenthub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrAccountHome is a home DevKit cannot prepare without risking data.
var ErrAccountHome = errors.New("agenthub.accountHome")

// sharedStoreEntries are the entries of a vendor's home that hold its
// conversations. An added account links them to the built-in account's, so a
// session moved to another account resumes the same native conversation while
// the sign-in (auth.json, credentials, .claude.json) stays per account.
// Measured in Phase 0 (spec "Measured"); add to this table only what that
// measurement says a resume needs.
var sharedStoreEntries = map[AgentType][]string{
	AgentTypeClaude: {"projects"},
	AgentTypeCodex:  {"sessions"},
}

// SharedStorePaths are the directories of sharedFrom (the built-in account's
// home) an account's links point at: the jail grants them read-write.
func SharedStorePaths(vendor AgentType, sharedFrom string) []string {
	var out []string
	for _, name := range sharedStoreEntries[vendor] {
		out = append(out, filepath.Join(sharedFrom, name))
	}
	return out
}

// PrepareAccountHome creates an account's home (0700) and links its
// conversation store to the shared one. It never removes or replaces anything:
// a real directory where a link belongs is refused.
func PrepareAccountHome(vendor AgentType, home, sharedFrom string) error {
	if home == "" || !filepath.IsAbs(home) || !filepath.IsAbs(sharedFrom) {
		return fmt.Errorf("%w: homes must be absolute paths", ErrAccountHome)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("%w: create %s: %v", ErrAccountHome, home, err)
	}
	for _, name := range sharedStoreEntries[vendor] {
		target := filepath.Join(sharedFrom, name)
		link := filepath.Join(home, name)
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("%w: create %s: %v", ErrAccountHome, target, err)
		}
		info, err := os.Lstat(link)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			if current, _ := os.Readlink(link); current == target {
				continue
			}
			return fmt.Errorf("%w: %s already links somewhere else", ErrAccountHome, link)
		case err == nil:
			return fmt.Errorf("%w: %s exists and is not a link; DevKit will not replace it", ErrAccountHome, link)
		case !os.IsNotExist(err):
			return fmt.Errorf("%w: %v", ErrAccountHome, err)
		}
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("%w: link %s: %v", ErrAccountHome, link, err)
		}
	}
	return nil
}

// carveOutAll is carveOut for several paths at once: every deny path that holds
// one of keeps is replaced by its entries, recursively, except the way down to
// each kept path. Calling carveOut once per root cannot do this — the second
// call would hide the first root's siblings' contents again.
//
// Entries are listed when a session opens; an entry created later beside a kept
// path is not hidden from that session (as with carveOut). An unlistable
// directory stays hidden whole, so the kept path cannot be reached and the spawn
// fails closed.
func carveOutAll(deny []string, keeps ...string) []string {
	var out []string
	for _, path := range deny {
		out = append(out, carveKeeps(path, keeps)...)
	}
	return out
}

func carveKeeps(dir string, keeps []string) []string {
	var inside []string
	for _, keep := range keeps {
		if keep == dir {
			return nil
		}
		if pathIsInside(keep, dir) {
			inside = append(inside, keep)
		}
	}
	if len(inside) == 0 {
		return []string{dir}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{dir}
	}
	var out []string
	for _, entry := range entries {
		out = append(out, carveKeeps(filepath.Join(dir, entry.Name()), inside)...)
	}
	return out
}
