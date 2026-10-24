package agenthub_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// safeBuffer is a bytes.Buffer a runtime goroutine may write to while the test
// reads it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// timingCapture collects what a service reports about its turns and what it
// wrote to the log. log.SetOutput is process-wide, so a test using it does not
// run in parallel; the writer goes back when the test ends.
type timingCapture struct {
	mu   sync.Mutex
	got  []agenthub.TurnTiming
	logs safeBuffer
}

func captureTurnTiming(t *testing.T, svc *agenthub.Service) *timingCapture {
	t.Helper()
	c := &timingCapture{}
	restoreOut, restoreFlags := log.Writer(), log.Flags()
	log.SetOutput(&c.logs)
	// Without this the line starts with the date and time, and the prefix the
	// test looks for is not at the start.
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(restoreOut)
		log.SetFlags(restoreFlags)
	})
	svc.OnTurnTiming = func(timing agenthub.TurnTiming) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.got = append(c.got, timing)
	}
	return c
}

func (c *timingCapture) timings() []agenthub.TurnTiming {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agenthub.TurnTiming(nil), c.got...)
}

// lines are the timing lines and nothing else: a test may also provoke a
// vendor error, which the service logs on its own.
func (c *timingCapture) lines() []string {
	var out []string
	for _, line := range strings.Split(c.logs.String(), "\n") {
		if strings.HasPrefix(line, "agenthub: turn timing") {
			out = append(out, line)
		}
	}
	return out
}

// one asserts the turn answered exactly once — one callback and one line — and
// returns both. Every exit from invoke owes exactly that, however it left.
func (c *timingCapture) one(t *testing.T) (agenthub.TurnTiming, string) {
	t.Helper()
	timings, lines := c.timings(), c.lines()
	if len(timings) != 1 || len(lines) != 1 {
		t.Fatalf("one turn answered %d times (%d lines): %+v\n%s",
			len(timings), len(lines), timings, strings.Join(lines, "\n"))
	}
	return timings[0], lines[0]
}

// timingService is newTestService for any vendor, keeping the DB so a test can
// seed the session row or take the store away before the turn.
func timingService(t *testing.T, adapter agenthub.VendorAdapter) (*agenthub.Service, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc, db
}

// timingAdapter opens one runtime, whatever the test wants it to be, under a
// vendor name of its own so it cannot collide with a real adapter.
type timingAdapter struct {
	vendor agenthub.AgentType
	rt     agenthub.SessionRuntime
}

func (a timingAdapter) Type() agenthub.AgentType { return a.vendor }
func (a timingAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a timingAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, nil
}
func (a timingAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a timingAdapter) Extensions() []string { return nil }

// timingLoader is timingAdapter plus the resume path, so a session that lost
// its process can be reopened.
type timingLoader struct {
	timingAdapter
	loaded chan string
}

func (a *timingLoader) Load(_ context.Context, _ agenthub.OpenSpec, acpSessionID string) (agenthub.SessionRuntime, error) {
	a.loaded <- acpSessionID
	return a.rt, nil
}

// publishing publishes one update the way a vendor's runtime does.
func publishing(handlers agenthub.RuntimeHandlers, raw string) {
	if handlers.OnUpdate != nil {
		handlers.OnUpdate([]byte(raw), nil)
	}
}

const (
	messageChunkUpdate = `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"mock reply"}}`
	thoughtChunkUpdate = `{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking"}}`
	configOptionsJSON  = `{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","type":"select","currentValue":"m1"}]}`
)

// A turn says where its time went, in the order the phases happened, and the
// line it writes names the session and the kinds but never the prompt.
func TestTurnTimingMeasuresThePhasesInOrder(t *testing.T) {
	svc := newTestService(t)
	capture := captureTurnTiming(t, svc)

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A string that must not reach the log: it is the prompt, and the timing
	// line is about phases, not about what the person said.
	const prompt = "PROMPT-MARKER-9f08e9-do-not-log-me"
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: prompt}); err != nil {
		t.Fatal(err)
	}

	timing, line := capture.one(t)
	if timing.SessionID != sess.ID {
		t.Fatalf("session = %q, want %q", timing.SessionID, sess.ID)
	}
	if timing.Vendor != agenthub.AgentTypeMock {
		t.Fatalf("vendor = %q", timing.Vendor)
	}
	if !timing.PromptSentSeen || !timing.FirstUpdateSeen || !timing.FirstVisibleSeen {
		t.Fatalf("the mock always sends an update: %+v", timing)
	}
	if timing.VisibleKind != agenthub.UpdateAgentMessageChunk {
		t.Fatalf("visible kind = %q, want %q", timing.VisibleKind, agenthub.UpdateAgentMessageChunk)
	}
	// The phases before the prompt are disjoint and sequential, and everything
	// after it is an offset from the same start, so these cannot cross.
	beforePrompt := timing.Authorize + timing.Claim + timing.CheckpointWait + timing.Runtime
	if beforePrompt > timing.PromptSent {
		t.Fatalf("phases before the prompt (%s) exceed it (%s): %+v", beforePrompt, timing.PromptSent, timing)
	}
	if timing.PromptSent > timing.FirstUpdate {
		t.Fatalf("first update (%s) arrived before the prompt was sent (%s)", timing.FirstUpdate, timing.PromptSent)
	}
	if timing.FirstUpdate > timing.FirstVisible {
		t.Fatalf("first visible (%s) before the first update (%s)", timing.FirstVisible, timing.FirstUpdate)
	}

	if !strings.Contains(line, "session="+sess.ID) {
		t.Fatalf("the line does not name the session: %q", line)
	}
	if !strings.Contains(line, "first_visible_kind=agent_message_chunk") {
		t.Fatalf("the line does not name the visible kind: %q", line)
	}
	if strings.Contains(line, prompt) {
		t.Fatalf("the line carries the prompt: %q", line)
	}
}

// A turn that never drew a row still says how far it got: the phases are
// logged at the end, and the phases it never reached read as -1 rather than as
// an instant update.
func TestTurnTimingFallsBackAtTurnEndWithoutAVisibleRow(t *testing.T) {
	svc := newTestService(t)
	capture := captureTurnTiming(t, svc)

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The turn is refused before it ever runs: no update, no visible row.
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "FAIL_TURN_PLAIN"}); err == nil {
		t.Fatal("want the refusal")
	}

	timing, line := capture.one(t)
	if timing.FirstVisibleSeen || timing.FirstUpdateSeen {
		t.Fatalf("a refused turn drew nothing: %+v", timing)
	}
	if !strings.Contains(line, "first_visible_ms=-1") || !strings.Contains(line, "first_visible_kind=-") {
		t.Fatalf("an unreached phase must say so: %q", line)
	}
}

// A turn that ends before the session row is read still answers once, and says
// it does not know whose turn it was rather than inventing an id.
func TestTurnTimingAnswersOnceWithoutASession(t *testing.T) {
	svc := newTestService(t)
	capture := captureTurnTiming(t, svc)

	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: "no-such-session", Prompt: "hi"}); err == nil {
		t.Fatal("want the store's refusal")
	}

	timing, line := capture.one(t)
	if timing.SessionID != "" || timing.Vendor != "" {
		t.Fatalf("a session that was never read has no identity: %+v", timing)
	}
	if timing.PromptSentSeen || timing.FirstUpdateSeen || timing.FirstVisibleSeen {
		t.Fatalf("nothing ran, so nothing was reached: %+v", timing)
	}
	for _, want := range []string{"session=- vendor=-", "prompt_ms=-1", "first_update_ms=-1", "first_visible_ms=-1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the line is missing %q: %q", want, line)
		}
	}
}

// A store that cannot answer the session read is still one answer, not none:
// the timing is registered before the read.
func TestTurnTimingAnswersOnceWhenTheStoreFails(t *testing.T) {
	svc, db := timingService(t, agenthub.NewMockAdapter())
	capture := captureTurnTiming(t, svc)

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err == nil {
		t.Fatal("want the store's error")
	}

	timing, line := capture.one(t)
	if timing.SessionID != "" || timing.Vendor != "" {
		t.Fatalf("the row was never read: %+v", timing)
	}
	if !strings.Contains(line, "session=- vendor=-") {
		t.Fatalf("the line must say it has no identity: %q", line)
	}
}

// A context that is already cancelled stops the turn at the session read, and
// that exit is timed like any other.
func TestTurnTimingAnswersOnceWhenTheTurnIsCancelled(t *testing.T) {
	svc := newTestService(t)
	capture := captureTurnTiming(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: "cancelled", Prompt: "hi"}); err == nil {
		t.Fatal("want the cancellation")
	}

	timing, line := capture.one(t)
	if timing.PromptSentSeen || timing.FirstVisibleSeen {
		t.Fatalf("a cancelled turn ran nothing: %+v", timing)
	}
	if timing.SessionID != "" {
		t.Fatalf("the row was never read: %+v", timing)
	}
	if !strings.Contains(line, "prompt_ms=-1") {
		t.Fatalf("the line must say the prompt was never sent: %q", line)
	}
}

// A vendor that panics inside Prompt unwinds the turn through its defers; the
// timing is one of them, so the panic still answers once and still says the
// prompt went out.
func TestTurnTimingAnswersOnceWhenTheAgentPanics(t *testing.T) {
	svc, _ := timingService(t, timingAdapter{vendor: "panicky", rt: &panickyRuntime{}})
	capture := captureTurnTiming(t, svc)

	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: "panicky",
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	panicked := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		_, _ = svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"})
		return false
	}()
	if !panicked {
		t.Fatal("want the agent's panic to reach the caller")
	}

	timing, line := capture.one(t)
	if timing.SessionID != sess.ID {
		t.Fatalf("session = %q, want %q", timing.SessionID, sess.ID)
	}
	if !timing.PromptSentSeen {
		t.Fatalf("the panic happened after the prompt went out: %+v", timing)
	}
	if timing.FirstUpdateSeen || timing.FirstVisibleSeen {
		t.Fatalf("nothing arrived before the panic: %+v", timing)
	}
	if !strings.Contains(line, "first_visible_ms=-1") {
		t.Fatalf("an unreached phase must say so: %q", line)
	}
}

// A dead process is part of the runtime phase: the resolve evicts and closes
// it before it can resume, and both the teardown and the reopen are inside the
// interval the line calls runtime_ms.
func TestTurnTimingMeasuresTheDeadProcessItReplaces(t *testing.T) {
	const closeFor = 50 * time.Millisecond
	rt := &deadTeardownRuntime{closeFor: closeFor}
	adapter := &timingLoader{
		timingAdapter: timingAdapter{vendor: "deadly", rt: rt},
		loaded:        make(chan string, 1),
	}
	svc, db := timingService(t, adapter)
	capture := captureTurnTiming(t, svc)

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: "deadly",
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A session nobody prompted has no conversation to load, so it would be
	// reopened with Open and never reach the dead process at all.
	markPrompted(t, agenthub.NewSQLStore(db), sess.ID)

	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	select {
	case loaded := <-adapter.loaded:
		if loaded != sess.ACPSessionID {
			t.Fatalf("resumed %q, want %q", loaded, sess.ACPSessionID)
		}
	default:
		t.Fatal("the dead process was not resumed")
	}
	if dead, closeCalls := rt.deadCalls.Load(), rt.closeCalls.Load(); dead != 1 || closeCalls != 1 {
		t.Fatalf("Dead called %d times, Close %d, want one of each", dead, closeCalls)
	}
	timing, line := capture.one(t)
	if !timing.RuntimeResume {
		t.Fatalf("the process was opened again: %+v", timing)
	}
	if timing.Runtime < closeFor {
		t.Fatalf("runtime %s is smaller than the %s teardown inside it", timing.Runtime, closeFor)
	}
	if !strings.Contains(line, "runtime=resume") {
		t.Fatalf("the line must say the runtime was resumed: %q", line)
	}
}

// A runtime publishes its settings as it attaches, before the model is asked
// anything. That is not the agent starting to answer, so neither "first update"
// nor "first visible" may be measured from it.
func TestTurnTimingIgnoresUpdatesFromBeforeThePrompt(t *testing.T) {
	const promptFor = 40 * time.Millisecond
	svc, _ := timingService(t, timingAdapter{vendor: "chatty", rt: &prePromptRuntime{promptFor: promptFor}})
	capture := captureTurnTiming(t, svc)

	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: "chatty",
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	timing, _ := capture.one(t)
	if !timing.FirstUpdateSeen || !timing.FirstVisibleSeen {
		t.Fatalf("the answer is an update: %+v", timing)
	}
	if timing.FirstUpdate < promptFor {
		t.Fatalf("first update %s came from the attach, not the answer (%s): %+v",
			timing.FirstUpdate, promptFor, timing)
	}
	if timing.VisibleKind != agenthub.UpdateAgentMessageChunk {
		t.Fatalf("visible kind = %q, want the answer, not the thought", timing.VisibleKind)
	}
}

// panickyRuntime is a vendor that dies inside the turn, after the prompt.
type panickyRuntime struct{ handlers agenthub.RuntimeHandlers }

func (r *panickyRuntime) ACPSessionID() string                   { return "acp-panicky" }
func (r *panickyRuntime) SetHandlers(h agenthub.RuntimeHandlers) { r.handlers = h }
func (r *panickyRuntime) Cancel(context.Context) error           { return nil }
func (r *panickyRuntime) Close(context.Context) error            { return nil }

func (r *panickyRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	panic("the vendor exploded")
}

// deadTeardownRuntime is a process that has already died: it reports itself
// dead, and closing it costs the time a real teardown does.
type deadTeardownRuntime struct {
	handlers agenthub.RuntimeHandlers

	deadCalls  atomic.Int32
	closeCalls atomic.Int32
	closeFor   time.Duration
}

func (r *deadTeardownRuntime) ACPSessionID() string                   { return "acp-dead" }
func (r *deadTeardownRuntime) SetHandlers(h agenthub.RuntimeHandlers) { r.handlers = h }
func (r *deadTeardownRuntime) Cancel(context.Context) error           { return nil }

func (r *deadTeardownRuntime) Dead() (bool, error) {
	r.deadCalls.Add(1)
	return true, nil
}

func (r *deadTeardownRuntime) Close(context.Context) error {
	r.closeCalls.Add(1)
	time.Sleep(r.closeFor)
	return nil
}

func (r *deadTeardownRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	publishing(r.handlers, messageChunkUpdate)
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}

// prePromptRuntime publishes settings and a thought as it attaches — a runtime
// that has not been asked anything — and only answers in Prompt.
type prePromptRuntime struct {
	handlers  agenthub.RuntimeHandlers
	attach    atomic.Int32
	promptFor time.Duration
}

func (r *prePromptRuntime) ACPSessionID() string         { return "acp-chatty" }
func (r *prePromptRuntime) Cancel(context.Context) error { return nil }
func (r *prePromptRuntime) Close(context.Context) error  { return nil }

func (r *prePromptRuntime) SetHandlers(h agenthub.RuntimeHandlers) {
	r.handlers = h
	// The first call is CreateSession attaching its handlers; from the second
	// on this is the turn replacing them, which is where a vendor like Claude
	// announces its config options.
	if r.attach.Add(1) == 1 {
		return
	}
	publishing(h, configOptionsJSON)
	publishing(h, thoughtChunkUpdate)
}

func (r *prePromptRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	time.Sleep(r.promptFor)
	publishing(r.handlers, messageChunkUpdate)
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}
