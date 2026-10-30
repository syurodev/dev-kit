package arch

import (
	"sort"
	"strings"
	"testing"
)

// The shape these rules protect:
//
//	React/Wails · native shell · CLI · MCP client       <- clients
//	                    ↓
//	        the adapter for each (root package, bridge, mcp)
//	                    ↓
//	             the application API (bridge's methods over app.Container)
//	                    ↓
//	   harness · tools · policy · domain services       <- internal/*
//	                    ↓
//	         storage · OS · external APIs
//
// Everything above the adapters may change or be replaced - a different window
// toolkit, no window at all, a CLI, an HTTP host - without touching a line below
// them, as long as nothing below imports upward. So the toolkit is imported by the
// shell and nowhere else, and the adapter packages are imported only by the entry
// points that assemble them.

const wailsPrefix = "github.com/wailsapp/wails"

// wailsImporters is a CLOSED list of the packages that may import the window
// toolkit: the desktop shell (the root package, which owns the windows, the menus,
// the native dialogs and the Wails service binding) and the iOS scaffolding.
// Nothing under internal/ or cmd/ may - not the bridge, not app, not any domain
// package - so the Go side can be hosted by something that is not Wails.
var wailsImporters = map[string]bool{
	"devkit":           true,
	"devkit/build/ios": true,
}

func TestOnlyTheDesktopShellImportsTheWindowToolkit(t *testing.T) {
	out := goList(t, "{{.ImportPath}}\t{{join .Imports \" \"}}\t{{join .TestImports \" \"}}\t{{join .XTestImports \" \"}}", "devkit/...")
	var bad []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || wailsImporters[fields[0]] {
			continue
		}
		// Tests are held to it too: a test that needs a window is a test of the shell.
		for _, imps := range fields[1:] {
			for _, imp := range strings.Fields(imps) {
				if strings.HasPrefix(imp, wailsPrefix) {
					bad = append(bad, fields[0]+" imports "+imp)
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("window toolkit leak: %s; the toolkit belongs to the desktop shell (root package). Take what you need through a small port the shell implements (see bridge.NativeHost)", b)
	}
}
