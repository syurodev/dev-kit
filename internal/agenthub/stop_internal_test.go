package agenthub

import (
	"encoding/json"
	"net"

	"context"
	"devkit/internal/acp"
	"testing"
	"time"
)

// A stop caused by the app's own restart is what a resume is for.
func TestARestartInterruptionStaysResumable(t *testing.T) {
	f := newChoiceFixture(t)
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	stop := InterruptedStop(time.Now())
	if err := f.svc.store.UpdateSessionStop(context.Background(), child.ID, &stop, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResumeChild(f.ctx, child.ID, false); err != nil {
		t.Fatalf("resume after a restart: %v", err)
	}
}

// RecoverStaleSessions records the interruption it notices, so a parent can
// tell a restart from a provider failure.
func TestRecoveryRecordsTheInterruptionAsAStop(t *testing.T) {
	f := newChoiceFixture(t)
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.svc.store.UpdateSessionStatus(ctx, child.ID, SessionStatusRunning, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecoverStaleSessions(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.store.GetSession(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stop == nil || got.Stop.Kind != StopInterruptedByRestart || !got.Stop.Retryable || got.Status != SessionStatusIdle {
		t.Fatalf("after recovery: %+v", got)
	}
}

// The evidence, end to end through the opencode runtime: eleven attempts at a
// provider answering 503, then a bare "Internal error". The stop a parent reads
// names the provider, the attempts and the provider's own sentence, and says a
// resume would fail the same way.
func TestOpenCodeProviderOutageBecomesAProviderUnavailableStop(t *testing.T) {
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
	now := time.Now()
	stop := StopFromError(err, now, false)
	if stop.Kind != StopProviderUnavailable || stop.Retryable || stop.Attempts != 11 || stop.RetryAfter != nil ||
		stop.Message != "Upstream request failed: Endpoint is unavailable (503)" || !stop.BlocksResume(now) {
		t.Fatalf("stop = %+v", stop)
	}
}
