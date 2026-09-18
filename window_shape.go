package main

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The frontend's window-shape handshake; see frontend/src/lib/windowShape.ts.
//
// The shell's cards sit a gap in from the window edge, and a card corner only
// looks concentric with the window corner when card radius = window radius −
// gap. The window radius is the OS's, and it differs: 16pt on macOS 26+, 10pt
// before, 8px on Windows 11, square on Windows 10 and on Linux (frameless). So
// it is read here and handed to the CSS as --window-radius.
//
// It is the radius of the window at rest, not of its current state. A
// maximised or full-screen window is square, but the cards keep their radius
// then: corners that change shape every time the window is maximised would be
// worse than corners that are not concentric with a window that has none.
const (
	windowShapeRequestEvent = "devkit:window-shape:request"
	windowShapeEvent        = "devkit:window-shape"
)

// trackWindowShape answers the frontend's request with the window radius. A
// request rather than a push at startup: the page may mount (or reload) after
// any startup emit has gone by, and a request is always heard.
func trackWindowShape(app *application.App, win application.Window) {
	var (
		once   sync.Once
		radius float64
		known  bool
	)
	app.Event.On(windowShapeRequestEvent, func(*application.CustomEvent) {
		// Off the event goroutine: the macOS read hops to AppKit's main thread.
		go func() {
			// Fixed for the life of the window, so read once.
			once.Do(func() { radius, known = nativeWindowCornerRadius(win) })
			if !known {
				// Nothing reliable to say; the CSS keeps its default.
				return
			}
			app.Event.Emit(windowShapeEvent, map[string]any{"radius": radius})
		}()
	})
}

// The published values: 16pt from macOS 26, 10pt for 11–15.
func macFallbackCornerRadius(major int) float64 {
	if major >= 26 {
		return 16
	}
	return 10
}
