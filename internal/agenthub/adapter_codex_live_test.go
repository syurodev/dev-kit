package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/sandbox"
)

// codexForReal is the real codex binary under the real jail, or a skip.
// DEVKIT_CODEX_COMMAND names the binary (the native one, not the npm
// launcher): the managed install's vendor/<triple>/bin/codex.
func codexForReal(t *testing.T, home string) *CodexAdapter {
	t.Helper()
	bin := os.Getenv("DEVKIT_CODEX_COMMAND")
	if bin == "" {
		t.Skip("set DEVKIT_CODEX_COMMAND to a codex 0.147.0 binary to run codex in the real jail")
	}
	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	return NewCodexAdapter(CodexOptions{Command: bin, Home: home, Version: "test"})
}

// The real binary in the real jail, signed out: the install verifies, the
// status reads, and a session is refused as a sign-in to offer — not as a
// crash. Uses no quota and no network beyond what codex does on start.
func TestCodexRealBinaryInTheJail(t *testing.T) {
	home := filepath.Join(t.TempDir(), "agent-homes", "codex")
	a := codexForReal(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	l, err := a.resolveCommand()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Probe(ctx, agentruntime.Resolved{Command: l.path, ReadOnlyPaths: l.ro}); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	status, err := a.SignInStatus(ctx)
	if err != nil || status.SignedIn {
		t.Fatalf("status of a fresh home = %+v, %v", status, err)
	}
	// With an MCP server on the command line: codex parses every -c before
	// it answers initialize, so a value it could not read would fail here as
	// a dead process, not as a sign-in.
	spec := OpenSpec{Cwd: t.TempDir(), McpServers: []acp.McpServerStdio{{
		Name: "devkit", Command: "/bin/cat", Args: []string{"--socket", "/tmp/a b.sock"}, Env: []acp.EnvVariable{{Name: "X", Value: `q"uote`}},
	}}}
	if _, err := a.Open(ctx, spec); !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("Open signed out = %v, want ErrAgentAuthRequired", err)
	}
	// Codex wrote its state into the home, and not into the policy files.
	if _, err := os.Stat(filepath.Join(home, "installation_id")); err != nil {
		t.Errorf("codex did not use DevKit's home: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, "config.toml")); string(got) != codexConfigHeader {
		t.Errorf("config.toml = %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, "skills")); len(entries) != 0 {
		t.Errorf("skills/ was written: %v", entries)
	}
}

// One real turn with one real approval (spec §9, Giai đoạn 4). Needs a
// ChatGPT sign-in in a scratch CODEX_HOME — never ~/.codex, never a copy of
// its auth.json — and spends quota, so it runs only when asked:
//
//	DEVKIT_CODEX_LIVE=1 DEVKIT_CODEX_LIVE_HOME=<signed-in scratch home> \
//	DEVKIT_CODEX_COMMAND=<codex binary> go test ./internal/agenthub/ -run CodexLive -v
func TestCodexLiveTurnWithAnApproval(t *testing.T) {
	if os.Getenv("DEVKIT_CODEX_LIVE") != "1" {
		t.Skip("set DEVKIT_CODEX_LIVE=1 (spends Codex quota)")
	}
	home := os.Getenv("DEVKIT_CODEX_LIVE_HOME")
	if home == "" {
		t.Fatal("DEVKIT_CODEX_LIVE_HOME must name a signed-in scratch CODEX_HOME")
	}
	a := codexForReal(t, home)
	project := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rt, err := a.Open(ctx, OpenSpec{Cwd: project})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close(context.Background())
	var cards []json.RawMessage
	var transcript updateLog
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: transcript.add,
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			cards = append(cards, params)
			return selected(codexOptionAllow), nil
		},
	})
	out, err := rt.Prompt(ctx, TextPrompt(`Create a file named done.txt in the current directory containing exactly "ok". Do nothing else.`), nil)
	if err != nil {
		t.Fatalf("Prompt: %v\n%s", err, transcript.joined())
	}
	t.Logf("stop %s, %d approval(s)\n%s", out.StopReason, len(cards), transcript.joined())
	if len(cards) == 0 {
		t.Fatal("Ask must ask before writing a file")
	}
	if got, _ := os.ReadFile(filepath.Join(project, "done.txt")); strings.TrimSpace(string(got)) != "ok" {
		t.Fatalf("done.txt = %q", got)
	}
}
