//go:build !windows || server

package main

// nativeBackdropTranslucent is Windows-only; macOS uses the Mac.Backdrop
// vibrancy path instead, and Linux has no compositor backdrop API through
// Wails, so both keep a solid window background.
func nativeBackdropTranslucent() bool {
	return false
}
