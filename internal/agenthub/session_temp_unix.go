//go:build darwin || linux

package agenthub

import (
	"errors"
	"os"
	"syscall"
)

func tempOwnedByUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
func openTempMarker(path string, flags int) (*os.File, error) {
	return os.OpenFile(path, flags|syscall.O_NOFOLLOW, 0o600)
}
func lockTempMarker(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func tempPIDAlive(pid int) bool {
	if pid <= 0 {
		return true
	} // malformed marker is never permission to delete
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
