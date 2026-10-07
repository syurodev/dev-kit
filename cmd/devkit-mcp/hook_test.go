package main

import (
	"encoding/json"
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

// Claude Code spills a large tool result into this session's tool-results
// directory and tells the agent to read it. A reading tool may reach that
// directory and nothing else in ~/.claude.
func TestSessionToolResultsAreReadable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	home = filepath.ToSlash(home)
	const session = "42e768a5-3a41-4ea3-8824-a1f4d67e3430"
	project := home + "/.claude/projects/-home-u-Workspace-kong"
	spill := project + "/" + session + "/tool-results/mcp-Atlassian-getConfluencePage-1.txt"

	payload := func(tool, field, path, sessionID, transcript string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"tool_name":       tool,
			"tool_input":      map[string]any{field: path},
			"session_id":      sessionID,
			"transcript_path": transcript,
		})
		return raw
	}
	own := project + "/" + session + ".jsonl"
	cases := []struct {
		name   string
		raw    []byte
		denied bool
	}{
		{"read own spill", payload("Read", "file_path", spill, session, own), false},
		{"grep own spill directory", payload("Grep", "path", project+"/"+session+"/tool-results", session, own), false},
		{"glob own spill directory", payload("Glob", "path", project+"/"+session+"/tool-results/", session, own), false},
		{"another session's spill", payload("Read", "file_path", project+"/other-session/tool-results/x.txt", session, own), true},
		{"another project's spill", payload("Read", "file_path", home+"/.claude/projects/-home-u-other/"+session+"/tool-results/x.txt", session, own), true},
		{"traversal out of the spill", payload("Read", "file_path", project+"/"+session+"/tool-results/../../../../.credentials.json", session, own), true},
		{"the transcript itself", payload("Read", "file_path", own, session, own), true},
		{"credentials", payload("Read", "file_path", home+"/.claude/.credentials.json", session, own), true},
		{"settings", payload("Read", "file_path", home+"/.claude/settings.json", session, own), true},
		{"write is not a reading tool", payload("Write", "file_path", spill, session, own), true},
		{"shell is not a reading tool", payload("Bash", "command", "cat "+spill, session, own), true},
		{"no transcript_path", payload("Read", "file_path", spill, session, ""), true},
		{"no session_id", payload("Read", "file_path", spill, "", own), true},
		{"transcript of another session", payload("Read", "file_path", spill, session, project+"/other-session.jsonl"), true},
		{"transcript outside projects", payload("Read", "file_path", spill, session, home+"/elsewhere/"+session+".jsonl"), true},
		{"transcript too deep", payload("Read", "file_path", spill, session, project+"/sub/"+session+".jsonl"), true},
		{"transcript traversing projects", payload("Read", "file_path", spill, session, home+"/.claude/projects/../"+session+".jsonl"), true},
		{"session id carrying a path", payload("Read", "file_path", home+"/.claude/projects/p/../x/tool-results/y", "../x", home+"/.claude/projects/p/../x.jsonl"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preToolUseDecision(tc.raw) != nil
			if got != tc.denied {
				t.Fatalf("denied = %v, want %v", got, tc.denied)
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

// Git writes in the shell are sent to DevKit's tools; everything else git does
// is the agent's to run. The first case is the command a real session ran.
func TestShellGitWriteIsRedirected(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"cd /home/u/fsap/kong-plugin\ngit commit -F - <<'MSG'\nfeat: x\nMSG", "commit"},
		{"git add a.lua && git commit -m 'x'", "commit"},
		{"git -C /w/kong-plugin commit -am x", "commit"},
		{"GIT_AUTHOR_NAME=x git -c user.email=y commit -m z", "commit"},
		{"git push origin feature/x", "push"},
		{"sudo /usr/bin/git push", "push"},
		{"(cd repo; git push -u origin HEAD)", "push"},
		{"git commit --amend --no-edit", "commit"},
		{"git fetch && git rebase origin/main", "rebase"},
		{"git rebase --abort", "rebase"},
		{"git -C ../auth cherry-pick abc123", "cherry-pick"},
		{"git revert --no-edit HEAD", "revert"},
		{"git status && git diff --stat", ""},
		{"git merge-base HEAD main", ""},
		{"git pull --rebase origin main", "pull"},
		{"git fetch origin", ""},
		{"git log --oneline -5 | head", ""},
		{"git add -A", ""},
		{"git commit-tree HEAD^{tree}", ""},
		{`echo "remember to git commit"`, ""},
		{`grep -rn "git push" docs/`, ""},
	}
	for _, c := range cases {
		if got := shellGitWrite("Bash", map[string]any{"command": c.command}); got != c.want {
			t.Errorf("%q = %q, want %q", c.command, got, c.want)
		}
	}
	if shellGitWrite("Read", map[string]any{"command": "git push"}) != "" {
		t.Error("only Bash runs commands")
	}
}

// The deny names the tool to call: a model reads the reason as the result.
func TestShellGitWriteDenyNamesTheTool(t *testing.T) {
	decision := preToolUseDecision([]byte(`{"tool_name":"Bash","tool_input":{"command":"git commit -m x"}}`))
	out, _ := decision["hookSpecificOutput"].(map[string]any)
	if out["permissionDecision"] != "deny" || !strings.Contains(out["permissionDecisionReason"].(string), "mcp__devkit__git_commit") {
		t.Fatalf("decision = %v", decision)
	}
	if preToolUseDecision([]byte(`{"tool_name":"Bash","tool_input":{"command":"git status"}}`)) != nil {
		t.Fatal("a read-only git command was not left alone")
	}
}
