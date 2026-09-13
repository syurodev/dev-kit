//go:build linux && !server

package main

/*
#cgo pkg-config: gtk4
#include <stdlib.h>
#include <gtk/gtk.h>
*/
import "C"

import (
	"log"
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"

	"devkit/internal/gpu"
)

// WebKitGTK composites the page into a DMABUF-backed buffer it shares with GTK.
// When that buffer cannot be shared — a VM with no real GPU, a container, an
// Xvfb session — WebKitGTK does not fall back and does not warn: the page
// loads, the DOM is live and inspectable, and the window paints nothing. DevKit
// makes that failure look total rather than merely wrong, because main.tsx
// marks the document `native` and index.css then leaves html/body transparent,
// so what shows through is the window's own near-black BackgroundColour and the
// app appears to have died at startup.
//
// Turning the renderer off is the cure, and it is not free: it takes WebKitGTK
// off GPU buffer sharing and onto a CPU path. Doing that unconditionally — as
// this did until the render-node check below — cost every Linux machine its
// compositing, including the ones that share buffers perfectly well. The
// original diagnosis was made under Xvfb, where there is no GPU at all, and the
// conclusion generalised further than the evidence did: measured on an Ubuntu
// desktop, the difference was roughly 30fps against a smooth window.
//
// A DRM render node is the handle a process opens to allocate and export a GPU
// buffer, so its presence is the actual precondition rather than a proxy for
// it. Absent, there is nothing to share and the renderer goes off exactly as
// before; present, WebKitGTK keeps the fast path.
//
// An operator overrides either way by exporting WEBKIT_DISABLE_DMABUF_RENDERER
// themselves — any value at all, including "0", leaves this untouched and hands
// the interpretation to WebKitGTK. The startup line below names that escape
// hatch, because the failure it guards against is silent by nature and the
// person hitting it has a black window and nothing to search for.
const webkitDisableDMABUFRenderer = "WEBKIT_DISABLE_DMABUF_RENDERER"

func configureWebviewEnv() {
	if _, set := os.LookupEnv(webkitDisableDMABUFRenderer); set {
		return
	}
	if gpu.HasRenderNode(gpu.DRMDeviceDir) {
		return
	}
	if err := os.Setenv(webkitDisableDMABUFRenderer, "1"); err != nil {
		log.Printf("webview: could not set %s: %v", webkitDisableDMABUFRenderer, err)
		return
	}
	log.Printf(
		"webview: no DRM render node in %s, disabling the DMABUF renderer so the window paints; "+
			"set %s=0 to override if this machine composites correctly",
		gpu.DRMDeviceDir, webkitDisableDMABUFRenderer,
	)
}

// webviewStartURL tells the frontend, before its first paint, when WebKitGTK is
// compositing on the CPU and will not draw backdrop-filter. index.css then makes
// the glass tiers opaque (:root.no-backdrop-blur). Called after
// configureWebviewEnv, so it sees the renderer that was actually chosen.
func webviewStartURL() string {
	if gpu.SoftwareCompositing(os.LookupEnv) {
		return "/?" + gpu.SoftwareCompositingQuery
	}
	return "/"
}

// GTK draws its own decoration — a title bar carrying the window name and the
// minimise/maximise/close cluster on the RIGHT — above whatever the webview
// paints. DevKit's own 40px title bar then lands under it, so the window wears
// two of them and loses ~45px of height to a bar it does not control.
//
// macOS has no such problem: MacTitleBarHiddenInset in main.go makes the app's
// bar BE the window's bar, with the traffic lights inset into it. Frameless is
// the Linux equivalent — Wails drops the GTK decoration and the app's bar
// becomes the only one. Wails' JS runtime keeps the window draggable through
// --wails-draggable (already set across the whole header) and re-implements
// edge resizing for exactly this case (drag.ts gates it on Linux + frameless),
// so nothing is lost but the duplicate bar.
//
// The frontend must agree about who draws the window controls: see
// ownsWindowControls in frontend/src/lib/platform.ts.
const framelessWindow = true

// applyWindowTitle puts the window's name back after Frameless took it away.
//
// Wails skips gtk_window_set_title entirely for a frameless window
// (linux_cgo.go: setTitle is guarded on !options.Frameless), reasoning that a
// window with no decoration has nowhere to draw a title. But WM_NAME is not
// only decoration: it is what the GNOME overview prints under the window
// thumbnail and what a window list or a screenshot tool reads. Left unset, GTK
// falls back to the program name and DevKit shows up as "dev-kit".
//
// Setting it costs nothing visible — an undecorated window still draws no title
// — and restores exactly what the window had before it went frameless.
func applyWindowTitle(window application.Window, title string) {
	if window == nil {
		return
	}
	handle := window.NativeWindow()
	if handle == nil {
		return
	}
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	// InvokeSync is what keeps the CString alive across the call: it returns
	// only once GTK's main loop has run the closure.
	application.InvokeSync(func() {
		C.gtk_window_set_title((*C.GtkWindow)(handle), cTitle)
	})
}
