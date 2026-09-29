// internal/codegraph/toolchain_lua_test.go
package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func luaStatus(t *testing.T, statuses []ToolchainStatus) ToolchainStatus {
	t.Helper()
	for _, status := range statuses {
		if status.Language == "lua" {
			return status
		}
	}
	t.Fatalf("no lua toolchain in %v", statuses)
	return ToolchainStatus{}
}

// The whole point of a built-in analyzer: nothing to install, nothing to
// build, no setup panel — on a machine with no Lua on it at all.
func TestLuaAnalyzerIsReadyWithNothingInstalled(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(t.TempDir()))

	status := luaStatus(t, service.AnalyzerStatus(context.Background()))
	if !status.Ready {
		t.Error("the Lua analyzer ships inside DevKit and must always be ready")
	}
	if !status.HasArtifact {
		t.Error("a built-in analyzer has its artifact by definition")
	}
	if len(status.Requirements) != 0 {
		t.Errorf("a built-in analyzer requires nothing, got %v", status.Requirements)
	}
}

func TestBuildingABuiltInAnalyzerIsRefusedRatherThanFaked(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(t.TempDir()))

	err := service.BuildAnalyzer(context.Background(), "lua")
	if !errors.Is(err, ErrAnalyzerIsBuiltIn) {
		t.Fatalf("BuildAnalyzer(lua) = %v, want ErrAnalyzerIsBuiltIn", err)
	}
}

func TestDetectsALuaProject(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains("."))

	root := t.TempDir()
	for _, name := range []string{
		"kong/plugins/one/handler.lua",
		"kong/plugins/two/handler.lua",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("return {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	detected, err := service.DetectProject(root)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if detected.Language != "lua" {
		t.Fatalf("language = %q, want lua", detected.Language)
	}
	// handler.lua is what Kong names a plugin's entry file, so a repository of
	// plugins reads as one module per plugin.
	if len(detected.Modules) != 2 {
		t.Errorf("modules = %v, want one per plugin", detected.Modules)
	}
}

func TestABareFolderOfLuaIsStillAProject(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains("."))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "init.lua"), []byte("return {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detected, err := service.DetectProject(root)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if detected.Language != "lua" || len(detected.Modules) != 1 || detected.Modules[0] != "." {
		t.Errorf("detected = %+v, want the folder itself as one Lua module", detected)
	}
}

// The registry is keyed by language for a reason: opening a Lua project must
// not start the Java analyzer, whatever was opened before it.
func TestOpenPicksTheAnalyzerForTheLanguageItFound(t *testing.T) {
	service := newTestService(t, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(t.TempDir()))
	service.SetConfigDir(t.TempDir())
	t.Cleanup(func() { service.Close() })

	root := t.TempDir()
	source := `
local M = {}
--- Says hello.
function M.hello(name)
  return "hi " .. name
end
return M
`
	if err := os.WriteFile(filepath.Join(root, "greet.lua"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := service.OpenProjectDir(context.Background(), root); err != nil {
		t.Fatalf("OpenProjectDir: %v", err)
	}
	matches, err := service.SearchSymbol(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected the Lua analyzer to have indexed the project")
	}
	if matches[0].Label != "greet.hello" {
		t.Errorf("label = %q, want greet.hello", matches[0].Label)
	}

	info, err := service.IndexInfo(context.Background())
	if err != nil {
		t.Fatalf("IndexInfo: %v", err)
	}
	if info.Language != "lua" {
		t.Errorf("IndexInfo language = %q, want lua — a restored session needs it", info.Language)
	}
}

// An explicitly installed analyzer is the owner saying they know better, and
// it has to keep winning: it is what every test double in this package is.
func TestAnExplicitAnalyzerBeatsTheRegistry(t *testing.T) {
	service := newTestService(t, fakeAnalyzer{result: sampleResult()})
	service.SetToolchains(DefaultToolchains(t.TempDir()))
	service.SetConfigDir(t.TempDir())
	t.Cleanup(func() { service.Close() })

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "thing.lua"), []byte("return {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.OpenProjectDir(context.Background(), root); err != nil {
		t.Fatalf("OpenProjectDir: %v", err)
	}
	endpoints, err := service.ListEntryPoints(context.Background())
	if err != nil {
		t.Fatalf("ListEntryPoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Error("expected the injected analyzer's result, not the Lua analyzer's")
	}
}
