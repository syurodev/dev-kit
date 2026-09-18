// internal/codegraph/ignore.go
package codegraph

// ignoredDirs are never descended into — not by detection, not by the file
// watcher, not by the file list: dependency caches, build output and VCS
// metadata. They hold no source the user wrote, they are where the file count
// explodes, and a build directory full of generated sources would otherwise
// register as a module of its own, or keep the watcher re-indexing while a
// build writes into it.
//
// One list on purpose. The watcher and the file list used to carry their own
// shorter ones, so a build writing into out/ or dist/ looked like a source
// change and the tree listed every vendored dependency.
var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "venv": true, ".venv": true,
	"target": true, "build": true, "out": true, "dist": true, "bin": true,
	".gradle": true, ".idea": true, ".vscode": true, ".devkit": true,
}

// ignoredDir reports whether a directory with this name is skipped.
func ignoredDir(name string) bool {
	return ignoredDirs[name]
}
