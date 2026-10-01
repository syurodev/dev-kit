package agenthub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/codexapp"
)

func workspaceWireBlocks(t *testing.T) []acp.ContentBlock {
	t.Helper()
	svc := &Service{WorkspaceInstructions: func(string) (string, error) { return "Use workspace rules", nil }, SessionBrief: func(string, string, []string) string { return "Current primary: /new" }}
	svc.WorkspaceMemory = func(context.Context, string) (string, error) {
		return `{"entries":[{"id":"memory-1","summary":"Keep shared API compatibility","revision":1}],"omitted":false}`, nil
	}
	text, err := svc.workspaceTurnContext(context.Background(), AgentSession{WorkspaceID: "selected", ProjectPath: "/project"})
	if err != nil {
		t.Fatal(err)
	}
	return []acp.ContentBlock{
		{Type: "text", Text: text},
		{Type: "text", Text: "user request"},
		{Type: "image", Data: "aGk=", MimeType: "image/png"},
		{Type: "resource", Resource: &acp.ResourceContents{URI: "file:///a.go", Text: "package a"}},
	}
}
func TestClaudeWorkspaceContextPrecedesUserAndPreservesAttachments(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sent.jsonl")
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	adapter := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedVendor{capture: capture}})
	rt, err := adapter.Open(context.Background(), OpenSpec{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	blocks := workspaceWireBlocks(t)
	if _, err = rt.Prompt(context.Background(), blocks, nil); err != nil {
		t.Fatal(err)
	}
	for _, line := range sentFrames(t, capture) {
		var frame struct {
			Method string            `json:"method"`
			Params acp.PromptRequest `json:"params"`
		}
		if err = json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Method != "session/prompt" {
			continue
		}
		want, _ := json.Marshal(blocks)
		got, _ := json.Marshal(frame.Params.Prompt)
		if string(want) != string(got) {
			t.Fatalf("prompt=%s want=%s", got, want)
		}
		return
	}
	t.Fatal("no session/prompt sent")
}
func TestCodexWorkspaceContextPrecedesUserAndPreservesAttachments(t *testing.T) {
	fixture := newCodexFixture(t, "turn")
	rt := fixture.open(t)
	blocks := workspaceWireBlocks(t)
	if _, err := rt.Prompt(context.Background(), blocks, nil); err != nil {
		t.Fatal(err)
	}
	var turn codexapp.TurnStartParams
	if err := json.Unmarshal(fixture.runner.sent(t, "turn/start")[0], &turn); err != nil {
		t.Fatal(err)
	}
	if len(turn.Input) != 2 {
		t.Fatalf("input=%s", turn.Input)
	}
	var text struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(turn.Input[0], &text); err != nil {
		t.Fatal(err)
	}
	if text.Text != blocks[0].Text+"\n\nuser request\n\nfile:///a.go:\npackage a" {
		t.Fatalf("text=%q", text.Text)
	}
	if !strings.Contains(string(turn.Input[1]), "data:image/png;base64,aGk=") {
		t.Fatalf("image=%s", turn.Input[1])
	}
}

func TestUnboundWorkspaceContextRetiresRulesWithoutResolvingByCwd(t *testing.T) {
	svc := &Service{WorkspaceInstructions: func(string) (string, error) { t.Fatal("instructions queried for unbound session"); return "", nil }}
	svc.WorkspaceMemory = func(context.Context, string) (string, error) {
		t.Fatal("memory queried for unbound session")
		return "", nil
	}
	text, err := svc.workspaceTurnContext(context.Background(), AgentSession{ProjectPath: "/shared/project"})
	if err != nil || !strings.Contains(text, "No workspace is selected") || !strings.Contains(text, "retire any previously") || !strings.Contains(text, "Retire previously supplied workspace memory") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}
