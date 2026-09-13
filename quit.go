package main

import (
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// beforeQuitEvent is frontend/src/bridge/lifecycle.ts's BEFORE_QUIT_EVENT.
const beforeQuitEvent = "devkit:before-quit"

/*
quitAfterFlush gives the frontend a moment to write what is still inside a
debounce window before the app goes (#50): the last edits to a console, the last
change to UI state. Both were simply lost on quit.

ShouldQuit runs on the main thread, which is also where the webview's messages
are delivered, so it cannot wait for the frontend to answer. It declines the
first request, asks the frontend to flush, and quits a moment later; the second
request is let through.

ponytail: a fixed 300ms rather than an acknowledgement. A flush is a few local
writes; an ack from the frontend is the upgrade if a slow disk ever loses one.
*/
func quitAfterFlush() func() bool {
	var asked atomic.Bool
	return func() bool {
		if asked.Swap(true) {
			return true
		}
		app := application.Get()
		app.Event.Emit(beforeQuitEvent)
		time.AfterFunc(300*time.Millisecond, app.Quit)
		return false
	}
}
