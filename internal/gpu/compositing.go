package gpu

// SoftwareCompositingQuery is the start-URL flag the frontend reads to know
// that nothing will paint behind its glass surfaces. See SoftwareCompositing.
const SoftwareCompositingQuery = "software-compositing"

// SoftwareCompositing reports whether WebKitGTK will composite this window on
// the CPU, where it parses backdrop-filter and paints nothing: every
// translucent surface that relies on a blur behind it becomes a window onto
// whatever it covers, text and all.
//
// CSS cannot find this out — @supports asks whether a declaration parses, and
// WebKitGTK says yes — so the native side, which chose the path, says so.
//
// Read from the environment after configureWebviewEnv has had its say, so it
// covers both the case DevKit decides (no DRM render node) and an operator's
// own override. WebKit treats either variable as set when it has any value
// other than "0", and so does this.
//
// ponytail: WebKitGTK with DMABUF off may still composite on a GPU through its
// shared-memory path on some drivers. Flattening the glass there too costs a
// look, never legibility; narrow it if someone measures blur painting there.
func SoftwareCompositing(lookup func(string) (string, bool)) bool {
	return setAndNotZero(lookup, "WEBKIT_DISABLE_COMPOSITING_MODE") ||
		setAndNotZero(lookup, "WEBKIT_DISABLE_DMABUF_RENDERER")
}

func setAndNotZero(lookup func(string) (string, bool), name string) bool {
	value, ok := lookup(name)
	return ok && value != "0"
}
