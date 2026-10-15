package agenthub

import (
	"bufio"
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
	"devkit/internal/preferences"
	"devkit/internal/sandbox"
)

// codexForReal is the real codex binary under the real jail, or a skip.
// DEVKIT_CODEX_COMMAND names the binary (the native one, not the npm
// launcher): the managed install's vendor/<triple>/bin/codex.
func codexForReal(t *testing.T, home string) *CodexAdapter {
	t.Helper()
	bin := os.Getenv("DEVKIT_CODEX_COMMAND")
	if bin == "" {
		t.Skip("set DEVKIT_CODEX_COMMAND to a native codex binary to run codex in the real jail")
	}
	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	return NewCodexAdapter(CodexOptions{Command: bin, Home: home, Version: "test"})
}

// Skill discovery needs no signed-in account or model call. Exercise the
// actual runtime and jail so linking a skill is not mistaken for loading it.
func TestCodexRealBinaryDiscoversInheritedSkills(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	skill := filepath.Join(host, ".codex", "skills", "devkit-test-skill")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: devkit-test-skill\ndescription: A test skill inherited from the user environment\n---\nUse the test workflow.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := codexForReal(t, filepath.Join(t.TempDir(), "managed"))
	l, err := a.resolveCommand()
	if err != nil {
		t.Fatal(err)
	}
	shared := false
	cwd := t.TempDir()
	p, err := a.start(cwd, l, OpenSpec{PrivateHome: &shared}, codexStoreFile, codexNoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.initialize(ctx, p); err != nil {
		t.Fatal(err)
	}
	var result json.RawMessage
	if err := p.conn.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}}, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), `"name":"devkit-test-skill"`) {
		t.Fatal("runtime did not discover the inherited user skill")
	}
	rt := &codexRuntime{conn: p.conn, environment: p.environment}
	report, err := rt.reportEnvironment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if entry := findEnvironment(t, report, "skills", "devkit-test-skill"); entry.Status != "loaded" {
		t.Fatalf("live skill status = %s", entry.Status)
	}

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
	// The runtime may install bundled system skills in its writable skills/.
}

// Reloading configuration must work inside the jail, even when signed out:
// newer app-servers also reload it from account/read for ChatGPT accounts.
func TestCodexRealBinaryConfigReloadInTheJail(t *testing.T) {
	a := codexForReal(t, filepath.Join(t.TempDir(), "home"))
	l, err := a.resolveCommand()
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.start(t.TempDir(), l, OpenSpec{}, codexStoreEphemeral, codexNoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()
	ctx, cancel := p.bounded(context.Background(), probeTimeout)
	defer cancel()
	if err := a.initialize(ctx, p); err != nil {
		t.Fatal(err)
	}
	var result json.RawMessage
	if err := p.conn.Call(ctx, "config/read", map[string]any{"includeLayers": false}, &result); err != nil {
		t.Fatalf("config reload: %v", err)
	}
}

func TestCodexRealBinaryCatalogBeforeConversation(t *testing.T) {
	a := codexForReal(t, filepath.Join(t.TempDir(), "home"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	opts, err := a.ConfigCatalog(ctx, OpenSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) == 0 || len(opts[0].Options) == 0 {
		t.Fatal("catalog has no models")
	}
	for _, choice := range opts[0].Options {
		if len(choice.SupportedEfforts) == 0 {
			t.Fatalf("model %s lost supported efforts", choice.Value)
		}
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

// TestCodexFakeMcpServer is not a test: it is a one-tool MCP server over
// stdio, this test binary run again with DEVKIT_FAKE_MCP set. The tool
// declares no annotations, so Codex treats it as able to write and asks
// before each call under Ask.
func TestCodexFakeMcpServer(t *testing.T) {
	if os.Getenv("DEVKIT_FAKE_MCP") == "" {
		t.Skip("helper process for the live MCP approval test")
	}
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			os.Exit(0)
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &m) != nil || len(m.ID) == 0 {
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(m.Params, &p)
			result = map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "probe", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "mark", "description": "Records a mark. Call it when asked to.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "marked"}}}
		default:
			result = map[string]any{}
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
}

// Two things only a real turn can settle (handoff, "Chưa đo"): Codex's MCP
// tool approval arrives as the elicitation DevKit shows as a card, and a
// resumed thread's first turn is billed for itself, not for the thread.
// Spends quota; same env as TestCodexLiveTurnWithAnApproval.
func TestCodexLiveMcpApprovalAndResume(t *testing.T) {
	if os.Getenv("DEVKIT_CODEX_LIVE") != "1" {
		t.Skip("set DEVKIT_CODEX_LIVE=1 (spends Codex quota)")
	}
	home := os.Getenv("DEVKIT_CODEX_LIVE_HOME")
	if home == "" {
		t.Fatal("DEVKIT_CODEX_LIVE_HOME must name a signed-in scratch CODEX_HOME")
	}
	a := codexForReal(t, home)
	self, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	spec := OpenSpec{
		Cwd:           t.TempDir(),
		ReadOnlyPaths: []string{filepath.Dir(self)},
		McpServers: []acp.McpServerStdio{{Name: "probe", Command: self, Args: []string{"-test.run=^TestCodexFakeMcpServer$"},
			Env: []acp.EnvVariable{{Name: "DEVKIT_FAKE_MCP", Value: "1"}}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rt, err := a.Open(ctx, spec)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var cards []json.RawMessage
	var transcript updateLog
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: transcript.add,
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			cards = append(cards, params)
			return selected(codexOptionAllow), nil
		},
	})
	first, err := rt.Prompt(ctx, TextPrompt("Call the `mark` tool of the `probe` MCP server exactly once, then reply with the word done."), nil)
	if err != nil {
		t.Fatalf("Prompt: %v\n%s", err, transcript.joined())
	}
	for _, c := range cards {
		t.Logf("card: %s", c)
	}
	if len(cards) == 0 || !strings.Contains(transcript.joined(), `"title":"probe.mark"`) {
		t.Fatalf("no MCP approval card for probe.mark:\n%s", transcript.joined())
	}
	if !strings.Contains(transcript.joined(), "marked") {
		t.Fatalf("the approved call did not run:\n%s", transcript.joined())
	}
	firstUsage, _ := a.NormalizeUsage(first.Meta)
	thread := rt.ACPSessionID()
	_ = rt.Close(context.Background())

	resumed, err := a.Load(ctx, spec, thread)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer resumed.Close(context.Background())
	var second updateLog
	resumed.SetHandlers(RuntimeHandlers{OnUpdate: second.add})
	out, err := resumed.Prompt(ctx, TextPrompt("Reply with the single word ok."), nil)
	if err != nil {
		t.Fatalf("Prompt after resume: %v", err)
	}
	usage, _ := a.NormalizeUsage(out.Meta)
	t.Logf("first turn %+v (meta %s)\nresumed turn %+v (meta %s)\nresumed updates:\n%s", firstUsage, first.Meta, usage, out.Meta, second.joined())
	// A turn billed with the whole thread would carry the first turn's output
	// as well; one word should cost far less than the first turn did.
	if usage.OutputTokens <= 0 || usage.OutputTokens >= firstUsage.OutputTokens+usage.InputTokens || usage.InputTokens <= 0 {
		t.Fatalf("the resumed turn's usage looks wrong: %+v", usage)
	}
}

func TestCodexRealBinaryManagedSkillEnablement(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	folder := customizationSkill(t, "devkit-managed-test")
	a := codexForReal(t, filepath.Join(t.TempDir(), "managed"))
	value := preferences.AgentCustomization{SkillFolders: []string{folder}}
	for _, disabled := range []bool{false, true, false} {
		if disabled {
			value.DisabledSkills = []string{filepath.Join(folder, "SKILL.md")}
		} else {
			value.DisabledSkills = nil
		}
		l, err := a.resolveCommand()
		if err != nil {
			t.Fatal(err)
		}
		cwd := t.TempDir()
		p, err := a.start(cwd, l, OpenSpec{Customizations: map[string]preferences.AgentCustomization{"codex": value}}, codexStoreFile, codexNoHandler{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := a.initialize(ctx, p); err != nil {
			cancel()
			p.stop()
			t.Fatal(err)
		}
		var result struct {
			Data []struct {
				Skills []struct {
					Name    string `json:"name"`
					Path    string `json:"path"`
					Enabled bool   `json:"enabled"`
				} `json:"skills"`
			} `json:"data"`
		}
		err = p.conn.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}}, &result)
		cancel()
		p.stop()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, group := range result.Data {
			for _, skill := range group.Skills {
				if skill.Name == "devkit-managed-test" {
					found = true
					if skill.Enabled == disabled {
						t.Fatalf("enabled=%v, disabled=%v, skill=%+v", skill.Enabled, disabled, skill)
					}
				}
			}
		}
		if !found {
			t.Fatal("managed skill not discovered")
		}
	}
}
