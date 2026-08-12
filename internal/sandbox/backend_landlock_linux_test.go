//go:build linux

package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLandlockAvailableDoesNotPanic(t *testing.T) {
	t.Parallel()
	_ = LandlockAvailable()
}

func TestRestrictSelfSkippedOrApplies(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "probe"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RestrictSelf(Policy{
		WorkDir: workDir,
	})

	if errors.Is(err, ErrLandlockSkipped) {
		t.Log("Landlock skipped on this kernel (ENOSYS/ENOPKG or unavailable)")
		return
	}
	if err != nil {
		t.Fatalf("RestrictSelf: %v", err)
	}

	if !LandlockAvailable() {
		t.Fatal("RestrictSelf succeeded but LandlockAvailable is false")
	}
	t.Log("Landlock rules applied to current thread")
}
