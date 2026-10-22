package agenthub

import (
	"errors"
	"fmt"
	"io/fs"
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
// conversation store to the shared one. It also creates the shared store
// directory inside the built-in account's home (sharedFrom): the link needs a
// target. It never removes or replaces anything: a real directory or a link to
// somewhere else where a link belongs is refused, as is a home that is a
// symlink, not a directory, or inside the shared store (a cycle).
func PrepareAccountHome(vendor AgentType, home, sharedFrom string) error {
	if home == "" || sharedFrom == "" || !filepath.IsAbs(home) || !filepath.IsAbs(sharedFrom) {
		return fmt.Errorf("%w: homes must be absolute paths", ErrAccountHome)
	}
	home, sharedFrom = filepath.Clean(home), filepath.Clean(sharedFrom)
	entries := sharedStoreEntries[vendor]
	for _, name := range entries {
		target := filepath.Join(sharedFrom, name)
		if home == target || pathIsInside(home, target) {
			return fmt.Errorf("%w: home %s is inside the shared store %s", ErrAccountHome, home, target)
		}
	}
	switch info, err := os.Lstat(home); {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%w: home %s is not a directory", ErrAccountHome, home)
	case err == nil:
		if err := os.Chmod(home, 0o700); err != nil {
			return fmt.Errorf("%w: protect %s: %w", ErrAccountHome, home, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(home, 0o700); err != nil {
			return fmt.Errorf("%w: create %s: %w", ErrAccountHome, home, err)
		}
	default:
		return fmt.Errorf("%w: %s: %w", ErrAccountHome, home, err)
	}
	for _, name := range entries {
		target := filepath.Join(sharedFrom, name)
		link := filepath.Join(home, name)
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("%w: create %s: %w", ErrAccountHome, target, err)
		}
		info, err := os.Lstat(link)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			if current, _ := os.Readlink(link); filepath.Clean(current) == target {
				continue
			}
			return fmt.Errorf("%w: %s already links somewhere else", ErrAccountHome, link)
		case err == nil:
			return fmt.Errorf("%w: %s exists and is not a link; DevKit will not replace it", ErrAccountHome, link)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%w: %s: %w", ErrAccountHome, link, err)
		}
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("%w: link %s: %w", ErrAccountHome, link, err)
		}
	}
	return nil
}
