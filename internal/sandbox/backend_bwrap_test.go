//go:build linux

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBwrapArgsContainsWorkDirBindAndDenySSH(t *testing.T) {
	workDir := t.TempDir()
	home := filepath.Join(workDir, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	args := buildBwrapArgs(Policy{
		WorkDir:       workDir,
		DenyReadPaths: []string{sshDir},
		Env:           map[string]string{"HOME": home},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "true"}})

	assertArgsContain(t, args, "--bind", workDir, workDir)
	assertArgsContain(t, args, "--tmpfs", sshDir)
	assertArgsContain(t, args, "--chdir", workDir)
	assertArgsContain(t, args, "--", "/bin/sh", "-c", "true")
}

func TestBwrapCannotReadDeniedSSH(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
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

func TestBwrapArgsPrivateHomeSetsHomeAndTMPDIR(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	args := buildBwrapArgs(Policy{
		WorkDir: t.TempDir(),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "true"}})

	assertArgsContain(t, args, "--tmpfs", home)
	assertArgsContain(t, args, "--setenv", "HOME", home)
	assertArgsContain(t, args, "--setenv", "TMPDIR", "/tmp")
}

func TestBwrapPrivateHomeRuntimeSetsHome(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir: t.TempDir(),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "printf %s \"$HOME\""}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := proc.Output()
	if got := string(stdout); got != home {
		t.Fatalf("HOME = %q, want %q; stderr=%q", got, home, stderr)
	}
}

func TestBwrapCanWriteWorkDir(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}

	workDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir: workDir,
		Env:     map[string]string{"HOME": filepath.Join(workDir, "home")},
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "printf hello > sandbox-write.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, "sandbox-write.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file contents = %q, want %q", got, "hello")
	}
}

func TestBwrapCanWriteWorkDirUnderHome(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(realHome, "devkit-bwrap-test-"+time.Now().Format("20060102150405"))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir: workDir,
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "printf under-home > sandbox-under-home.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, "sandbox-under-home.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "under-home" {
		t.Fatalf("file contents = %q, want %q", got, "under-home")
	}
}

// TMPDIR is set to /tmp for every private-home jail (withPrivateHomeEnv), so
// /tmp inside the jail must actually exist and be writable — a Node CLI
// reaching for os.tmpdir() early in its own startup (lockfiles, IPC sockets,
// npm/V8 caches) can hang rather than error when it doesn't, which from the
// outside looks exactly like a session stuck on "starting" forever.
//
// WorkDir deliberately lives under $HOME, not under t.TempDir(): a WorkDir
// under /tmp would make bwrap auto-create /tmp as a bind-mount ancestor
// directory anyway, passing this test even with the /tmp grant missing
// entirely — hiding the exact bug this guards (a real project checkout,
// like an agent session's actual WorkDir, is never under /tmp).
func TestBwrapCanWriteTmpDir(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(realHome, "devkit-bwrap-tmp-test-"+time.Now().Format("20060102150405"))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir: workDir,
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "printf hello > \"$TMPDIR/probe.txt\" && cat \"$TMPDIR/probe.txt\""}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		stdout, stderr := proc.Output()
		t.Fatalf("write to $TMPDIR failed: %v; stdout=%q stderr=%q", err, stdout, stderr)
	}

	stdout, _ := proc.Output()
	if got := strings.TrimSpace(string(stdout)); got != "hello" {
		t.Fatalf("$TMPDIR/probe.txt contents = %q, want %q", got, "hello")
	}
}

// On a systemd-resolved host /etc/resolv.conf is a symlink into /run, which
// nothing binds — so the jail gets a dangling resolv.conf, no nameservers, and
// every DNS lookup inside it fails (measured: `getent hosts github.com` returns
// nothing in a jail built exactly like this one). The file the symlink resolves
// to has to come along.
func TestBwrapArgsBindsResolvConfTargetOutsideEtc(t *testing.T) {
	root := t.TempDir()
	stub := filepath.Join(root, "run", "systemd", "resolve", "stub-resolv.conf")
	if err := os.MkdirAll(filepath.Dir(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub, []byte("nameserver 127.0.0.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "resolv.conf")
	if err := os.Symlink(stub, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(stub)
	if err != nil {
		t.Fatal(err)
	}

	original := resolvConfPath
	resolvConfPath = link
	t.Cleanup(func() { resolvConfPath = original })

	args := buildBwrapArgs(Policy{
		WorkDir: t.TempDir(),
		Network: NetworkHost,
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "true"}})
	assertArgsContain(t, args, "--ro-bind", resolved, resolved)

	// A jail with no network has no names to resolve, so it stays out.
	offline := buildBwrapArgs(Policy{
		WorkDir: t.TempDir(),
		Network: NetworkNone,
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "true"}})
	for i := 0; i+2 < len(offline); i++ {
		if offline[i] == "--ro-bind" && offline[i+1] == resolved {
			t.Fatalf("offline jail bound resolv.conf target %q", resolved)
		}
	}
}

// A resolv.conf that is a real file needs nothing extra: the /etc bind already
// carries it, and a second bind of the same path is noise.
func TestBwrapArgsSkipsResolvConfInsideEtc(t *testing.T) {
	original := resolvConfPath
	resolvConfPath = "/etc/hostname" // a real file under /etc, whatever the host
	t.Cleanup(func() { resolvConfPath = original })

	args := buildBwrapArgs(Policy{
		WorkDir: t.TempDir(),
		Network: NetworkHost,
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", "true"}})

	for i := 0; i+2 < len(args); i++ {
		if args[i] == "--ro-bind" && args[i+1] == "/etc/hostname" {
			t.Fatal("bound a resolv.conf that /etc already covers")
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func assertArgsContain(t *testing.T, args []string, want ...string) {
	t.Helper()
	for i := 0; i+len(want) <= len(args); i++ {
		matches := true
		for j := range want {
			if args[i+j] != want[j] {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("args %q do not contain sequence %q", args, want)
}

// A workspace root read-only with one service inside it read-write is the
// layout workspace.mdx's tiers are built on. bwrap applies mounts in argv
// order, so the parent's --ro-bind has to come before the child's --bind.
func TestBwrapArgsBindParentBeforeNestedChild(t *testing.T) {
	root := t.TempDir()
	service := filepath.Join(root, "billing")
	if err := os.MkdirAll(service, 0o755); err != nil {
		t.Fatal(err)
	}

	args := buildBwrapArgs(Policy{
		WorkDir:       service,
		ReadOnlyPaths: []string{root},
		Env:           map[string]string{"HOME": filepath.Join(root, "home")},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh"})

	ro, rw := bindIndex(args, "--ro-bind", root), bindIndex(args, "--bind", service)
	if ro < 0 || rw < 0 {
		t.Fatalf("args %q lack the parent RO bind or the child RW bind", args)
	}
	if ro > rw {
		t.Fatalf("parent --ro-bind at %d comes after child --bind at %d: the child would end up read-only", ro, rw)
	}
}

func TestBwrapNestedBindsKeepEachModeAtRuntime(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}

	root := t.TempDir()
	service := filepath.Join(root, "billing")
	sibling := filepath.Join(root, "refund")
	for _, dir := range []string{service, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir:       service,
		ReadOnlyPaths: []string{root},
		Env:           map[string]string{"HOME": filepath.Join(root, "home")},
		PrivateHome:   boolPtr(false),
	}, Cmd{Path: "/bin/sh", Args: []string{"-c",
		"printf ok > " + filepath.Join(service, "rw.txt") + " && ! printf no > " + filepath.Join(sibling, "ro.txt") + " 2>/dev/null"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("sandboxed shell: %v (the service must be writable, its sibling must not)", err)
	}
	if got, err := os.ReadFile(filepath.Join(service, "rw.txt")); err != nil || string(got) != "ok" {
		t.Fatalf("write inside the RW service = %q, %v; want ok", got, err)
	}
	if _, err := os.Stat(filepath.Join(sibling, "ro.txt")); err == nil {
		t.Fatal("a sibling under the RO parent was written to")
	}
}

func bindIndex(args []string, flag, path string) int {
	for i := 0; i+2 < len(args); i++ {
		if args[i] == flag && args[i+1] == path && args[i+2] == path {
			return i
		}
	}
	return -1
}
