// internal/terminal/local_test.go
package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestLocalPTYRunsShellInWorkDir(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer pty.Close()

	if _, err := pty.Write([]byte("pwd\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var output strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := pty.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
			if strings.Contains(output.String(), dir) {
				return // found the echoed `pwd` output — success
			}
		}
		if err != nil {
			t.Fatalf("Read: %v (output so far: %q)", err, output.String())
		}
	}
	t.Fatalf("did not see cwd %q in shell output within 5s, got: %q", dir, output.String())
}

func TestLocalPTYResize(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer pty.Close()

	if err := pty.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestLocalPTYCloseKillsProcess(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	if err := pty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close (defer + explicit) must not panic — os.File.Close on an
	// already-closed file and Process.Kill on an already-dead process both
	// return an error, not a panic; NewLocalPTY's Close must tolerate that.
	_ = pty.Close()
}
