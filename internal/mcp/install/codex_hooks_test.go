package install

import (
	"os"
	"path/filepath"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"devkit/internal/codexhook"
)

func TestCodexInstallWritesOneHandlerPerRegisteredEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmd := stableTestCommand(t)

	if err := Install(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: cmd}); err != nil {
		t.Fatal(err)
	}
	hooks := readCodexHooks(t, filepath.Join(home, ".codex", "config.toml"))

	if len(hooks) != len(codexhook.Registered) {
		t.Fatalf("wrote %d hook events, want %d (%v)", len(hooks), len(codexhook.Registered), hooks)
	}
	for _, event := range codexhook.Registered {
		groups, ok := hooks[string(event)]
		if !ok {
			t.Fatalf("no [hooks.%s] written", event)
		}
		if len(groups) != 1 {
			t.Fatalf("[hooks.%s] has %d matcher groups, want 1", event, len(groups))
		}
		group := groups[0]
		handlers, _ := group["hooks"].([]any)
		if len(handlers) != 1 {
			t.Fatalf("[hooks.%s] handler count = %d, want 1", event, len(handlers))
		}
		handler := handlers[0].(map[string]any)
		if handler["type"] != "mcp_tool" || handler["server"] != ServerName || handler["tool"] != codexhook.MCPToolName {
			t.Fatalf("[hooks.%s] handler = %+v", event, handler)
		}
		input, ok := handler["input"].(map[string]any)
		if !ok || len(input) == 0 {
			t.Fatalf("[hooks.%s] handler.input = %+v", event, handler["input"])
		}
		wantMatcher := codexhook.NeedsMatcher(event)
		_, hasMatcher := group["matcher"]
		if hasMatcher != wantMatcher {
			t.Fatalf("[hooks.%s] has matcher=%v, want %v", event, hasMatcher, wantMatcher)
		}
	}
}

// A placeholder naming a field the event's own command input does not carry
// fails the whole hook in Codex, so the exact argument template
// codexhook.Arguments builds has to reach the file unmodified.
func TestCodexInstallInputTemplateMatchesArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := Install(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: stableTestCommand(t)}); err != nil {
		t.Fatal(err)
	}
	hooks := readCodexHooks(t, filepath.Join(home, ".codex", "config.toml"))

	for _, event := range codexhook.Registered {
		want := codexhook.Arguments(event)
		handler := hooks[string(event)][0]["hooks"].([]any)[0].(map[string]any)
		got, _ := handler["input"].(map[string]any)
		if len(got) != len(want) {
			t.Fatalf("%s: input has %d keys, want %d (%v vs %v)", event, len(got), len(want), got, want)
		}
		for key, template := range want {
			if got[key] != template {
				t.Fatalf("%s.input[%q] = %v, want %q", event, key, got[key], template)
			}
		}
	}
}

// Re-running Install must not accumulate a second DevKit group per event —
// each install call replaces its own, never adds beside it.
func TestCodexInstallIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	req := InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: stableTestCommand(t)}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	if err := Install(req); err != nil {
		t.Fatal(err)
	}
	hooks := readCodexHooks(t, filepath.Join(home, ".codex", "config.toml"))
	for _, event := range codexhook.Registered {
		if got := len(hooks[string(event)]); got != 1 {
			t.Fatalf("[hooks.%s] has %d groups after three installs, want 1", event, got)
		}
	}
}

// A matcher group the user wrote by hand — for their own command, or for a
// different MCP server entirely — must survive both install and uninstall.
func TestCodexInstallLeavesTheUsersOwnHooksAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	seed := []byte(`
[[hooks.PreToolUse]]
matcher = "^Bash$"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "echo mine"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "mcp_tool"
server = "someone-else"
tool = "their.tool"
`)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Install(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: stableTestCommand(t)}); err != nil {
		t.Fatal(err)
	}
	hooks := readCodexHooks(t, path)
	if len(hooks[string(codexhook.EventPreToolUse)]) != 2 {
		t.Fatalf("PreToolUse groups = %d, want the user's plus DevKit's", len(hooks[string(codexhook.EventPreToolUse)]))
	}
	if len(hooks[string(codexhook.EventStop)]) != 2 {
		t.Fatalf("Stop groups = %d, want someone-else's plus DevKit's", len(hooks[string(codexhook.EventStop)]))
	}

	if err := Uninstall(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	hooks = readCodexHooks(t, path)
	if len(hooks[string(codexhook.EventPreToolUse)]) != 1 {
		t.Fatalf("after uninstall, PreToolUse groups = %d, want the user's own left alone", len(hooks[string(codexhook.EventPreToolUse)]))
	}
	if len(hooks[string(codexhook.EventStop)]) != 1 {
		t.Fatalf("after uninstall, Stop groups = %d, want someone-else's left alone", len(hooks[string(codexhook.EventStop)]))
	}
	// And neither group is now mistaken for DevKit's own.
	remainingPreToolUse := hooks[string(codexhook.EventPreToolUse)][0]["hooks"].([]any)[0].(map[string]any)
	if remainingPreToolUse["command"] != "echo mine" {
		t.Fatalf("wrong PreToolUse group survived: %+v", remainingPreToolUse)
	}
}

// Uninstall drops an event key entirely once DevKit's group was the only
// thing in it, and drops [hooks] entirely once nothing is left at all — the
// same shrink-to-nothing rule mcp_servers already follows.
func TestCodexUninstallDropsEmptyHooksEntirely(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := Install(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: stableTestCommand(t)}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["hooks"]; ok {
		t.Fatalf("[hooks] table survived an uninstall with nothing else in it:\n%s", raw)
	}
}

// A hand-authored "state" table (trust/enable flags Codex itself manages)
// must never be mistaken for an event's matcher-group list.
func TestUninstallLeavesTheHookStateTableAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	seed := []byte(`
[hooks.state]
"some-hook-key" = { enabled = true }
`)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal, Command: stableTestCommand(t)}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(InstallRequest{Agent: AgentCodex, Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("[hooks.state] was dropped along with DevKit's own entries:\n%s", raw)
	}
	if _, ok := hooks["state"]; !ok {
		t.Fatalf("[hooks.state] is gone:\n%s", raw)
	}
}

func readCodexHooks(t *testing.T, path string) map[string][]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	hooksTable, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("[hooks] missing in %s:\n%s", path, raw)
	}
	out := map[string][]map[string]any{}
	for key, raw := range hooksTable {
		list, ok := raw.([]any)
		if !ok {
			continue // "state"
		}
		var groups []map[string]any
		for _, item := range list {
			if group, ok := item.(map[string]any); ok {
				groups = append(groups, group)
			}
		}
		out[key] = groups
	}
	return out
}
