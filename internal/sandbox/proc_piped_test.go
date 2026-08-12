package sandbox

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"testing"
	"time"
)

func TestStartPipedRoundTripEcho(t *testing.T) {
	r := DefaultRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Available(ctx); err != nil {
		t.Skip(err)
	}

	dir := t.TempDir()
	proc, err := r.StartPiped(ctx, Policy{
		WorkDir: dir,
		Network: NetworkNone,
	}, Cmd{Path: "/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()

	stdin := proc.StdinWriter()
	stdout := proc.StdoutReader()
	if stdin == nil {
		t.Fatal("StdinWriter is nil")
	}
	if stdout == nil {
		t.Fatal("StdoutReader is nil")
	}
	if _, err := io.WriteString(stdin, "hello-acp\n"); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("hello-acp")) {
		t.Fatalf("got %q", got)
	}
	_ = proc.Wait()
}

func TestStartPipedAppliesPolicyNetworkNone(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl unavailable:", err)
	}

	r := DefaultRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Available(ctx); err != nil {
		t.Skip(err)
	}

	dir := t.TempDir()
	proc, err := r.StartPiped(ctx, Policy{
		WorkDir: dir,
		Network: NetworkNone,
		Env: map[string]string{
			"PATH": "/usr/bin:/bin",
			"HOME": dir,
		},
	}, Cmd{
		Path: "/bin/sh",
		Args: []string{"-c", "curl -sS --connect-timeout 2 https://1.1.1.1/ >/dev/null 2>&1; echo EXIT:$?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()

	if w := proc.StdinWriter(); w != nil {
		_ = w.Close()
	}

	out, err := io.ReadAll(proc.StdoutReader())
	if err != nil {
		t.Fatal(err)
	}
	_ = proc.Wait()
	_, stderr := proc.Output()

	if bytes.Contains(out, []byte("EXIT:0")) {
		t.Fatalf("curl succeeded under NetworkNone; stdout=%q stderr=%q", out, stderr)
	}
}
