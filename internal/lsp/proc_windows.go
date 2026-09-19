//go:build windows

// internal/lsp/proc_windows.go
package lsp

import "os/exec"

// killProcessGroup is the default on Windows: Process.Kill. Killing a whole
// tree there needs a job object, which nothing here has needed yet; WaitDelay
// still bounds the wait for a stray grandchild holding the pipes.
func killProcessGroup(cmd *exec.Cmd) {}
