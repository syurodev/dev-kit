package agenthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"devkit/internal/acp"
)

// VendorQuirks describes what a vendor adapter supports and how to drive it
// for the conformance suite. Unsupported capabilities are skipped with a
// stated reason.
type VendorQuirks struct {
	Name      string
	Supports  CapabilitySet
	Open      func(t *testing.T, script []string) (SessionRuntime, error)
	Load      func(t *testing.T, acpSessionID string) (SessionRuntime, error)
	Normalize func(meta json.RawMessage) (NormalizedUsage, error)
	// SetModeIDs are the mode IDs the vendor accepts. Empty = skip.
	SetModeIDs []string
	// SetConfigOptionIDs are the option IDs the vendor accepts. Empty = skip.
	SetConfigOptionIDs []string
	// SetConfigOptionValues are valid values aligned with SetConfigOptionIDs.
	SetConfigOptionValues []string
	ExpectedStream        string
	ExpectedUsage         NormalizedUsage
	ExpectedUpdateUsage   *Usage
	ExpectedRefusal       string
	wireLogPath           func() string
}

// CapabilitySet flags which conformance cases a vendor can exercise.
type CapabilitySet struct {
	LoadResume       bool // has SessionLoader
	Elicitation      bool // handles elicitation/create
	ProcessDeath     bool // RuntimeLiveness.Dead reports a death
	SetMode          bool // RuntimeConfigurator.SetMode
	SetConfigOption  bool // RuntimeConfigurator.SetConfigOption
	StreamingUpdates bool // OnUpdate receives agent_message_chunk
	Permission       bool // OnPermission is invoked
	VendorRefusal    bool // can produce a vendor refusal (ErrTurnFailed with vendor sentence)
	Cancel           bool // the test wire can hold a turn until Cancel
	UsageUpdate      bool // the test wire emits a usage-bearing update
}

// MockQuirks returns quirks for the in-process mock adapter.
func MockQuirks() VendorQuirks {
	return VendorQuirks{
		Name:     "mock",
		Supports: CapabilitySet{LoadResume: false, Elicitation: false, ProcessDeath: false, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: true, Cancel: true, UsageUpdate: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			a := NewMockAdapter()
			return a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
		},
		Load: nil, // mock has no SessionLoader
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			return (&MockAdapter{}).NormalizeUsage(meta)
		},
		SetModeIDs:         []string{"ask", "auto", "bypass"},
		SetConfigOptionIDs: []string{"model", "effort"},
		ExpectedStream:     "mock reply", ExpectedUsage: NormalizedUsage{InputTokens: 1, OutputTokens: 1, CostUSD: 0.001},
		ExpectedRefusal: "Upstream request failed: Endpoint is unavailable.",
	}
}

// ClaudeQuirks uses the existing scriptedVendor fake and its ACP runtime.
func ClaudeQuirks() VendorQuirks {
	wireLog := ""
	return VendorQuirks{
		Name:     "claude",
		Supports: CapabilitySet{LoadResume: true, Elicitation: true, ProcessDeath: true, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: true, Cancel: true, UsageUpdate: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			scenario := ""
			if len(script) > 0 {
				switch script[0] {
				case "NEED_PERMISSION":
					scenario = "permission"
				case "ELICITATION":
					scenario = "elicitation"
				case "HOLD_AFTER":
					scenario = "cancel"
				case "die":
					scenario = "die"
				case "FAIL_PROVIDER_503":
					scenario = "refusal"
				}
			}
			runner := scriptedVendor{capture: filepath.Join(t.TempDir(), "claude-wire.jsonl"), scenario: scenario}
			a := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: runner})
			wireLog = runner.capture
			return a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
		},
		Load: func(t *testing.T, acpSessionID string) (SessionRuntime, error) {
			runner := scriptedVendor{capture: filepath.Join(t.TempDir(), "claude-wire.jsonl")}
			a := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: runner})
			return a.Load(context.Background(), OpenSpec{Cwd: t.TempDir()}, acpSessionID)
		},
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			return NewClaudeAdapter(ClaudeOptions{}).NormalizeUsage(meta)
		},
		ExpectedStream: "ok", ExpectedUsage: NormalizedUsage{InputTokens: 12, OutputTokens: 4},
		ExpectedRefusal:    "The provider is unavailable: 503",
		SetModeIDs:         []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs: []string{"model", "effort"},
		wireLogPath:        func() string { return wireLog },
	}
}

// OpenCodeQuirks returns quirks for the opencode adapter via recordingRunner.
func OpenCodeQuirks() VendorQuirks {
	wireLog := ""
	return VendorQuirks{
		Name:     "opencode",
		Supports: CapabilitySet{LoadResume: true, Elicitation: true, ProcessDeath: false, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: true, Cancel: true, UsageUpdate: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			scenario := "turn"
			if len(script) > 0 {
				scenario = script[0]
			}
			logFile := filepath.Join(t.TempDir(), "requests.jsonl")
			wireLog = logFile
			a, _ := newOpenCodeTestAdapter(t, openCodeConformanceRunner{scenario: scenario, log: logFile})
			sharedHome := false
			rt, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir(), PrivateHome: &sharedHome})
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Load: func(t *testing.T, acpSessionID string) (SessionRuntime, error) {
			logFile := filepath.Join(t.TempDir(), "requests.jsonl")
			wireLog = logFile
			a, _ := newOpenCodeTestAdapter(t, openCodeConformanceRunner{scenario: "turn", log: logFile})
			sharedHome := false
			rt, err := a.Load(context.Background(), OpenSpec{Cwd: t.TempDir(), PrivateHome: &sharedHome}, acpSessionID)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
		},
		ExpectedStream: "ok", ExpectedUpdateUsage: &Usage{Used: 10, Size: 100},
		ExpectedRefusal:    "The provider is unavailable: 503",
		SetModeIDs:         []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs: []string{"model", "effort"},
		wireLogPath:        func() string { return wireLog },
	}
}

// CodexQuirks returns quirks for the Codex adapter via fakeCodexRunner scenarios.
func CodexQuirks() VendorQuirks {
	wireLog := ""
	return VendorQuirks{
		Name:     "codex",
		Supports: CapabilitySet{LoadResume: true, Elicitation: false, ProcessDeath: true, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: true, Cancel: true, UsageUpdate: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			// Codex uses fakeCodexRunner scenarios; "turn" is the default happy path
			scenario := "turn"
			var servers []acp.McpServerStdio
			if len(script) > 0 {
				switch script[0] {
				case "die":
					scenario = "die"
				case "NEED_PERMISSION":
					scenario = "approval"
				case "ELICITATION":
					scenario = "mcp"
					servers = []acp.McpServerStdio{{Name: "devkit", Command: "/bin/devkit-mcp"}}
				case "FAIL_PROVIDER_503":
					scenario = "refusal"
				case "HOLD_AFTER":
					scenario = "interrupt"
				}
			}
			f := newCodexFixture(t, scenario)
			wireLog = f.runner.log
			rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd, McpServers: servers})
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Load: func(t *testing.T, acpSessionID string) (SessionRuntime, error) {
			f := newCodexFixture(t, "turn")
			rt, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, acpSessionID)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Normalize:      func(meta json.RawMessage) (NormalizedUsage, error) { return (&CodexAdapter{}).NormalizeUsage(meta) },
		ExpectedStream: "done", ExpectedUsage: NormalizedUsage{InputTokens: 100, OutputTokens: 20},
		ExpectedRefusal:       "The provider is unavailable: 503",
		SetModeIDs:            []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs:    []string{"model", "effort"},
		SetConfigOptionValues: []string{"gpt-5.5-mini", "low"},
		wireLogPath:           func() string { return wireLog },
	}
}
