// internal/codegraph/toolchain_ts_test.go
package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func toolchainFor(t *testing.T, statuses []ToolchainStatus, language string) ToolchainStatus {
	t.Helper()
	for _, status := range statuses {
		if status.Language == language {
			return status
		}
	}
	t.Fatalf("no %s toolchain in %v", language, statuses)
	return ToolchainStatus{}
}

// The third shape a Toolchain takes: the artifact launches itself, so there is
// no runtime to probe and nothing for the user to install in order to RUN it.
func TestATypeScriptArtifactIsItsOwnRuntime(t *testing.T) {
	root := t.TempDir()
	toolchain := TypeScriptToolchain(root)

	if toolchain.Run[0] != ArtifactArg {
		t.Fatalf("Run[0] = %q, want the artifact itself", toolchain.Run[0])
	}
	// No requirement is consulted, so an empty resolved list is enough.
	if !toolchain.runtimeMet(nil) {
		t.Error("a self-launching artifact has no runtime to be missing")
	}

	// Without the artifact it is not ready, and the reason is the artifact
	// rather than a missing runtime — which is what lets the setup panel say
	// "build this" instead of "install something".
	status := toolchain.status(context.Background())
	if status.Ready || status.HasArtifact {
		t.Errorf("status with no artifact = %+v, want not ready", status)
	}

	// With it, ready — whatever else is or is not installed.
	artifact := toolchain.Artifact
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if status := toolchain.status(context.Background()); !status.Ready {
		t.Errorf("status with an artifact = %+v, want ready", status)
	}
}

// And the argv it runs is the artifact, not a runtime that launches it.
func TestTypeScriptServerCommandRunsTheArtifact(t *testing.T) {
	root := t.TempDir()
	toolchain := TypeScriptToolchain(root)
	if err := os.MkdirAll(filepath.Dir(toolchain.Artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolchain.Artifact, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	argv, _, err := toolchain.serverCommand(context.Background())
	if err != nil {
		t.Fatalf("serverCommand: %v", err)
	}
	if len(argv) != 3 || argv[0] != toolchain.Artifact || argv[1] != "--lsp" {
		t.Errorf("argv = %v, want the artifact launching its own language server", argv)
	}
}

func TestDetectsATypeScriptProject(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains("."))

	root := t.TempDir()
	for name, content := range map[string]string{
		"tsconfig.json":                 `{"include": ["src"]}`,
		"package.json":                  `{"name": "api"}`,
		"src/users/users.controller.ts": "export class UsersController {}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	detected, err := service.DetectProject(root)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if detected.Language != "typescript" {
		t.Fatalf("language = %q, want typescript", detected.Language)
	}
	if len(detected.Modules) != 1 || detected.Modules[0] != "." {
		t.Errorf("modules = %v, want the root", detected.Modules)
	}
}

// A workspace is one module per package, the same way a Kong repository is one
// per plugin and a Maven repository is one per pom.
func TestATypeScriptWorkspaceIsOneModulePerPackage(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains("."))

	root := t.TempDir()
	for _, pkg := range []string{"packages/api", "packages/worker"} {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("export const x = 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	detected, err := service.DetectProject(root)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if detected.Language != "typescript" || len(detected.Modules) != 2 {
		t.Errorf("detected = %+v, want two TypeScript modules", detected)
	}
}

// One analyzer per language, kept: a language server that is rebuilt on every
// open is a language server that is cold on every open.
func TestTheSameAnalyzerIsReusedForALanguage(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(t.TempDir()))

	first, ok := service.analyzerFor("typescript")
	if !ok {
		t.Fatal("no analyzer registered for typescript")
	}
	second, _ := service.analyzerFor("typescript")
	if first != second {
		t.Error("expected the analyzer to be kept between opens")
	}
}

// Closing the service has to reach a language server; one left running after
// the app quits is a process the user has no way to find.
type closingAnalyzer struct {
	fakeAnalyzer
	closed *bool
}

func (c closingAnalyzer) CloseAnalyzer() error {
	*c.closed = true
	return nil
}

func TestClosingTheServiceClosesItsAnalyzers(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	closed := false
	service.mu.Lock()
	service.analyzers = map[string]Analyzer{"typescript": closingAnalyzer{closed: &closed}}
	service.mu.Unlock()

	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closed {
		t.Error("expected the analyzer to be told the app is going away")
	}
}
