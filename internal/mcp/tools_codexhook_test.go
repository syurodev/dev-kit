package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"devkit/internal/codexhook"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeCodexHook struct {
	recorded []codexhook.Call
	err      error
}

func (f *fakeCodexHook) Record(call codexhook.Call) error {
	if f.err != nil {
		return f.err
	}
	f.recorded = append(f.recorded, call)
	return nil
}

func newCodexHookServer(t *testing.T, hook *fakeCodexHook, caller *fakeCaller) *Server {
	t.Helper()
	return NewServer(Deps{
		Caller:    caller,
		Notes:     &fakeNotes{},
		Vault:     &fakeVault{},
		Search:    &fakeSearch{},
		Tools:     fakeTools{},
		Envs:      &fakeEnvs{},
		CodexHook: hook,
	})
}

func TestCodexHookEventRecordsAndAnswersContinue(t *testing.T) {
	hook := &fakeCodexHook{}
	caller := &fakeCaller{}
	srv := newCodexHookServer(t, hook, caller)

	call := codexhook.Call{
		Event: codexhook.EventPreToolUse, SessionID: "s1", CWD: "/repo",
		TurnID: "t1", ToolName: "shell", ToolInput: json.RawMessage(`{"command":["ls"]}`),
	}
	_, v, err := srv.toolCodexHookEvent(context.Background(), &sdk.CallToolRequest{}, call)
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := unwrap(v).(codexhook.Reply)
	if !ok || !reply.Continue {
		t.Fatalf("reply = %#v, want Observed()", v)
	}
	if len(hook.recorded) != 1 || hook.recorded[0].SessionID != "s1" {
		t.Fatalf("recorded = %+v", hook.recorded)
	}

	// The Capability Gateway is exercised with the right subject/cap/scope,
	// and the codex event + session id land in metadata for the audit trail —
	// never the tool input or the prompt.
	if caller.last.subject != "builtin.codexhook" || caller.last.capability != "codexhook:report" || caller.last.scope != "event" {
		t.Fatalf("Call = %+v", caller.last)
	}
	if caller.last.metadata["codex_event"] != "PreToolUse" || caller.last.metadata["session_id"] != "s1" {
		t.Fatalf("meta = %v", caller.last.metadata)
	}
}

// An event DevKit never registered — PermissionRequest, say — is refused
// before it ever reaches Record. See codexhook.Registered on why: DevKit has
// no UI to decide with yet, and a hook that silently accepted one would be a
// gate quietly replaced by an always-allow.
func TestCodexHookEventRefusesAnUnregisteredEvent(t *testing.T) {
	hook := &fakeCodexHook{}
	srv := newCodexHookServer(t, hook, &fakeCaller{})

	call := codexhook.Call{Event: codexhook.Event("PermissionRequest"), SessionID: "s1"}
	_, _, err := srv.toolCodexHookEvent(context.Background(), &sdk.CallToolRequest{}, call)
	var unknown codexhook.ErrUnknownEvent
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want ErrUnknownEvent", err)
	}
	if len(hook.recorded) != 0 {
		t.Fatal("an unregistered event was recorded anyway")
	}
}

func TestCodexHookEventRefusesAMissingSessionID(t *testing.T) {
	hook := &fakeCodexHook{}
	srv := newCodexHookServer(t, hook, &fakeCaller{})

	call := codexhook.Call{Event: codexhook.EventStop, CWD: "/repo"}
	if _, _, err := srv.toolCodexHookEvent(context.Background(), &sdk.CallToolRequest{}, call); err == nil {
		t.Fatal("a call with no session id was accepted")
	}
	if len(hook.recorded) != 0 {
		t.Fatal("a call with no session id was recorded anyway")
	}
}

// A recording failure (a locked vault, a disk error) has to reach Codex as a
// tool error too, not as a silent "continue" — codexhook's own doc says a
// failing hook costs Codex nothing but a warning, which is only true if the
// failure actually gets reported as one.
func TestCodexHookEventSurfacesARecordFailure(t *testing.T) {
	hook := &fakeCodexHook{err: errors.New("disk full")}
	srv := newCodexHookServer(t, hook, &fakeCaller{})

	call := codexhook.Call{Event: codexhook.EventStop, SessionID: "s1"}
	if _, _, err := srv.toolCodexHookEvent(context.Background(), &sdk.CallToolRequest{}, call); err == nil {
		t.Fatal("a failed Record was reported as success")
	}
}

// Without CodexHook wired, the tool must not even register — the same rule
// every optional Deps field in this package follows (see registerAPIClient).
func TestCodexHookToolIsAbsentWithoutCodexHookWired(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(Deps{
		Caller: &fakeCaller{},
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
	})

	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := srv.MCP().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == codexhook.MCPToolName {
			t.Fatal("codex.hookEvent registered with no CodexHook wired")
		}
	}
}

// With it wired, the tool IS reachable over the real transport — a schema
// that satisfies AddTool at registration time is not proof the JSON-RPC round
// trip actually decodes an object argument list into codexhook.Call.
func TestCodexHookToolIsReachableOverRealTransport(t *testing.T) {
	ctx := context.Background()
	hook := &fakeCodexHook{}
	srv := newCodexHookServer(t, hook, &fakeCaller{})

	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := srv.MCP().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{
		Name: codexhook.MCPToolName,
		Arguments: map[string]any{
			"event": "Stop", "sessionId": "s1", "cwd": "/repo",
			"turnId": "t1", "model": "gpt-5-codex",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool call reported an error: %+v", result)
	}
	if len(hook.recorded) != 1 || hook.recorded[0].SessionID != "s1" || hook.recorded[0].Event != codexhook.EventStop {
		t.Fatalf("recorded = %+v", hook.recorded)
	}
}
