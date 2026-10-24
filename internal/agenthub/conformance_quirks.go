package agenthub

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
	}
}

// scriptedACPRunner is a scripted agent on a net.Pipe for Claude/opencode ACP tests.
type scriptedACPRunner struct {
	replies []string
	index   int
	mu      sync.Mutex
}

func (r *scriptedACPRunner) serve(t *testing.T, agent net.Conn, done chan<- struct{}) {
	t.Helper()
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			_, err := agent.Read(buf)
			if err != nil {
				return
			}
			r.mu.Lock()
			if r.index >= len(r.replies) {
				r.mu.Unlock()
				return
			}
			reply := r.replies[r.index]
			r.index++
			r.mu.Unlock()
			if _, err := agent.Write([]byte(reply + "\n")); err != nil {
				return
			}
		}
	}()
}

func newScriptedACPRunner(replies []string) *scriptedACPRunner {
	return &scriptedACPRunner{replies: replies}
}

// ClaudeQuirks returns quirks for the Claude adapter via scripted ACP on a pipe.
func ClaudeQuirks() VendorQuirks {
	return VendorQuirks{
		Name:     "claude",
		Supports: CapabilitySet{LoadResume: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			client, agent := net.Pipe()
			t.Cleanup(func() { client.Close(); agent.Close() })

			runner := newScriptedACPRunner(script)
			done := make(chan struct{})
			runner.serve(t, agent, done)

			rt := &acpRuntime{acpSessionID: "acp-test"}
			rt.conn = acp.NewConn(client, silentHandler{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			go rt.conn.Serve(ctx)

			return rt, nil
		},
		Load: func(t *testing.T, acpSessionID string) (SessionRuntime, error) {
			client, agent := net.Pipe()
			t.Cleanup(func() { client.Close(); agent.Close() })

			// Load sends session/load or session/resume; we script a resume
			runner := newScriptedACPRunner([]string{
				`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`,
				`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"` + acpSessionID + `"}}`,
			})
			done := make(chan struct{})
			runner.serve(t, agent, done)

			rt := &acpRuntime{acpSessionID: acpSessionID}
			rt.conn = acp.NewConn(client, silentHandler{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			go rt.conn.Serve(ctx)
			return rt, nil
		},
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			// Claude's usage comes via rate_limit_event; ParseSessionUpdate handles it.
			return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
		},
		SetModeIDs:         []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs: []string{"model", "effort"},
	}
}

// OpenCodeQuirks returns quirks for the opencode adapter via recordingRunner.
func OpenCodeQuirks() VendorQuirks {
	return VendorQuirks{
		Name:     "opencode",
		Supports: CapabilitySet{LoadResume: true, Elicitation: true, ProcessDeath: false, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: true, Cancel: true, UsageUpdate: true},
		Open: func(t *testing.T, script []string) (SessionRuntime, error) {
			turnReply := openCodeConformanceTurn(script)
			replies := []string{
				openCodeInit,
				`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_conformance","configOptions":` + openCodeOptionsWire + `}}`,
				turnReply,
				`{"jsonrpc":"2.0","id":3,"result":{}}`,
			}
			a, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: replies})
			rt, err := a.Open(context.Background(), OpenSpec{Cwd: conformanceWorkDir(t)})
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Load: func(t *testing.T, acpSessionID string) (SessionRuntime, error) {
			noResume := strings.Replace(openCodeInit, `,"resume":{}`, "", 1)
			a, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{
				noResume,
				`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"` + acpSessionID + `","configOptions":` + openCodeOptionsWire + `}}`,
				`{"jsonrpc":"2.0","id":3,"result":{}}`,
			}})
			rt, err := a.Load(context.Background(), OpenSpec{Cwd: conformanceWorkDir(t)}, acpSessionID)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = rt.Close(context.Background()) })
			return rt, nil
		},
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			// opencode reports usage via sessionUpdate; NormalizeUsage is not implemented
			return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
		},
		SetModeIDs:         []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs: []string{"model", "effort"},
	}
}

func openCodeConformanceTurn(script []string) string {
	if len(script) == 0 {
		return `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_conformance","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_conformance","update":{"sessionUpdate":"usage_update","size":100,"used":10}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`
	}
	switch script[0] {
	case "die":
		return "__exit__"
	case "FAIL_PROVIDER_503":
		return `{"jsonrpc":"2.0","id":3,"error":{"code":-32603,"message":"The provider is unavailable: 503"}}`
	case "NEED_PERMISSION":
		return `{"jsonrpc":"2.0","id":77,"method":"session/request_permission","params":{"sessionId":"ses_conformance","toolCall":{"toolCallId":"call","kind":"execute"},"options":[{"optionId":"allow","kind":"allow_once","name":"Allow once"},{"optionId":"reject","kind":"reject_once","name":"Reject"}]}}`
	case "ELICITATION":
		return `{"jsonrpc":"2.0","id":78,"method":"elicitation/create","params":{"mode":"form","sessionId":"ses_conformance","message":"Question?","requestedSchema":{"type":"object","properties":{}}}}`
	default:
		return `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_conformance","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`
	}
}

func conformanceWorkDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agenthub-conformance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// CodexQuirks returns quirks for the Codex adapter via fakeCodexRunner scenarios.
func CodexQuirks() VendorQuirks {
	return VendorQuirks{
		Name:     "codex",
		Supports: CapabilitySet{LoadResume: true, Elicitation: false, ProcessDeath: true, SetMode: true, SetConfigOption: true, StreamingUpdates: true, Permission: true, VendorRefusal: false, Cancel: true, UsageUpdate: true},
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
				case "HOLD_AFTER":
					scenario = "interrupt"
				}
			}
			f := newCodexFixture(t, scenario)
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
		Normalize: func(meta json.RawMessage) (NormalizedUsage, error) {
			// Codex usage comes via thread/tokenUsage/updated; ParseSessionUpdate extracts it.
			return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
		},
		SetModeIDs:            []string{"ask", "auto", "bypassPermissions"},
		SetConfigOptionIDs:    []string{"model", "effort"},
		SetConfigOptionValues: []string{"gpt-5.5-mini", "low"},
	}
}
