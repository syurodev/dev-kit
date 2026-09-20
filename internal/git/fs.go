package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// writeWorkTreeFile writes one project-relative path, refusing to escape the
// work tree. A path from the frontend or from an agent is input, not a fact.
func writeWorkTreeFile(work, rel, content string) error {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return errors.New("git: path escapes the project")
	}
	full := filepath.Join(work, filepath.FromSlash(rel))
	cleanWork := filepath.Clean(work)
	if full != cleanWork && !strings.HasPrefix(full, cleanWork+string(os.PathSeparator)) {
		return errors.New("git: path escapes the project")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}
