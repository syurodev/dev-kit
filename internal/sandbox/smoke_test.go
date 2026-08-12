//go:build linux

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSmokeAgentShapedProcess(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	wd := filepath.Join(root, "proj")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("OUT"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := filepath.Join(wd, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/bin:/bin"
	}

	policy := Policy{
		WorkDir: wd,
		Network: NetworkNone,
		Env: map[string]string{
			"HOME": home,
			"PATH": pathEnv,
		},
	}

	r := DefaultRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	catProc, err := r.Start(ctx, policy, Cmd{
		Path: "/bin/sh",
		Args: []string{"-c", "cat " + outside},
	})
	if err != nil {
		t.Fatal(err)
	}
	catWait := catProc.Wait()
	stdout, stderr := catProc.Output()
	combined := string(stdout) + string(stderr)
	if catWait == nil && strings.Contains(combined, "OUT") {
		t.Fatalf("sandbox leaked outside file; stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Contains(combined, "OUT") {
		t.Fatalf("outside file contents leaked; stdout=%q stderr=%q", stdout, stderr)
	}

	writeProc, err := r.Start(ctx, policy, Cmd{
		Path: "/bin/sh",
		Args: []string{"-c", "echo IN > inside.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProc.Wait(); err != nil {
		stdout, stderr := writeProc.Output()
		t.Fatalf("write inside workdir failed: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}

	got, err := os.ReadFile(filepath.Join(wd, "inside.txt"))
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(got))
	if trimmed != "IN" {
		t.Fatalf("inside.txt = %q, want IN", got)
	}
}
