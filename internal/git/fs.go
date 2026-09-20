package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// workTreePath resolves one project-relative path inside the work tree,
// refusing to escape it. A path from the frontend or from an agent is input,
// not a fact.
func workTreePath(work, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return "", errors.New("git: path escapes the project")
	}
	full := filepath.Join(work, filepath.FromSlash(rel))
	cleanWork := filepath.Clean(work)
	if full != cleanWork && !strings.HasPrefix(full, cleanWork+string(os.PathSeparator)) {
		return "", errors.New("git: path escapes the project")
	}
	return full, nil
}

// writeWorkTreeFile writes one project-relative path.
func writeWorkTreeFile(work, rel, content string) error {
	full, err := workTreePath(work, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// readWorkTreeFile reads one project-relative path. A missing file is empty
// and not an error: during a merge it means the path is gone from the work
// tree, which is something the caller has to show rather than fail on.
func readWorkTreeFile(work, rel string) (string, bool, error) {
	full, err := workTreePath(work, rel)
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(content), true, nil
}
