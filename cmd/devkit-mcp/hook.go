package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"devkit/internal/agenthub"
)

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
// Everything else returns an empty decision so the normal flow — including
// DevKit's own permission gate — still runs. This hook is a floor, not a gate.
func runPreToolUseHook() error {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}

	var payload struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	// A payload this build cannot parse must not block the turn: an unparseable
	// hook that failed closed would break every session on a schema change.
	// Deferring is the same answer as "nothing to say".
	if err := json.Unmarshal(raw, &payload); err != nil {
		return emitHookDecision(nil)
	}

	if hit := protectedPathHit(payload.ToolInput); hit != "" {
		return emitHookDecision(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": agenthub.HookDenyReason + " (" + hit + "). The agent process authenticates with them; tools may not read them.",
			},
		})
	}
	return emitHookDecision(nil)
}

// protectedPaths are the grants claudeCredentialPaths widens the jail by, and
// only those: $HOME/.claude and $HOME/.claude.json.
//
// Deliberately anchored to home rather than matching ".claude/" anywhere. A
// project of its own can have a .claude directory — this repo does — holding
// settings, skills and hooks the agent has every reason to read. Blocking that
// would make DevKit unable to work on projects that use Claude Code, which is
// most of the ones it exists for.
//
// Both the absolute path and the tilde form are matched: a Bash command names
// it "~/.claude", a Read tool names it "/Users/x/.claude", and the check has to
// hold for tool shapes this build does not know about.
// One prefix per spelling, not one per file: "$HOME/.claude" already covers
// the directory, everything under it, and .claude.json, because the filename
// starts with the same characters. "$HOME/.claude-backup" matches too, which
// is the right side to err on.
func protectedPaths() []string {
	out := []string{"~/.claude"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, strings.TrimSuffix(home, "/")+"/.claude")
	}
	return out
}

func protectedPathHit(input map[string]any) string {
	paths := protectedPaths()
	var hit string
	var walk func(any)
	walk = func(v any) {
		if hit != "" {
			return
		}
		switch t := v.(type) {
		case string:
			for _, p := range paths {
				if strings.Contains(t, p) {
					hit = p
					return
				}
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
	walk(input)
	return hit
}

// emitHookDecision writes the decision, or "{}" to defer. Exit status stays 0
// either way: a non-zero exit is how a hook reports its own failure, and this
// hook failing is not a reason to block the agent's work.
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
