package codegraph

import (
	"os"
	"path/filepath"
	"testing"
)

// The artifact search is language-agnostic, so these tests name no language:
// they use a made-up one to prove the machinery does not care which it is.
const testLanguage = "fictional"

func TestAnalyzerOverrideEnvIsPerLanguage(t *testing.T) {
	// One variable for every analyzer would mean a machine could not override
	// one and leave the others alone, which is the only reason to override at
	// all.
	if got := AnalyzerOverrideEnv("java"); got != "DEVKIT_CODEGRAPH_ANALYZER_JAVA" {
		t.Fatalf("unexpected override variable: %q", got)
	}
	if AnalyzerOverrideEnv("java") == AnalyzerOverrideEnv("typescript") {
		t.Fatal("two languages must not share one override variable")
	}
}

func TestAnalyzerSearchPrefersAnExplicitOverride(t *testing.T) {
	buildOutput := filepath.Join(t.TempDir(), "analyzer.bin")
	writeArtifact(t, buildOutput, "built here")

	override := filepath.Join(t.TempDir(), "analyzer.bin")
	writeArtifact(t, override, "pinned by the operator")
	t.Setenv(AnalyzerOverrideEnv(testLanguage), override)

	paths := analyzerSearchPaths(testLanguage, "analyzer.bin", buildOutput)
	if len(paths) == 0 || paths[0] != override {
		t.Fatalf("an explicit override must come first, got %v", paths)
	}
	if got := firstExisting(paths); got != override {
		t.Fatalf("expected the override to win, got %q", got)
	}
}

func TestAnalyzerSearchPutsTheBuildOutputLast(t *testing.T) {
	// A packaged app should trust the artifact it shipped with. Someone running
	// from source has no shipped copy and falls through to their own build.
	buildOutput := filepath.Join(t.TempDir(), "analyzer.bin")

	paths := analyzerSearchPaths(testLanguage, "analyzer.bin", buildOutput)
	if len(paths) < 2 {
		t.Fatalf("expected install locations before the build output, got %v", paths)
	}
	if paths[len(paths)-1] != buildOutput {
		t.Fatalf("the build output should be the last resort, got %v", paths)
	}

	if firstExisting(paths) != "" {
		t.Fatal("nothing is built and nothing shipped — nothing should be found")
	}
	writeArtifact(t, buildOutput, "built here")
	if got := firstExisting(paths); got != buildOutput {
		t.Fatalf("a locally built artifact still counts, got %q", got)
	}
}

func TestAnalyzerSearchLooksWhereThePackagingWrites(t *testing.T) {
	// The layouts the packaging manifests write into: Contents/Resources on
	// macOS, <prefix>/share/dev-kit on Linux and the AppImage, beside the .exe
	// on Windows. Asserted as arithmetic against this binary rather than per
	// platform, since the test only ever runs on one of them.
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exeDir := filepath.Dir(exe)

	paths := installedResourcePaths("analyzer.bin")
	if len(paths) == 0 {
		t.Fatal("an installed app must look somewhere relative to its own binary")
	}
	var besideBinary bool
	for _, path := range paths {
		if filepath.Dir(path) == exeDir {
			besideBinary = true
		}
		if !filepath.IsAbs(path) {
			t.Fatalf("install locations must be absolute, got %q", path)
		}
	}
	if !besideBinary {
		t.Fatalf("the directory holding the binary must be searched, got %v", paths)
	}
}

func TestFirstExistingSkipsAnEmptyArtifact(t *testing.T) {
	// A zero-byte file is what a killed build leaves behind. Treating it as
	// usable trades a clear "not built yet" for a crash inside the runtime.
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.bin")
	writeArtifact(t, empty, "")
	real := filepath.Join(dir, "real.bin")
	writeArtifact(t, real, "content")

	if got := firstExisting([]string{empty, real}); got != real {
		t.Fatalf("expected the non-empty artifact, got %q", got)
	}
	if got := firstExisting([]string{dir}); got != "" {
		t.Fatalf("a directory is not an artifact, got %q", got)
	}
}

func writeArtifact(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
