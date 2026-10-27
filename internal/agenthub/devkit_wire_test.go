package agenthub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/codexapp"
)

// These read what each vendor's adapter actually puts on its wire (or into its
// config) for a prepared launch that hides some DevKit tools: not only the
// OpenSpec the service builds. The vendors are scripted; nothing is billed.

// hiddenSpec is a launch whose endpoint serves vault.status and the read-only
// agenthub tools but neither git nor pdf; its permission lists were narrowed to
// that by the endpoint, as OpenWithPolicy does.
func hiddenSpec(cwd string) OpenSpec {
	return OpenSpec{
		Cwd:            cwd,
		Brief:          "Working directory: " + cwd,
		DevKit:         NewDevKitServing([]string{"vault.status", "agenthub.listPeers", "agenthub.readSession", "tasks.get"}),
		McpServers:     []acp.McpServerStdio{{Name: "devkit", Command: "/jail/devkit-mcp"}},
		GateOwnedMCP:   map[string][]string{"devkit": {"tasks.get"}},
		PreApprovedMCP: map[string][]string{"devkit": {"agenthub.listPeers", "agenthub.readSession"}},
	}
}

var hiddenGroups = []string{"git", "pdf"}

func hiddenToolsOf(t *testing.T) []string {
	t.Helper()
	var out []string
	for n := range mcpToolNames(t) {
		for _, g := range hiddenGroups {
			if groupOf(n) == g {
				out = append(out, n)
			}
		}
	}
	return out
}

func assertNoHidden(t *testing.T, where, text string, naming toolNaming) {
	t.Helper()
	for _, tool := range hiddenToolsOf(t) {
		if strings.Contains(text, naming.tool(tool)) {
			t.Errorf("%s names the hidden tool %s", where, tool)
		}
		raw := "mcp__devkit__" + strings.ReplaceAll(tool, ".", "_")
		if strings.Contains(text, "\""+raw+"\"") {
			t.Errorf("%s delegates the hidden tool %s", where, tool)
		}
	}
}

func TestClaudeSessionNewCarriesGuidanceAndDelegationOfTheServedToolsOnly(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sent.jsonl")
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedVendor{capture: capture}})
	spec := hiddenSpec(t.TempDir())
	rt, err := adapter.Open(context.Background(), spec)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close(context.Background())

	var frame string
	for _, f := range sentFrames(t, capture) {
		if strings.Contains(f, `"method":"session/new"`) {
			frame = f
		}
	}
	if frame == "" {
		t.Fatal("no session/new was sent")
	}
	var wire struct {
		Params struct {
			Meta struct {
				SystemPrompt struct {
					Append string `json:"append"`
				} `json:"systemPrompt"`
				ClaudeCode struct {
					Options struct {
						AllowedTools []string `json:"allowedTools"`
					} `json:"options"`
				} `json:"claudeCode"`
			} `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(frame), &wire); err != nil {
		t.Fatal(err)
	}
	if got, want := wire.Params.Meta.SystemPrompt.Append, devkitPrompt(claudeNaming, spec.Brief, spec.DevKit); got != want {
		t.Fatal("session/new did not carry the prompt rendered for the prepared policy")
	}
	assertNoHidden(t, "Claude's prompt", wire.Params.Meta.SystemPrompt.Append, claudeNaming)
	want := []string{"mcp__devkit__tasks_get", "mcp__devkit__agenthub_listPeers", "mcp__devkit__agenthub_readSession"}
	got := wire.Params.Meta.ClaudeCode.Options.AllowedTools
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("allowedTools = %v, want %v", got, want)
	}
	assertNoHidden(t, "Claude's allowedTools", frame, claudeNaming)
	// Legacy is unchanged: the same adapter given no served value sends the full prompt.
	spec.DevKit = nil
	if devkitPrompt(claudeNaming, spec.Brief, spec.DevKit) == wire.Params.Meta.SystemPrompt.Append {
		t.Fatal("a prepared and a legacy launch rendered the same prompt")
	}
}

func TestCodexThreadStartCarriesGuidanceForThePreparedPolicy(t *testing.T) {
	f := newCodexFixture(t, "turn")
	spec := hiddenSpec(f.cwd)
	rt, err := f.adapter.Open(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	start := f.runner.sent(t, "thread/start")
	if len(start) != 1 {
		t.Fatalf("thread/start sent %d times", len(start))
	}
	var ts codexapp.ThreadStartParams
	if err := json.Unmarshal(start[0], &ts); err != nil {
		t.Fatal(err)
	}
	if ts.DeveloperInstructions != devkitPrompt(codexNaming, spec.Brief, spec.DevKit) {
		t.Fatal("thread/start did not carry the prompt rendered for the prepared policy")
	}
	assertNoHidden(t, "Codex's developer instructions", ts.DeveloperInstructions, codexNaming)
	if strings.Contains(ts.DeveloperInstructions, "mcp__devkit__git_status") {
		t.Error("the Codex note exemplifies a hidden tool")
	}
	assertNoHidden(t, "thread/start", string(start[0]), codexNaming)

	// Resume carries the same text for the same prepared value.
	loaded, err := f.adapter.Load(context.Background(), spec, "th-old")
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close(context.Background())
	resume := f.runner.sent(t, "thread/resume")
	var p codexapp.ThreadResumeParams
	if len(resume) != 1 || json.Unmarshal(resume[0], &p) != nil || p.DeveloperInstructions != ts.DeveloperInstructions {
		t.Fatalf("thread/resume = %s", resume)
	}
}

func TestOpenCodeInstructionsFileIsRenderedForThePreparedPolicy(t *testing.T) {
	a, home := newOpenCodeTestAdapter(t, nil)
	spec := hiddenSpec(t.TempDir())
	pol, _, err := a.buildSandbox(spec)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(pol.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instructions) != 1 || filepath.Dir(cfg.Instructions[0]) != filepath.Join(home, "prompts") {
		t.Fatalf("instructions = %v", cfg.Instructions)
	}
	text, err := os.ReadFile(cfg.Instructions[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != devkitPrompt(opencodeNaming, spec.Brief, spec.DevKit) {
		t.Fatal("the instructions file is not the prompt rendered for the prepared policy")
	}
	assertNoHidden(t, "opencode's instructions", string(text), opencodeNaming)
	if strings.Contains(string(text), "tools.devkit.git_status") {
		t.Error("the opencode note exemplifies a hidden tool")
	}
	if strings.Contains(pol.Env["OPENCODE_CONFIG_CONTENT"], "git_") || strings.Contains(pol.Env["OPENCODE_CONFIG_CONTENT"], "pdf_") {
		t.Errorf("opencode's config names a hidden tool: %s", pol.Env["OPENCODE_CONFIG_CONTENT"])
	}
}

// The same documents and peer text that the model reads in a turn.
func TestTurnTextDoesNotAdvertiseHiddenTools(t *testing.T) {
	served := NewDevKitServing([]string{"vault.status"})
	guide := pdfToolsGuide(claudeNaming, served)
	if strings.Contains(guide, "pdf_") || !strings.Contains(guide, "tell the person") {
		t.Errorf("PDF guide with the PDF tools hidden: %s", guide)
	}
	if !strings.Contains(pdfToolsGuide(claudeNaming, nil), "mcp__devkit__pdf_readText") {
		t.Error("the legacy PDF guide lost its tools")
	}
	msg := &peerMessage{id: "11111111-2222-3333-4444-555555555555", from: MessageSource{Kind: "session", SessionID: "sess", Name: "x", AgentType: AgentTypeClaude, Hop: 1}, text: "hello"}
	hidden := peerEnvelope(claudeNaming, served, msg)
	if strings.Contains(hidden, "agenthub_sendMessage") || !strings.Contains(hidden, "not available in this session") {
		t.Errorf("peer envelope advertises a hidden reply tool:\n%s", hidden)
	}
	if !strings.Contains(peerEnvelope(claudeNaming, nil, msg), "agenthub_sendMessage") {
		t.Error("the legacy peer envelope lost its reply tool")
	}
}
