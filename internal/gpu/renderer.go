// Package gpu answers the one question WebKitGTK's renderer choice turns on:
// can this machine share GPU buffers at all?
package gpu

import (
	"os"
	"strings"
)

// DRMDeviceDir is where the kernel exposes DRM devices. A parameter on the
// functions below rather than a constant baked into them, so the decision can
// be exercised in a test without a GPU.
const DRMDeviceDir = "/dev/dri"

// HasRenderNode reports whether dir contains a DRM render node (renderD128 and
// friends).
//
// This is the precise condition WebKitGTK's DMABUF renderer needs: a render
// node is the handle a process opens to allocate and export a GPU buffer for
// another process to import. No render node means no buffer to share, which is
// the case on a machine with no GPU at all — a CI container, a VM without
// passthrough, an Xvfb session. There, DMABUF does not fail loudly; WebKitGTK
// composites into a buffer nobody can read and the window paints nothing.
//
// A missing or unreadable directory answers false. Both mean the same thing in
// practice — no buffer can be shared — and guessing "yes" would reintroduce the
// black window this check exists to avoid.
func HasRenderNode(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "renderD") {
			return true
		}
	}
	return false
}
