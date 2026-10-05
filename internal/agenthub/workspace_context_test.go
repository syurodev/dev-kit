package agenthub_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

type contextAdapter struct {
	*agenthub.MockAdapter
	prompts         [][]acp.ContentBlock
	opens           int
	db              *sql.DB
	deleteWorkspace func(string)
	// failNext makes the next prompt fail after it was recorded, as a turn that
	// died on the vendor's side would.
	failNext bool
}
type typedContextAdapter struct {
	*contextAdapter
	vendor agenthub.AgentType
}

func (a *typedContextAdapter) Type() agenthub.AgentType { return a.vendor }

type contextRuntime struct {
	agenthub.SessionRuntime
	owner *contextAdapter
}

func (a *contextAdapter) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	a.opens++
	rt, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &contextRuntime{SessionRuntime: rt, owner: a}, nil
}
func (r *contextRuntime) Prompt(_ context.Context, blocks []acp.ContentBlock, _ json.RawMessage) (agenthub.PromptOutcome, error) {
	r.owner.prompts = append(r.owner.prompts, append([]acp.ContentBlock(nil), blocks...))
	if r.owner.failNext {
		r.owner.failNext = false
		return agenthub.PromptOutcome{}, errors.New("vendor died mid-turn")
	}
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
}

// Load makes the adapter resumable, so a closed session can be reopened.
func (a *contextAdapter) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	return a.Open(ctx, spec)
}
func contextService(t *testing.T) (*agenthub.Service, *contextAdapter) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err = db.Exec("INSERT INTO workspaces (id,name,root) VALUES (?,?,'')", id, id); err != nil {
			t.Fatal(err)
		}
	}
	a := &contextAdapter{MockAdapter: agenthub.NewMockAdapter(), db: db}
	a.deleteWorkspace = func(id string) {
		t.Helper()
		if _, err := db.Exec("DELETE FROM workspaces WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
	}
	reg := agenthub.NewRegistry()
	reg.Register(a)
	reg.Register(&typedContextAdapter{contextAdapter: a, vendor: agenthub.AgentTypeClaude})
	reg.Register(&typedContextAdapter{contextAdapter: a, vendor: agenthub.AgentTypeCodex})
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"a", "b"}, nil }
	return svc, a
}
func TestWorkspaceContextRefreshesEachTurnAndKeepsUserEcho(t *testing.T) {
	svc, adapter := contextService(t)
	instructions := "Use {{git.commit}} literally; first rules"
	brief := "primary folder: /first"
	svc.WorkspaceInstructions = func(id string) (string, error) {
		if id != "a" {
			t.Fatalf("workspace=%s", id)
		}
		return instructions, nil
	}
	svc.SessionBrief = func(id, cwd string, grants []string) string { return brief }
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() {
		t.Helper()
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "user request"}); err != nil {
			t.Fatal(err)
		}
	}
	invoke()
	instructions = "second rules"
	brief = "primary folder: /second"
	invoke()
	instructions = ""
	invoke()
	if adapter.opens != 1 {
		t.Fatalf("opened %d times", adapter.opens)
	}
	for i, p := range adapter.prompts {
		if len(p) != 2 || p[1].Text != "user request" {
			t.Fatalf("turn %d blocks=%+v", i, p)
		}
	}
	if !strings.Contains(adapter.prompts[0][0].Text, "{{git.commit}}") {
		t.Fatal("user instruction placeholder changed")
	}
	if got := adapter.prompts[1][0].Text; !strings.Contains(got, "second rules") || !strings.Contains(got, "/second") || strings.Contains(got, "first rules") {
		t.Fatal(got)
	}
	if got := adapter.prompts[2][0].Text; !strings.Contains(got, "No workspace instructions are configured") || !strings.Contains(got, "Retire any previously") {
		t.Fatal(got)
	}
	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("entries=%+v", page.Entries)
	}
	for _, entry := range page.Entries {
		if entry.Text != "user request" {
			t.Fatalf("echo=%q", entry.Text)
		}
	}
}
func TestWorkspaceContextUsesIdentityForOverlappingProjects(t *testing.T) {
	svc, adapter := contextService(t)
	svc.WorkspaceInstructions = func(id string) (string, error) { return "Rules for " + id, nil }
	ctx := context.Background()
	cwd := t.TempDir()
	for _, id := range []string{"a", "b"} {
		sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd, WorkspaceID: id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "work"}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(adapter.prompts[0][0].Text, "Rules for a") || strings.Contains(adapter.prompts[0][0].Text, "Rules for b") {
		t.Fatal(adapter.prompts[0])
	}
	if !strings.Contains(adapter.prompts[1][0].Text, "Rules for b") {
		t.Fatal(adapter.prompts[1])
	}
}
func TestWorkspaceContextReadFailureDoesNotStartOrRecordTurn(t *testing.T) {
	svc, adapter := contextService(t)
	failure := errors.New("cannot read instructions")
	svc.WorkspaceInstructions = func(string) (string, error) { return "", failure }
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "work"}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if len(adapter.prompts) != 0 {
		t.Fatal("runtime prompted")
	}
	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	state, err := svc.GetStatus(ctx, sess.ID)
	if err != nil || state.Status != agenthub.SessionStatusIdle {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestDeletedWorkspaceRetiresContextOnExistingRuntime(t *testing.T) {
	svc, adapter := contextService(t)
	calls := 0
	svc.WorkspaceInstructions = func(id string) (string, error) {
		calls++
		if id != "a" {
			t.Fatalf("id=%q", id)
		}
		return "Former workspace rules", nil
	}
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() {
		t.Helper()
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "continue"}); err != nil {
			t.Fatal(err)
		}
	}
	invoke()
	adapter.deleteWorkspace("a")
	invoke()
	if adapter.opens != 1 || calls != 1 {
		t.Fatalf("opens=%d getter calls=%d", adapter.opens, calls)
	}
	got := adapter.prompts[1][0].Text
	if !strings.Contains(got, "No workspace is selected") || strings.Contains(got, "Former workspace rules") {
		t.Fatal(got)
	}
}

// invokeTurns sends n prompts and reports, per turn, whether it carried the
// workspace context block (a first block ahead of the user's text).
func invokeTurns(t *testing.T, svc *agenthub.Service, adapter *contextAdapter, sessionID string, n int) []bool {
	t.Helper()
	from := len(adapter.prompts)
	for range n {
		if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: sessionID, Prompt: "user request"}); err != nil {
			t.Fatal(err)
		}
	}
	var carried []bool
	for _, p := range adapter.prompts[from:] {
		carried = append(carried, len(p) == 2)
		if p[len(p)-1].Text != "user request" {
			t.Fatalf("blocks=%+v", p)
		}
	}
	return carried
}

func TestUnchangedWorkspaceContextIsSentOnceThenOnChange(t *testing.T) {
	svc, adapter := contextService(t)
	instructions := "rules v1"
	svc.WorkspaceInstructions = func(string) (string, error) { return instructions, nil }
	memory := "- id-1 [decision] Use SQLite\n"
	svc.WorkspaceMemory = func(context.Context, string) (string, error) { return memory, nil }
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeTurns(t, svc, adapter, sess.ID, 3); !slices.Equal(got, []bool{true, false, false}) {
		t.Fatalf("unchanged turns carried=%v", got)
	}
	memory += "- id-2 [convention] Tabs\n"
	if got := invokeTurns(t, svc, adapter, sess.ID, 2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("after memory change carried=%v", got)
	}
	if !strings.Contains(adapter.prompts[3][0].Text, "id-2") {
		t.Fatal(adapter.prompts[3][0].Text)
	}
	instructions = "rules v2"
	if got := invokeTurns(t, svc, adapter, sess.ID, 2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("after instructions change carried=%v", got)
	}
}

func TestWorkspaceContextIsRefreshedAfterManyUnchangedTurns(t *testing.T) {
	svc, adapter := contextService(t)
	svc.WorkspaceInstructions = func(string) (string, error) { return "rules", nil }
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	got := invokeTurns(t, svc, adapter, sess.ID, agenthub.WorkspaceContextRefreshTurns+2)
	for i, carried := range got {
		want := i == 0 || i == agenthub.WorkspaceContextRefreshTurns+1
		if carried != want {
			t.Fatalf("turn %d carried=%v want %v (all=%v)", i, carried, want, got)
		}
	}
}

func TestWorkspaceContextIsResentAfterClosingTheSession(t *testing.T) {
	svc, adapter := contextService(t)
	svc.WorkspaceInstructions = func(string) (string, error) { return "rules", nil }
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeTurns(t, svc, adapter, sess.ID, 2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("carried=%v", got)
	}
	// Closing parks the process; the next prompt resumes a runtime whose
	// conversation the vendor reloads, and the agent is told again rather than
	// trusting what that history still holds.
	if err = svc.CloseSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := invokeTurns(t, svc, adapter, sess.ID, 2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("after close carried=%v", got)
	}
	if adapter.opens != 2 {
		t.Fatalf("opens=%d, want the session resumed once", adapter.opens)
	}
}

func TestWorkspaceContextIsResentAfterAFailedTurn(t *testing.T) {
	svc, adapter := contextService(t)
	instructions := "rules v1"
	svc.WorkspaceInstructions = func(string) (string, error) { return instructions, nil }
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeTurns(t, svc, adapter, sess.ID, 1); !slices.Equal(got, []bool{true}) {
		t.Fatalf("carried=%v", got)
	}
	// The turn that was to carry the change fails: the agent may never have
	// seen it, so it must not count as delivered.
	instructions = "rules v2"
	adapter.failNext = true
	if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "user request"}); err == nil {
		t.Fatal("the failing turn succeeded")
	}
	if got := invokeTurns(t, svc, adapter, sess.ID, 2); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("after failed turn carried=%v", got)
	}
	if !strings.Contains(adapter.prompts[len(adapter.prompts)-2][0].Text, "rules v2") {
		t.Fatal(adapter.prompts[len(adapter.prompts)-2])
	}
}
