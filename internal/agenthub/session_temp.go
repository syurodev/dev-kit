package agenthub

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
)

// macOS sun_path is only 104 bytes. Never nest session scratch under the data
// directory or worktree. Linux already gets a private /tmp from bwrap.
func sessionTempPath(id string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ""
	}
	return "/private/tmp/dk-" + parsed.String()[:8]
}

func prepareSessionTemp(spec *OpenSpec) error {
	dir := sessionTempPath(spec.SessionID)
	if dir == "" {
		return nil
	}
	// Refuse symlinks and unexpected permissions instead of following a stale
	// entry planted in the shared temp parent. Mkdir is atomic on first use.
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("session temp: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("session temp: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("session temp: expected private directory at %s", dir)
	}
	// Opening with O_EXCL verifies this user can write before promising TMPDIR.
	probe, err := os.CreateTemp(dir, ".check-")
	if err != nil {
		return fmt.Errorf("session temp: %w", err)
	}
	probe.Close()
	os.Remove(probe.Name())
	spec.SessionTempDir = dir
	return nil
}

func applySessionTempEnv(env map[string]string, spec OpenSpec) {
	if spec.SessionTempDir != "" {
		env["TMPDIR"] = spec.SessionTempDir
		env["GOTMPDIR"] = spec.SessionTempDir
	}
}

func removeSessionTemp(id string) {
	if dir := sessionTempPath(id); dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// SweepSessionTemps runs once at app startup, before any vendor is started.
// Only the exact dk-<eight hex digits> directory shape belongs to DevKit.
func SweepSessionTemps() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return sweepSessionTemps("/private/tmp")
}

func sweepSessionTemps(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 11 || name[:3] != "dk-" || !entry.IsDir() {
			continue
		}
		valid := true
		for _, c := range name[3:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				valid = false
			}
		}
		if valid {
			if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
