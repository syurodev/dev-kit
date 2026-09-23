package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"devkit/internal/agenthub"
)

const maxHookPayloadBytes = 4 << 20

// runPreToolUseHook is the PreToolUse hook Agent Hub installs into every Claude
// session. It reads one hook payload on stdin and writes one decision on stdout.
//
// Why a hook and not the permission gate: the gate (ACP session/request_permission,
// the SDK's canUseTool) is the LAST step of Claude Code's evaluation order —
// hooks, deny rules, ask rules, permission mode, allow rules, then the gate.
// Anything approved earlier never reaches it. Hooks run before all of it, and a
// hook deny holds even in bypassPermissions mode.
//
// It denies one thing and defers everything else. The jail binds ~/.claude and
// ~/.claude.json read-write because Claude Code needs its own credentials to
// authenticate at all (see claudeCredentialPaths). That grant is for the
// process, not for the model: an agent that can ask a tool to read its own
// OAuth token can put it in a transcript, and DevKit stores transcripts. The
// process keeps its access; the tool surface does not.
//
// Valid, unrelated inputs return an empty decision so the normal flow —
// including DevKit's own permission gate — still runs. Inputs the hook cannot
// safely inspect are denied because the hook is the only check before the
// vendor's earlier allow rules. This hook is defense-in-depth, not a complete
// security boundary against arbitrary command/path construction.
func runPreToolUseHook() error {
	// Read one byte beyond the limit so an exact-size payload is accepted but a
	// truncated or oversized payload is never mistaken for a complete one.
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookPayloadBytes+1))
	if err != nil {
		return emitHookDecision(hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (payload read failed); the tool call was denied."))
	}
	return emitHookDecision(preToolUseDecision(raw))
}

func preToolUseDecision(raw []byte) map[string]any {
	if len(raw) > maxHookPayloadBytes {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (payload too large); the tool call was denied.")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (malformed JSON); the tool call was denied.")
	}

	toolNameRaw, hasToolName := fields["tool_name"]
	toolInputRaw, hasToolInput := fields["tool_input"]
	if !hasToolName || !hasToolInput || bytes.Equal(bytes.TrimSpace(toolInputRaw), []byte("null")) {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (missing required fields); the tool call was denied.")
	}

	var toolName string
	var toolInput map[string]any
	if err := json.Unmarshal(toolNameRaw, &toolName); err != nil || strings.TrimSpace(toolName) == "" {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (invalid tool_name); the tool call was denied.")
	}
	if err := json.Unmarshal(toolInputRaw, &toolInput); err != nil || toolInput == nil {
		return hookDenyDecision("DevKit could not safely inspect the Claude Code tool payload (invalid tool_input); the tool call was denied.")
	}

	if hit := protectedPathHit(toolName, toolInput); hit != "" {
		return hookDenyDecision(agenthub.HookDenyReason + " (" + hit + "). The agent process authenticates with them; tools may not read them.")
	}
	return nil
}

func hookDenyDecision(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
}

func currentUsername() (string, error) {
	if value := os.Getenv("USER"); value != "" {
		return value, nil
	}
	if current, err := user.Current(); err == nil {
		return current.Username, nil
	}
	return "", fmt.Errorf("current user unavailable")
}

func protectedPathHit(toolName string, input map[string]any) string {
	// Built-in tools have documented, stable path/command fields. Inspect only
	// those fields so source code, prompts, and replacement text may discuss
	// Claude configuration without being mistaken for an attempted read.
	var values []any
	switch toolName {
	case "Bash", "PowerShell":
		values = []any{input["command"]}
	case "Read", "Edit", "Write":
		values = []any{input["file_path"]}
	case "Glob", "Grep":
		values = []any{input["path"]}
	case "NotebookEdit":
		values = []any{input["notebook_path"]}
	case "WebFetch", "WebSearch", "Agent", "Workflow", "AskUserQuestion", "ExitPlanMode", "Skill":
		return ""
	default:
		// MCP and future tools have vendor-defined shapes. Keep the conservative
		// recursive behavior for those unknown surfaces until their semantics are
		// known; false positives there are preferable to silently exposing auth.
		values = []any{input}
	}

	var hit string
	var walk func(any)
	walk = func(v any) {
		if hit != "" {
			return
		}
		switch t := v.(type) {
		case string:
			if matched := protectedReferenceHit(t); matched != "" {
				hit = matched
				return
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, value := range values {
		walk(value)
	}
	return hit
}

// protectedReferenceHit recognizes direct paths and the common shell spellings
// Claude emits. It intentionally does not try to interpret arbitrary shell
// programs. Complete isolation requires keeping vendor credentials outside the
// tool process; this hook only reduces exposure while that is not yet possible.
func protectedReferenceHit(value string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	home = filepath.ToSlash(filepath.Clean(home))

	for _, token := range referenceTokens(value) {
		if hit := credentialFileReference(token); hit != "" {
			return hit
		}
		expanded := expandHomeReferences(token, home)
		// Options such as --file=/Users/x/.claude.json retain the path in the
		// same shell word. Test each suffix beginning at the effective home.
		for offset := indexPath(expanded, home); offset >= 0; {
			candidate := strings.TrimRight(expanded[offset:], ",:]}\"")
			candidate = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(candidate, "\\", "/")))
			if isProtectedHomePath(candidate, home) {
				return candidate
			}
			next := indexPath(expanded[offset+1:], home)
			if next < 0 {
				break
			}
			offset += next + 1
		}
	}
	return ""
}

func expandHomeReferences(value, home string) string {
	value = strings.NewReplacer(
		"${HOME}", home,
		"$HOME", home,
		"~/", home+"/",
	).Replace(value)
	if username, err := currentUsername(); err == nil && username != "" {
		parent := filepath.ToSlash(filepath.Dir(home))
		value = strings.NewReplacer(
			parent+"/${USER}", home,
			parent+"/$USER", home,
		).Replace(value)
	}
	return filepath.ToSlash(value)
}

func referenceTokens(value string) []string {
	// Quotes group shell words but do not become part of the resulting path.
	// Removing them also joins common concatenated spellings such as
	// "$HOME"/.claude.json before tokenization.
	value = strings.NewReplacer("\"", "", "'", "").Replace(value)
	return strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(";&|<>()`", r)
	})
}

func indexPath(value, path string) int {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.Index(strings.ToLower(value), strings.ToLower(path))
	}
	return strings.Index(value, path)
}

func isProtectedHomePath(candidate, home string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		candidate = strings.ToLower(candidate)
		home = strings.ToLower(home)
	}
	dir := home + "/.claude"
	if candidate == home+"/.claude.json" {
		return true
	}
	if candidate != dir && !strings.HasPrefix(candidate, dir+"/") {
		return false
	}
	return !isAgentOwnedClaudePath(strings.TrimPrefix(candidate, dir))
}

// isAgentOwnedClaudePath reports whether rel — a path inside ~/.claude,
// starting with "/" — is one the agent itself is meant to read and write:
//
//   - projects/<project>/memory/…, Claude Code's auto-memory. The agent
//     saves what it learns there with its own Write/Bash tools, and a
//     blanket block on ~/.claude made every save fail with this hook's
//     denial.
//   - CLAUDE.md, the user-level memory file the agent is asked to edit
//     ("remember this for every project").
//
// Neither holds credentials, and both are exact shapes — a path is cleaned
// before it gets here, so memory/../../../.credentials.json is no longer
// under memory by then. Everything else in ~/.claude stays protected:
// .credentials.json, settings.json (its env block can carry API keys, and a
// tool rewriting it could loosen the agent's own permission rules), and the
// other projects' transcripts.
func isAgentOwnedClaudePath(rel string) bool {
	if rel == "/CLAUDE.md" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(rel, "/"), "/")
	return len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" && parts[2] == "memory"
}

// credentialFileReference catches the credential files by name when the path
// does not start at the home directory — a relative one the home-prefix
// check cannot see: "cat .claude/.credentials.json" run from $HOME, or
// "cat ../../../.credentials.json" run from inside a memory directory, which
// is reachable now that memory is allowed. .credentials.json is Claude
// Code's own token file and nothing else is called that; .claude.json only
// counts when relative, since an absolute one outside $HOME (a project's
// file of that name) is not the agent's.
func credentialFileReference(token string) string {
	token = strings.TrimRight(token, ",:]}\"")
	if i := strings.LastIndex(token, "="); i >= 0 {
		token = token[i+1:] // --file=.claude/.credentials.json
	}
	path := filepath.ToSlash(strings.ReplaceAll(token, "\\", "/"))
	base := path[strings.LastIndex(path, "/")+1:]
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		base = strings.ToLower(base)
	}
	relative := !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~") && !strings.HasPrefix(path, "$")
	if base == ".credentials.json" || (relative && base == ".claude.json") {
		return path
	}
	return ""
}

// emitHookDecision writes the decision, or "{}" to defer. A deny is emitted
// as a normal successful hook response so Claude can apply it even when the
// payload itself was malformed or oversized.
func emitHookDecision(decision map[string]any) error {
	if decision == nil {
		_, err := os.Stdout.WriteString("{}\n")
		return err
	}
	body, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(body, '\n'))
	return err
}
