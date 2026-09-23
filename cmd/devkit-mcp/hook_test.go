package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decisionFor(t *testing.T, toolInput map[string]any) string {
	t.Helper()
	hit := protectedPathHit("Unknown", toolInput)
	if hit == "" {
		return ""
	}
	return hit
}

func TestProtectedPathHit(t *testing.T) {
	// The jail binds these read-write so Claude Code can authenticate at all.
	// That grant is for the process; a tool reading the token would put it in a
	// transcript DevKit then stores.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	home = filepath.ToSlash(home)
	parent := filepath.ToSlash(filepath.Dir(home))
	cases := []struct {
		name  string
		input map[string]any
		want  bool
	}{
		{"config by path", map[string]any{"file_path": home + "/.claude.json"}, true},
		{"credentials file", map[string]any{"file_path": home + "/.claude/.credentials.json"}, true},
		{"bash cat, tilde form", map[string]any{"command": "cat ~/.claude/.credentials.json"}, true},
		{"bash cat, home variable", map[string]any{"command": `cat "$HOME/.claude.json"`}, true},
		{"bash cat, braced home variable", map[string]any{"command": `cat "${HOME}/.claude.json"`}, true},
		{"bash concatenated quote segments", map[string]any{"command": `cat "$HOME"/.claude.json`}, true},
		{"bash cat, user variable", map[string]any{"command": "cat \"" + parent + "/$USER/.claude.json\""}, true},
		{"bash cat, braced user variable", map[string]any{"command": "cat \"" + parent + "/${USER}/.claude.json\""}, true},
		{"repeated separators", map[string]any{"command": `cat "$HOME//./.claude.json"`}, true},
		{"parent traversal", map[string]any{"command": "cat " + home + "/../" + filepath.Base(home) + "/.claude.json"}, true},
		{"directory listing, no trailing slash", map[string]any{"command": "ls " + home + "/.claude"}, true},
		{"nested in a list", map[string]any{"paths": []any{"/tmp/ok", home + "/.claude/"}}, true},
		{"nested in an object", map[string]any{"opts": map[string]any{"cwd": home + "/.claude/"}}, true},
		{"project Claude directory", map[string]any{"file_path": "/Users/x/project/.claude/settings.json"}, false},
		{"home subdirectory named Claude", map[string]any{"command": `cat "$HOME/project/.claude/settings.json"`}, false},
		{"similar HOME variable", map[string]any{"command": `cat "$HOMER/.claude.json"`}, false},
		{"similar Claude filename", map[string]any{"command": `cat "$HOME/.claude-backup"`}, false},
		{"user variable outside home", map[string]any{"command": `cat "/Users/$USER/project/.claude.json"`}, false},
		// Claude Code's auto-memory and the user CLAUDE.md are the agent's own
		// to write; a save there used to be denied as a credential read.
		{"memory write via bash", map[string]any{"command": "cat > " + home + "/.claude/projects/-home-u-Workspace-kong/memory/request_integrity_doc.md"}, false},
		{"memory write via tool", map[string]any{"file_path": home + "/.claude/projects/-home-u-Workspace-kong/memory/MEMORY.md"}, false},
		{"memory directory listing", map[string]any{"command": "ls ~/.claude/projects/p/memory"}, false},
		{"user CLAUDE.md", map[string]any{"file_path": home + "/.claude/CLAUDE.md"}, false},
		// ...and nothing next to them is.
		{"project transcript", map[string]any{"file_path": home + "/.claude/projects/p/session.jsonl"}, true},
		{"projects root", map[string]any{"command": "ls ~/.claude/projects"}, true},
		{"memory-named sibling", map[string]any{"file_path": home + "/.claude/projects/p/memory-notes/x.md"}, true},
		{"settings", map[string]any{"file_path": home + "/.claude/settings.json"}, true},
		{"traversal out of memory", map[string]any{"command": "cat ~/.claude/projects/p/memory/../../../.credentials.json"}, true},
		{"relative credentials from memory", map[string]any{"command": "cd ~/.claude/projects/p/memory && cat ../../../.credentials.json"}, true},
		{"relative credentials from home", map[string]any{"command": "cd && cat .claude/.credentials.json"}, true},
		{"relative config from home", map[string]any{"command": "cat ./.claude.json"}, true},
		{"relative credentials in an option", map[string]any{"command": "jq . --file=.claude/.credentials.json"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decisionFor(t, tc.input) != ""
			if got != tc.want {
				t.Fatalf("protectedPathHit(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestHookDefersEverythingElse(t *testing.T) {
	// A floor, not a gate: everything else must still reach DevKit's own
	// permission gate rather than being decided here.
	for _, input := range []map[string]any{
		{"command": "ls /tmp"},
		{"file_path": "/Users/x/project/main.go"},
		{"command": "git status"},
		// A project's own .claude directory is not the protected grant. This
		// repo has one; blocking it would stop the agent reading the settings
		// and skills of the very projects DevKit exists to work on.
		{"file_path": "/Users/x/project/.claude/settings.json"},
		{"command": "cat .claude/settings.json"},
		{},
	} {
		if hit := protectedPathHit("Unknown", input); hit != "" {
			t.Fatalf("input %v was blocked by %q, want deferred", input, hit)
		}
	}
}

func TestPreToolUseDecisionPayloads(t *testing.T) {
	largeInput := strings.Repeat("x", maxHookPayloadBytes)
	cases := []struct {
		name       string
		raw        string
		wantDenied bool
	}{
		{"allowed unrelated tool input", `{"tool_name":"Bash","tool_input":{"command":"ls /tmp"}}`, false},
		{"denied missing tool input", `{"tool_name":"Unknown"}`, true},
		{"denied missing tool name", `{"tool_input":{}}`, true},
		{"denied empty tool name", `{"tool_name":"","tool_input":{}}`, true},
		{"denied nested credential path", `{"tool_name":"mcp__files__read","tool_input":{"options":{"paths":["/tmp/ok","$HOME/.claude.json"]}}}`, true},
		{"denied malformed JSON", `{"tool_name":"Bash","tool_input":{"command":"cat ~/.claude.json"}`, true},
		{"denied mismatched tool input shape", `{"tool_name":"Unknown","tool_input":"not an object"}`, true},
		{"denied null tool input", `{"tool_name":"Unknown","tool_input":null}`, true},
		{"denied oversized payload", `{"tool_name":"Bash","tool_input":{"command":"` + largeInput + `"}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preToolUseDecision([]byte(tc.raw)) != nil
			if got != tc.wantDenied {
				t.Fatalf("preToolUseDecision denied = %v, want %v", got, tc.wantDenied)
			}
		})
	}
}

func TestKnownToolsInspectOnlySecurityRelevantFields(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	credential := filepath.ToSlash(filepath.Join(home, ".claude.json"))
	cases := []struct {
		name     string
		toolName string
		input    map[string]any
		denied   bool
	}{
		{"read credential path", "Read", map[string]any{"file_path": credential}, true},
		{"write credential path", "Write", map[string]any{"file_path": credential, "content": "safe"}, true},
		{"write documentation mentioning credential", "Write", map[string]any{"file_path": "/tmp/docs.md", "content": `Use "$HOME/.claude.json"`}, false},
		{"edit documentation mentioning credential", "Edit", map[string]any{"file_path": "/tmp/docs.md", "old_string": "$HOME/.claude", "new_string": "updated"}, false},
		{"agent prompt mentioning credential", "Agent", map[string]any{"prompt": "Explain $HOME/.claude.json"}, false},
		{"bash command reading credential", "Bash", map[string]any{"command": `cat "$HOME/./.claude.json"`}, true},
		{"unknown nested tool remains conservative", "mcp__files__read", map[string]any{"options": map[string]any{"paths": []any{credential}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := protectedPathHit(tc.toolName, tc.input) != ""
			if got != tc.denied {
				t.Fatalf("protectedPathHit denied = %v, want %v", got, tc.denied)
			}
		})
	}
}

func TestPreToolUseDecisionAcceptsMaximumPayload(t *testing.T) {
	// Padding inside a valid, unrelated JSON string exercises the boundary
	// without depending on the exact number of bytes in the envelope.
	prefix := `{"tool_name":"Bash","tool_input":{"command":"`
	suffix := `"}}`
	raw := []byte(prefix + strings.Repeat("x", maxHookPayloadBytes-len(prefix)-len(suffix)) + suffix)
	if len(raw) != maxHookPayloadBytes {
		t.Fatalf("payload length = %d, want %d", len(raw), maxHookPayloadBytes)
	}
	if decision := preToolUseDecision(raw); decision != nil {
		t.Fatalf("maximum-size unrelated payload was denied: %v", decision)
	}
}
