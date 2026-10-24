//go:build !darwin && !linux

package agenthub

import "os"

// Session temp directories are macOS-only; never sweep on other platforms.
func tempOwnedByUser(os.FileInfo) bool                        { return false }
func openTempMarker(path string, flags int) (*os.File, error) { return os.OpenFile(path, flags, 0o600) }
func lockTempMarker(*os.File) error                           { return os.ErrPermission }
func tempPIDAlive(int) bool                                   { return true }
