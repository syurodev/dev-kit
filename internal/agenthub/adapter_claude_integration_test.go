package agenthub_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
)

// Integration smoke: live Claude ACP binary inside sandbox.StartPiped.
// Skip unless DEVKIT_CLAUDE_ACP=1 and an absolute binary (or AllowNpx) is available.
func TestClaudeACPIntegration(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_ACP") != "1" {
		skipOrFail(t, "set DEVKIT_CLAUDE_ACP=1 to run Claude ACP integration")
	}

	a := agenthub.NewClaudeAdapter(claudeIntegrationOptions(t))
	proj := t.TempDir()
	rt, err := a.Open(context.Background(), agenthub.OpenSpec{Cwd: proj})
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

// TestClaudeACPMetaChannelStillWorks guards the one thing DevKit's agent
// isolation rests on and cannot see: session/new._meta.claudeCode.options.
//
// That field is claude-agent-acp's own extension, not part of ACP. Two things
// ride it — settingSources: [] (so a user's ~/.claude/settings.json cannot
// hand the agent allow rules that skip DevKit's permission gate) and the
// PreToolUse hook (so a tool cannot read the credentials the jail had to bind
// read-write). If a future release of that package renames or drops the field,
// both revert silently: the agent keeps working, the isolation is simply gone.
//
// agent-hub.mdx calls this out as the risk it would not accept — "core
// dependency chạm tới execute-permission, rủi ro bus-factor của 1 package chưa
// ai official-support" — and DevKit honoured it on the client side by writing
// internal/acp by hand. The agent side is still that package, so the guard has
// to be a test that runs it.
//
// One assertion covers both: the hook is installed through the same channel as
// settingSources, so a hook that never fires means the channel is gone.
//
//	DEVKIT_CLAUDE_ACP=1 go test ./internal/agenthub/ -run TestClaudeACPMetaChannel -v -timeout 10m
func TestClaudeACPMetaChannelStillWorks(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_ACP") != "1" {
		skipOrFail(t, "set DEVKIT_CLAUDE_ACP=1 to run Claude ACP integration")
	}

	// The adapter reads the hook's path off the devkit MCP server entry, which
	// is how SessionHost supplies it in production. Building the real binary
	// keeps the test on that path instead of a stand-in that could pass while
	// the shipped one does not.
	hookBin := filepath.Join(t.TempDir(), "devkit-mcp")
	build := exec.Command("go", "build", "-o", hookBin, "./cmd/devkit-mcp")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build devkit-mcp: %v\n%s", err, out)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		skipOrFail(t, "no home directory: %v", err)
	}

	a := agenthub.NewClaudeAdapter(claudeIntegrationOptions(t))
	rt, err := a.Open(context.Background(), agenthub.OpenSpec{
		Cwd:           t.TempDir(),
		ReadOnlyPaths: []string{hookBin},
		McpServers: []acp.McpServerStdio{{
			Name:    "devkit",
			Command: hookBin,
			Args:    []string{"--socket", filepath.Join(t.TempDir(), "absent.sock")},
			Env:     []acp.EnvVariable{},
		}},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	var mu sync.Mutex
	var seen []string
	rt.SetHandlers(agenthub.RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
			mu.Lock()
			seen = append(seen, string(update))
			mu.Unlock()
		},
		// Allow everything the gate is asked about: this test is about the
		// calls that never reach the gate, and a denial would end the turn
		// before the hook is exercised.
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			return allowOutcome(params), nil
		},
	})

	// A path under the protected prefix that does not exist. The hook decides
	// on the path alone, before anything is read, so no real file is touched —
	// and a name that reads as boring keeps the model from refusing on its own
	// before it calls the tool, which is what happens if you ask it outright
	// for .credentials.json.
	probe := filepath.Join(home, ".claude", "devkit-hook-probe-does-not-exist.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := rt.Prompt(ctx, "Use the Read tool on "+probe+
		" and report in one line exactly what happened. Do not retry."); err != nil {
		t.Logf("Prompt returned %v (the assertion below is what matters)", err)
	}

	mu.Lock()
	defer mu.Unlock()

	toolCalls := 0
	for _, update := range seen {
		if strings.Contains(update, agenthub.HookDenyReason) {
			return
		}
		if strings.Contains(update, `"sessionUpdate":"tool_call"`) {
			toolCalls++
		}
	}

	// Two very different failures share this line, and CI has to tell them
	// apart: an agent that never called a tool proves nothing about the hook —
	// it usually means auth or the sandbox, not a regression upstream.
	if toolCalls == 0 {
		t.Fatalf("the agent made no tool call at all, so the hook was never reached: "+
			"check credentials and the sandbox before suspecting the _meta channel. "+
			"Updates seen: %d", len(seen))
	}
	t.Fatalf("the agent called %d tool(s) and the PreToolUse hook denied none of them — "+
		"session/new._meta.claudeCode.options no longer installs it, so settingSources "+
		"is probably ignored too and the agent is running with the user's own permission "+
		"rules. Probe path: %s", toolCalls, probe)
}

// repoRoot walks up from the test's working directory to the module root, so
// `go build ./cmd/devkit-mcp` runs where go.mod is.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test's working directory")
		}
		dir = parent
	}
}

// claudeIntegrationOptions resolves the agent the same way main.go does:
// an installed claude-agent-acp first, npx only as a last resort.
//
// The old default here was npx, which cannot work — main.go has the
// measurement: "npx needs a writable ~/.npm and the whole npm lib tree,
// neither of which exists inside the jail (npx dies on EPERM before it
// downloads anything)". So running this suite with DEVKIT_CLAUDE_ACP=1 and no
// DEVKIT_CLAUDE_ACP_BIN failed on a handshake that never had a chance, which
// reads as a broken adapter rather than a missing binary.
func claudeIntegrationOptions(t *testing.T) agenthub.ClaudeOptions {
	t.Helper()
	if bin := os.Getenv("DEVKIT_CLAUDE_ACP_BIN"); bin != "" {
		if !filepath.IsAbs(bin) {
			t.Fatalf("DEVKIT_CLAUDE_ACP_BIN must be absolute, got %q", bin)
		}
		return agenthub.ClaudeOptions{Command: bin}
	}
	if abs, err := exec.LookPath("claude-agent-acp"); err == nil {
		return agenthub.ClaudeOptions{Command: abs}
	}
	skipOrFail(t, "claude-agent-acp not on PATH and DEVKIT_CLAUDE_ACP_BIN unset; "+
		"install it with: npm i -g @agentclientprotocol/claude-agent-acp")
	return agenthub.ClaudeOptions{}
}

// skipOrFail skips locally and fails under DEVKIT_CLAUDE_ACP_REQUIRED=1.
//
// These tests skip themselves when the agent, the credentials or the platform
// are missing, which is right on a laptop and wrong in CI: a guard that skips
// is a green tick protecting nothing, and the whole reason this suite exists is
// that the thing it guards fails silently. CI sets the variable so a missing
// prerequisite is reported as the failure it is.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("DEVKIT_CLAUDE_ACP_REQUIRED") == "1" {
		t.Fatalf("required by DEVKIT_CLAUDE_ACP_REQUIRED=1: "+format, args...)
	}
	t.Skipf(format, args...)
}

// TestClaudeACPResumeAcrossProcesses is the only thing that proves session/load
// resumes rather than merely returning without an error.
//
// service.go carried the symptom in a comment since Phase 4: "after a process
// restart, store rows may outlive in-memory runtimes (orphan sessions).
// Invoke/Cancel return ErrUnavailable for those". Every restart ended every
// open conversation, and the only way forward was a new session that had never
// heard any of it.
//
//	DEVKIT_CLAUDE_ACP=1 go test ./internal/agenthub/ -run TestClaudeACPResume -v -timeout 10m
func TestClaudeACPResumeAcrossProcesses(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_ACP") != "1" {
		skipOrFail(t, "set DEVKIT_CLAUDE_ACP=1 to run Claude ACP integration")
	}

	a := agenthub.NewClaudeAdapter(claudeIntegrationOptions(t))
	loader, ok := any(a).(agenthub.SessionLoader)
	if !ok {
		t.Fatal("ClaudeAdapter no longer satisfies SessionLoader")
	}

	proj := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	rt, err := a.Open(ctx, agenthub.OpenSpec{Cwd: proj})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	acpID := rt.ACPSessionID()
	// A codeword rather than a fact the model could answer from training: the
	// question is whether the earlier turn came back, not whether it can guess.
	if _, err := rt.Prompt(ctx, "Remember this codeword: PANGOLIN-47. Just say OK."); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	_ = rt.Close(ctx) // what a restart does to the process

	resumed, err := loader.Load(ctx, agenthub.OpenSpec{Cwd: proj}, acpID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = resumed.Close(context.Background()) }()

	var mu sync.Mutex
	var said []string
	resumed.SetHandlers(agenthub.RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
			var wire struct {
				Kind    string `json:"sessionUpdate"`
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(update, &wire) == nil && wire.Kind == "agent_message_chunk" {
				mu.Lock()
				said = append(said, wire.Content.Text)
				mu.Unlock()
			}
		},
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			return allowOutcome(params), nil
		},
	})

	if _, err := resumed.Prompt(ctx, "What codeword did I ask you to remember? Answer with just the word."); err != nil {
		t.Fatalf("prompt after resume: %v", err)
	}

	mu.Lock()
	answer := strings.Join(said, "")
	mu.Unlock()
	if !strings.Contains(answer, "PANGOLIN-47") {
		t.Fatalf("context did not survive the resume; agent said %q", answer)
	}
}
