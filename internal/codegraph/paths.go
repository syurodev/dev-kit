// internal/codegraph/paths.go
package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectDBPath derives the on-disk SQLite index path for a project folder.
// Same convention as internal/cloudconfig and internal/mcp: callers pass the
// raw OS config dir (os.UserConfigDir()), this appends "DevKit" and its own
// subpath. The project path is hashed rather than used as a filename since
// it may contain characters invalid in a path segment (and to keep the
// resulting filename length-bounded regardless of how deep the project
// folder is nested).
func ProjectDBPath(configDir, projectPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(projectPath)))
	name := hex.EncodeToString(sum[:]) + ".db"
	return filepath.Join(configDir, "DevKit", "codegraph", name)
}

// resolvePath validates that relativePath, once joined onto projectRoot and
// resolved through any symlinks, stays inside projectRoot. Returns the
// resolved absolute path, or ErrPathOutsideProject if it escapes.
//
// relativePath's target does not need to exist yet (WriteFile's first save
// of a brand new file resolves its not-yet-existing leaf name against its
// parent directory instead).
func resolvePath(projectRoot, relativePath string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return "", fmt.Errorf("codegraph: resolve project root: %w", err)
	}

	target := relativePath
	if !filepath.IsAbs(target) {
		target = filepath.Join(projectRoot, relativePath)
	}

	resolved, err := evalSymlinksNonExistent(target)
	if err != nil {
		return "", fmt.Errorf("codegraph: resolve path: %w", err)
	}

	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideProject
	}
	return resolved, nil
}

func evalSymlinksNonExistent(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}

	// If the leaf itself exists on disk as a symlink (mode ModeSymlink),
	// but filepath.EvalSymlinks failed above, it is a broken/dangling symlink.
	// Dangling symlinks must never be treated as "non-existent new files",
	// because writing to a dangling symlink would follow it and create a file
	// at its (potentially external) target.
	if fi, err := os.Lstat(path); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return "", ErrPathOutsideProject
	}

	var rest []string
	curr := path
	for {
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		rest = append([]string{filepath.Base(curr)}, rest...)
		curr = parent
		if resolved, err := filepath.EvalSymlinks(curr); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if fi, err := os.Lstat(curr); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
			return "", ErrPathOutsideProject
		}
	}
	return filepath.Clean(path), nil
}
