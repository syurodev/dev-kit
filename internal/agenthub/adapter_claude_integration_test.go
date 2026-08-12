package agenthub_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
)

// Integration smoke: live Claude ACP binary inside sandbox.StartPiped.
// Skip unless DEVKIT_CLAUDE_ACP=1 and an absolute binary (or AllowNpx) is available.
func TestClaudeACPIntegration(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_ACP") != "1" {
		t.Skip("set DEVKIT_CLAUDE_ACP=1 to run Claude ACP integration")
	}

	cmdPath := os.Getenv("DEVKIT_CLAUDE_ACP_BIN")
	opts := agenthub.ClaudeOptions{}
	if cmdPath != "" {
		if !filepath.IsAbs(cmdPath) {
			t.Fatalf("DEVKIT_CLAUDE_ACP_BIN must be absolute, got %q", cmdPath)
		}
		opts.Command = cmdPath
	} else {
		opts.AllowNpx = true
		opts.Command = "npx"
		if _, err := exec.LookPath("npx"); err != nil {
			t.Skip("npx not on PATH and DEVKIT_CLAUDE_ACP_BIN unset")
		}
	}

	a := agenthub.NewClaudeAdapter(opts)
	proj := t.TempDir()
	rt, err := a.Open(context.Background(), proj)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	if rt.ACPSessionID() == "" {
		t.Fatal("expected ACP session id")
	}
	out, err := rt.Prompt(context.Background(), "Reply with exactly: pong")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if out.StopReason == "" {
		t.Fatal("expected stopReason")
	}
}
