package agenthub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agentruntime"
)

// TestClaudeACPAuthMethodsLive probes the claude-agent-acp DevKit installed —
// the runtime under DevKit's own data directory, in the sandbox a session gets
// — and checks that it offers both terminal sign-ins, and that the command line
// it attaches passes acceptTerminalAuth rather than falling back. It never runs
// the login.
//
//	DEVKIT_CLAUDE_ACP=1 go test ./internal/agenthub/ -run TestClaudeACPAuthMethodsLive -v
//
// DEVKIT_RUNTIMES_ROOT points it at another runtimes root, so a version can be
// measured before it replaces the one the app is using.
func TestClaudeACPAuthMethodsLive(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_ACP") != "1" {
		t.Skip("set DEVKIT_CLAUDE_ACP=1 to probe the installed claude-agent-acp")
	}
	root := os.Getenv("DEVKIT_RUNTIMES_ROOT")
	if root == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		root = filepath.Join(config, "DevKit", "runtimes")
	}
	manager := agentruntime.New(root)
	rt, err := manager.Resolve(agentruntime.ClaudeACP)
	if err != nil {
		t.Skipf("no managed Claude runtime: %v", err)
	}
	for _, remote := range []string{"NO_BROWSER", "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", "CLAUDE_CODE_REMOTE"} {
		if os.Getenv(remote) != "" {
			t.Skipf("%s is set; the adapter offers the remote TUI login instead", remote)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a := NewClaudeAdapter(ClaudeOptions{Runtime: func() (agentruntime.Resolved, error) { return rt, nil }})
	l := runtimeLaunch(rt)
	res, err := a.initialize(ctx, l)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	accepted := map[string]bool{}
	for _, m := range res.AuthMethods {
		var meta terminalAuthMeta
		if json.Unmarshal(m.Meta, &meta) != nil || meta.TerminalAuth == nil {
			continue
		}
		accepted[m.ID] = acceptTerminalAuth(meta.TerminalAuth.Command, meta.TerminalAuth.Args, l)
		t.Logf("%s (%s): %s %v accepted=%v", m.ID, meta.TerminalAuth.Label, meta.TerminalAuth.Command, meta.TerminalAuth.Args, accepted[m.ID])
	}
	for _, id := range []string{"claude-ai-login", "console-login"} {
		if !accepted[id] {
			t.Fatalf("%s: missing or its terminal-auth command was refused (got %v)", id, accepted)
		}
	}

	methods, err := a.AuthMethods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0].Command != rt.Command {
		t.Fatalf("resolved methods = %+v", methods)
	}
}
