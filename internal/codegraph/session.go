package codegraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RestoreLastProject enables session persistence and reopens the last selected
// project. Call after wiring the analyzer, before accepting MCP requests.
func (s *Service) RestoreLastProject(ctx context.Context, configDir string) error {
	s.openMu.Lock()
	s.sessionPath = filepath.Join(configDir, "DevKit", "codegraph", "active-project")
	data, err := os.ReadFile(s.sessionPath)
	s.openMu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("codegraph: restore project: %w", err)
	}
	root := strings.TrimSpace(string(data))
	if !filepath.IsAbs(root) {
		return fmt.Errorf("codegraph: saved project path must be absolute")
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("codegraph: saved project unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("codegraph: saved project is not a directory")
	}
	return s.OpenProjectAt(ctx, ProjectDBPath(configDir, root), root)
}

// Caller holds openMu. Atomic rename preserves the last selection on a crash.
func (s *Service) saveProject(root string) error {
	if s.sessionPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.sessionPath), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.sessionPath), ".active-project-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(root); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.sessionPath)
}
