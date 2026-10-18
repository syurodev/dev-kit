package agenthub

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/identity"
)

func TestATitleIsReadOffTheFirstLineThatSaysSomething(t *testing.T) {
	for prompt, want := range map[string]string{
		"Fix the flaky login test":                   "Fix the flaky login test",
		"\n\n  ## Review the billing module  \nmore": "Review the billing module",
		"```\nfoo":                       "foo",
		"/compact":                       "",
		"/etc/hosts has the wrong entry": "",
		"@main.go explain this":          "",
		"   \n\t":                        "",
		"Giúp tôi viết hàm Go đảo ngược chuỗi": "Giúp tôi viết hàm Go đảo ngược chuỗi",
		strings.Repeat("word ", 30):            "word word word word word word word word word word word word…",
		strings.Repeat("x", 100):               strings.Repeat("x", 60) + "…",
	} {
		if got := titleFromPrompt(prompt); got != want {
			t.Errorf("titleFromPrompt(%q) = %q, want %q", prompt, got, want)
		}
	}
}

func TestACodexOrOpencodeSessionIsNamedAfterItsFirstPrompt(t *testing.T) {
	f := newChoiceFixture(t) // the mock vendor: it names nothing, like Codex and opencode
	ui := identity.WithCaller(context.Background(), identity.UI())
	sess, err := f.svc.CreateSession(ui, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Invoke(ui, InvokeInput{SessionID: sess.ID, Prompt: "Refactor the billing client\nand its tests"}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.store.GetSession(ui, sess.ID)
	if derefString(got.Title) != "Refactor the billing client" {
		t.Fatalf("title = %q", derefString(got.Title))
	}

	// A later prompt, or a rename, is never overwritten.
	if _, err := f.svc.Invoke(ui, InvokeInput{SessionID: sess.ID, Prompt: "Now something else"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.store.GetSession(ui, sess.ID); derefString(got.Title) != "Refactor the billing client" {
		t.Fatalf("title changed to %q", derefString(got.Title))
	}
}

func TestAClaudeSessionIsLeftToClaudeToName(t *testing.T) {
	f := newChoiceFixture(t)
	sess := AgentSession{ID: "s", AgentType: AgentTypeClaude}
	f.svc.nameFromPrompt(context.Background(), sess, "Anything")
	if got, _ := f.svc.store.GetSession(context.Background(), f.parent.ID); derefString(got.Title) != "" {
		t.Fatalf("title = %q", derefString(got.Title))
	}
	if !vendorNamesSessions(AgentTypeClaude) || vendorNamesSessions(AgentTypeCodex) || vendorNamesSessions(AgentTypeOpenCode) {
		t.Fatal("vendorNamesSessions: Claude names its sessions, Codex and opencode do not")
	}
}

func TestStartupNamesTheSessionsThatNeverGotATitle(t *testing.T) {
	f := newChoiceFixture(t)
	ctx := context.Background()
	untitled, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	recordText(t, f.svc, untitled.ID, UpdateUserMessageChunk, "u1", "Explain the retry policy of the sync client")
	if err := f.svc.recorder.Flush(ctx, untitled.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := f.svc.store.GetSession(ctx, untitled.ID)

	if err := f.svc.BackfillSessionTitles(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := f.svc.store.GetSession(ctx, untitled.ID)
	if derefString(after.Title) != "Explain the retry policy of the sync client" {
		t.Fatalf("title = %q", derefString(after.Title))
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("the row's time moved (%v → %v): the rail would reorder", before.UpdatedAt, after.UpdatedAt)
	}
	// The parent has no user message at all: it stays as it was.
	if got, _ := f.svc.store.GetSession(ctx, f.parent.ID); derefString(got.Title) != "" {
		t.Fatalf("parent title = %q", derefString(got.Title))
	}
}
