//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestSmokeSeatbeltNetworkHost verifies with the real sandbox-exec that a
// NetworkHost policy actually allows a DNS-resolved HTTPS request. Opt-in via
// DEVKIT_SANDBOX_SMOKE=1 (needs network).
func TestSmokeSeatbeltNetworkHost(t *testing.T) {
	if os.Getenv("DEVKIT_SANDBOX_SMOKE") != "1" {
		t.Skip("set DEVKIT_SANDBOX_SMOKE=1 to run the live sandbox network smoke test")
	}
	requireHostSeatbelt(t)
	runner := DefaultRunner()
	if err := runner.Available(context.Background()); err != nil {
		t.Skipf("sandbox backend unavailable: %v", err)
	}
	dir := t.TempDir()
	proc, err := runner.Start(context.Background(), Policy{
		WorkDir: dir,
		Network: NetworkHost,
	}, Cmd{Path: "/usr/bin/curl", Args: []string{"-s", "--noproxy", "*", "-m", "10", "-o", "/dev/null", "-w", "%{http_code}", "https://www.apple.com/library/test/success.html"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case <-time.After(20 * time.Second):
		proc.Kill()
		t.Fatal("curl timed out")
	case err := <-done:
		// Exit 56 (recv failure) still yields the HTTP status when the CDN
		// resets the connection right after the response — the status below is
		// the actual assertion.
		_ = err
	}
	if code := strings.TrimSpace(proc.Stdout().(*bytes.Buffer).String()); code != "200" {
		t.Fatalf("http status = %q, want 200 (stderr: %s)", code, proc.Stderr())
	}
}

// TestSmokeSeatbeltNetworkHostNarrowVar checks on a host that narrowing the
// /var read for NetworkHost kept the jail's clock on the host time zone and
// really closed the rest of /var. Opt-in via DEVKIT_SANDBOX_SMOKE=1.
func TestSmokeSeatbeltNetworkHostNarrowVar(t *testing.T) {
	if os.Getenv("DEVKIT_SANDBOX_SMOKE") != "1" {
		t.Skip("set DEVKIT_SANDBOX_SMOKE=1 to run the live sandbox smoke test")
	}
	requireHostSeatbelt(t)
	runner := DefaultRunner()
	if err := runner.Available(context.Background()); err != nil {
		t.Skipf("sandbox backend unavailable: %v", err)
	}
	policy := Policy{WorkDir: t.TempDir(), Network: NetworkHost}
	run := func(path string, args ...string) (string, error) {
		t.Helper()
		proc, err := runner.Start(context.Background(), policy, Cmd{Path: path, Args: args})
		if err != nil {
			t.Fatalf("start %s: %v", path, err)
		}
		done := make(chan error, 1)
		go func() { done <- proc.Wait() }()
		select {
		case <-time.After(20 * time.Second):
			proc.Kill()
			t.Fatalf("%s timed out", path)
		case err = <-done:
		}
		return strings.TrimSpace(proc.Stdout().(*bytes.Buffer).String()), err
	}

	host, err := exec.Command("/bin/date", "+%z").Output()
	if err != nil {
		t.Fatalf("host date: %v", err)
	}
	if got, err := run("/bin/date", "+%z"); err != nil || got != strings.TrimSpace(string(host)) {
		t.Fatalf("jail time zone offset = %q (err %v), host %q: the timezone database must stay readable", got, err, strings.TrimSpace(string(host)))
	}
	for _, dir := range []string{"/private/var/log", "/private/var/folders"} {
		if out, err := run("/bin/ls", dir); err == nil {
			t.Fatalf("jail listed %s: %s", dir, out)
		}
	}
}
