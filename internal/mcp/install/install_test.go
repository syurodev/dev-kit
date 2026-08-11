package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestListAgents(t *testing.T) {
	agents := ListAgents()
	if len(agents) != 2 || agents[0] != AgentClaudeCode || agents[1] != AgentCodex {
		t.Fatalf("ListAgents() = %v, want [claude-code codex]", agents)
	}
}

func stableTestCommand(t *testing.T) string {
	dir := filepath.Join("/workspace", ".mcp-install-test", strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(dir, "devkit-mcp")
	if err := os.WriteFile(cmd, []byte{0}, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return cmd
}

func TestClaudeInstallGlobalIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := stableTestCommand(t)
	req := InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal, Command: cmd}

	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, ".claude.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(servers))
	}
	entry := servers[ServerName].(map[string]any)
	if entry["command"] != cmd {
		t.Fatalf("command = %v, want %s", entry["command"], cmd)
	}

	// Pre-seed another server and ensure Install preserves it.
	servers["other"] = map[string]any{"command": "/other", "args": []any{}}
	doc["mcpServers"] = servers
	seed, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, append(seed, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers = doc["mcpServers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("want 2 servers after preserve, got %d", len(servers))
	}
	if _, ok := servers["other"]; !ok {
		t.Fatal("expected other server to be preserved")
	}
}

func TestClaudeInstallProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	cmd := stableTestCommand(t)
	req := InstallRequest{
		Agent:       AgentClaudeCode,
		Scope:       ScopeProject,
		ProjectPath: project,
		Command:     cmd,
	}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".mcp.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	entry := servers[ServerName].(map[string]any)
	if entry["command"] != cmd {
		t.Fatalf("command = %v, want %s", entry["command"], cmd)
	}
}

func TestCodexInstallGlobalAndProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := stableTestCommand(t)

	globalReq := InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: cmd}
	if err := Install(globalReq); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(home, ".codex", "config.toml")
	assertCodexEntry(t, globalPath, cmd)

	// Preserve unrelated tables / other MCP servers.
	seed := []byte("model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"/other\"\nargs = []\n")
	if err := os.WriteFile(globalPath, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install(globalReq); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "gpt-5" {
		t.Fatalf("model = %v, want gpt-5 preserved", doc["model"])
	}
	servers := doc["mcp_servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("expected other mcp server preserved")
	}
	assertCodexEntry(t, globalPath, cmd)

	project := t.TempDir()
	projectReq := InstallRequest{
		Agent:       AgentCodex,
		Scope:       ScopeProject,
		ProjectPath: project,
		Command:     cmd,
	}
	if err := Install(projectReq); err != nil {
		t.Fatal(err)
	}
	assertCodexEntry(t, filepath.Join(project, ".codex", "config.toml"), cmd)
}

func TestUninstallRemovesOnlyDevkit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := stableTestCommand(t)

	claudePath := filepath.Join(home, ".claude.json")
	seedClaude := map[string]any{
		"mcpServers": map[string]any{
			ServerName: map[string]any{"command": cmd, "args": []any{}},
			"other":    map[string]any{"command": "/other", "args": []any{}},
		},
	}
	raw, _ := json.MarshalIndent(seedClaude, "", "  ")
	if err := os.WriteFile(claudePath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatal("devkit should be removed")
	}
	if _, ok := servers["other"]; !ok {
		t.Fatal("other server should remain")
	}

	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	seedCodex := []byte("[mcp_servers.devkit]\ncommand = \"/x\"\nargs = []\n\n[mcp_servers.other]\ncommand = \"/other\"\nargs = []\n")
	if err := os.WriteFile(codexPath, seedCodex, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	var tomlDoc map[string]any
	if err := toml.Unmarshal(raw, &tomlDoc); err != nil {
		t.Fatal(err)
	}
	servers = tomlDoc["mcp_servers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatal("devkit should be removed from codex config")
	}
	if _, ok := servers["other"]; !ok {
		t.Fatal("other codex server should remain")
	}

	// Idempotent uninstall when missing.
	if err := Uninstall(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectScopeRequiresPath(t *testing.T) {
	err := Install(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeProject, Command: "/bin/true"})
	if !errors.Is(err, ErrProjectPathRequired) {
		t.Fatalf("error = %v, want ErrProjectPathRequired", err)
	}

	err = Install(InstallRequest{
		Agent:       AgentClaudeCode,
		Scope:       ScopeProject,
		ProjectPath: filepath.Join(t.TempDir(), "missing"),
		Command:     "/bin/true",
	})
	if !errors.Is(err, ErrProjectPathMissing) {
		t.Fatalf("error = %v, want ErrProjectPathMissing", err)
	}
}

func TestInstallRequiresAbsoluteCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	err := Install(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal, Command: "devkit-mcp"})
	if !errors.Is(err, ErrCommandNotAbsolute) {
		t.Fatalf("error = %v, want ErrCommandNotAbsolute", err)
	}
}

func TestRejectEphemeralCommandPath(t *testing.T) {
	ephemeral := filepath.Join(os.TempDir(), "go-build123456789", "exe", "devkit-mcp")
	if err := rejectEphemeralCommandPath(ephemeral); !errors.Is(err, ErrEphemeralCommand) {
		t.Fatalf("rejectEphemeralCommandPath() = %v, want ErrEphemeralCommand", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	err := Install(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal, Command: ephemeral})
	if !errors.Is(err, ErrEphemeralCommand) {
		t.Fatalf("Install() error = %v, want ErrEphemeralCommand", err)
	}

	stable := stableTestCommand(t)
	if err := rejectEphemeralCommandPath(stable); err != nil {
		t.Fatalf("stable path rejected: %v", err)
	}
}

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := "/tmp/proj"

	path, err := ConfigPath(InstallRequest{Agent: AgentClaudeCode, Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".claude.json") {
		t.Fatalf("path = %q", path)
	}
	path, err = ConfigPath(InstallRequest{Agent: AgentCodex, Scope: ScopeProject, ProjectPath: project})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(project, ".codex", "config.toml") {
		t.Fatalf("path = %q", path)
	}
}

func TestResolveCommandFallback(t *testing.T) {
	// With empty PATH and no sibling binary, ResolveCommand should fail clearly.
	t.Setenv("PATH", "")
	_, err := ResolveCommand()
	if err == nil {
		// May succeed if a sibling of the test binary exists named devkit-mcp —
		// only assert error message shape when it fails.
		return
	}
	if !errors.Is(err, ErrProxyNotFound) && !strings.Contains(err.Error(), "devkit-mcp") {
		t.Fatalf("error = %v, want proxy-not-found", err)
	}
}

func assertCodexEntry(t *testing.T, path, wantCmd string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	servers, ok := doc["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers missing in %s:\n%s", path, raw)
	}
	entry, ok := servers[ServerName].(map[string]any)
	if !ok {
		t.Fatalf("devkit entry missing in %s:\n%s", path, raw)
	}
	if entry["command"] != wantCmd {
		t.Fatalf("command = %v, want %s", entry["command"], wantCmd)
	}
}
