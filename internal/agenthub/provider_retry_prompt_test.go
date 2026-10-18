package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
)

// promptAgent runs a runtime against an agent that reads one session/prompt
// and answers with reply (nil = never answers).
func promptAgent(t *testing.T, prepare func(rt *claudeRuntime), reply func(id any) *acp.Response) (error, []json.RawMessage) {
	t.Helper()
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })

	rt := &claudeRuntime{vendor: AgentTypeOpenCode, acpSessionID: "acp-1", serveDone: make(chan struct{})}
	var (
		mu      sync.Mutex
		updates []json.RawMessage
	)
	rt.handlers.OnUpdate = func(update json.RawMessage, _ json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	}
	rt.conn = acp.NewConn(client, silentHandler{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	if prepare != nil {
		prepare(rt)
	}

	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		if response := reply(id); response != nil {
			_ = acp.WriteMessage(agent, *response)
		}
	}()

	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "go"}}, nil)
	mu.Lock()
	defer mu.Unlock()
	return err, append([]json.RawMessage(nil), updates...)
}

func rpcFailure(code int, message, data string) func(any) *acp.Response {
	return func(id any) *acp.Response {
		e := &acp.RPCError{Code: code, Message: message}
		if data != "" {
			e.Data = json.RawMessage(data)
		}
		return &acp.Response{JSONRPC: "2.0", ID: id, Error: e}
	}
}

// opencode's provider failed, it retried, and its prompt then failed. The
// process is fine: that is a turn it ended, not a crash, and the sentence is
// the vendor's.
func TestPromptRefusedByALiveAgentIsATurnFailureWithItsSentence(t *testing.T) {
	err, updates := promptAgent(t, nil, rpcFailure(-32603, "Upstream request failed: Endpoint is unavailable.", ""))
	if !errors.Is(err, ErrTurnFailed) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want ErrTurnFailed and not ErrProcessFailed", err)
	}
	if got := ErrorVendorMessage(err); got != "Upstream request failed: Endpoint is unavailable." {
		t.Fatalf("vendor message = %q", got)
	}
	if got := ErrorVendor(err); got != AgentTypeOpenCode {
		t.Fatalf("vendor = %q", got)
	}
	if got := PreviewFromError(err); got != "Upstream request failed: Endpoint is unavailable." {
		t.Fatalf("preview = %q", got)
	}
	// The banner is said in the session before the error goes back.
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want the failure banner", len(updates))
	}
	failure := ParseSessionUpdate(updates[0]).Failure
	if failure == nil || failure.Category != "turn_failed" || failure.Details != "Upstream request failed: Endpoint is unavailable." || len(failure.Actions) != 1 || failure.Actions[0] != "retry" {
		t.Fatalf("failure = %+v", failure)
	}
}

// A bare "Internal error" says nothing; the provider's own words, shown while
// the agent retried, are the better sentence.
func TestPromptFailureUsesWhatTheProviderSaidWhileRetrying(t *testing.T) {
	// Fed the way the agent does: a notice before the answer, through the
	// connection's update handler.
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })
	rt := &claudeRuntime{vendor: AgentTypeOpenCode, acpSessionID: "acp-1", serveDone: make(chan struct{})}
	rt.conn = acp.NewConn(client, &claudeConnHandler{rt: rt})
	rt.handlers.OnUpdate = func(json.RawMessage, json.RawMessage) {}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		_ = acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: json.RawMessage(
			`{"sessionId":"acp-1","update":{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":{"attempt":11,"nextRetryAt":"2026-10-18T06:28:00.642Z","error":{"type":"provider.internal","message":"Upstream request failed: Endpoint is unavailable.","status":503}}}}}`)})
		_ = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: id, Error: &acp.RPCError{Code: -32603, Message: "Internal error"}})
	}()

	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "go"}}, nil)
	if !errors.Is(err, ErrTurnFailed) {
		t.Fatalf("err = %v", err)
	}
	if got := PreviewFromError(err); got != "Upstream request failed: Endpoint is unavailable (503)" {
		t.Fatalf("preview = %q", got)
	}
}

func TestPromptFailureReadsTheReasonFromData(t *testing.T) {
	err, _ := promptAgent(t, nil, rpcFailure(-32603, "Internal error", `{"details":"The context window is full."}`))
	if !errors.Is(err, ErrTurnFailed) || ErrorVendorMessage(err) != "The context window is full." {
		t.Fatalf("err = %v (%q)", err, ErrorVendorMessage(err))
	}
	// Nothing more specific than "Internal error": a turn failure without a
	// sentence, so the preview names the kind rather than echoing it.
	err, _ = promptAgent(t, nil, rpcFailure(-32603, "Internal error", ""))
	if !errors.Is(err, ErrTurnFailed) || ErrorVendorMessage(err) != "" {
		t.Fatalf("err = %v (%q)", err, ErrorVendorMessage(err))
	}
	if got := PreviewFromError(err); got != "The agent could not finish this turn." {
		t.Fatalf("preview = %q", got)
	}
}

// The process leaving is still a process failure: a hung-up pipe, and an
// error response that arrives after the serve loop has ended.
func TestPromptOnAProcessThatLeftIsStillAProcessFailure(t *testing.T) {
	// The pipe to the process is gone: the request cannot even be written.
	err, updates := promptAgent(t, func(rt *claudeRuntime) {
		rt.conn.Close()
	}, func(any) *acp.Response { return nil })
	if !errors.Is(err, ErrProcessFailed) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("pipe closed: err = %v, want ErrProcessFailed", err)
	}
	if len(updates) != 0 {
		t.Fatalf("a crash is not a vendor failure banner: %v", updates)
	}

	err, _ = promptAgent(t, func(rt *claudeRuntime) { close(rt.serveDone) }, rpcFailure(-32603, "Internal error", ""))
	if !errors.Is(err, ErrProcessFailed) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("serve loop ended: err = %v, want ErrProcessFailed", err)
	}
}

// Claude's expired login arrives the same way and stays a sign-in.
func TestPromptAuthFailureIsStillASignIn(t *testing.T) {
	err, _ := promptAgent(t, nil, rpcFailure(-32603, "Internal error", `{"errorKind":"authentication_failed"}`))
	if !errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("err = %v, want ErrAgentAuthRequired", err)
	}
}
