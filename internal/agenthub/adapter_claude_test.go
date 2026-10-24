package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"

	"devkit/internal/acp"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"devkit/internal/agentruntime"
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

func TestClaudeRequiresAbsoluteCommand(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{Command: "npx"})
	if _, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()}); !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("relative command: %v", err)
	}
}

// With neither an override nor a runtime there is nothing to run, and the
// answer is "not installed" — never a PATH lookup or npx.
func TestClaudeWithNothingToRunIsNotInstalled(t *testing.T) {
	_, _, err := NewClaudeAdapter(ClaudeOptions{}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("err = %v, want ErrAgentNotInstalled", err)
	}
}

func TestClaudeRuntimeNotInstalled(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{}, agentruntime.ErrNotInstalled
	}})
	_, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, ErrAgentNotInstalled) || !errors.Is(err, agentruntime.ErrNotInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaudeRuntimeRequirementKeepsItsSentinel(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{}, agentruntime.ErrRequirementNotMet
	}})
	_, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, agentruntime.ErrRequirementNotMet) || errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaudeRuntimeResolvedLaunch(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-logged")
	versionDir, nodePrefix, proxy := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "devkit-mcp")
	node := filepath.Join(nodePrefix, "bin", "node")
	entry := filepath.Join(versionDir, "node_modules", "x", "dist", "index.js")
	a := NewClaudeAdapter(ClaudeOptions{Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{
			Command:       node,
			Args:          []string{entry},
			ReadOnlyPaths: []string{versionDir, nodePrefix},
			Env: map[string]string{
				"PATH":                   filepath.Dir(node) + string(os.PathListSeparator) + "/usr/bin",
				"CLAUDE_CODE_EXECUTABLE": "/machine/claude",
			},
			Version: "0.74.0",
		}, nil
	}})

	pol, cmd, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir(), ReadOnlyPaths: []string{proxy}})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != node || len(cmd.Args) != 1 || cmd.Args[0] != entry {
		t.Fatalf("cmd = %+v, want node running the entry", cmd)
	}
	for _, want := range []string{versionDir, nodePrefix, proxy} {
		var found bool
		for _, p := range pol.ReadOnlyPaths {
			found = found || p == want
		}
		if !found {
			t.Fatalf("ReadOnlyPaths %v missing %s", pol.ReadOnlyPaths, want)
		}
	}
	if !strings.HasPrefix(pol.Env["PATH"], filepath.Dir(node)) {
		t.Fatalf("PATH = %q, want the runtime's, node first", pol.Env["PATH"])
	}
	if pol.Env["CLAUDE_CODE_EXECUTABLE"] != "/machine/claude" || pol.Env["ANTHROPIC_API_KEY"] != "sk-test-not-logged" {
		t.Fatal("runtime env must merge over the allowlist, not replace it")
	}
	if pol.Network != sandbox.NetworkHost || !pol.AllowKeychain {
		t.Fatalf("runtime launch lost the jail's settings: %+v", pol)
	}
}

func TestClaudeCommandOverridesRuntime(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runtime: func() (agentruntime.Resolved, error) {
		t.Fatal("Runtime consulted despite DEVKIT_CLAUDE_ACP_COMMAND")
		return agentruntime.Resolved{}, nil
	}})
	if _, cmd, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()}); err != nil || cmd.Path != bin {
		t.Fatalf("cmd = %+v, err = %v", cmd, err)
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

// OpenSpec.PrivateHome, set by applySandboxExtras from ShareHome, must
// reach sandbox.Policy.PrivateHome unchanged — nil leaves the adapter's own
// default (private home on), a pointer to false turns it off.
func TestClaudeSandboxPolicyCarriesPrivateHome(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	a := NewClaudeAdapter(ClaudeOptions{Command: bin})

	pol, _, err := a.buildSandbox(OpenSpec{Cwd: proj})
	if err != nil {
		t.Fatal(err)
	}
	if pol.PrivateHome != nil {
		t.Fatalf("pol.PrivateHome = %v, want nil when the spec carries none", pol.PrivateHome)
	}

	shared := false
	hidden := filepath.Join(t.TempDir(), "devkit.db")
	pol, _, err = a.buildSandbox(OpenSpec{Cwd: proj, PrivateHome: &shared, DenyReadPaths: []string{hidden}})
	if err != nil {
		t.Fatal(err)
	}
	if pol.PrivateHome == nil || *pol.PrivateHome != false {
		t.Fatalf("pol.PrivateHome = %v, want a pointer to false", pol.PrivateHome)
	}
	if pol.EffectivePrivateHome() {
		t.Fatal("EffectivePrivateHome() = true, want false with ShareHome carried through")
	}
	if !slices.Contains(pol.DenyReadPaths, hidden) {
		t.Fatalf("pol.DenyReadPaths = %v, want the spec's protected path", pol.DenyReadPaths)
	}
}

// DOCKER_HOST must reach the jail's env: the socket bind alone is not
// enough, since the jail's private HOME hides the docker CLI's own context
// config that would otherwise say which socket to use.
func TestClaudeSandboxPolicyCarriesExtraEnv(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	a := NewClaudeAdapter(ClaudeOptions{Command: bin})
	pol, _, err := a.buildSandbox(OpenSpec{
		Cwd:      proj,
		ExtraEnv: map[string]string{"DOCKER_HOST": "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pol.Env["DOCKER_HOST"]; got != "unix:///var/run/docker.sock" {
		t.Fatalf("Env[DOCKER_HOST] = %q, want the configured socket", got)
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

func TestSessionNewInheritsUserAndProjectSettings(t *testing.T) {
	// Verify the wire format restores Claude's normal discovery sources.
	body, err := json.Marshal(acp.SessionNewRequest{
		Cwd:        "/x",
		McpServers: []any{},
		Meta:       sessionNewMeta("", devkitPrompt(claudeNaming, "")),
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
	if !slices.Equal(*got, []string{"user", "project", "local"}) {
		t.Fatalf("settingSources = %v, want user, project and local settings", *got)
	}
}

func TestSessionNewAppendsDevKitPromptToClaudeCodePreset(t *testing.T) {
	var wire struct {
		SystemPrompt struct {
			Type   string `json:"type"`
			Preset string `json:"preset"`
			Append string `json:"append"`
		} `json:"systemPrompt"`
	}
	if err := json.Unmarshal(sessionNewMeta("", devkitPrompt(claudeNaming, "")), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire.SystemPrompt.Type != "preset" || wire.SystemPrompt.Preset != "claude_code" {
		t.Fatalf("systemPrompt = %+v, want the Claude Code preset", wire.SystemPrompt)
	}
	if wire.SystemPrompt.Append != devkitPrompt(claudeNaming, "") {
		t.Fatal("session/new did not append DevKit's prompt")
	}
}

func TestSessionNewInstallsThePreToolUseHook(t *testing.T) {
	// Measured against the real agent: session/new._meta accepts a settings
	// object with a command hook, the hook fires before the tool runs, and its
	// tool_use_id is the same id the ACP permission request carries — so the
	// hook and the gate are talking about the same call.
	spec := OpenSpec{McpServers: []acp.McpServerStdio{{Name: "devkit", Command: "/jail/devkit-mcp"}}}
	if got := devkitHookCommand(spec); got != "/jail/devkit-mcp" {
		t.Fatalf("hook command = %q, want the devkit MCP server's own binary", got)
	}

	var wire struct {
		ClaudeCode struct {
			Options struct {
				SettingSources *[]string `json:"settingSources"`
				Settings       struct {
					Hooks struct {
						PreToolUse []struct {
							Hooks []struct {
								Type    string   `json:"type"`
								Command string   `json:"command"`
								Args    []string `json:"args"`
							} `json:"hooks"`
						} `json:"PreToolUse"`
					} `json:"hooks"`
				} `json:"settings"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal(sessionNewMeta(devkitHookCommand(spec), devkitPrompt(claudeNaming, "")), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	opts := wire.ClaudeCode.Options
	if opts.SettingSources == nil || !slices.Equal(*opts.SettingSources, []string{"user", "project", "local"}) {
		t.Fatalf("settingSources = %v, want normal sources alongside the hook", opts.SettingSources)
	}
	groups := opts.Settings.Hooks.PreToolUse
	if len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("PreToolUse hooks = %+v, want exactly one", groups)
	}
	h := groups[0].Hooks[0]
	if h.Type != "command" || h.Command != "/jail/devkit-mcp" {
		t.Fatalf("hook = %+v, want a command hook on DevKit's own binary", h)
	}
	if len(h.Args) != 2 || h.Args[0] != "hook" || h.Args[1] != "pretooluse" {
		t.Fatalf("hook args = %v, want [hook pretooluse]", h.Args)
	}
}

func TestSessionNewOmitsTheHookWhenDevKitIsNotInTheJail(t *testing.T) {
	// No devkit MCP server means no binary bound inside the jail. Naming a
	// command that is not there would make every tool call log a hook error.
	if got := devkitHookCommand(OpenSpec{}); got != "" {
		t.Fatalf("hook command = %q, want empty", got)
	}
	if bytes.Contains(sessionNewMeta("", devkitPrompt(claudeNaming, "")), []byte("PreToolUse")) {
		t.Fatal("hook installed without a binary to run")
	}
}

func TestUnknownMethodIsRefusedAndReportedOnce(t *testing.T) {
	// Refusing is right — answering fs/read_text_file with a made-up result
	// would be worse than an error. What was wrong is that the error went only
	// to the agent, so an ACP surface DevKit is specified to serve could have
	// been requested every turn since Phase 4 without leaving a trace.
	//
	// The dedupe is process-wide on purpose; start from a clean slate so a
	// second run in the same process (-count=N) asserts the same thing.
	unknownMethodSeen.Clear()
	t.Cleanup(unknownMethodSeen.Clear)
	var got []string
	h := &acpConnHandler{onUnknownMethod: func(method string, _ json.RawMessage) {
		got = append(got, method)
	}}

	for i := 0; i < 3; i++ {
		if _, err := h.UnknownMethod(context.Background(), "fs/read_text_file", nil); err == nil {
			t.Fatal("unimplemented method was not refused")
		}
	}
	if _, err := h.UnknownMethod(context.Background(), "terminal/create", nil); err == nil {
		t.Fatal("unimplemented method was not refused")
	}

	// Once per method: the question is whether the vendor uses it at all, and
	// the second call adds nothing but noise to a desktop log.
	if len(got) != 2 || got[0] != "fs/read_text_file" || got[1] != "terminal/create" {
		t.Fatalf("reported %v, want each method once", got)
	}
}

func TestAuthStatusNotificationsAreNotReportedAsUnknown(t *testing.T) {
	// claude-agent-acp 0.75+ pushes _auth/status_update after every background
	// sign-in check. Reporting it would put an "unimplemented ACP method" line
	// in the log of every session for something DevKit chose to ignore.
	unknownMethodSeen.Clear()
	t.Cleanup(unknownMethodSeen.Clear)
	var got []string
	h := &acpConnHandler{onUnknownMethod: func(method string, _ json.RawMessage) {
		got = append(got, method)
	}}
	_, _ = h.UnknownMethod(context.Background(), "_auth/status_update", json.RawMessage(`{"authStatus":{}}`))
	if len(got) != 0 {
		t.Fatalf("reported %v, want nothing", got)
	}
}

func TestClientCapabilitiesDeclareFormElicitationOnly(t *testing.T) {
	// Marshalled, because the agent reads JSON: `form` must arrive as an object
	// (truthy), and `url` must not arrive at all — DevKit serves no browser
	// handoff and no elicitation/complete, and declaring it would send MCP URL
	// elicitations to a client that cannot finish them.
	body, err := json.Marshal(devkitClientCapabilities(false))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var caps struct {
		Elicitation map[string]json.RawMessage `json:"elicitation"`
	}
	if err := json.Unmarshal(body, &caps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(caps.Elicitation["form"]) != "{}" {
		t.Fatalf("elicitation.form = %s, want {}; without it AskUserQuestion is disallowed", caps.Elicitation["form"])
	}
	if _, ok := caps.Elicitation["url"]; ok {
		t.Fatal("elicitation.url declared, which DevKit does not serve")
	}
	if len(caps.Elicitation) != 1 {
		t.Fatalf("elicitation = %v, want form only", caps.Elicitation)
	}
}

func TestClientCapabilitiesDeclareOnlyWhatDevKitServes(t *testing.T) {
	caps := devkitClientCapabilities(false)

	// Measured: the agent reads this to decide what to send back. With a prompt
	// that spawns two subagents, declaring `subagents` took agent_message_chunk
	// through this adapter from 7 to 20 — everything the subagents said was
	// being dropped on the wire before, in the module built around running
	// several agents at once.
	if _, ok := caps["subagents"]; !ok {
		t.Fatal("subagents not declared; subagent output is dropped on the wire")
	}

	// fs and terminal must stay absent while ClientHandler has no
	// implementation for them: UnknownMethod refuses the call, so advertising
	// them invites a request DevKit answers with an error. Measurement says
	// this vendor never asks, but the rule is to declare what is true rather
	// than what is harmless — and OnUnknownMethod is what reports it if the
	// vendor ever starts asking.
	for _, unserved := range []string{"fs", "terminal"} {
		if _, ok := caps[unserved]; ok {
			t.Fatalf("declared %q, which ClientHandler does not implement", unserved)
		}
	}

	// Terminal sign-in IS served now: DevKit runs the method's command itself in
	// a terminal tab (AuthMethods → AgentHubStartSignIn), so nothing about it
	// reaches ClientHandler. Without the declaration claude-agent-acp 0.74.0
	// returns no authMethods and a signed-out user has no button to press.
	// Both gates: auth.terminal is the ACP field, _meta["terminal-auth"] the one
	// that makes the adapter attach the full command line.
	auth, _ := caps["auth"].(map[string]any)
	if auth["terminal"] != true {
		t.Fatalf("auth.terminal not declared: %v", caps["auth"])
	}
	meta, _ := caps["_meta"].(map[string]any)
	if meta["terminal-auth"] != true {
		t.Fatalf(`_meta["terminal-auth"] not declared: %v`, caps["_meta"])
	}
}

// TestRuntimeIsDeadWhenTheProcessLeavesOnItsOwn pins the meaning of Dead.
//
// The regression it guards: Dead used to read the serve loop's error, and
// acp.Conn.Serve reports a clean EOF — an agent process that exited, was killed
// by Cancel's shutdown ladder, or was reaped by the OS — as no error at all. So
// a session whose process was gone kept a runtime that answered every prompt
// with agenthub.sessionNotFound, and nothing evicted it: reopening the session
// and retrying hit the same corpse.
func TestRuntimeIsDeadWhenTheProcessLeavesOnItsOwn(t *testing.T) {
	t.Run("clean EOF still means the process is gone", func(t *testing.T) {
		rt := &acpRuntime{}
		if dead, _ := rt.Dead(); dead {
			t.Fatal("a runtime whose serve loop is still running is not dead")
		}

		rt.noteServeExit(nil) // what Serve returns on EOF

		dead, err := rt.Dead()
		if !dead {
			t.Fatal("serve loop ended without Close asking: the session must resume on a fresh process")
		}
		if err != nil {
			t.Fatalf("no error to report for a clean exit, got %v", err)
		}
	})

	t.Run("an exit we asked for is not a death", func(t *testing.T) {
		rt := &acpRuntime{serveDone: closedChan()}
		_ = rt.Close(context.Background())
		rt.noteServeExit(context.Canceled)

		if dead, _ := rt.Dead(); dead {
			t.Fatal("Close parks the process on purpose; nothing should resume behind it")
		}
	})

	t.Run("a crash keeps its reason", func(t *testing.T) {
		rt := &acpRuntime{}
		boom := errors.New("read |0: file already closed")
		rt.noteServeExit(boom)

		dead, err := rt.Dead()
		if !dead || !errors.Is(err, boom) {
			t.Fatalf("Dead() = %v, %v; want true with the crash's reason", dead, err)
		}
	})
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// wireProbe is the agent end of a pipe: it reads one request, hands the raw
// params to the test, and answers so the caller unblocks.
type wireProbe struct {
	method string
	params map[string]any
	err    error
}

func (p *wireProbe) serve(t *testing.T, agent net.Conn, done chan<- struct{}) {
	t.Helper()
	go func() {
		defer close(done)
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			p.err = err
			return
		}
		p.method = msg.Method
		if err := json.Unmarshal(msg.Params, &p.params); err != nil {
			p.err = err
			return
		}
		var id any
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			p.err = err
			return
		}
		p.err = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: id, Result: json.RawMessage(`{}`)})
	}()
}

// silentHandler is a ClientHandler for a runtime that will not be called back.
type silentHandler struct{}

func (silentHandler) RequestPermission(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}
func (silentHandler) SessionUpdate(context.Context, json.RawMessage) error { return nil }
func (silentHandler) UnknownMethod(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

// TestSessionSettingsUseTheFieldNamesTheAgentReads pins the wire shape of the
// two settings calls, which is the one thing every test above this mocked.
//
// The regression: session/set_config_option carried `optionId`. The agent
// reads `params.configId` — the asymmetry is the vendor's, since it *emits*
// options under `id` — so every request resolved to "Unknown config option:
// undefined". Model and effort changes had never worked, and nothing said so:
// the picker moved on click, the call was refused, and the optimistic update
// put the old value back before anyone could read the error.
func TestSessionSettingsUseTheFieldNamesTheAgentReads(t *testing.T) {
	call := func(t *testing.T, invoke func(rt *acpRuntime) error) *wireProbe {
		t.Helper()
		client, agent := net.Pipe()
		t.Cleanup(func() { client.Close(); agent.Close() })

		rt := &acpRuntime{acpSessionID: "acp-1"}
		rt.conn = acp.NewConn(client, silentHandler{})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		go rt.conn.Serve(ctx)

		probe := &wireProbe{}
		done := make(chan struct{})
		probe.serve(t, agent, done)
		if err := invoke(rt); err != nil {
			t.Fatalf("call: %v", err)
		}
		<-done
		if probe.err != nil {
			t.Fatalf("probe: %v", probe.err)
		}
		return probe
	}

	t.Run("set_config_option says configId", func(t *testing.T) {
		probe := call(t, func(rt *acpRuntime) error {
			return rt.SetConfigOption(context.Background(), "model", "opus")
		})

		if probe.method != "session/set_config_option" {
			t.Fatalf("method = %q", probe.method)
		}
		if probe.params["configId"] != "model" {
			t.Fatalf("configId = %v, want \"model\" — the agent reads this field and nothing else", probe.params["configId"])
		}
		if _, wrong := probe.params["optionId"]; wrong {
			t.Fatal("optionId is the permission outcome's field, not this one")
		}
		if probe.params["value"] != "opus" || probe.params["sessionId"] != "acp-1" {
			t.Fatalf("params = %v", probe.params)
		}
	})

	t.Run("set_mode says modeId", func(t *testing.T) {
		handshake := &acp.SessionModeState{CurrentModeID: "auto", AvailableModes: []acp.SessionMode{{ID: "auto"}, {ID: "plan"}}}
		var after *acp.SessionModeState
		probe := call(t, func(rt *acpRuntime) error {
			rt.modes = handshake
			err := rt.SetMode(context.Background(), "plan")
			after = rt.Modes()
			return err
		})

		if probe.method != "session/set_mode" || probe.params["modeId"] != "plan" {
			t.Fatalf("method = %q, params = %v", probe.method, probe.params)
		}
		// Modes() is what a resumed session is re-seeded from: it must name the
		// mode the agent is in now, and the handshake's own answer stays as read.
		if after.CurrentModeID != "plan" || len(after.AvailableModes) != 2 || handshake.CurrentModeID != "auto" {
			t.Fatalf("after set_mode, Modes() = %+v (handshake %+v)", after, handshake)
		}
	})
}

// TestElicitationAnswerUsesTheShapeTheAgentReads pins the response DevKit writes
// to elicitation/create, end to end through the real transport: acp.Conn,
// acpConnHandler, the runtime's OnElicit, and the normalisation in between.
//
// Every assertion is a fact that is invisible when wrong. The agent does not
// reject a malformed answer — it reads it as no answer — so a mistake here shows
// up only as a model that insists the user said nothing.
func TestElicitationAnswerUsesTheShapeTheAgentReads(t *testing.T) {
	ask := func(t *testing.T, resolver ElicitationResolver) map[string]json.RawMessage {
		t.Helper()
		client, agent := net.Pipe()
		t.Cleanup(func() { client.Close(); agent.Close() })

		svc := &Service{ResolveElicitation: resolver}
		rt := &acpRuntime{}
		rt.SetHandlers(RuntimeHandlers{OnElicit: svc.elicitHandler("sess-1")})
		conn := acp.NewConn(client, &acpConnHandler{rt: rt})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		go conn.Serve(ctx)

		req := `{"jsonrpc":"2.0","id":42,"method":"elicitation/create","params":` + compactJSON(t, askUserQuestionRequest) + "}\n"
		go func() { _, _ = agent.Write([]byte(req)) }()

		msg, err := acp.ReadMessage(agent)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if string(msg.ID) != "42" {
			t.Fatalf("response id = %s, want the request's 42", msg.ID)
		}
		if msg.Error != nil {
			t.Fatalf("an answer went out as a JSON-RPC error: %v", msg.Error)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(msg.Result, &result); err != nil {
			t.Fatalf("result %s: %v", msg.Result, err)
		}
		return result
	}

	t.Run("accept", func(t *testing.T) {
		result := ask(t, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"action":"accept","content":{"question_0":"date-fns","question_1":["Caching"],"retries":3}}`), nil
		})

		if string(result["action"]) != `"accept"` {
			t.Fatalf("action = %s", result["action"])
		}
		// Permission's answer is {"outcome":{...}}; the two are one copy-paste apart.
		if _, wrong := result["outcome"]; wrong {
			t.Fatal("an elicitation answer carries permission's outcome wrapper")
		}
		var content map[string]json.RawMessage
		if err := json.Unmarshal(result["content"], &content); err != nil {
			t.Fatalf("content %s: %v", result["content"], err)
		}
		if string(content["question_0"]) != `"date-fns"` {
			t.Fatalf("question_0 = %s, want a JSON string", content["question_0"])
		}
		// A multi-select is an array of strings. The agent joins it itself; a
		// joined string sent from here fails its guard and the answer is lost.
		if string(content["question_1"]) != `["Caching"]` {
			t.Fatalf("question_1 = %s, want a JSON array of strings", content["question_1"])
		}
		if string(content["retries"]) != `3` {
			t.Fatalf("retries = %s, want a JSON number, not text", content["retries"])
		}
	})

	t.Run("decline has no content key at all", func(t *testing.T) {
		result := ask(t, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"action":"decline","content":{"question_0":"date-fns"}}`), nil
		})
		if string(result["action"]) != `"decline"` {
			t.Fatalf("action = %s", result["action"])
		}
		if _, has := result["content"]; has {
			t.Fatalf("decline carried content %s", result["content"])
		}
	})
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return buf.String()
}

func TestToolCallNameTellsACompactionFromASubagent(t *testing.T) {
	// claude-agent-acp 0.78 sends both as kind "think". A tool call carries its
	// name at the top level (0.77+); the compaction only in _meta.claudeCode.
	agent := ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"toolu_1","title":"Explore the repo","kind":"think","name":"Agent","_meta":{"claudeCode":{"toolName":"Agent"}}}`))
	compact := ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Compact conversation","kind":"think","status":"in_progress","_meta":{"contextCompaction":{"version":1},"claudeCode":{"toolName":"compact"}}}`))
	if agent.ToolCall == nil || agent.ToolCall.Name != "Agent" {
		t.Fatalf("agent tool call = %+v, want name Agent", agent.ToolCall)
	}
	if compact.ToolCall == nil || compact.ToolCall.Name != "compact" {
		t.Fatalf("compaction tool call = %+v, want name compact from _meta", compact.ToolCall)
	}

	// An update without a name keeps the one the call started with.
	merged := mergeToolCall(compact.ToolCall, &ToolCall{ID: "c1", Status: "completed"})
	if merged.Name != "compact" || merged.Status != "completed" {
		t.Fatalf("merged = %+v, want the name kept and the status updated", merged)
	}
}

func TestSubagentUpdatesCarryTheirOrigin(t *testing.T) {
	// Measured on claude-agent-acp 0.78: a subagent's messages arrive on the
	// parent's connection under the subagent's sessionId, with message ids of
	// their own. Recorded untagged, a rewind could pick one as a fork point.
	rt := &acpRuntime{acpSessionID: "parent"}
	var got []json.RawMessage
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
		got = append(got, update)
	}})
	h := &acpConnHandler{rt: rt}

	chunk := `{"sessionUpdate":"agent_message_chunk","messageId":"msg_%s","content":{"type":"text","text":"hi"}}`
	for _, sid := range []string{"parent", "a0549d5cb1bae7c3b"} {
		params, _ := json.Marshal(map[string]any{"sessionId": sid, "update": json.RawMessage(fmt.Sprintf(chunk, sid))})
		if err := h.SessionUpdate(context.Background(), params); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("updates = %d, want 2", len(got))
	}
	own, sub := ParseSessionUpdate(got[0]), ParseSessionUpdate(got[1])
	if own.Origin != "" {
		t.Fatalf("the session's own chunk has origin %q", own.Origin)
	}
	if sub.Origin != "a0549d5cb1bae7c3b" || sub.MessageID != "msg_a0549d5cb1bae7c3b" || sub.Text() != "hi" {
		t.Fatalf("subagent chunk = %+v, want its origin and the rest intact", sub)
	}
}

// Asking the agent for its own file-change report is not free: it switches on
// Claude's native checkpointing for every session the process serves, which
// writes file history under ~/.claude. A user who turned DevKit's checkpoints
// off must get no file history from either side, so the capability has to be
// absent, not merely unused.
func TestFileChangeReportCapabilityIsDeclaredOnlyWhenAsked(t *testing.T) {
	off, err := json.Marshal(devkitClientCapabilities(false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(off), "agentFileChangeReport") {
		t.Fatalf("declared with checkpoints off: %s", off)
	}
	on, err := json.Marshal(devkitClientCapabilities(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "agentFileChangeReport") {
		t.Fatalf("not declared with checkpoints on: %s", on)
	}
	// It belongs inside the vendor's own extension block, beside the other
	// capabilities that gate on it — not at the top level, where the agent
	// never looks.
	var caps struct {
		Meta struct {
			JetBrains struct {
				Air struct {
					Capabilities []string `json:"capabilities"`
				} `json:"air"`
			} `json:"jetbrains"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(on, &caps); err != nil {
		t.Fatal(err)
	}
	if !contains(caps.Meta.JetBrains.Air.Capabilities, "agentFileChangeReport") {
		t.Fatalf("air capabilities = %v", caps.Meta.JetBrains.Air.Capabilities)
	}
	// And asking for it must not drop what was already there.
	for _, want := range []string{"nativeSubagentSessions", "asyncTasks", "sessionFailure"} {
		if !contains(caps.Meta.JetBrains.Air.Capabilities, want) {
			t.Errorf("%s went missing when the report was added", want)
		}
	}
}

// capturingRunner keeps the first frame DevKit sends and then exits, so the
// handshake fails fast and what went out is still readable. One line, not a
// byte count: a byte count would block waiting for frames that never come, and
// the hang TestOpenFailsFastWhenVendorDiesDuringHandshake exists to prevent
// would be back in the test rather than in the code.
type capturingRunner struct {
	path string
	// lines is how many frames to keep before closing. One is enough to see the
	// initialize; a fork needs the call after it too.
	lines int
}

func (r capturingRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	lines := r.lines
	if lines <= 0 {
		lines = 1
	}
	cmd := exec.Command("sh", "-c", `head -n "$1" > "$0"`, r.path, strconv.Itoa(lines))
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

// The gate, on the wire. The unit above pins what the declaration holds; this
// pins that the adapter consults the switch at all — a build that wired
// checkpoints and then asked for the report regardless would turn on the
// vendor's file history for a user who said no.
func TestOpenAsksForTheFileChangeReportOnlyWhenCheckpointsAreOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ClaudeOptions
		want bool
	}{
		{"no hook at all", ClaudeOptions{}, false},
		{"switched off", ClaudeOptions{FileChangeReports: func() bool { return false }}, false},
		{"switched on", ClaudeOptions{FileChangeReports: func() bool { return true }}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "sent.jsonl")
			bin := filepath.Join(dir, "claude-agent-acp")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			opts := tc.opts
			opts.Command = bin
			opts.Runner = capturingRunner{path: capture}
			if _, err := NewClaudeAdapter(opts).Open(context.Background(), OpenSpec{Cwd: t.TempDir()}); err == nil {
				t.Fatal("expected the handshake to fail against a runner that never answers")
			}
			sent, err := os.ReadFile(capture)
			if err != nil {
				t.Fatalf("nothing was sent: %v", err)
			}
			if !strings.Contains(string(sent), "initialize") {
				t.Fatalf("no initialize went out: %s", sent)
			}
			if got := strings.Contains(string(sent), "agentFileChangeReport"); got != tc.want {
				t.Fatalf("declared = %v, want %v: %s", got, tc.want, sent)
			}
		})
	}
}

// scriptedVendor answers the handshake instead of dying in the middle of it.
//
// A runner that only records cannot see past the first frame: the handshake is
// sequential, so the second call is never sent if the first is never answered.
// This one replies per method, which is what lets a test watch a whole sequence
// rather than its opening move.
type scriptedVendor struct{ capture string }

func (v scriptedVendor) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	script := `
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$0"
  id=$(printf '%s' "$line" | sed -n 's/^{"jsonrpc":"2.0","id":\([0-9]*\).*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *'"method":"initialize"'*)   printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id" ;;
    *'"method":"session/fork"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"forked-1"}}\n' "$id" ;;
    *'"method":"session/load"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"forked-1"}}\n' "$id" ;;
    *'"method":"session/new"'*)  printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fresh-1"}}\n' "$id" ;;
    *)                           printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
  esac
done
`
	cmd := exec.Command("sh", "-c", script, v.capture)
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

// sentFrames reads back what the adapter sent, one JSON-RPC frame per line.
func sentFrames(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing was sent: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// Fork is two calls in one process: branch, then load the branch. The wire
// shape is the vendor's own — the message to branch at rides
// `_meta.jetbrains.air.fork`, not a named field — so what goes out is pinned
// here rather than left to the next reader to rediscover from a spike log.
func TestForkSendsInitializeThenSessionFork(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sent.jsonl")
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedVendor{capture: capture}})
	rt, err := adapter.Fork(context.Background(), OpenSpec{Cwd: t.TempDir()}, "src-session", "msg_abc")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	defer rt.Close(context.Background())

	// The branch, not the source: a runtime attached to the session the rewind
	// is leaving behind would answer every later prompt with the history the
	// user just asked to drop.
	if got := rt.ACPSessionID(); got != "forked-1" {
		t.Fatalf("ACPSessionID = %q, want the forked session", got)
	}

	frames := sentFrames(t, capture)
	var methods []string
	for _, frame := range frames {
		var call struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(frame), &call); err == nil && call.Method != "" {
			methods = append(methods, call.Method)
		}
	}
	// Both calls in one process: session/fork does not leave the branch loaded
	// (spike S1), and doing the load from a second process would leave the
	// first one running after a failure.
	if len(methods) != 3 || methods[0] != "initialize" || methods[1] != "session/fork" || methods[2] != "session/load" {
		t.Fatalf("methods = %v, want initialize, session/fork, session/load", methods)
	}

	var fork struct {
		Params struct {
			SessionID  string `json:"sessionId"`
			Cwd        string `json:"cwd"`
			McpServers []any  `json:"mcpServers"`
			Meta       struct {
				SystemPrompt struct {
					Append string `json:"append"`
				} `json:"systemPrompt"`
				ClaudeCode struct {
					Options struct {
						SettingSources []string `json:"settingSources"`
					} `json:"options"`
				} `json:"claudeCode"`
				JetBrains struct {
					Air struct {
						Fork struct {
							Version   int    `json:"version"`
							MessageID string `json:"messageId"`
						} `json:"fork"`
					} `json:"air"`
				} `json:"jetbrains"`
			} `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(frames[1]), &fork); err != nil {
		t.Fatalf("fork frame = %s: %v", frames[1], err)
	}
	if fork.Params.SessionID != "src-session" {
		t.Errorf("sessionId = %q, want the source session", fork.Params.SessionID)
	}
	if fork.Params.Cwd == "" || fork.Params.McpServers == nil {
		t.Errorf("params = %+v, want cwd and a non-nil mcpServers", fork.Params)
	}
	// The branch point rides `_meta.jetbrains.air.fork`, not a named field.
	if got := fork.Params.Meta.JetBrains.Air.Fork; got.Version != 1 || got.MessageID != "msg_abc" {
		t.Errorf("fork meta = %+v, want version 1 at msg_abc", got)
	}
	// Forks inherit the same customization sources as a fresh session.
	if sources := fork.Params.Meta.ClaudeCode.Options.SettingSources; !slices.Equal(sources, []string{"user", "project", "local"}) {
		t.Errorf("settingSources = %v, want user, project and local", sources)
	}
	if fork.Params.Meta.SystemPrompt.Append != devkitPrompt(claudeNaming, "") {
		t.Error("fork did not carry DevKit's prompt")
	}

	// And the load names the branch, not the source.
	var load struct {
		Params struct {
			SessionID string `json:"sessionId"`
			Meta      struct {
				SystemPrompt struct {
					Append string `json:"append"`
				} `json:"systemPrompt"`
			} `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(frames[2]), &load); err != nil {
		t.Fatal(err)
	}
	if load.Params.SessionID != "forked-1" {
		t.Errorf("session/load asked for %q, want the branch", load.Params.SessionID)
	}
	if load.Params.Meta.SystemPrompt.Append != devkitPrompt(claudeNaming, "") {
		t.Error("session/load did not carry DevKit's prompt")
	}
}

// A vendor that answers a fork with the source's own id has branched nothing.
// Loading it would attach the session to the conversation the rewind is trying
// to leave behind, which is the one outcome worse than the fork failing.
func TestForkRefusesAnEchoedSourceID(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The scripted vendor always answers "forked-1"; ask it to fork that.
	adapter := NewClaudeAdapter(ClaudeOptions{
		Command: bin,
		Runner:  scriptedVendor{capture: filepath.Join(dir, "sent.jsonl")},
	})
	_, err := adapter.Fork(context.Background(), OpenSpec{Cwd: t.TempDir()}, "forked-1", "msg_abc")
	if !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want ErrProcessFailed", err)
	}
}

// Fork needs both halves. Neither is something a caller can be trusted to have
// worked out: a missing message id would branch at the vendor's default, which
// is not the point the user picked.
func TestForkRefusesMissingArguments(t *testing.T) {
	adapter := NewClaudeAdapter(ClaudeOptions{})
	if _, err := adapter.Fork(context.Background(), OpenSpec{}, "", "msg"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("no session = %v, want ErrSessionNotFound", err)
	}
	if _, err := adapter.Fork(context.Background(), OpenSpec{}, "src", ""); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("no message = %v, want ErrSessionNotFound", err)
	}
}
