package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// newServiceForPreviewTest is a Service over a migrated SQL store with the
// given adapter, returning the store so a test can read the row back.
func newServiceForPreviewTest(t *testing.T, adapter agenthub.VendorAdapter) (*agenthub.Service, *agenthub.SQLStore) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	st := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	return agenthub.NewService(reg, st, func(string, json.RawMessage) {}), st
}

func previewOf(t *testing.T, st *agenthub.SQLStore, id string) string {
	t.Helper()
	got, err := st.GetSession(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Preview
}

func TestInvokeWritesPreviewOnSuccess(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	want := agenthub.PreviewFromMessage("mock reply")
	if got := previewOf(t, st, sess.ID); got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

// A failed turn says why, whether it broke the agent or the vendor refused it.
func TestInvokeWritesPreviewOnFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"process failed", fmt.Errorf("%w: codex exited", agenthub.ErrProcessFailed)},
		{"usage limit", fmt.Errorf("%w: try again at 3:10 PM", agenthub.ErrAgentUsageLimit)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, st := newServiceForPreviewTest(t, refusingAdapter{rt: &refusingRuntime{err: tc.err}})
			sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "refusing", Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			_, invokeErr := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"})
			if !errors.Is(invokeErr, tc.err) {
				t.Fatalf("Invoke = %v, want %v", invokeErr, tc.err)
			}
			want := agenthub.PreviewFromError(invokeErr)
			if want == "" {
				t.Fatal("PreviewFromError gave nothing to compare against")
			}
			if got := previewOf(t, st, sess.ID); got != want {
				t.Fatalf("preview = %q, want %q", got, want)
			}
		})
	}
}

// A cancelled turn is the person's own doing; the line keeps saying what the
// last finished turn ended on.
func TestInvokeCancelledKeepsPreviousPreview(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc, st := newServiceForPreviewTest(t, adapter)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	before := previewOf(t, st, sess.ID)
	if before == "" {
		t.Fatal("first turn wrote no preview")
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "BLOCK"})
		errCh <- err
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got, err := svc.GetStatus(context.Background(), sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		// Running is set before the vendor has the prompt; a Cancel in that gap
		// reaches nothing, so wait until the runtime is in the turn.
		if got.Status == agenthub.SessionStatusRunning && adapter.LastRuntime().InTurn() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session never became running (status=%s)", got.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := svc.Cancel(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, agenthub.ErrCancelled) {
			t.Fatalf("Invoke = %v, want ErrCancelled", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancelled invoke never returned")
	}
	if got := previewOf(t, st, sess.ID); got != before {
		t.Fatalf("preview = %q, want the previous %q", got, before)
	}
}

// The preview is the top-level agent's LAST message: an earlier message of the
// same turn is replaced, and a subagent's chunk (tagged with an origin) never
// counts.
func TestInvokePreviewIsTheLastTopLevelMessage(t *testing.T) {
	chunk := func(messageID, text, origin string) json.RawMessage {
		fields := map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"messageId":     messageID,
			"content":       map[string]any{"type": "text", "text": text},
		}
		if origin != "" {
			fields[agenthub.DevKitWireKey] = map[string]string{"origin": origin}
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	rt := &scriptedRuntime{updates: []json.RawMessage{
		chunk("m1", "Looking at the cache first.", ""),
		chunk("m2", "Fixed the", ""),
		chunk("m2", " TTL bug. Then more detail", ""),
		chunk("s1", "Subagent finished its search.", "sub-session-1"),
	}}
	svc, st := newServiceForPreviewTest(t, scriptedAdapter{rt: rt})
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "scripted", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if got, want := previewOf(t, st, sess.ID), "Fixed the TTL bug."; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

// Codex (and the mock) send no message ids, so a message ends where anything
// else of the turn comes between two runs of chunks: the preview is the text
// after the tool call, not the turn's first sentence.
func TestInvokePreviewWithoutMessageIDsKeepsTheLastMessage(t *testing.T) {
	update := func(fields map[string]any) json.RawMessage {
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	text := func(s string) json.RawMessage {
		return update(map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": s},
		})
	}
	rt := &scriptedRuntime{updates: []json.RawMessage{
		text("I'll inspect"),
		text(" the cache."),
		update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Read cache.go", "status": "pending"}),
		update(map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100}),
		text("Fixed the"),
		update(map[string]any{"sessionUpdate": "usage_update", "used": 11, "size": 100}),
		text(" TTL bug. And more."),
	}}
	svc, st := newServiceForPreviewTest(t, scriptedAdapter{rt: rt})
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "scripted", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if got, want := previewOf(t, st, sess.ID), "Fixed the TTL bug."; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

// A busy session is a refusal before anything ran; it leaves the line alone.
func TestInvokeBusyRefusalWritesNoPreview(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, refusingAdapter{rt: &refusingRuntime{err: agenthub.ErrSessionBusy}})
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "refusing", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"}); !errors.Is(err, agenthub.ErrSessionBusy) {
		t.Fatalf("Invoke = %v, want ErrSessionBusy", err)
	}
	if got := previewOf(t, st, sess.ID); got != "" {
		t.Fatalf("preview = %q, want none", got)
	}
}

// A turn that fails after the agent answered says it failed, in plain words,
// rather than quoting what the agent said before it.
func TestInvokeWritesPreviewWhenAFinishedTurnFailsToRecord(t *testing.T) {
	rt := &scriptedRuntime{updates: []json.RawMessage{
		json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"All done."}}`),
	}}
	svc, st := newServiceForPreviewTest(t, scriptedAdapter{rt: rt, usageErr: errors.New("bad usage meta")})
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "scripted", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"}); err == nil {
		t.Fatal("Invoke succeeded, want the usage error")
	}
	if got, want := previewOf(t, st, sess.ID), "The agent could not finish this turn."; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

// scriptedAdapter's runtime sends a fixed list of updates and ends the turn.
type scriptedAdapter struct {
	rt       *scriptedRuntime
	usageErr error
}

func (a scriptedAdapter) Type() agenthub.AgentType { return agenthub.AgentType("scripted") }
func (a scriptedAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a scriptedAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, a.usageErr
}
func (a scriptedAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a scriptedAdapter) Extensions() []string { return nil }

type scriptedRuntime struct {
	mu       sync.Mutex
	handlers agenthub.RuntimeHandlers
	updates  []json.RawMessage
}

func (r *scriptedRuntime) ACPSessionID() string         { return "acp-scripted" }
func (r *scriptedRuntime) Cancel(context.Context) error { return nil }
func (r *scriptedRuntime) Close(context.Context) error  { return nil }
func (r *scriptedRuntime) SetHandlers(h agenthub.RuntimeHandlers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = h
}
func (r *scriptedRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	r.mu.Lock()
	h := r.handlers
	r.mu.Unlock()
	for _, u := range r.updates {
		if h.OnUpdate != nil {
			h.OnUpdate(u, nil)
		}
	}
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}
