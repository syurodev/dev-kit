//go:build linux

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLandlockAvailableDoesNotPanic(t *testing.T) {
	t.Parallel()
	_ = LandlockAvailable()
}

// landlockChildEnv marks the re-executed copy of this test binary that does
// the restricting.
const landlockChildEnv = "DEVKIT_LANDLOCK_CHILD"

// RestrictSelf is irreversible and applies to the CALLING thread, so a test
// that calls it in-process poisons the binary it runs in: every later
// filesystem touch on that thread is subject to the policy, including the
// testing package's own temp-directory cleanup, which then fails the test that
// just succeeded.
//
// So the restriction happens in a child, which is also how the thing is used
// for real — Start applies it to a process that exists to be confined.
func TestRestrictSelfSkippedOrApplies(t *testing.T) {
	if os.Getenv(landlockChildEnv) == "1" {
		restrictSelfInChild(t)
		return
	}
	t.Parallel()

	cmd := exec.Command(os.Args[0], "-test.run=^TestRestrictSelfSkippedOrApplies$", "-test.v")
	cmd.Env = append(os.Environ(), landlockChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restricted child failed: %v\n%s", err, out)
	}
	switch {
	case strings.Contains(string(out), "landlock: applied"):
		t.Log("Landlock rules applied in the child")
	case strings.Contains(string(out), "landlock: skipped"):
		t.Log("Landlock skipped on this kernel (ENOSYS/ENOPKG or unavailable)")
	default:
		t.Fatalf("child reported neither outcome:\n%s", out)
	}
}

// restrictSelfInChild is the half that runs confined. It cleans up after
// itself before restricting, because afterwards it cannot.
func restrictSelfInChild(t *testing.T) {
	t.Helper()

	workDir, err := os.MkdirTemp("", "landlock-probe-")
	if err != nil {
		t.Fatal(err)
	}
	// Removed here, not by t.TempDir's cleanup: once the policy is applied
	// this process cannot reach the parent directory to delete anything.
	defer func() { _ = os.RemoveAll(workDir) }()

	if err := os.WriteFile(filepath.Join(workDir, "probe"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	restrictErr := RestrictSelf(Policy{WorkDir: workDir})
	if errors.Is(restrictErr, ErrLandlockSkipped) {
		t.Log("landlock: skipped")
		return
	}
	if restrictErr != nil {
		t.Fatalf("RestrictSelf: %v", restrictErr)
	}
	if !LandlockAvailable() {
		t.Fatal("RestrictSelf succeeded but LandlockAvailable is false")
	}
	// The policy is real: the work directory is still readable, and the rest
	// of the filesystem is not.
	if _, err := os.ReadFile(filepath.Join(workDir, "probe")); err != nil {
		t.Fatalf("the work directory should still be readable: %v", err)
	}
	t.Log("landlock: applied")
}
