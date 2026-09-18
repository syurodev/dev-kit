//go:build windows && !server

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// Windows has no API that reports the radius in pixels —
// DWMWA_WINDOW_CORNER_PREFERENCE says whether a window is rounded, not by how
// much. Microsoft documents it instead: 8px for top-level windows on Windows 11
// (learn.microsoft.com/windows/apps/design/signature-experiences/geometry).
// Windows 10, builds below 22000, does not round, so there is nothing to match
// and the CSS keeps its default.
func nativeWindowCornerRadius(application.Window) (float64, bool) {
	return windowsCornerRadius(windows.RtlGetVersion().BuildNumber)
}

func windowsCornerRadius(build uint32) (float64, bool) {
	if build >= 22000 {
		return 8, true
	}
	return 0, false
}
