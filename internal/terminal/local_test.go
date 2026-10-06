// internal/terminal/local_test.go
package terminal

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
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

// A command, not a shell: it runs with exactly the env it is given, its output
// reaches onOutput, and its exit code reaches OnExit — which is how Agent Hub's
// sign-in learns the login finished.
func TestLocalCommandPTYReportsOutputAndExitCode(t *testing.T) {
	var mu sync.Mutex
	var output strings.Builder
	reg := NewRegistry(func(_ string, chunk []byte) {
		mu.Lock()
		output.Write(chunk)
		mu.Unlock()
	})
	exited := make(chan [2]any, 1)
	reg.OnExit = func(id string, code int) { exited <- [2]any{id, code} }

	pty, err := NewLocalCommandPTY("/bin/sh", []string{"-c", `echo "mark=$SIGNIN_MARK"; exit 3`},
		[]string{"SIGNIN_MARK=from-env", "PATH=/usr/bin:/bin"}, t.TempDir(), 24, 80)
	if err != nil {
		t.Fatalf("NewLocalCommandPTY: %v", err)
	}
	defer pty.Close()
	id := reg.Register("builtin.agenthub", pty)

	select {
	case got := <-exited:
		if got[0] != id || got[1] != 3 {
			t.Fatalf("OnExit(%v, %v), want (%s, 3)", got[0], got[1], id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit never fired")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := strings.Contains(output.String(), "mark=from-env")
		mu.Unlock()
		if seen {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output never showed the env value, got %q", output.String())
}

func TestLocalPTYCloseReapsZombieProcess(t *testing.T) {
	dir := t.TempDir()
	pty, err := NewLocalPTY("/bin/sh", dir, 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	lp := pty.(*localPTY)
	pid := lp.cmd.Process.Pid

	if err := pty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify the process is reaped (not left as a zombie).
	// On Unix/macOS, a zombie process has state 'Z' in `ps -o state= -p <pid>`.
	// Once reaped by cmd.Wait(), ps will report nothing (or error/exit non-zero) and state won't be 'Z'.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
		state := strings.TrimSpace(string(out))
		if err != nil || state == "" {
			return // process is completely gone from process table — reaped!
		}
		if state != "Z" {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
	state := strings.TrimSpace(string(out))
	if err == nil && state == "Z" {
		t.Fatalf("process pid=%d is a zombie (state=Z) after Close", pid)
	}
}

// The bug this pins: a desktop launcher gives DevKit no TERM, zsh then finds
// no terminfo, and its prompt glyphs come out as "?➜" with every redraw
// leaving the old text behind. The shell must see xterm-256color.
func TestLocalPTYShellSeesXtermTerm(t *testing.T) {
	t.Setenv("TERM", "")
	pty, err := NewLocalPTY("/bin/sh", t.TempDir(), 24, 80)
	if err != nil {
		t.Fatalf("NewLocalPTY: %v", err)
	}
	defer pty.Close()

	if _, err := pty.Write([]byte("echo \"T=[$TERM]\"\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var output strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := pty.Read(buf)
		output.Write(buf[:n])
		if strings.Contains(output.String(), "T=[xterm-256color]") {
			return
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("shell did not see TERM=xterm-256color, got: %q", output.String())
}
