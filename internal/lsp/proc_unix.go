//go:build !windows

// internal/lsp/proc_unix.go
package lsp

import (
	"errors"
	"os/exec"
	"syscall"
)

// killProcessGroup puts the server in a process group of its own so that
// killing it reaches whatever it forked.
//
// A language server is not always one process — it may spawn workers, and a
// wrapper script execs the real one — and killing only the child leaves the
// work running, orphaned, still holding the pipes this client waits on.
func killProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil // already gone
		}
		if err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
