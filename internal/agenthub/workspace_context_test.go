package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
	deleteWorkspace func(string)
}
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
	return agenthub.PromptOutcome{StopReason: "end_turn"}, nil
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
	a := &contextAdapter{MockAdapter: agenthub.NewMockAdapter()}
	a.deleteWorkspace = func(id string) {
		t.Helper()
		if _, err := db.Exec("DELETE FROM workspaces WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
	}
	reg := agenthub.NewRegistry()
	reg.Register(a)
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
