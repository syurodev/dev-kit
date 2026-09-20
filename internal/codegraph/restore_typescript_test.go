// internal/codegraph/restore_typescript_test.go
package codegraph

import (
	"path/filepath"
	"testing"
)

// The registry main.go builds names the TypeScript analyzer relative to the
// working directory ("analyzer-ts/bin/tsgo"), and the language server starts
// with the opened project as ITS working directory. Opening a project, and
// restoring it on the next launch, must both survive that difference — the
// restore is the one a person meets first, because it runs before they have
// touched anything and a failure sits in the sidebar until they reopen.
//
// Runs the real tsgo, so it skips where that has not been built.
func TestTypeScriptProjectOpensAndRestoresWithRelativeToolchain(t *testing.T) {
	tsgoPath(t)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	config := t.TempDir()
	root := writeTSProject(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"target":"es2020","module":"commonjs","strict":true}}`,
		"src/a.ts":      "export const a = 1\n",
	})

	first := NewService(NewNilStore(), nil)
	first.SetToolchains(DefaultToolchains("."))
	if err := first.RestoreLastProject(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if err := first.OpenProjectAt(t.Context(), ProjectDBPath(config, root), root); err != nil {
		t.Fatalf("open: %v", err)
	}
	first.Close()
	first.store.Close()

	next := NewService(NewNilStore(), nil)
	next.SetToolchains(DefaultToolchains("."))
	t.Cleanup(func() { next.Close(); next.store.Close() })
	if err := next.RestoreLastProject(t.Context(), config); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := next.WaitForIndexReady(t.Context()); err != nil {
		t.Fatalf("index after restore: %v", err)
	}
	// The refresh the restore starts in the background reaches the analyzer too.
	next.runReindex(t.Context())
	if _, err := next.WaitForIndexReady(t.Context()); err != nil {
		t.Fatalf("index after the restore's refresh: %v", err)
	}
}
