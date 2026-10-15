package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/preferences"
	"github.com/pelletier/go-toml/v2"
)

func customizationSkill(t *testing.T, name string) string {
	t.Helper()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test helper\n---\nUse helper.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "helper.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	return folder
}
func TestCustomizationValidationAndSourceLifecycle(t *testing.T) {
	folder := customizationSkill(t, "local-test")
	value, err := ValidateCustomization("codex", preferences.AgentCustomization{SkillFolders: []string{folder}, DisabledSkills: []string{filepath.Join(folder, "SKILL.md")}})
	if err != nil {
		t.Fatal(err)
	}
	plugin, cleanup, err := claudeCustomization(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plugin, "skills", "local-test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled skill linked")
	}
	cleanup()
	value.DisabledSkills = nil
	plugin, cleanup, err = claudeCustomization(value)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(plugin, "skills", "local-test", "helper.txt"))
	if err != nil || string(data) != "original" {
		t.Fatalf("helper lost: %s, %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(folder, "helper.txt"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(plugin, "skills", "local-test", "helper.txt"))
	if string(data) != "updated" {
		t.Fatal("source updates not visible")
	}
	cleanup()
	if _, err := os.Stat(folder); err != nil {
		t.Fatal("cleanup deleted source", err)
	}
	for _, server := range []preferences.AgentMCPConfig{{Name: "devkit", Command: "node"}, {Name: "x.y", Command: "node"}, {Name: "remote", URL: "http://example.com/mcp"}, {Name: "remote", URL: "https://user:secret@example.com/mcp"}, {Name: "env", Command: "node", EnvKeys: []string{"TOKEN=secret"}}} {
		if _, err := ValidateCustomization("codex", preferences.AgentCustomization{Servers: []preferences.AgentMCPConfig{server}}); !errors.Is(err, ErrInvalidConfigOption) {
			t.Fatalf("accepted invalid MCP %s", server.Name)
		}
	}
}
func TestCodexManagedCustomizationDoesNotEditSources(t *testing.T) {
	home := t.TempDir()
	if _, err := prepareCodexHome(home); err != nil {
		t.Fatal(err)
	}
	folder := customizationSkill(t, "local-test")
	value := preferences.AgentCustomization{SkillFolders: []string{folder}, DisabledSkills: []string{filepath.Join(folder, "SKILL.md")}, DisabledMCP: []string{"inherited"}, Servers: []preferences.AgentMCPConfig{{Name: "local", Command: "node", Args: []string{"mcp.js"}, EnvKeys: []string{"API_TOKEN"}, Enabled: true}}}
	_, args, err := applyCodexCustomization(home, value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "mcp_servers.inherited.enabled=false") {
		t.Fatal(args)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "env_vars") || !strings.Contains(string(data), "enabled = false") {
		t.Fatal(string(data))
	}
	if _, _, err := applyCodexCustomization(home, preferences.AgentCustomization{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, "skills", importedSkillAlias(folder))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("removed import still linked")
	}
	if _, err := os.Stat(filepath.Join(folder, "SKILL.md")); err != nil {
		t.Fatal("removed source", err)
	}
}
func TestClaudeCustomizationPreservesGuardAndUsesOnlyReferencedEnv(t *testing.T) {
	folder := customizationSkill(t, "local-test")
	value := preferences.AgentCustomization{DisabledSkills: []string{filepath.Join(folder, "SKILL.md")}, DisabledMCP: []string{"external"}, Servers: []preferences.AgentMCPConfig{{Name: "stdio", Command: "node", EnvKeys: []string{"TOKEN"}, Enabled: true}, {Name: "off", Command: "node", Enabled: false}}}
	meta := claudeCustomizationMeta(sessionNewMeta("/devkit/hook", "prompt", "mcp__devkit__git_commit"), value, "/plugin")
	var body map[string]any
	if err := json.Unmarshal(meta, &body); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"PreToolUse", "allowedTools", "settingSources", "Skill(local-test)", "mcp__external__*", "/plugin"} {
		if !strings.Contains(string(meta), part) {
			t.Fatalf("lost %s", part)
		}
	}
	servers := customizationMCP(value, map[string]string{"TOKEN": "value", "UNRELATED": "private"})
	if len(servers) != 1 {
		t.Fatal("disabled server provided")
	}
	server := servers[0].(acp.McpServerStdio)
	if len(server.Env) != 1 || server.Env[0].Name != "TOKEN" {
		t.Fatal("wrong env refs")
	}
	report := EnvironmentReport{}
	customizationEnvironment(context.Background(), &report, value)
}
