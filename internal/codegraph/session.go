package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// RestoreLastProject enables session persistence and reopens the last selected
// project. Call after wiring the analyzer, before accepting MCP requests.
//
// It returns as soon as the project is marked as opening, and analyzes in the
// background. Startup used to wait here for the whole open — ten minutes, at
// the analyzer's timeout — before the window appeared at all. Now the window
// comes up at once: codegraph:projectChanged tells it which folder is open,
// readiness reports "opening" until the analysis lands, and
// codegraph:indexUpdated / WaitForIndexReady carry the rest. A failure is
// recorded as the index error (WaitForIndexReady returns it) and logged.
//
// ctx bounds only the synchronous part; the background analysis belongs to
// the project, and Close or the next open cancels it.
func (s *Service) RestoreLastProject(ctx context.Context, configDir string) error {
	s.SetConfigDir(configDir)
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
	if err := ctx.Err(); err != nil {
		return err
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
	run, err := s.beginOpen(ProjectDBPath(configDir, root), root)
	if err != nil {
		return err
	}
	go func() {
		if err := run(context.Background()); err != nil && !errors.Is(err, ErrProjectChanged) {
			log.Printf("codegraph: could not restore last project: %v", err)
		}
	}()
	return nil
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
