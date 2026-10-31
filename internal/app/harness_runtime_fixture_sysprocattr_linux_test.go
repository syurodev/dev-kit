//go:build linux

package app

import "syscall"

// Linux fixture subprocesses must die if the test runner exits unexpectedly.
func e2eChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
