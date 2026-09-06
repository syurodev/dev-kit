package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newTestService(t *testing.T) *agenthub.Service {
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
	reg.Register(agenthub.NewMockAdapter())
	return agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
}

func TestCreateAndListSession(t *testing.T) {
	svc := newTestService(t)
	s, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status=%s", s.Status)
	}
	if s.ID == "" {
		t.Fatal("expected session id")
	}
	if s.ACPSessionID == "" {
		t.Fatal("expected acp session id")
	}
	if s.Depth != 0 {
		t.Fatalf("depth=%d want 0", s.Depth)
	}
	list, err := svc.ListSessions(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestCreateUnknownVendor(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: "nope", Cwd: t.TempDir()})
	if !errors.Is(err, agenthub.ErrVendorUnknown) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateChildIncrementsDepth(t *testing.T) {
	svc := newTestService(t)
	parent, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType:       agenthub.AgentTypeMock,
		Cwd:             t.TempDir(),
		ParentSessionID: parent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.Depth != 1 || child.ParentSessionID == nil || *child.ParentSessionID != parent.ID {
		t.Fatalf("%+v", child)
	}
}

func TestInvokeCompletesAndRecordsTurn(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" || out.Summary == "" {
		t.Fatalf("%+v", out)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("stopReason=%q", out.StopReason)
	}
	if out.TurnCount != 1 {
		t.Fatalf("turnCount=%d want 1", out.TurnCount)
	}
	st, err := svc.GetStatus(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status=%s", st.Status)
	}
}

func TestInvokeBusy(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	svc.ResolvePermission = func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`), nil
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{
			SessionID: sess.ID,
			Prompt:    "NEED_PERMISSION",
		})
		errCh <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("permission resolver not entered")
	}

	_, err = svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "second",
	})
	if !errors.Is(err, agenthub.ErrSessionBusy) {
		t.Fatalf("got %v want ErrSessionBusy", err)
	}

	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first invoke: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first invoke")
	}
}

func TestCancelDuringInvoke(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "BLOCK"})
		errCh <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		st, err := svc.GetStatus(context.Background(), sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status == agenthub.SessionStatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session never became running (status=%s)", st.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := svc.Cancel(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, agenthub.ErrCancelled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	st, err := svc.GetStatus(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != agenthub.SessionStatusIdle {
		t.Fatalf("status=%s want idle", st.Status)
	}
}

func TestInvokeCycleOnCrossSession(t *testing.T) {
	svc := newTestService(t)
	parent, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType:       agenthub.AgentTypeMock,
		Cwd:             t.TempDir(),
		ParentSessionID: parent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID:       parent.ID,
		Prompt:          "hello",
		CallerSessionID: child.ID,
	})
	if !errors.Is(err, agenthub.ErrCycleDetected) {
		t.Fatalf("got %v want ErrCycleDetected", err)
	}
}

func TestInvokeAppliesDefaultDeadline(t *testing.T) {
	prev := agenthub.DefaultInvokeTimeout
	agenthub.DefaultInvokeTimeout = 40 * time.Millisecond
	t.Cleanup(func() { agenthub.DefaultInvokeTimeout = prev })

	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// No caller deadline — Invoke must still bound the turn and cancel BLOCK.
	_, err = svc.Invoke(context.Background(), agenthub.InvokeInput{
		SessionID: sess.ID,
		Prompt:    "BLOCK",
	})
	if !errors.Is(err, agenthub.ErrCancelled) {
		t.Fatalf("got %v want ErrCancelled from default Invoke deadline", err)
	}
}

func TestDeleteSessionTakesTheSubtreeWithIt(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	parent, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType:       agenthub.AgentTypeMock,
		Cwd:             parent.ProjectPath,
		ParentSessionID: parent.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	other, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	if err := svc.DeleteSession(ctx, parent.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	list, err := svc.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != other.ID {
		// A child left behind would render as a root and silently reshape
		// the swarm; an unrelated session must survive untouched.
		t.Fatalf("remaining = %+v, want only %s", list, other.ID)
	}
	if _, err := svc.GetStatus(ctx, child.ID); err == nil {
		t.Fatal("child session still readable after parent delete")
	}
}

func TestDeleteSessionUnknownID(t *testing.T) {
	svc := newTestService(t)
	if err := svc.DeleteSession(context.Background(), "no-such-session"); !errors.Is(err, agenthub.ErrSessionNotFound) {
		t.Fatalf("err = %v, want agenthub.ErrSessionNotFound", err)
	}
}

// dyingRuntime reproduces the failure the desktop log showed: the agent dies,
// the caller's context is cancelled, and the update carrying the agent's own
// error message arrives after that. Recording it under the turn's context made
// that last update — the only one that says why — the one guaranteed to be lost.
type dyingRuntime struct {
	handlers agenthub.RuntimeHandlers
	cancel   context.CancelFunc
}

func (r *dyingRuntime) ACPSessionID() string                   { return "acp-dying" }
func (r *dyingRuntime) SetHandlers(h agenthub.RuntimeHandlers) { r.handlers = h }
func (r *dyingRuntime) Cancel(context.Context) error           { return nil }
func (r *dyingRuntime) Close(context.Context) error            { return nil }

func (r *dyingRuntime) Prompt(ctx context.Context, _ string) (agenthub.PromptOutcome, error) {
	r.cancel() // the process died; whoever was waiting on this turn gave up
	update, _ := json.Marshal(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "401 token revoked"},
	})
	if r.handlers.OnUpdate != nil {
		r.handlers.OnUpdate(update, nil)
	}
	return agenthub.PromptOutcome{}, ctx.Err()
}

type dyingAdapter struct{ rt *dyingRuntime }

func (a dyingAdapter) Type() agenthub.AgentType { return agenthub.AgentType("dying") }
func (a dyingAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a dyingAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, nil
}
func (a dyingAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a dyingAdapter) Extensions() []string { return nil }

func TestTranscriptSurvivesTheContextThatKilledTheTurn(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rt := &dyingRuntime{}
	reg := agenthub.NewRegistry()
	reg.Register(dyingAdapter{rt: rt})
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	var recordErr error
	svc.OnRecordError = func(_ string, err error) { recordErr = err }

	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentType("dying"),
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.cancel = cancel
	_, _ = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"})

	if recordErr != nil {
		t.Fatalf("transcript write failed: %v", recordErr)
	}
	entries, err := svc.Transcript(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	// Two rows: the prompt DevKit echoes in itself, then the agent's reply.
	// Both are written under the context the dead turn cannot cancel.
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want the user echo and the agent's error message", len(entries))
	}
	if entries[0].Kind != agenthub.UpdateUserMessageChunk || entries[0].Text != "go" {
		t.Fatalf("first entry = %+v, want the user's own prompt", entries[0])
	}
	if entries[1].Text != "401 token revoked" {
		t.Fatalf("second entry = %+v, want the agent's error message", entries[1])
	}

	// And the session must not be left saying "running" forever: the status
	// write happens on the same dead context.
	after, err := svc.GetStatus(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if after.Status == agenthub.SessionStatusRunning {
		t.Fatal("session stuck in running after the turn died")
	}
}

// titlingRuntime emits the session_info_update a real agent sends after the
// first exchange, so the test drives the same path production does rather than
// poking the store.
type titlingRuntime struct{ handlers agenthub.RuntimeHandlers }

func (r *titlingRuntime) ACPSessionID() string                   { return "acp-titling" }
func (r *titlingRuntime) SetHandlers(h agenthub.RuntimeHandlers) { r.handlers = h }
func (r *titlingRuntime) Cancel(context.Context) error           { return nil }
func (r *titlingRuntime) Close(context.Context) error            { return nil }

func (r *titlingRuntime) Prompt(context.Context, string) (agenthub.PromptOutcome, error) {
	if r.handlers.OnUpdate != nil {
		r.handlers.OnUpdate([]byte(`{"sessionUpdate":"session_info_update","title":"Dev-kit-backend"}`), nil)
	}
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}

type titlingAdapter struct{ rt *titlingRuntime }

func (a titlingAdapter) Type() agenthub.AgentType { return agenthub.AgentType("titling") }
func (a titlingAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a titlingAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, nil
}
func (a titlingAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a titlingAdapter) Extensions() []string { return nil }

func TestSessionTakesTheAgentsNameOnlyWhenUnnamed(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(titlingAdapter{rt: &titlingRuntime{}})
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	chosen := "my own name"
	for _, tc := range []struct {
		name  string
		title *string
		want  string
	}{
		{"unnamed takes the agent's", nil, "Dev-kit-backend"},
		// A name a person chose is not a guess to be overwritten as the
		// conversation drifts.
		{"named keeps the human's", &chosen, "my own name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
				AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir(), Title: tc.title,
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
				t.Fatalf("invoke: %v", err)
			}
			got, err := svc.GetStatus(ctx, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title == nil || *got.Title != tc.want {
				t.Fatalf("title = %v, want %q", got.Title, tc.want)
			}
		})
	}
}

// loadableAdapter is titlingAdapter plus SessionLoader, so a test can drive the
// orphan path without a real vendor.
type loadableAdapter struct {
	titlingAdapter
	loaded  chan string
	failNow bool
}

func (a *loadableAdapter) Load(_ context.Context, _ agenthub.OpenSpec, acpSessionID string) (agenthub.SessionRuntime, error) {
	if a.failNow {
		return nil, agenthub.ErrProcessFailed
	}
	a.loaded <- acpSessionID
	return a.rt, nil
}

func TestOrphanSessionResumesInsteadOfDyingThere(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	adapter := &loadableAdapter{
		titlingAdapter: titlingAdapter{rt: &titlingRuntime{}},
		loaded:         make(chan string, 1),
	}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	store := agenthub.NewSQLStore(db)
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A restart, as the store sees it: the row survives, every runtime is gone.
	// Before this change that was the end of the conversation — the only way
	// forward was a new session, which loses the context the agent still has.
	restarted := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	if _, err := restarted.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "still there?"}); err != nil {
		t.Fatalf("invoke after restart: %v", err)
	}
	select {
	case got := <-adapter.loaded:
		if got != sess.ACPSessionID {
			t.Fatalf("resumed %q, want the vendor id from the store %q", got, sess.ACPSessionID)
		}
	default:
		t.Fatal("session was not resumed")
	}
}

func TestOrphanSessionStaysUnavailableWithoutALoader(t *testing.T) {
	// The mock adapter has no SessionLoader. A vendor that cannot resume must
	// be exactly as available as before — the old behaviour is the floor.
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	store := agenthub.NewSQLStore(db)

	sess, err := agenthub.NewService(reg, store, nil).CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	restarted := agenthub.NewService(reg, store, nil)
	_, err = restarted.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"})
	if !errors.Is(err, agenthub.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// deletableRuntime records whether the vendor was asked to forget the session.
type deletableRuntime struct {
	titlingRuntime
	deleted chan struct{}
}

func (r *deletableRuntime) Delete(context.Context) error {
	close(r.deleted)
	return nil
}

func TestDeleteSessionAsksTheVendorToForgetItToo(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rt := &deletableRuntime{deleted: make(chan struct{})}
	reg := agenthub.NewRegistry()
	reg.Register(deletableAdapter{rt: rt})
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The confirmation says the transcript is deleted for good. Before this the
	// vendor kept its own copy on disk, so it was not.
	select {
	case <-rt.deleted:
	default:
		t.Fatal("vendor was never asked to delete its copy of the session")
	}
}

type deletableAdapter struct{ rt *deletableRuntime }

func (a deletableAdapter) Type() agenthub.AgentType { return agenthub.AgentType("titling") }
func (a deletableAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a deletableAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, nil
}
func (a deletableAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a deletableAdapter) Extensions() []string { return nil }
