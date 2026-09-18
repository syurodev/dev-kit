//go:build (!darwin && !windows) || ios || server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Linux runs frameless (native_webview_linux.go), and a frameless window has
// square corners: the compositor rounds decorated windows, not this one. Other
// builds have no native window. Either way there is no radius to match, and the
// CSS keeps its default.
func nativeWindowCornerRadius(application.Window) (float64, bool) {
	return 0, false
}
