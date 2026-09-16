package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devkit/internal/storage"
)

// closedLog collects ElicitationClosed events without a data race.
type closedLog struct {
	mu     sync.Mutex
	events []ElicitationClosed
}

func (l *closedLog) add(c ElicitationClosed) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, c)
}

func (l *closedLog) all() []ElicitationClosed {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ElicitationClosed(nil), l.events...)
}

// waitAsync runs Wait on a goroutine and reports its answer.
type waitResult struct {
	body json.RawMessage
	err  error
}

func waitAsync(w *ElicitationWaiter, ctx context.Context, sessionID string) <-chan waitResult {
	out := make(chan waitResult, 1)
	go func() {
		body, err := w.Wait(ctx, sessionID, json.RawMessage(`{"mode":"form","message":"?","toolCallId":"toolu_1"}`))
		out <- waitResult{body, err}
	}()
	return out
}

// pendingWithin waits until the waiter holds n questions.
func pendingWithin(t *testing.T, w *ElicitationWaiter, n int) []PendingElicitation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if list := w.PendingList(); len(list) == n {
			return list
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiter never held %d questions (has %d)", n, len(w.PendingList()))
	return nil
}

func answer(t *testing.T, ch <-chan waitResult) waitResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("Wait never returned")
		return waitResult{}
	}
}

// TestElicitationWaiterQueuesMoreThanOnePerSession pins the deliberate
// difference from PermissionWaiter, which denies a second prompt from the same
// session outright. A subagent and an MCP server can each ask while the main
// turn keeps working; the second question is a queue, not a protocol violation.
func TestElicitationWaiterQueuesMoreThanOnePerSession(t *testing.T) {
	w := NewElicitationWaiter(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := waitAsync(w, ctx, "s1")
	second := waitAsync(w, ctx, "s1")
	list := pendingWithin(t, w, 2)

	// And neither has been answered on its own.
	select {
	case r := <-first:
		t.Fatalf("first question answered unasked: %s", r.body)
	case r := <-second:
		t.Fatalf("second question answered unasked: %s", r.body)
	default:
	}
	if list[0].ToolCallID != "toolu_1" || list[0].Mode != "form" || list[0].Message != "?" {
		t.Fatalf("pending = %+v; the head of the request should be read out", list[0])
	}
}

func TestElicitationCompleteAnswersOnceAndSaysSo(t *testing.T) {
	log := &closedLog{}
	var notified []PendingElicitation
	var notifyMu sync.Mutex
	w := NewElicitationWaiter(func(p PendingElicitation) {
		notifyMu.Lock()
		notified = append(notified, p)
		notifyMu.Unlock()
	}, log.add)

	got := waitAsync(w, context.Background(), "s1")
	id := pendingWithin(t, w, 1)[0].ID

	body := json.RawMessage(`{"action":"accept","content":{"question_0":"yes"}}`)
	if err := w.Complete(id, body); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if r := answer(t, got); r.err != nil || string(r.body) != string(body) {
		t.Fatalf("Wait = %s, %v", r.body, r.err)
	}
	if err := w.Complete(id, body); err == nil {
		t.Fatal("a question answered twice")
	}

	events := log.all()
	if len(events) != 1 || events[0].Reason != ElicitationAnswered || events[0].ID != id {
		t.Fatalf("closed events = %+v, want exactly one answered", events)
	}
	notifyMu.Lock()
	defer notifyMu.Unlock()
	if len(notified) != 1 || notified[0].ID != id {
		t.Fatalf("notified = %+v", notified)
	}
}

// TestCancelSessionAnswersCancelNotDecline: Stop means abandon. A declined
// question would let the tool it belongs to run on with empty answers — the turn
// continuing after the user stopped it.
func TestCancelSessionAnswersCancelNotDecline(t *testing.T) {
	log := &closedLog{}
	w := NewElicitationWaiter(nil, log.add)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := waitAsync(w, ctx, "stopped")
	other := waitAsync(w, ctx, "other")
	pendingWithin(t, w, 2)

	if n := w.CancelSession("stopped"); n != 1 {
		t.Fatalf("cancelled %d, want 1", n)
	}
	if r := answer(t, stopped); string(r.body) != string(cancelledElicitation) {
		t.Fatalf("stopped session answered %s, want cancel", r.body)
	}
	rest := w.PendingList()
	if len(rest) != 1 || rest[0].SessionID != "other" {
		t.Fatalf("pending after stop = %+v, want only the other session's question", rest)
	}
	select {
	case r := <-other:
		t.Fatalf("another session's question was answered: %s", r.body)
	default:
	}

	events := log.all()
	if len(events) != 1 || events[0].Reason != ElicitationTurnStopped {
		t.Fatalf("closed events = %+v, want one turn_stopped", events)
	}
}

// TestAgentTakingTheQuestionBackAnswersCancel covers $/cancel_request and a
// lost connection, both of which end ctx. It is not an error — the response is
// still written — and the card has to be told, because nothing else will take
// it down.
func TestAgentTakingTheQuestionBackAnswersCancel(t *testing.T) {
	log := &closedLog{}
	w := NewElicitationWaiter(nil, log.add)
	ctx, cancel := context.WithCancel(context.Background())

	got := waitAsync(w, ctx, "s1")
	pendingWithin(t, w, 1)
	cancel()

	r := answer(t, got)
	if r.err != nil || string(r.body) != string(cancelledElicitation) {
		t.Fatalf("Wait = %s, %v; want cancel and no error", r.body, r.err)
	}
	if len(w.PendingList()) != 0 {
		t.Fatal("a withdrawn question is still pending")
	}
	events := log.all()
	if len(events) != 1 || events[0].Reason != ElicitationCancelled {
		t.Fatalf("closed events = %+v, want one cancelled", events)
	}
}

// askUserQuestionRequest is the request claude-agent-acp 0.74 sends for an
// AskUserQuestion with one single-select and one multi-select question
// (elicitation.js askUserQuestionsToCreateRequest).
var askUserQuestionRequest = json.RawMessage(`{
	"mode": "form",
	"sessionId": "acp-1",
	"toolCallId": "toolu_01",
	"message": "Please answer the following questions.",
	"requestedSchema": {
		"type": "object",
		"properties": {
			"question_0": {"type": "string", "title": "Library", "oneOf": [{"const": "date-fns", "title": "date-fns"}]},
			"question_0_custom": {"type": "string", "title": "Other"},
			"question_1": {"type": "array", "title": "Features", "items": {"anyOf": [{"const": "Caching", "title": "Caching"}]}},
			"retries": {"type": "integer"},
			"ratio": {"type": "number"},
			"confirm": {"type": "boolean"}
		}
	}
}`)

// TestNormalizeElicitationResponseDropsWhatTheAgentWouldDrop covers the one
// failure in this path with no symptom. The agent reads content values of four
// kinds and treats anything else as no content — the tool runs, the model is
// told nothing was answered, and nothing on screen says why.
func TestNormalizeElicitationResponseDropsWhatTheAgentWouldDrop(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keeps each kind where its property asks for it",
			in:   `{"action":"accept","content":{"question_0":"date-fns","question_1":["Caching"],"retries":3,"ratio":0.5,"confirm":true}}`,
			want: `{"action":"accept","content":{"confirm":true,"question_0":"date-fns","question_1":["Caching"],"ratio":0.5,"retries":3}}`,
		},
		{
			name: "a multi-select sent as one joined string is not a string array",
			in:   `{"action":"accept","content":{"question_1":"Caching, Telemetry"}}`,
			want: `{"action":"accept","content":{}}`,
		},
		{
			name: "a key the question never asked for",
			in:   `{"action":"accept","content":{"question_9":"x"}}`,
			want: `{"action":"accept","content":{}}`,
		},
		{
			name: "objects, nulls and mixed arrays",
			in:   `{"action":"accept","content":{"question_0":{"v":1},"question_0_custom":null,"question_1":["a",2]}}`,
			want: `{"action":"accept","content":{}}`,
		},
		{
			name: "an integer that is not whole, and a number sent as text",
			in:   `{"action":"accept","content":{"retries":2.5,"ratio":"0.5"}}`,
			want: `{"action":"accept","content":{}}`,
		},
		{
			name: "decline carries no content",
			in:   `{"action":"decline","content":{"question_0":"date-fns"}}`,
			want: `{"action":"decline"}`,
		},
		{
			name: "cancel carries no content",
			in:   `{"action":"CANCEL","content":{"question_0":"date-fns"}}`,
			want: `{"action":"cancel"}`,
		},
		{
			name: "an action the protocol does not have is a skip, never an answer",
			in:   `{"action":"approve","content":{"question_0":"date-fns"}}`,
			want: `{"action":"decline"}`,
		},
		{
			name: "a body that does not parse",
			in:   `not json`,
			want: `{"action":"decline"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeElicitationResponse(json.RawMessage(tc.in), askUserQuestionRequest)
			if string(got) != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestMediateElicitationNeverFails: nothing short of an answer may become an
// error. Returned as one, the vendor reads a failure to present the question
// and aborts the tool.
func TestMediateElicitationNeverFails(t *testing.T) {
	ctx := context.Background()

	t.Run("nobody to ask declines", func(t *testing.T) {
		got, err := (&Service{}).mediateElicitation(ctx, "s1", askUserQuestionRequest)
		if err != nil || string(got) != string(declinedElicitation) {
			t.Fatalf("got %s, %v", got, err)
		}
	})

	t.Run("a resolver that fails declines", func(t *testing.T) {
		svc := &Service{ResolveElicitation: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("webview gone")
		}}
		got, err := svc.mediateElicitation(ctx, "s1", askUserQuestionRequest)
		if err != nil || string(got) != string(declinedElicitation) {
			t.Fatalf("got %s, %v", got, err)
		}
	})

	t.Run("a withdrawn question cancels whatever the resolver said", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		svc := &Service{ResolveElicitation: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			cancel()
			return json.RawMessage(`{"action":"accept","content":{"question_0":"date-fns"}}`), nil
		}}
		got, err := svc.mediateElicitation(cctx, "s1", askUserQuestionRequest)
		if err != nil || string(got) != string(cancelledElicitation) {
			t.Fatalf("got %s, %v", got, err)
		}
	})

	t.Run("an answer is normalised against the question it answers", func(t *testing.T) {
		svc := &Service{ResolveElicitation: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"action":"accept","content":{"question_0":"date-fns","question_1":"Caching"}}`), nil
		}}
		got, _ := svc.mediateElicitation(ctx, "s1", askUserQuestionRequest)
		if string(got) != `{"action":"accept","content":{"question_0":"date-fns"}}` {
			t.Fatalf("got %s", got)
		}
	})
}

// elicitationService opens a mock session and returns its runtime.
func elicitationService(t *testing.T) (*Service, *MockAdapter, string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	adapter := NewMockAdapter()
	reg := NewRegistry()
	reg.Register(adapter)
	svc := NewService(reg, NewSQLStore(db), nil)
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return svc, adapter, sess.ID
}

// TestEveryRuntimeCanAsk: SetHandlers replaces the whole set, and there are two
// places that call it. A question handler missing from either turns "ask the
// person" into "decline without asking" — between turns if attachUpdates drops
// it, and during a turn, when the agent is most likely to ask, if Invoke does.
func TestEveryRuntimeCanAsk(t *testing.T) {
	svc, adapter, id := elicitationService(t)
	var asked []string
	svc.ResolveElicitation = func(_ context.Context, sessionID string, _ json.RawMessage) (json.RawMessage, error) {
		asked = append(asked, sessionID)
		return json.RawMessage(`{"action":"decline"}`), nil
	}
	rt := adapter.LastRuntime()

	elicit := func(when string) {
		t.Helper()
		rt.mu.Lock()
		handler := rt.handlers.OnElicit
		rt.mu.Unlock()
		if handler == nil {
			t.Fatalf("%s: the runtime has no question handler", when)
		}
		if _, err := handler(context.Background(), askUserQuestionRequest); err != nil {
			t.Fatalf("%s: %v", when, err)
		}
	}

	elicit("after the session opened")
	if _, err := svc.Invoke(context.Background(), InvokeInput{SessionID: id, Prompt: "hi"}); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	elicit("after a turn installed its own handlers")

	if len(asked) != 2 || asked[0] != id || asked[1] != id {
		t.Fatalf("asked for %v, want the session's own id twice", asked)
	}
}

// TestStopTurnCancelsPendingQuestions: Stop reaches the questions, not only the
// permissions, before the runtime is killed.
func TestStopTurnCancelsPendingQuestions(t *testing.T) {
	svc, _, id := elicitationService(t)
	var cancelled []string
	svc.CancelPendingElicitations = func(sessionID string) int {
		cancelled = append(cancelled, sessionID)
		return 1
	}
	if err := svc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(cancelled) != 1 || cancelled[0] != id {
		t.Fatalf("questions cancelled for %v, want %s", cancelled, id)
	}
}
