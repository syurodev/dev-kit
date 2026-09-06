package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"

	"devkit/internal/acp"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	pol, cmd, err := a.buildSandbox(OpenSpec{Cwd: proj})
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
	_, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
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
	_, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for non-absolute command without AllowNpx")
	}
}

type unavailableRunner struct{}

func (unavailableRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	return nil, sandbox.ErrUnavailable
}

func TestClaudeSandboxPolicyCarriesSpec(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(t.TempDir(), "devkit-mcp")
	if err := os.WriteFile(proxy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	sock := filepath.Join(t.TempDir(), "s.sock")
	a := NewClaudeAdapter(ClaudeOptions{Command: bin})
	pol, _, err := a.buildSandbox(OpenSpec{Cwd: proj, AllowUnixSockets: []string{sock}, ReadOnlyPaths: []string{proxy}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pol.AllowUnixSockets) != 1 || pol.AllowUnixSockets[0] != sock {
		t.Fatalf("AllowUnixSockets = %v", pol.AllowUnixSockets)
	}
	found := false
	for _, p := range pol.ReadOnlyPaths {
		if p == proxy {
			found = true
		}
	}
	if !found {
		t.Fatalf("proxy must be readable inside the jail, ReadOnlyPaths = %v", pol.ReadOnlyPaths)
	}
}

func TestClaudeGrantsNodeModulesTreeOfTheCommand(t *testing.T) {
	// A Node CLI entry imports its siblings; granting the entry file alone
	// fails with ERR_MODULE_NOT_FOUND inside the jail.
	root := t.TempDir()
	pkgBin := filepath.Join(root, "lib", "node_modules", "@scope", "tool", "dist")
	if err := os.MkdirAll(pkgBin, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkgBin, "index.js")
	if err := os.WriteFile(entry, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "tool")
	if err := os.Symlink(entry, link); err != nil {
		t.Fatal(err)
	}

	proj := t.TempDir()
	pol, _, err := NewClaudeAdapter(ClaudeOptions{Command: link}).buildSandbox(OpenSpec{Cwd: proj})
	if err != nil {
		t.Fatal(err)
	}
	// nodeModulesRoot walks the resolved path, so compare resolved forms.
	wantTree, err := filepath.EvalSymlinks(filepath.Join(root, "lib", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	var foundTree bool
	for _, p := range pol.ReadOnlyPaths {
		if p == wantTree {
			foundTree = true
		}
	}
	if !foundTree {
		t.Fatalf("expected %s in ReadOnlyPaths, got %v", wantTree, pol.ReadOnlyPaths)
	}
}

func TestClaudeCommandOutsideNodeModulesGrantsOnlyItself(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pol, _, err := NewClaudeAdapter(ClaudeOptions{Command: bin}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(pol.ReadOnlyPaths) != 1 || pol.ReadOnlyPaths[0] != bin {
		t.Fatalf("a plain binary must not widen the grant: %v", pol.ReadOnlyPaths)
	}
}

func TestClaudeGrantsItsOwnCredentialPaths(t *testing.T) {
	// Subscription login keeps credentials in ~/.claude*; without them the
	// agent handshakes and then dies on the first model call.
	home := t.TempDir()
	for _, name := range []string{".claude", ".claude.json"} {
		path := filepath.Join(home, name)
		if name == ".claude" {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)

	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pol, _, err := NewClaudeAdapter(ClaudeOptions{Command: bin}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")} {
		var found bool
		for _, p := range pol.ReadWritePaths {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %s in ReadWritePaths, got %v", want, pol.ReadWritePaths)
		}
	}
	// The grant must stay two exact paths — never the whole home.
	for _, p := range pol.ReadWritePaths {
		if p == home {
			t.Fatalf("must not grant all of $HOME: %v", pol.ReadWritePaths)
		}
	}
}

func TestClaudeEnvCarriesUser(t *testing.T) {
	// Subscription auth resolves the account from USER; without it the agent
	// fails session/prompt with "Authentication required".
	t.Setenv("USER", "alice")
	t.Setenv("LOGNAME", "alice")
	env := claudeEnvAllowlist()
	if env["USER"] != "alice" || env["LOGNAME"] != "alice" {
		t.Fatalf("USER/LOGNAME must reach the jail: %v", env)
	}
}

func TestClaudeEnvFallsBackToCurrentUser(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	if got := claudeEnvAllowlist()["USER"]; got == "" {
		t.Fatal("USER must fall back to the current OS user")
	}
}

// deadVendorRunner starts a process that exits immediately, which is what a
// missing or crashing vendor binary looks like from Open's side: the pipes open,
// then stdout closes without a single JSON-RPC frame ever arriving.
type deadVendorRunner struct{}

func (deadVendorRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	cmd := exec.Command("sh", "-c", "exit 0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{}), nil
}

// Open used to hang forever here. acp.Conn.Call selects on ctx.Done() and the
// response channel; Open deliberately passes a ctx with no deadline, and nothing
// cancelled it when the process died, so both handshake calls parked and every
// ErrProcessFailed branch below them was unreachable. The capture test still
// carries the measurement: "a missing binary hung Open for the full 15m".
//
// The bound is the serve loop ending, not the timeout — so this returns in
// milliseconds, and a five second ceiling here is a hang detector, not a wait.
func TestOpenFailsFastWhenVendorDiesDuringHandshake(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: deadVendorRunner{}})

	done := make(chan error, 1)
	go func() {
		_, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrProcessFailed) {
			t.Fatalf("want ErrProcessFailed, got %v", err)
		}
		if !strings.Contains(err.Error(), "exited during handshake") {
			t.Fatalf("error should name the process exit, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open did not return: the handshake is unbounded again")
	}
}

func TestSessionNewCutsOffUserSettings(t *testing.T) {
	// claude-agent-acp passes settingSources ["user","project","local"] to the
	// Agent SDK unless session/new._meta overrides it, which let a user's own
	// ~/.claude/settings.json reach inside the jail. Measured against the real
	// CLI: with permissions.allow ["Bash"], a shell command that wrote a file
	// ran and the permission gate was never consulted — the gate is the last
	// step of the evaluation order, so an allow rule skips it entirely.
	//
	// Asserting the wire bytes, not the Go value: the agent reads this as JSON
	// at a path it chose, and a struct that marshals to the wrong shape would
	// silently restore the hole.
	body, err := json.Marshal(acp.SessionNewRequest{
		Cwd:        "/x",
		McpServers: []any{},
		Meta:       sessionNewMeta,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire struct {
		Meta struct {
			ClaudeCode struct {
				Options struct {
					SettingSources *[]string `json:"settingSources"`
				} `json:"options"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := wire.Meta.ClaudeCode.Options.SettingSources
	if got == nil {
		t.Fatalf("settingSources missing from session/new: %s", body)
	}
	if len(*got) != 0 {
		t.Fatalf("settingSources = %v, want empty so no user settings apply", *got)
	}
}
