//go:build unix

package fchdirexec_test

import "syscall"

// syscallKill0 is kill(pid, 0): nil while the process exists (a zombie included).
func syscallKill0(pid int) error { return syscall.Kill(pid, 0) }
