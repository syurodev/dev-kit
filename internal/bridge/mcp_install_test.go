package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMcpListAgents(t *testing.T) {
	service := newTestService(t)
	agents, err := service.McpListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0] != "claude-code" || agents[1] != "codex" {
		t.Fatalf("agents = %v", agents)
	}
}

func TestMcpInstallAndUninstallClaudeGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(binDir, "devkit-mcp")
	if err := os.WriteFile(proxy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	service := newTestService(t)
	path, err := service.McpInstall("claude-code", "global", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude.json")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}

	path, err = service.McpUninstall("claude-code", "global", "")
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("uninstall path = %q, want %q", path, want)
	}
}

func TestMcpInstallUnknownAgentDeniedOrError(t *testing.T) {
	service := newTestService(t)
	_, err := service.McpInstall("cursor", "global", "")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestMcpInstallProjectRequiresDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(binDir, "devkit-mcp")
	if err := os.WriteFile(proxy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	service := newTestService(t)
	_, err := service.McpInstall("codex", "project", "")
	if err == nil {
		t.Fatal("expected error for missing project path")
	}

	project := t.TempDir()
	path, err := service.McpInstall("codex", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".codex", "config.toml")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}
