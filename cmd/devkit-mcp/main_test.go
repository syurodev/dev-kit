package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProxyMissingSocket(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
	// Ensure DevKit dir exists but no socket.
	if err := os.MkdirAll(filepath.Join(cfg, "DevKit"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := runProxy("")
	if err == nil {
		t.Fatal("expected error when socket is missing")
	}
	if !strings.Contains(err.Error(), "DevKit is not running or MCP host is unavailable") {
		t.Fatalf("error = %q, want clear unavailable message", err.Error())
	}
}

func TestRunInstallCmdClaudeGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := filepath.Join(home, "bin", "devkit-mcp")
	if err := runInstallCmd("install", []string{
		"--agent", "claude-code",
		"--scope", "global",
		"--command", cmd,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	entry := servers["devkit"].(map[string]any)
	if entry["command"] != cmd {
		t.Fatalf("command = %v, want %s", entry["command"], cmd)
	}
}
