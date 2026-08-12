//go:build darwin

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSeatbeltDeniesDotSSH(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	workDir := t.TempDir()
	home := filepath.Join(workDir, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_rsa"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir:       workDir,
		DenyReadPaths: []string{sshDir},
		Env:           map[string]string{"HOME": home},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", `cat "$HOME/.ssh/id_rsa" 2>/dev/null || true`}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := proc.Output()
	if strings.Contains(string(stdout), "SECRET") || strings.Contains(string(stderr), "SECRET") {
		t.Fatalf("secret leaked; stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSeatbeltPrivateHomeForcesHomeEnv(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	env := seatbeltProcessEnv(Policy{WorkDir: t.TempDir()})

	if got := envValue(env, "HOME"); got != home {
		t.Fatalf("HOME = %q, want %q in %v", got, home, env)
	}
	if got := envValue(env, "TMPDIR"); got != "/tmp" {
		t.Fatalf("TMPDIR = %q, want /tmp in %v", got, env)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
