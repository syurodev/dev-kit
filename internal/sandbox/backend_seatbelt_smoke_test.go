//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"os"
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
