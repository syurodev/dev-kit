//go:build !windows

// internal/codegraph/proc_unix.go
package codegraph

import (
	"errors"
	"os/exec"
	"syscall"
)

// killProcessGroup puts cmd in a process group of its own and makes
// cancellation kill the whole group rather than the one process.
//
// exec.CommandContext kills only the child it started. A build tool is
// usually a launcher — mvn is a shell script that execs a JVM, which may fork
// compilers and test JVMs of its own — so killing the launcher left the real
// work running, orphaned, and still holding the output pipes open. Signalling
// the negative pid reaches every process in the group.
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
