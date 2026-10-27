package agenthub

import (
	"errors"
	"os"
	"path/filepath"
)

// sessionWorktreePath resolves the existing part of a session cwd, retaining any
// missing directory suffix. An unrelated deleted project must not block removal,
// but a deleted subdirectory inside a checkout still protects that checkout.
// Broken symlinks, cycles and access errors remain unknown and fail closed.
func sessionWorktreePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// A present path that fails EvalSymlinks may be a dangling alias. Do not
		// treat its spelling as proof that it points outside the worktree.
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			if statErr != nil {
				return "", statErr
			}
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
