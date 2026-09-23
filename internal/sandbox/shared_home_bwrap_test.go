//go:build linux

package sandbox

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A shared home is the real one, read-write: before this the jail started
// from an empty mount namespace, so PrivateHome=false produced a home that
// simply was not there — and no /tmp and no $HOME either.
func TestBwrapSharedHomeIsTheRealHome(t *testing.T) {
	backend := NewBwrapBackend()
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	workDir := filepath.Join(root, "proj")
	for _, dir := range []string{filepath.Join(home, ".kube"), filepath.Join(home, ".ssh"), filepath.Join(home, "devkit"), workDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(home, ".kube", "config"), "KUBECONFIG")
	mustWrite(t, filepath.Join(home, ".ssh", "id_rsa"), "SSHSECRET")
	mustWrite(t, filepath.Join(home, "devkit", "devkit.db"), "VAULTDB")
	// The one socket inside the hidden directory the process may use.
	sock := filepath.Join(home, "devkit", "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proc, err := backend.Start(ctx, Policy{
		WorkDir:          workDir,
		Env:              map[string]string{"HOME": home},
		PrivateHome:      boolPtr(false),
		DenyReadPaths:    []string{filepath.Join(home, "devkit")},
		AllowUnixSockets: []string{sock},
	}, Cmd{Path: "/bin/sh", Args: []string{"-c", strings.Join([]string{
		`echo "home=$HOME"`,
		`cat "$HOME/.kube/config"`,
		`cat "$HOME/.ssh/id_rsa" 2>/dev/null`,
		`cat "$HOME/devkit/devkit.db" 2>/dev/null`,
		`echo w > "$HOME/written" && echo wrote-home`,
		`echo t > /tmp/x && echo wrote-tmp`,
		`test -S "$HOME/devkit/s.sock" && echo socket-visible`,
		`true`,
	}, "; ")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := proc.Output()
	out := string(stdout) + string(stderr)
	for _, want := range []string{"home=" + home, "KUBECONFIG", "wrote-home", "wrote-tmp", "socket-visible"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	for _, secret := range []string{"SSHSECRET", "VAULTDB"} {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked:\n%s", secret, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "written")); err != nil {
		t.Errorf("write to the shared home did not reach the host: %v", err)
	}
}

// A socket may sit inside a directory the caller hid; one under the
// built-in deny list may not.
func TestValidateSocketInsideCallerDenyIsAllowed(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "hidden")
	if err := os.MkdirAll(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(hidden, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	p := Policy{WorkDir: root, DenyReadPaths: []string{hidden}, AllowUnixSockets: []string{sock}}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil for a socket inside the caller's own deny path", err)
	}
	p = Policy{WorkDir: root, DenyReadPaths: []string{hidden}, ReadOnlyPaths: []string{hidden}}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate = nil, want an error for a read-only bind inside a deny path")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
