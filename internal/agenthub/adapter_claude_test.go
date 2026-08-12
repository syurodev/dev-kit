package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/sandbox"
)

func TestClaudeSandboxPolicy(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-logged")
	t.Setenv("CLAUDE_API_KEY", "claude-test-not-logged")

	a := NewClaudeAdapter(ClaudeOptions{
		Command: bin,
	})
	proj := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	pol, cmd, err := a.buildSandbox(proj)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Network != sandbox.NetworkHost {
		t.Fatalf("claude v1 requires NetworkHost, got %q", pol.Network)
	}
	if pol.WorkDir != proj {
		t.Fatalf("WorkDir=%q want %q", pol.WorkDir, proj)
	}
	if cmd.Path == "" || !filepath.IsAbs(cmd.Path) {
		t.Fatalf("Cmd.Path must be absolute for jail exec, got %q", cmd.Path)
	}
	if cmd.Path != bin {
		t.Fatalf("Cmd.Path=%q want %q", cmd.Path, bin)
	}
	if pol.Env["ANTHROPIC_API_KEY"] != "sk-test-not-logged" {
		t.Fatal("expected ANTHROPIC_API_KEY from host env allowlist")
	}
	if pol.Env["CLAUDE_API_KEY"] != "claude-test-not-logged" {
		t.Fatal("expected CLAUDE_API_KEY from host env allowlist")
	}
	if pol.Env["PATH"] == "" {
		t.Fatal("expected PATH in Policy.Env for bwrap --clearenv (AllowNpx / relative lookups)")
	}
	foundBin := false
	for _, p := range pol.ReadOnlyPaths {
		if p == bin {
			foundBin = true
			break
		}
	}
	if !foundBin {
		t.Fatalf("expected binary in ReadOnlyPaths, got %v", pol.ReadOnlyPaths)
	}
}

func TestClaudeRefusesUnsandboxedFallback(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := NewClaudeAdapter(ClaudeOptions{
		Command: bin,
		Runner:  unavailableRunner{},
	})
	_, err := a.Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrSandboxUnavailable) && !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("want fail-closed, got %v", err)
	}
}

func TestClaudeNormalizeUsage(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{})
	meta := json.RawMessage(`{"_claude":{"rateLimit":{}},"claudeCode":{}}`)
	u, err := a.NormalizeUsage(meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.RawMeta) == 0 {
		t.Fatal("raw meta must be preserved")
	}
	if string(u.RawMeta) != string(meta) {
		t.Fatalf("RawMeta=%s want %s", u.RawMeta, meta)
	}
}

func TestClaudeRequiresAbsoluteOrAllowNpx(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{
		Command: "npx", // relative; AllowNpx false
	})
	_, _, err := a.buildSandbox(t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-absolute command without AllowNpx")
	}
}

type unavailableRunner struct{}

func (unavailableRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	return nil, sandbox.ErrUnavailable
}
