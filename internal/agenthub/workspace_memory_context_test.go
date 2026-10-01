package agenthub_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/workspacememory"
)

// Vendor-shaped test runtimes share one SQL store; the wire tests separately
// verify Claude and Codex preserve the supplied context.
func TestWorkspaceMemoryRefreshesAcrossVendorSessions(t *testing.T) {
	svc, adapter := contextService(t)
	memory := workspacememory.NewService(workspacememory.NewSQLRepository(adapter.db))
	svc.WorkspaceMemory = memory.Context
	ctx := context.Background()
	cwd := t.TempDir()
	claude, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: cwd, WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	codex, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeCodex, Cwd: cwd, WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeCodex, Cwd: cwd, WorkspaceID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	input := workspacememory.Input{Kind: "decision", Title: "API compatibility", Summary: "Keep contract v1", Body: "Detailed private body omitted from context."}
	entry, err := memory.Create(ctx, "a", input, workspacememory.Source{Caller: "agent:" + claude.ID, SessionID: claude.ID, AgentType: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(id string) string {
		t.Helper()
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: id, Prompt: "continue work"}); err != nil {
			t.Fatal(err)
		}
		return adapter.prompts[len(adapter.prompts)-1][0].Text
	}
	text := invoke(codex.ID)
	if !strings.Contains(text, "Keep contract v1") || strings.Contains(text, input.Body) || !strings.Contains(text, "not instructions or permission") {
		t.Fatal(text)
	}
	input.Summary = "Use contract v2"
	entry, err = memory.Update(ctx, "a", entry.ID, entry.Revision, input, workspacememory.Source{Caller: "agent:" + codex.ID, SessionID: codex.ID, AgentType: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	text = invoke(claude.ID)
	if !strings.Contains(text, "Use contract v2") || strings.Contains(text, "Keep contract v1") {
		t.Fatal(text)
	}
	text = invoke(other.ID)
	if strings.Contains(text, "Use contract v2") || strings.Contains(text, "API compatibility") {
		t.Fatal("other workspace received shared project's memory")
	}
	if _, err = memory.Delete(ctx, "a", entry.ID, entry.Revision, workspacememory.Source{Caller: "user-terminal"}); err != nil {
		t.Fatal(err)
	}
	text = invoke(codex.ID)
	if strings.Contains(text, "Use contract v2") {
		t.Fatal("deleted memory remained in context")
	}
	if adapter.opens != 3 {
		t.Fatalf("opened %d runtimes", adapter.opens)
	}
}

func TestWorkspaceMemoryReadFailureDoesNotSendUserPrompt(t *testing.T) {
	svc, adapter := contextService(t)
	failure := errors.New("memory unavailable")
	svc.WorkspaceMemory = func(ctx context.Context, workspaceID string) (string, error) { return "", failure }
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "work"}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if len(adapter.prompts) != 0 {
		t.Fatal("runtime prompted despite memory failure")
	}
	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("recorded failed prompt: %+v %v", page, err)
	}
}
