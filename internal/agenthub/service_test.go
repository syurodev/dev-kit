package agenthub_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
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

// newTestServiceWith is newTestService with the caller's own mock adapter, for
// a test that has to look at the runtime the service opened.
func newTestServiceWith(t *testing.T, adapter *agenthub.MockAdapter) *agenthub.Service {
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
	return agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
}

// A workspace's services do not necessarily share a root Cwd could cover
// (workspace.mdx), so picking "the whole workspace" in the draft's picker
// sends the other services as ExtraReadWritePaths. This is the UI (bridge)
// path — an unattributed caller defaults to identity.UI() (gateway.go).
func TestCreateSession_ExtraReadWritePathsReachTheSpec(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)
	cwd := t.TempDir()
	extra1, extra2 := t.TempDir(), t.TempDir()

	if _, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType:           agenthub.AgentTypeMock,
		Cwd:                 cwd,
		ExtraReadWritePaths: []string{extra1, extra2, extra1}, // a duplicate, on purpose
	}); err != nil {
		t.Fatal(err)
	}

	got := adapter.LastSpec().ExtraReadWritePaths
	if len(got) != 2 {
		t.Fatalf("ExtraReadWritePaths = %v, want [%s %s] (deduped)", got, extra1, extra2)
	}
	want := map[string]bool{extra1: true, extra2: true}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected path in spec: %s", p)
		}
	}
}

// "/" and the bare home directory are the same invariants resolveCwd holds
// Cwd to — a workspace service is never either of those in practice, but the
// jail should not trust that rather than check it.
func TestCreateSession_ExtraReadWritePathsDropsUnsafeEntries(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}

	if _, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType:           agenthub.AgentTypeMock,
		Cwd:                 t.TempDir(),
		ExtraReadWritePaths: []string{"/", home, "relative/path", ""},
	}); err != nil {
		t.Fatal(err)
	}

	if got := adapter.LastSpec().ExtraReadWritePaths; len(got) != 0 {
		t.Fatalf("ExtraReadWritePaths = %v, want none of the unsafe entries kept", got)
	}
}

// An agent spawning a session — a sub-agent, or one it opens directly — must
// never come out with a wider jail than a human choosing this in the UI
// would grant it. The MCP tool handler's own input struct has no field for
// this (tools_agenthub.go), so in practice an agent cannot reach it; this
// checks the belt on top of that structural suspenders.
func TestCreateSession_AgentCallerExtraReadWritePathsAreIgnored(t *testing.T) {
	adapter := agenthub.NewMockAdapter()
	svc := newTestServiceWith(t, adapter)
	parent, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx := identity.WithCaller(context.Background(), identity.Agent(parent.ID))

	if _, err := svc.CreateSession(agentCtx, agenthub.CreateInput{
		AgentType:           agenthub.AgentTypeMock,
		ParentSessionID:     parent.ID,
		ExtraReadWritePaths: []string{t.TempDir()},
	}); err != nil {
		t.Fatal(err)
	}

	if got := adapter.LastSpec().ExtraReadWritePaths; len(got) != 0 {
		t.Fatalf("ExtraReadWritePaths = %v, want none — an agent caller must not be able to set this", got)
	}
}

// The jail is rebuilt from the stored row on every resume: a session created
// with the whole workspace used to come back after a restart with RW on its
// first service only, because the grant lived in the first OpenSpec and
// nowhere else.
func TestResume_KeepsTheExtraReadWritePathsTheSessionWasCreatedWith(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	adapter := resumableMock{agenthub.NewMockAdapter()}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	extra1, extra2 := t.TempDir(), t.TempDir()
	sess, err := agenthub.NewService(reg, store, nil).CreateSession(ctx, agenthub.CreateInput{
		AgentType:           agenthub.AgentTypeMock,
		Cwd:                 t.TempDir(),
		ExtraReadWritePaths: []string{extra1, extra2},
	})
	if err != nil {
		t.Fatal(err)
	}

	restarted := agenthub.NewService(reg, store, nil)
	if err := restarted.SetMode(ctx, sess.ID, "plan"); err != nil {
		t.Fatalf("set mode after restart (resumes): %v", err)
	}
	got := adapter.LastSpec().ExtraReadWritePaths
	if len(got) != 2 || got[0] != extra1 || got[1] != extra2 {
		t.Fatalf("resumed ExtraReadWritePaths = %v, want [%s %s]", got, extra1, extra2)
	}
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

func TestSetModeAndConfigOptionUseRuntime(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	adapter := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMode(context.Background(), sess.ID, "plan"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetConfigOption(context.Background(), sess.ID, "model", "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := adapter.LastRuntime(); got == nil || got.SetModeValue() != "plan" || got.SetOptionValue("model") != "sonnet" {
		t.Fatalf("runtime did not receive controls: %+v", got)
	}
}

func TestRenameSessionPersistsTitle(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameSession(context.Background(), sess.ID, "Daily agent"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetStatus(context.Background(), sess.ID)
	if err != nil || got.Title == nil || *got.Title != "Daily agent" {
		t.Fatalf("title = %#v, err = %v", got.Title, err)
	}
}

func TestListContextFilesIsBoundedAndSkipsBuildTrees(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 120; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%03d.ts", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "hidden.ts"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := svc.ListContextFiles(context.Background(), sess.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 100 {
		t.Fatalf("got %d suggestions, want 100", len(files))
	}
	for _, file := range files {
		if filepath.Base(filepath.Dir(file.Path)) == "node_modules" {
			t.Fatalf("build tree leaked into suggestions: %s", file.Path)
		}
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

func (r *dyingRuntime) Prompt(ctx context.Context, _ []acp.ContentBlock, _ json.RawMessage) (agenthub.PromptOutcome, error) {
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
	page, err := svc.TranscriptPage(context.Background(), sess.ID, 0, 0)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	entries := page.Entries
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

func (r *titlingRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
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

func TestRestartResetsStaleMidTurnSessionsToIdle(t *testing.T) {
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

	svc := agenthub.NewService(reg, store, nil)
	running, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	awaiting, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The app died mid-turn: both rows still claim a turn is in flight, and
	// the processes that could finish those turns died with the app — no ACP
	// process survives a restart.
	running.Status = agenthub.SessionStatusRunning
	if err := store.UpdateSession(ctx, running); err != nil {
		t.Fatal(err)
	}
	awaiting.Status = agenthub.SessionStatusAwaitingPermission
	if err := store.UpdateSession(ctx, awaiting); err != nil {
		t.Fatal(err)
	}

	restarted := agenthub.NewService(reg, store, nil)
	if err := restarted.RecoverStaleSessions(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	for _, sess := range []agenthub.AgentSession{running, awaiting} {
		after, err := restarted.GetStatus(ctx, sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != agenthub.SessionStatusIdle {
			t.Fatalf("session %s status=%s, want idle", sess.ID, after.Status)
		}
	}

	// Without the reset this was ErrSessionBusy forever: the row said a turn
	// was in flight and nothing that could finish it existed anymore.
	if _, err := restarted.Invoke(ctx, agenthub.InvokeInput{
		SessionID: running.ID, Prompt: "still there?",
	}); err != nil {
		t.Fatalf("invoke after restart: %v", err)
	}
	select {
	case got := <-adapter.loaded:
		if got != running.ACPSessionID {
			t.Fatalf("resumed %q, want the vendor id from the store %q", got, running.ACPSessionID)
		}
	default:
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

// crashingRuntime stands in for a Claude process that died between turns:
// Dead() reports it, and Prompt refuses the way a closed pipe does.
type crashingRuntime struct {
	titlingRuntime
	dead bool
}

func (r *crashingRuntime) Dead() (bool, error) { return r.dead, nil }

func (r *crashingRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, _ json.RawMessage) (agenthub.PromptOutcome, error) {
	if r.dead {
		return agenthub.PromptOutcome{}, agenthub.ErrSessionNotFound
	}
	return r.titlingRuntime.Prompt(ctx, prompt, nil)
}

// crashResumeAdapter is a crashingRuntime plus SessionLoader: the vendor can
// resume, so the test drives the replace-on-death path the real adapter takes.
type crashResumeAdapter struct {
	rt    *crashingRuntime
	loads chan string
}

func (a *crashResumeAdapter) Type() agenthub.AgentType { return agenthub.AgentType("crashy") }
func (a *crashResumeAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a *crashResumeAdapter) Load(_ context.Context, _ agenthub.OpenSpec, acpSessionID string) (agenthub.SessionRuntime, error) {
	a.loads <- acpSessionID
	return &titlingRuntime{}, nil
}
func (a *crashResumeAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	return agenthub.NormalizedUsage{}, nil
}
func (a *crashResumeAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a *crashResumeAdapter) Extensions() []string { return nil }

func TestInvokeReplacesADeadRuntimeInsteadOfReusingIt(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	adapter := &crashResumeAdapter{
		rt:    &crashingRuntime{},
		loads: make(chan string, 1),
	}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("crashy"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The agent process crashed after its last turn. The runtime stays
	// registered — only CloseSession ever removed one — so every retry used
	// to prompt the corpse and fail with the crash's error, forever.
	adapter.rt.dead = true

	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "retry"}); err != nil {
		t.Fatalf("invoke after crash: %v", err)
	}
	select {
	case got := <-adapter.loads:
		if got != sess.ACPSessionID {
			t.Fatalf("resumed %q, want the vendor id from the store %q", got, sess.ACPSessionID)
		}
	default:
		t.Fatal("dead runtime was never replaced via session/load")
	}
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

func TestServiceTranscriptPageClampsAndAuthorizes(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Drive a couple of real turns so the rows carry the recorder's shapes.
	for _, prompt := range []string{"one", "two", "three"} {
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: prompt}); err != nil {
			t.Fatalf("invoke %q: %v", prompt, err)
		}
	}

	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) == 0 || page.Entries[len(page.Entries)-1].Seq == 0 {
		t.Fatalf("page = %+v, want entries carrying seq", page.Entries)
	}
	if !page.HasMore {
		t.Fatal("hasMore = false with three turns of history above")
	}
	// The page ends on the newest row and is oldest-first.
	if page.Entries[0].Seq >= page.Entries[len(page.Entries)-1].Seq {
		t.Fatalf("page not oldest-first: %v", page.Entries)
	}

	// limit <= 0 falls back to the default page size rather than an unbounded
	// listing — that unbounded call is exactly what this API exists to retire.
	page, err = svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) == 0 {
		t.Fatal("default-size page came back empty")
	}

	// Walking with the oldest seq eventually ends with hasMore=false.
	before := int64(0)
	for range 10 {
		page, err = svc.TranscriptPage(ctx, sess.ID, before, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) == 0 {
			t.Fatal("walk ended before reaching the start of the transcript")
		}
		before = page.Entries[0].Seq
		if !page.HasMore {
			break
		}
	}
	if page.HasMore {
		t.Fatal("walk did not terminate")
	}

	// Same descendant rule as every other read: an agent may not page through
	// a session outside its subtree.
	other, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx := identity.WithCaller(ctx, identity.Agent(other.ID))
	if _, err := svc.TranscriptPage(agentCtx, sess.ID, 0, 2); !errors.Is(err, agenthub.ErrNotDescendant) {
		t.Fatalf("err = %v, want ErrNotDescendant", err)
	}
}

func TestServiceToolCallDetailReturnsTheStoredRow(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	call, err := json.Marshal(agenthub.SessionUpdate{
		Kind: agenthub.UpdateToolCall,
		ToolCall: &agenthub.ToolCall{
			ID:    "tool-1",
			Title: "Read file",
			Kind:  "read",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:         "e1",
		SessionID:  sess.ID,
		Seq:        1,
		Kind:       agenthub.UpdateToolCall,
		ToolCallID: "tool-1",
		Payload:    call,
	}); err != nil {
		t.Fatal(err)
	}

	entry, err := svc.ToolCallDetail(ctx, sess.ID, "tool-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ToolCallID != "tool-1" || entry.ID != "e1" {
		t.Fatalf("entry = %+v, want the stored tool-1 row", entry)
	}
	if _, err := svc.ToolCallDetail(ctx, sess.ID, "no-such-call"); !errors.Is(err, agenthub.ErrTranscriptEntryNotFound) {
		t.Fatalf("err = %v, want ErrTranscriptEntryNotFound", err)
	}
}

// stallingRuntime streams part of a reply and then waits, the way a turn is
// still running when the user quits.
type stallingRuntime struct {
	titlingRuntime
	streamed chan struct{}
	release  chan struct{}
}

func (r *stallingRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	for _, token := range []string{"hello ", "wor", "ld"} {
		r.handlers.OnUpdate([]byte(`{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"`+token+`"}}`), nil)
	}
	close(r.streamed)
	<-r.release
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}

type stallingAdapter struct {
	titlingAdapter
	rt *stallingRuntime
}

func (a stallingAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}

// Quitting mid-stream used to lose whatever the recorder still held: nothing
// on the quit path flushed it before the database closed.
func TestShutdownLandsTheTailOfAStreamingTurn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rt := &stallingRuntime{streamed: make(chan struct{}), release: make(chan struct{})}
	reg := agenthub.NewRegistry()
	reg.Register(stallingAdapter{rt: rt})
	svc := agenthub.NewService(reg, store, nil)

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"})
	}()
	t.Cleanup(func() { close(rt.release); <-done })
	<-rt.streamed

	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListTranscript(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reply string
	for _, row := range rows {
		if row.Kind == agenthub.UpdateAgentMessageChunk {
			reply = row.Text
		}
	}
	if reply != "hello world" {
		t.Fatalf("reply on disk after shutdown = %q, want the whole streamed text", reply)
	}
}

// Opening a session mid-stream read the rows as they sat in SQLite, without
// the tail the recorder was still holding — the page showed a reply cut off
// at its first chunk until the next cadence, and live events only continued
// from there.
func TestTranscriptPageIncludesTheUnflushedTail(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	rt := &stallingRuntime{streamed: make(chan struct{}), release: make(chan struct{})}
	reg := agenthub.NewRegistry()
	reg.Register(stallingAdapter{rt: rt})
	svc := agenthub.NewService(reg, store, nil)

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"})
	}()
	t.Cleanup(func() { close(rt.release); <-done })
	<-rt.streamed

	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reply string
	for _, entry := range page.Entries {
		if entry.Kind == agenthub.UpdateAgentMessageChunk {
			reply = entry.Text
		}
	}
	if reply != "hello world" {
		t.Fatalf("reply on the newest page = %q, want everything streamed so far", reply)
	}
}

// Mode, config options, commands and usage are upserted in place, so their row
// keeps the seq where the kind first appeared. A session longer than one page
// reopened without them: no mode picker, no model picker, no command menu.
func TestNewestTranscriptPageCarriesSessionStateFromAboveTheWindow(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	sid := seedSession(t, store)
	svc := agenthub.NewService(agenthub.NewRegistry(), store, nil)

	rows := 0
	appendRow := func(kind agenthub.UpdateKind, payload string) {
		t.Helper()
		rows++
		if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
			ID: fmt.Sprintf("row-%d", rows), SessionID: sid, Kind: kind, Payload: json.RawMessage(payload),
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendRow(agenthub.UpdateUserMessageChunk, `{"kind":"user_message_chunk"}`)
	appendRow(agenthub.UpdateAgentMessageChunk, `{"kind":"agent_message_chunk"}`)
	appendRow(agenthub.UpdateCurrentMode, `{"kind":"current_mode_update","mode":"plan"}`)
	appendRow(agenthub.UpdateConfigOptions, `{"kind":"config_option_update","configOptions":[{"id":"model"}]}`)
	appendRow(agenthub.UpdateAvailableCommands, `{"kind":"available_commands_update","commands":[{"name":"review"}]}`)
	for i := 0; i < 500; i++ {
		kind := agenthub.UpdateAgentMessageChunk
		if i%2 == 0 {
			kind = agenthub.UpdateUserMessageChunk
		}
		appendRow(kind, fmt.Sprintf(`{"kind":%q,"n":%d}`, kind, i))
	}
	// Usage last: inside the window, so it must not be repeated in State.
	appendRow(agenthub.UpdateUsage, `{"kind":"usage_update","usage":{"used":1,"size":2}}`)

	page, err := svc.TranscriptPage(ctx, sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore {
		t.Fatal("precondition: the session must be longer than one page")
	}
	got := map[agenthub.UpdateKind]int{}
	for _, entry := range page.State {
		got[entry.Kind]++
	}
	for _, kind := range []agenthub.UpdateKind{agenthub.UpdateCurrentMode, agenthub.UpdateConfigOptions, agenthub.UpdateAvailableCommands} {
		if got[kind] != 1 {
			t.Fatalf("newest page state = %v, want one %s row", got, kind)
		}
	}
	if got[agenthub.UpdateUsage] != 0 {
		t.Fatalf("usage is already in the window but was repeated in state: %v", got)
	}

	older, err := svc.TranscriptPage(ctx, sid, page.Entries[0].Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.State) != 0 {
		t.Fatalf("an older page carried state %v; only the newest page does", older.State)
	}
}

// blockingLoadAdapter opens a runtime that is already dead and resumes slowly,
// so a second prompt can land while the first is still resuming.
type blockingLoadAdapter struct {
	titlingAdapter
	loads   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (a *blockingLoadAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return &crashingRuntime{dead: true}, nil
}

func (a *blockingLoadAdapter) Load(context.Context, agenthub.OpenSpec, string) (agenthub.SessionRuntime, error) {
	a.loads.Add(1)
	a.started <- struct{}{}
	<-a.release
	return &titlingRuntime{}, nil
}

// The busy claim used to come after the resume, so two prompts on a dead
// session both resumed it — two processes onto one MCP socket path.
func TestConcurrentInvokesOnADeadRuntimeResumeOnce(t *testing.T) {
	ctx := context.Background()
	adapter := &blockingLoadAdapter{started: make(chan struct{}, 2), release: make(chan struct{})}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, newTestStore(t), nil)
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentType("titling"), Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	first := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "one"})
		first <- err
	}()
	<-adapter.started

	second := make(chan error, 1)
	go func() {
		_, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "two"})
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, agenthub.ErrSessionBusy) {
			close(adapter.release)
			t.Fatalf("second invoke err = %v, want ErrSessionBusy", err)
		}
	case <-time.After(2 * time.Second):
		close(adapter.release)
		t.Fatal("second invoke did not come back busy; it went on to resume the session again")
	}
	close(adapter.release)
	if err := <-first; err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	if n := adapter.loads.Load(); n != 1 {
		t.Fatalf("resumed %d times, want once", n)
	}
}

// resumableMock is the mock adapter plus SessionLoader; its runtime records the
// mode and options it is set to.
type resumableMock struct{ *agenthub.MockAdapter }

func (a resumableMock) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	return a.Open(ctx, spec)
}

// After a restart no session has a process, and the settings calls only read
// the live map: the first model or mode change in a reopened session was
// refused as "agent does not support session mode".
func TestSettingsResumeTheAgentAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	adapter := resumableMock{agenthub.NewMockAdapter()}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	sess, err := agenthub.NewService(reg, store, nil).CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	restarted := agenthub.NewService(reg, store, nil)
	if err := restarted.SetMode(ctx, sess.ID, "plan"); err != nil {
		t.Fatalf("set mode after restart: %v", err)
	}
	resumed := adapter.LastRuntime()
	if got := resumed.SetModeValue(); got != "plan" {
		t.Fatalf("resumed runtime mode = %q, want plan", got)
	}
	if err := restarted.SetConfigOption(ctx, sess.ID, "model", "opus"); err != nil {
		t.Fatalf("set config option: %v", err)
	}
	if adapter.LastRuntime() != resumed {
		t.Fatal("second setting resumed the session again instead of using the live runtime")
	}
	if got := resumed.SetOptionValue("model"); got != "opus" {
		t.Fatalf("model = %q, want opus", got)
	}

	for name, err := range map[string]error{
		"blank mode":      restarted.SetMode(ctx, sess.ID, "  "),
		"blank option id": restarted.SetConfigOption(ctx, sess.ID, "", "opus"),
	} {
		if !errors.Is(err, agenthub.ErrInvalidConfigOption) {
			t.Fatalf("%s: err = %v, want ErrInvalidConfigOption", name, err)
		}
	}
}

// A runtime resumed by a settings call had no update handler until the next
// prompt, so the config_option_update the agent answers with was neither
// recorded nor shown: the picker kept the old model until something else ran.
func TestSettingsUpdateAfterResumeIsRecordedAndEmittedOnce(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	adapter := resumableMock{agenthub.NewMockAdapter()}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	sess, err := agenthub.NewService(reg, store, nil).CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	emitted := map[agenthub.UpdateKind]int{}
	restarted := agenthub.NewService(reg, store, func(_ string, update json.RawMessage) {
		mu.Lock()
		emitted[agenthub.ParseSessionUpdate(update).Kind]++
		mu.Unlock()
	})
	emittedCount := func(kind agenthub.UpdateKind) int {
		mu.Lock()
		defer mu.Unlock()
		return emitted[kind]
	}
	if err := restarted.SetConfigOption(ctx, sess.ID, "model", "opus"); err != nil {
		t.Fatalf("set config option: %v", err)
	}
	if n := emittedCount(agenthub.UpdateConfigOptions); n != 1 {
		t.Fatalf("config_option_update emitted %d times, want once", n)
	}
	page, err := restarted.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var model string
	for _, entry := range append(page.State, page.Entries...) {
		if entry.Kind != agenthub.UpdateConfigOptions {
			continue
		}
		var stored agenthub.SessionUpdate
		if err := json.Unmarshal(entry.Payload, &stored); err != nil || len(stored.ConfigOptions) != 1 {
			t.Fatalf("stored config options = %s (%v)", entry.Payload, err)
		}
		model = string(stored.ConfigOptions[0].CurrentValue)
	}
	if model != `"opus"` {
		t.Fatalf("recorded model = %s, want the agent's answer to the resume's setting", model)
	}

	if _, err := restarted.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hi"}); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if n := emittedCount(agenthub.UpdateAgentMessageChunk); n != 1 {
		t.Fatalf("reply emitted %d times, want once", n)
	}
	page, err = restarted.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Entries {
		if entry.Kind == agenthub.UpdateAgentMessageChunk && entry.Text != "mock reply" {
			t.Fatalf("reply recorded as %q, want it recorded once", entry.Text)
		}
	}
}

// modesRuntime answers session/new with a mode set, as claude-agent-acp does,
// and answers set_mode with the bare current_mode_update the agent sends.
type modesRuntime struct{ titlingRuntime }

func (r *modesRuntime) Modes() *acp.SessionModeState {
	return &acp.SessionModeState{CurrentModeID: "default", AvailableModes: []acp.SessionMode{
		{ID: "default", Name: "Default"},
		{ID: "plan", Name: "Plan", Description: "Read only"},
	}}
}

func (r *modesRuntime) SetMode(_ context.Context, modeID string) error {
	r.handlers.OnUpdate([]byte(`{"sessionUpdate":"current_mode_update","currentModeId":"`+modeID+`"}`), nil)
	return nil
}

func (r *modesRuntime) SetConfigOption(context.Context, string, string) error { return nil }

type modesAdapter struct{ titlingAdapter }

func (modesAdapter) Type() agenthub.AgentType { return agenthub.AgentType("modes") }
func (modesAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return &modesRuntime{}, nil
}

// The modes an agent advertises came back from session/new and were dropped
// there, so the picker could offer only the mode the session was already in.
func TestAdvertisedModesBecomeSessionState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	reg := agenthub.NewRegistry()
	reg.Register(modesAdapter{})
	var mu sync.Mutex
	var emitted []agenthub.SessionUpdate
	svc := agenthub.NewService(reg, store, func(_ string, update json.RawMessage) {
		mu.Lock()
		emitted = append(emitted, agenthub.ParseSessionUpdate(update))
		mu.Unlock()
	})
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentType("modes"), Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	modeState := func() agenthub.SessionUpdate {
		t.Helper()
		page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range append(page.State, page.Entries...) {
			if entry.Kind == agenthub.UpdateCurrentMode {
				var stored agenthub.SessionUpdate
				if err := json.Unmarshal(entry.Payload, &stored); err != nil {
					t.Fatal(err)
				}
				return stored
			}
		}
		t.Fatal("no mode state recorded")
		return agenthub.SessionUpdate{}
	}

	got := modeState()
	if got.Mode != "default" || len(got.AvailableModes) != 2 || got.AvailableModes[1] != (acp.SessionMode{ID: "plan", Name: "Plan", Description: "Read only"}) {
		t.Fatalf("recorded mode state = %+v", got)
	}
	mu.Lock()
	if len(emitted) != 1 || emitted[0].Kind != agenthub.UpdateCurrentMode || len(emitted[0].AvailableModes) != 2 {
		t.Fatalf("emitted %+v, want the mode set once", emitted)
	}
	mu.Unlock()

	if err := svc.SetMode(ctx, sess.ID, "plan"); err != nil {
		t.Fatal(err)
	}
	// The agent's update names only the new mode; the list must survive it.
	if got := modeState(); got.Mode != "plan" || len(got.AvailableModes) != 2 {
		t.Fatalf("after current_mode_update, mode state = %+v", got)
	}
}

func TestSettingsOnAnOrphanThatCannotResumeSayUnavailable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	sess, err := agenthub.NewService(reg, store, nil).CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := agenthub.NewService(reg, store, nil).SetMode(ctx, sess.ID, "plan"); !errors.Is(err, agenthub.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable rather than a claim the agent has no modes", err)
	}
}

// countingContext counts Err calls. ListContextFiles checks the context once
// per entry the walk visits, so the count is how much of the tree it read.
type countingContext struct {
	context.Context
	errCalls atomic.Int64
}

func (c *countingContext) Err() error {
	c.errCalls.Add(1)
	return c.Context.Err()
}

func contextFilesService(t *testing.T, root string) (*agenthub.Service, string) {
	t.Helper()
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, newTestStore(t), nil)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	return svc, sess.ID
}

// The cap returned SkipDir from a file, which skips only the rest of that
// directory: past 100 suggestions the walk still read every folder in the
// project.
func TestListContextFilesStopsWalkingAtTheCap(t *testing.T) {
	root := t.TempDir()
	mkfile := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 120 files reach the cap first; then 150 folders of folders. SkipDir from
	// a file skips only that file's siblings, so every folder was still opened.
	for f := 0; f < 120; f++ {
		mkfile(filepath.Join(root, "a-files", fmt.Sprintf("f-%03d.ts", f)))
	}
	for d := 0; d < 150; d++ {
		for s := 0; s < 3; s++ {
			mkfile(filepath.Join(root, fmt.Sprintf("b-%03d", d), fmt.Sprintf("s%d", s), "f.ts"))
		}
	}
	svc, id := contextFilesService(t, root)

	ctx := &countingContext{Context: context.Background()}
	files, err := svc.ListContextFiles(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 100 {
		t.Fatalf("got %d suggestions, want 100", len(files))
	}
	// ~103 entries up to the cap, plus the store's own context checks; the
	// whole tree is over 1,300.
	if n := ctx.errCalls.Load(); n > 500 {
		t.Fatalf("walk visited ~%d entries for 100 suggestions; it did not stop at the cap", n)
	}
}

// A walk error in one subdirectory used to fail the whole listing.
func TestListContextFilesSkipsAnUnreadableFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a directory unreadable on Windows")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "secret.ts"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readable.ts"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("directory permissions are not enforced here (running as root?)")
	}
	svc, id := contextFilesService(t, root)

	files, err := svc.ListContextFiles(context.Background(), id, "")
	if err != nil {
		t.Fatalf("one unreadable folder failed the whole listing: %v", err)
	}
	if len(files) != 1 || files[0].Name != "readable.ts" {
		t.Fatalf("files = %+v, want the readable file", files)
	}
}

// TestAClosedSessionReopensOnTheNextPrompt pins what Close means now.
//
// It used to mean "gone": Invoke answered ErrSessionNotFound for a closed
// session, so the button the error card put next to Retry ended the
// conversation for good. Closing parks the process; the row still carries the
// vendor's session id, and resuming it is the same path a restart takes.
func TestAClosedSessionReopensOnTheNextPrompt(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	adapter := &crashResumeAdapter{rt: &crashingRuntime{}, loads: make(chan string, 1)}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("crashy"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "carry on"}); err != nil {
		t.Fatalf("prompting a closed session: %v", err)
	}
	select {
	case got := <-adapter.loads:
		if got != sess.ACPSessionID {
			t.Fatalf("resumed %q, want the vendor id from the store %q", got, sess.ACPSessionID)
		}
	default:
		t.Fatal("a closed session must reopen through session/load, not stay closed")
	}

	after, err := svc.GetStatus(ctx, sess.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if after.Status == agenthub.SessionStatusClosed {
		t.Fatal("a session that just answered a prompt is not closed any more")
	}
}

// A settings change does not wake a parked process — and says so, rather than
// claiming the session does not exist.
func TestSettingsOnAClosedSessionSayItIsClosed(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	adapter := &crashResumeAdapter{rt: &crashingRuntime{}, loads: make(chan string, 1)}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentType("crashy"), Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	err = svc.SetMode(ctx, sess.ID, "plan")
	if !errors.Is(err, agenthub.ErrSessionClosed) {
		t.Fatalf("SetMode on a closed session = %v, want ErrSessionClosed", err)
	}
	if len(adapter.loads) != 0 {
		t.Fatal("a settings call must not spawn a process for a session nobody is using")
	}
}

func TestEveryPromptStartsATurnUnderOneName(t *testing.T) {
	// A prompt of only an image used to leave no echo: its reply folded into
	// the turn before, and nothing named the turn a rewind would go back to.
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
	store := agenthub.NewSQLStore(db)
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	image := agenthub.ImageAttachment{Name: "screen.png", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("png bytes"))}
	if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Images: []agenthub.ImageAttachment{image}}); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if len(page.Entries) == 0 || page.Entries[0].Kind != agenthub.UpdateUserMessageChunk {
		t.Fatalf("entries = %+v, want the prompt's echo first", page.Entries)
	}
	var echo agenthub.SessionUpdate
	if err := json.Unmarshal(page.Entries[0].Payload, &echo); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if echo.MessageID == "" || len(echo.Attachments) != 1 || echo.Attachments[0] != (agenthub.PromptAttachment{Name: "screen.png", Kind: "image"}) {
		t.Fatalf("echo = %+v, want a message id and the image named", echo)
	}

	turns, err := store.ListTurns(ctx, sess.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %v, %v", turns, err)
	}
	if turns[0].PromptMessageID != echo.MessageID {
		t.Fatalf("turn prompt id = %q, echo id = %q: one turn, two names", turns[0].PromptMessageID, echo.MessageID)
	}
}
