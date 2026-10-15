//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSeatbeltAgentCanRunLoopbackDevelopmentServers(t *testing.T) {
	if os.Getenv("DEVKIT_TEST_LOOPBACK_LISTENER") == "1" {
		for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				t.Fatalf("listen on %s: %v", addr, err)
			}
			listener.Close()
		}
		fmt.Println("LOOPBACK_LISTENERS_OK")
		return
	}
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir: t.TempDir(), Network: NetworkHost, AllowLoopbackListeners: true,
		ReadOnlyPaths: []string{exe}, Env: map[string]string{"DEVKIT_TEST_LOOPBACK_LISTENER": "1"},
	}, Cmd{Path: exe, Args: []string{"-test.run=^TestSeatbeltAgentCanRunLoopbackDevelopmentServers$"}})
	if err != nil {
		t.Fatal(err)
	}
	err = proc.Wait()
	stdout, stderr := proc.Output()
	if err != nil || !strings.Contains(string(stdout), "LOOPBACK_LISTENERS_OK") {
		t.Fatalf("loopback server failed: %v\n%s\n%s", err, stdout, stderr)
	}
}

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

// TestSeatbeltDeniesWriteToDenyPathUnderRWAncestor covers the case where the
// WorkDir grant is an ancestor of a denied subtree (WorkDir=$HOME): the deny
// must block writes, not just reads.
func TestSeatbeltDeniesWriteToDenyPathUnderRWAncestor(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	// Seatbelt matches resolved paths; t.TempDir() lives under the /var ->
	// /private/var symlink, so canonicalise or the WorkDir grant never matches.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(authKeys, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir:       home, // RW ancestor of the denied subtree
		DenyReadPaths: []string{sshDir},
		Env:           map[string]string{"HOME": home},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", `echo pwned >> "$HOME/.ssh/authorized_keys"; echo ok > "$HOME/notes.txt"`}})
	if err != nil {
		t.Fatal(err)
	}
	_ = proc.Wait()

	if got, _ := os.ReadFile(authKeys); len(got) != 0 {
		t.Fatalf("write into denied subtree succeeded: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, "notes.txt")); err != nil {
		t.Fatalf("write elsewhere in WorkDir must still work: %v", err)
	}
}

// TestSeatbeltNetworkHostDeniesUnixSockets is the docker.sock case: a unix
// socket on the host must not be reachable even with NetworkHost.
func TestSeatbeltNetworkHostDeniesUnixSockets(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	// Short socket path: sun_path is capped at 104 bytes on darwin.
	sockDir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const marker = "UNIX_SOCKET_REACHED"
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(marker)) })}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// Control: reachable from outside the sandbox, so a failure below is the
	// profile's doing and not a broken listener.
	if c, err := net.Dial("unix", sock); err != nil {
		t.Fatalf("control dial: %v", err)
	} else {
		c.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{WorkDir: t.TempDir(), Network: NetworkHost},
		Cmd{Path: "/usr/bin/curl", Args: []string{"-s", "-m", "5", "--unix-socket", sock, "http://localhost/"}})
	if err != nil {
		t.Fatal(err)
	}
	waitErr := proc.Wait()
	stdout, stderr := proc.Output()
	if strings.Contains(string(stdout), marker) {
		t.Fatalf("unix socket reachable from inside NetworkHost sandbox; stderr=%q", stderr)
	}
	if waitErr == nil {
		t.Fatalf("expected curl to fail connecting; stdout=%q stderr=%q", stdout, stderr)
	}
}

// TestSeatbeltDeniesAncestorRenameBypass: renaming the parent of a denied
// subtree must fail, otherwise (subpath) protection is lost after the move.
func TestSeatbeltDeniesAncestorRenameBypass(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(parent, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(authKeys, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	moved := filepath.Join(parent, "h2")
	proc, err := backend.Start(ctx, Policy{
		WorkDir:       parent, // ancestor of the denied path's parent
		DenyReadPaths: []string{sshDir},
		Env:           map[string]string{"HOME": home},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", `mv "` + home + `" "` + moved + `" && echo pwned >> "` + moved + `/.ssh/authorized_keys"`}})
	if err != nil {
		t.Fatal(err)
	}
	_ = proc.Wait()

	if _, err := os.Stat(moved); err == nil {
		t.Fatalf("ancestor of denied path was renamed inside the sandbox")
	}
	if got, _ := os.ReadFile(authKeys); len(got) != 0 {
		t.Fatalf("write into denied subtree succeeded after rename: %q", got)
	}
}

// TestSeatbeltAllowUnixSocketsConnects: the identity-bound MCP socket must be
// reachable even under NetworkNone, and a sibling socket must not be.
func TestSeatbeltAllowUnixSocketsConnects(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}
	sockDir, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	serve := func(name, marker string) string {
		path := filepath.Join(sockDir, name)
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(marker)) })}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
		return path
	}
	allowed := serve("a.sock", "ALLOWED_SOCKET")
	sibling := serve("b.sock", "SIBLING_SOCKET")

	curl := func(sock string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		proc, err := backend.Start(ctx, Policy{WorkDir: t.TempDir(), Network: NetworkNone, AllowUnixSockets: []string{allowed}},
			Cmd{Path: "/usr/bin/curl", Args: []string{"-s", "-m", "5", "--unix-socket", sock, "http://localhost/"}})
		if err != nil {
			t.Fatal(err)
		}
		_ = proc.Wait()
		out, _ := proc.Output()
		return string(out)
	}
	if got := curl(allowed); !strings.Contains(got, "ALLOWED_SOCKET") {
		t.Fatalf("allowed socket unreachable from inside the jail: %q", got)
	}
	if got := curl(sibling); strings.Contains(got, "SIBLING_SOCKET") {
		t.Fatalf("sibling socket must stay unreachable: %q", got)
	}
}

// TestSeatbeltExecutesThroughSymlinkedReadOnlyPath is the nvm/npx case: the
// binary is reached through a symlink, and reading it must work inside the jail.
func TestSeatbeltExecutesThroughSymlinkedReadOnlyPath(t *testing.T) {
	backend := NewSeatbeltBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(libDir, "tool.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho SYMLINKED_TOOL_RAN\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "tool")
	if err := os.Symlink(script, link); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{WorkDir: t.TempDir(), ReadOnlyPaths: []string{link}},
		Cmd{Path: "/bin/sh", Args: []string{"-c", "cat " + link}})
	if err != nil {
		t.Fatal(err)
	}
	_ = proc.Wait()
	stdout, stderr := proc.Output()
	if !strings.Contains(string(stdout), "SYMLINKED_TOOL_RAN") {
		t.Fatalf("symlinked ReadOnlyPath unreadable in jail; stdout=%q stderr=%q", stdout, stderr)
	}
}
