package agenthub_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

const (
	doneTurnJSON = `{"sessionUpdate":"async_task_state_update","asyncTaskId":"autonomous-turn-c1514d72","state":"completed"}`
)

// activityLog records OnActivityChanged calls.
type activityLog struct {
	mu    sync.Mutex
	calls []activityCall
}

type activityCall struct {
	id string
	a  agenthub.SessionActivity
}

func (l *activityLog) hook(id string, a agenthub.SessionActivity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, activityCall{id, a})
}

func (l *activityLog) all() []activityCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]activityCall(nil), l.calls...)
}

func TestActivityHookSeesTheTurnOnThenOffOnce(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()

	f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(spawnTurnJSON))
	f.svc.WaitActivityIdleForTest()
	f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(doneTurnJSON))
	f.svc.WaitActivityIdleForTest()

	want := []activityCall{
		{f.parent.ID, agenthub.SessionActivity{AgentTurn: true}},
		{f.parent.ID, agenthub.SessionActivity{}},
	}
	got := log.all()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hook calls = %+v, want %+v", got, want)
	}
}

func TestActivityHookIgnoresUpdatesThatChangeNothing(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	progress := `{"sessionUpdate":"async_task_progress","asyncTaskId":"t1","taskType":"shell"}`
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(progress))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON)) // an end for a task never seen
	f.svc.WaitActivityIdleForTest()
	if got := log.all(); len(got) != 0 {
		t.Fatalf("hook called for no change: %+v", got)
	}
}

// Releasing, closing, rewinding or switching the account of a process takes the
// service lock around forget. The hook may take that lock itself: a notification
// delivered on the caller's goroutine would deadlock here.
func TestActivityHookNeverRunsWithTheServiceLockHeld(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	var hookRan sync.WaitGroup
	hookRan.Add(1)
	var once sync.Once
	f.svc.OnActivityChanged = func(id string, a agenthub.SessionActivity) {
		if !a.Zero() {
			return
		}
		f.svc.WithLockForTest(func() {}) // would block forever if the caller held it
		once.Do(hookRan.Done)
	}
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.WaitActivityIdleForTest()

	done := make(chan struct{})
	go func() {
		f.svc.WithLockForTest(func() { f.svc.ForgetBackgroundWorkForTest(f.kid.ID) })
		hookRan.Wait()
		f.svc.WaitActivityIdleForTest()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the hook did not run, or deadlocked on the service lock")
	}
}

func TestActivityHookSeesAProcessGoingAway(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnTurnJSON))
	f.svc.WaitActivityIdleForTest()
	if err := f.svc.CloseSession(ctx, f.kid.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.WaitActivityIdleForTest()
	got := log.all()
	if len(got) == 0 || !got[len(got)-1].a.Zero() {
		t.Fatalf("closing the session left the last notification at %+v", got)
	}
}

func TestShutdownStopsTheActivityNotifier(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.WaitActivityIdleForTest()
	if err := f.svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	n := len(log.all())
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON))
	f.svc.WaitActivityIdleForTest() // returns at once: stopped
	time.Sleep(20 * time.Millisecond)
	if len(log.all()) != n {
		t.Fatal("the hook was called after Shutdown")
	}
}

func activityOf(t *testing.T, f *idleFixture, id string) *agenthub.SessionActivity {
	t.Helper()
	got, err := f.svc.GetStatus(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Activity
}

func listedActivity(t *testing.T, f *idleFixture, id string) *agenthub.SessionActivity {
	t.Helper()
	list, err := f.svc.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id {
			return s.Activity
		}
	}
	t.Fatalf("session %s not listed", id)
	return nil
}

func TestGetStatusAndListSessionsCarryTheActivity(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	if a := activityOf(t, f, f.kid.ID); a != nil {
		t.Fatalf("a quiet session carries activity %+v", a)
	}
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnTurnJSON))
	want := agenthub.SessionActivity{AgentTurn: true, Tasks: 1}
	if a := activityOf(t, f, f.kid.ID); a == nil || *a != want {
		t.Fatalf("GetStatus activity = %+v, want %+v", a, want)
	}
	if a := listedActivity(t, f, f.kid.ID); a == nil || *a != want {
		t.Fatalf("ListSessions activity = %+v, want %+v", a, want)
	}
	if a := listedActivity(t, f, f.parent.ID); a != nil {
		t.Fatalf("another session carries activity %+v", a)
	}
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(`{"sessionUpdate":"async_task_state_update","asyncTaskId":"autonomous-turn-c1514d72","state":"completed"}`))
	if a := activityOf(t, f, f.kid.ID); a != nil {
		t.Fatalf("after both ended: %+v", a)
	}
}

// Activity is read from the live process: the stored row never has it, so whatever
// else reads the store (idle release, peers, recovery) is untouched.
func TestActivityIsNeverStored(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnTurnJSON))
	row, err := f.store.GetSession(ctx, f.kid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Activity != nil || row.Status != agenthub.SessionStatusIdle {
		t.Fatalf("stored row = status %q activity %+v", row.Status, row.Activity)
	}
	list, err := f.store.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Activity != nil {
			t.Fatalf("the store's list carries activity for %s", s.ID)
		}
	}
}

// The owner's sequence, built synthetically: a shell task runs and ends, then the
// agent starts a turn of its own that talks, calls tools and ends.
const (
	previewBefore = "Build chạy nền, đợi kết quả."
	chunkOne      = `{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"Để mình xem log. "}}`
	chunkTwoA     = `{"sessionUpdate":"agent_message_chunk","messageId":"m2","content":{"type":"text","text":"Build xanh rồi. "}}`
	chunkTwoB     = `{"sessionUpdate":"agent_message_chunk","messageId":"m2","content":{"type":"text","text":"Mình sẽ tiếp tục."}}`
	subagentChunk = `{"sessionUpdate":"agent_message_chunk","messageId":"x1","_devkit":{"origin":"sub-1"},"content":{"type":"text","text":"Việc con xong."}}`
	toolCallJSON  = `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Read log","status":"completed"}`
)

func (f *idleFixture) previewOf(t *testing.T, id string) string {
	t.Helper()
	got, err := f.store.GetSession(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Preview
}

func (f *idleFixture) feed(updates ...string) {
	for _, u := range updates {
		f.svc.HandleUpdateForTest(context.Background(), f.parent.ID, []byte(u))
	}
}

func TestAnAgentStartedTurnWritesItsLastMessageAsThePreview(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	id := f.parent.ID
	if err := f.store.UpdateSessionPreview(ctx, id, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	var previewAtNotify string
	var offSeen int
	var wasOn bool
	f.svc.OnActivityChanged = func(sid string, a agenthub.SessionActivity) {
		if sid != id {
			return
		}
		if wasOn && !a.AgentTurn {
			offSeen++
			previewAtNotify = f.previewOf(t, id)
		}
		wasOn = a.AgentTurn
	}

	f.feed(spawnShellJSON, doneShellJSON)
	f.svc.WaitActivityIdleForTest()
	f.feed(spawnTurnJSON, chunkOne, toolCallJSON, chunkTwoA, subagentChunk, chunkTwoB)
	if got := f.previewOf(t, id); got != previewBefore {
		t.Fatalf("the preview moved before the turn ended: %q", got)
	}
	if a := activityOf(t, f, id); a == nil || !a.AgentTurn {
		t.Fatalf("activity during the turn = %+v", a)
	}
	f.feed(doneTurnJSON)
	f.svc.WaitActivityIdleForTest()

	want := agenthub.PreviewFromMessage("Build xanh rồi. Mình sẽ tiếp tục.")
	if want == "" || want == previewBefore {
		t.Fatalf("bad fixture: want %q", want)
	}
	if got := f.previewOf(t, id); got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
	if offSeen != 1 || previewAtNotify != want {
		t.Fatalf("the turn-off notification came %d times, with the preview %q: the window refetches on it and must find the new one", offSeen, previewAtNotify)
	}
	if got := f.status(t, id); got != agenthub.SessionStatusIdle {
		t.Fatalf("stored status changed to %q", got)
	}
}

func TestAnAgentStartedTurnWithoutAMessageKeepsThePreview(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	if err := f.store.UpdateSessionPreview(ctx, f.parent.ID, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.feed(spawnTurnJSON, toolCallJSON, doneTurnJSON)
	if got := f.previewOf(t, f.parent.ID); got != previewBefore {
		t.Fatalf("preview = %q", got)
	}
}

// What the person's own turn said before the agent's turn began is not the agent's turn's message.
func TestAnAgentStartedTurnStartsWithAnEmptyMessage(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	if err := f.store.UpdateSessionPreview(ctx, f.parent.ID, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.feed(chunkOne) // no agent turn yet: nothing is accumulated
	f.feed(spawnTurnJSON, toolCallJSON, doneTurnJSON)
	if got := f.previewOf(t, f.parent.ID); got != previewBefore {
		t.Fatalf("a message from before the turn became its preview: %q", got)
	}
	f.feed(spawnTurnJSON, chunkTwoA, doneTurnJSON)
	f.feed(spawnTurnJSON, toolCallJSON, doneTurnJSON)
	if got := f.previewOf(t, f.parent.ID); got != agenthub.PreviewFromMessage("Build xanh rồi.") {
		t.Fatalf("a second turn did not reset the buffer: %q", got)
	}
}

// An Invoke that starts while the agent's turn runs owns the preview: its own end writes it.
func TestAnInvokeClaimDuringAnAgentTurnKeepsThePreviewWithInvoke(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	if err := f.store.UpdateSessionPreview(ctx, f.parent.ID, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.feed(spawnTurnJSON, chunkOne)
	release := f.svc.ClaimInvokeForTest(f.parent.ID)
	f.feed(chunkTwoA, chunkTwoB)
	release()
	f.feed(doneTurnJSON)
	if got := f.previewOf(t, f.parent.ID); got != previewBefore {
		t.Fatalf("the agent turn wrote the preview although an Invoke ran meanwhile: %q", got)
	}
}

// A process that goes away mid-turn leaves nothing to preview: the message was cut off.
func TestAReleasedProcessDropsTheAgentTurnsMessage(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	if err := f.store.UpdateSessionPreview(ctx, f.parent.ID, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.feed(spawnTurnJSON, chunkTwoA)
	if !f.svc.AgentTurnMessageHeldForTest(f.parent.ID) {
		t.Fatal("the turn's message is not buffered")
	}
	f.svc.ForgetBackgroundWorkForTest(f.parent.ID)
	if f.svc.AgentTurnMessageHeldForTest(f.parent.ID) {
		t.Fatal("the buffer outlived the process")
	}
	f.feed(doneTurnJSON)
	if got := f.previewOf(t, f.parent.ID); got != previewBefore {
		t.Fatalf("preview = %q", got)
	}
}

// The claim can also appear only at the end: the Invoke still owns the preview.
func TestAnInvokeClaimAtTheEndOfAnAgentTurnKeepsThePreviewWithInvoke(t *testing.T) {
	f := newIdleFixture(t)
	if err := f.store.UpdateSessionPreview(context.Background(), f.parent.ID, previewBefore, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.feed(spawnTurnJSON, chunkTwoA)
	release := f.svc.ClaimInvokeForTest(f.parent.ID)
	f.feed(doneTurnJSON)
	release()
	if got := f.previewOf(t, f.parent.ID); got != previewBefore {
		t.Fatalf("the agent turn wrote the preview under an Invoke's claim: %q", got)
	}
	if f.svc.AgentTurnMessageHeldForTest(f.parent.ID) {
		t.Fatal("the buffer outlived the turn")
	}
}
