package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func decisionFor(t *testing.T, toolInput map[string]any) string {
	t.Helper()
	hit := protectedPathHit(toolInput)
	if hit == "" {
		return ""
	}
	return hit
}

func TestHookBlocksAgentAccessToItsOwnCredentials(t *testing.T) {
	// The jail binds these read-write so Claude Code can authenticate at all.
	// That grant is for the process; a tool reading the token would put it in
	// a transcript DevKit then stores.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	cases := []struct {
		name  string
		input map[string]any
	}{
		{"config by path", map[string]any{"file_path": filepath.Join(home, ".claude.json")}},
		{"credentials file", map[string]any{"file_path": filepath.Join(home, ".claude/.credentials.json")}},
		{"bash cat, tilde form", map[string]any{"command": "cat ~/.claude/.credentials.json"}},
		{"directory listing, no trailing slash", map[string]any{"command": "ls " + filepath.Join(home, ".claude")}},
		{"nested in a list", map[string]any{"paths": []any{"/tmp/ok", filepath.Join(home, ".claude/")}}},
		{"nested in an object", map[string]any{"opts": map[string]any{"cwd": filepath.Join(home, ".claude/")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if decisionFor(t, tc.input) == "" {
				t.Fatalf("allowed access to credentials: %v", tc.input)
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
		if hit := protectedPathHit(input); hit != "" {
			t.Fatalf("input %v was blocked by %q, want deferred", input, hit)
		}
	}
}

func TestHookDefersOnAnUnreadablePayload(t *testing.T) {
	// A schema change must not break every session. An unparseable payload has
	// nothing to say, which is the same answer as "no opinion".
	var payload struct {
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.Unmarshal([]byte(`{"tool_input": "not an object"}`), &payload); err == nil {
		t.Fatal("expected the mismatched shape to fail to unmarshal")
	}
	if hit := protectedPathHit(nil); hit != "" {
		t.Fatalf("nil input blocked by %q, want deferred", hit)
	}
}
