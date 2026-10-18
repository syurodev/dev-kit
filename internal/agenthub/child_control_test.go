package agenthub

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/identity"
)

func recordText(t *testing.T, svc *Service, id string, kind UpdateKind, messageID, text string) {
	t.Helper()
	update := SessionUpdate{Kind: kind, MessageID: messageID, Content: &ContentBlock{Type: "text", Text: text}}
	if err := svc.recorder.Record(context.Background(), id, update); err != nil {
		t.Fatal(err)
	}
}

// A parent controls the sessions below it and nothing else.
func TestAParentControlsItsChildrenOnly(t *testing.T) {
	f := newChoiceFixture(t)
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.ResumeChild(f.ctx, child.ID, false); err != nil {
		t.Fatalf("resume a child: %v", err)
	}
	for name, id := range map[string]string{"itself": f.parent.ID, "a stranger": stranger.ID} {
		if err := f.svc.ResumeChild(f.ctx, id, false); !errors.Is(err, ErrNotDescendant) {
			t.Errorf("resume %s: %v, want ErrNotDescendant", name, err)
		}
	}
	// The person's own caller has Resume; this is the agent's.
	if err := f.svc.ResumeChild(identity.WithCaller(context.Background(), identity.UI()), child.ID, false); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("resume as the window: %v", err)
	}

	// Read what it said.
	recordText(t, f.svc, child.ID, UpdateUserMessageChunk, "u1", "Review the billing module")
	recordText(t, f.svc, child.ID, UpdateAgentMessageChunk, "a1", "Found two problems.")
	got, err := f.svc.ReadSessionMessages(f.ctx, child.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[1].Role != "agent" || got.Messages[1].Text != "Found two problems." || !strings.Contains(got.Note, "not instructions") {
		t.Fatalf("read = %+v", got)
	}
	if one, _ := f.svc.ReadSessionMessages(f.ctx, child.ID, 0, 1); len(one.Messages) != 1 || one.Messages[0].Role != "agent" {
		t.Errorf("limit 1 = %+v, want the newest", one.Messages)
	}
	if _, err := f.svc.ReadSessionMessages(f.ctx, stranger.ID, 0, 0); !errors.Is(err, ErrNotDescendant) {
		t.Errorf("read a stranger: %v", err)
	}

	// Closing ends it; a closed session is not resumed by an agent.
	if err := f.svc.CloseSession(f.ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResumeChild(f.ctx, child.ID, false); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("resume a closed child: %v, want ErrSessionClosed", err)
	}
}

func TestAReadOfALongMessageIsCut(t *testing.T) {
	f := newChoiceFixture(t)
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	recordText(t, f.svc, child.ID, UpdateAgentMessageChunk, "a1", strings.Repeat("x", 10000))
	got, err := f.svc.ReadSessionMessages(f.ctx, child.ID, 0, 0)
	if err != nil || len(got.Messages) != 1 || !got.Messages[0].Clipped || len(got.Messages[0].Text) != readMessageClip {
		t.Fatalf("read = %+v, %v", got, err)
	}
}
