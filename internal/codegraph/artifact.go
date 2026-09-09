// internal/codegraph/artifact.go
package codegraph

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Where a built analyzer artifact already is.
//
// The shape is the one editors use for language servers: the artifact is built
// once, by CI, and travels inside the package, so a user installs the app and
// the analyzer is simply there. Building it on the user's machine is the
// fallback for people running from source, not the normal path.
//
// Nothing here is language-specific. A toolchain passes its own language, its
// own artifact file name and its own build output, and gets back the list to
// search in order.

// AnalyzerOverrideEnv names the environment variable that pins one language's
// analyzer artifact, overriding every search location.
//
// Per language rather than one variable for all of them, because a machine can
// legitimately want to override one analyzer and not another. Editors expose
// the same escape hatch as a setting; an environment variable is the shape
// DevKit already uses elsewhere, and it is what makes the search testable
// without a packaged install to test against.
func AnalyzerOverrideEnv(language string) string {
	return "DEVKIT_CODEGRAPH_ANALYZER_" + strings.ToUpper(language)
}

// analyzerSearchPaths lists, in order, every place a usable artifact may be.
//
// Bundled locations come before buildOutput because a packaged app should trust
// what it shipped with. Someone running from source has no bundled copy and
// falls through; someone who wants to override either takes the environment
// variable.
func analyzerSearchPaths(language, fileName, buildOutput string) []string {
	var paths []string
	if override := os.Getenv(AnalyzerOverrideEnv(language)); override != "" {
		paths = append(paths, override)
	}
	paths = append(paths, installedResourcePaths(fileName)...)
	if buildOutput != "" {
		paths = append(paths, buildOutput)
	}
	return paths
}

// installedResourcePaths lists where this platform's packaging puts a data file
// that ships beside the binary. Kept next to the search itself because it is
// the same knowledge the packaging manifests encode, and the two have to agree.
func installedResourcePaths(fileName string) []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	switch runtime.GOOS {
	case "darwin":
		// Contents/MacOS/<binary> -> Contents/Resources, where an .app bundle
		// keeps everything that is not the executable.
		return []string{
			filepath.Join(dir, "..", "Resources", fileName),
			filepath.Join(dir, fileName),
		}
	case "windows":
		// No convention to honour: an installed Windows app is a directory of
		// files next to the .exe.
		return []string{filepath.Join(dir, fileName)}
	default:
		// FHS: the binary lands in <prefix>/bin, so its data goes in
		// <prefix>/share/dev-kit. The same arithmetic works for an AppImage's
		// usr/bin and usr/share. lib/ is checked too because some packagers
		// treat a jar as a library rather than as data.
		return []string{
			filepath.Join(dir, "..", "share", "dev-kit", fileName),
			filepath.Join(dir, "..", "lib", "dev-kit", fileName),
			filepath.Join(dir, fileName),
		}
	}
}

// windowsAppRoots lists the directories Windows installers write into. Locators
// glob under these; on every other platform it is empty and the loop is skipped.
func windowsAppRoots() []string {
	var roots []string
	for _, variable := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
		if root := os.Getenv(variable); root != "" {
			roots = append(roots, root)
		}
	}
	return roots
}

// firstExisting returns the first path that is a non-empty file.
//
// Non-empty matters: a killed build leaves a zero-byte artifact behind, and
// wiring the analyzer at one trades a clear "not built yet" for a crash inside
// the runtime.
func firstExisting(paths []string) string {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			return path
		}
	}
	return ""
}
