//go:build !linux

package app

import "syscall"

// The runtime fixture is skipped off Linux because its network-isolation proof
// depends on bwrap's NetworkNone jail. Keep subprocess attributes portable when
// helper modes are compiled on other systems.
func e2eChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
