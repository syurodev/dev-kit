//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// devkitTitlebarInsetHeight returns how many points of the window's total
// height sit above its real content area right now
// (frame.height - contentLayoutRect.height) — the titlebar/toolbar chrome
// AppKit is actually rendering, including wherever it currently centers the
// traffic-light buttons. 0 if the window handle is unavailable.
static double devkitTitlebarInsetHeight(void *handle) {
	NSWindow *window = (NSWindow *)handle;
	if (window == nil) {
		return 0;
	}
	double inset = window.frame.size.height - window.contentLayoutRect.size.height;
	if (inset < 0) {
		return 0;
	}
	return inset;
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// nativeTitlebarInsetHeight returns the exact number of points AppKit's
// native titlebar/toolbar chrome occupies at the top of the window right
// now (see main.go's Mac.TitleBar = HiddenInset). This is what the frontend
// header needs to match to stay visually aligned with the traffic lights —
// reading it directly avoids hardcoding a guessed pixel value that only
// happens to hold for one macOS version or toolbar style.
func nativeTitlebarInsetHeight(window application.Window) float64 {
	if window == nil || window.NativeWindow() == nil {
		return 0
	}
	var height float64
	handle := window.NativeWindow()
	application.InvokeSync(func() {
		height = float64(C.devkitTitlebarInsetHeight(handle))
	})
	return height
}
