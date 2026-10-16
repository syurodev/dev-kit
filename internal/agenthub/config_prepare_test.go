package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

func newInternalStore(t *testing.T) Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewSQLStore(db)
}

// storedModelChoices is what a reader of the transcript sees for the model
// picker: the choices in the session's stored config_option_update.
func storedModelChoices(t *testing.T, svc *Service, id string) []string {
	t.Helper()
	page, err := svc.TranscriptPage(context.Background(), id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range append(page.State, page.Entries...) {
		if entry.Kind != UpdateConfigOptions {
			continue
		}
		var stored SessionUpdate
		if err := json.Unmarshal(entry.Payload, &stored); err != nil {
			t.Fatal(err)
		}
		for _, option := range stored.ConfigOptions {
			if option.ID != "model" {
				continue
			}
			out = nil
			for _, choice := range option.Options {
				out = append(out, choice.Value)
			}
		}
	}
	return out
}

func claudeFixtureAdapter(t *testing.T) *ClaudeAdapter {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: catalogVendor{capture: filepath.Join(dir, "sent.jsonl")}})
}

// "Models unavailable", Claude: session/new tells the model list in its answer
// and claude-agent-acp never sends it again until something changes, so a
// session that was created and not yet prompted had no model choices anywhere
// — the picker had nothing to show and nothing to refresh from.
func TestFreshClaudeSessionHasItsModelChoicesBeforeAnyPrompt(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(claudeFixtureAdapter(t))
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(ctx) })
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeClaude, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PrepareSession(ctx, sess.ID); err != nil {
		t.Fatalf("PrepareSession: %v", err)
	}
	got := storedModelChoices(t, svc, sess.ID)
	if len(got) != 3 || got[0] != "sonnet" {
		t.Fatalf("model choices before any prompt = %v, want sonnet, opus, haiku", got)
	}
}

// The model a session was created with is what the picker shows, not the
// agent's own default: InitialConfig is applied before anyone is listening.
func TestFreshClaudeSessionShowsTheModelItWasCreatedWith(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(claudeFixtureAdapter(t))
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(ctx) })
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeClaude, Cwd: t.TempDir(), InitialConfig: map[string]string{"model": "opus"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var current string
	for _, entry := range append(page.State, page.Entries...) {
		if entry.Kind != UpdateConfigOptions {
			continue
		}
		var stored SessionUpdate
		_ = json.Unmarshal(entry.Payload, &stored)
		for _, option := range stored.ConfigOptions {
			if option.ID == "model" {
				_ = json.Unmarshal(option.CurrentValue, &current)
			}
		}
	}
	if current != "opus" {
		t.Fatalf("current model = %q, want opus", current)
	}
}

// "Models unavailable", Codex: a thread that never ran a turn has no rollout
// on disk, so after the process is gone (a restart, an eviction) thread/resume
// refuses it — and so did every settings call, for a session with nothing to
// resume. Measured with the real app-server 0.163.0: thread/start, then a new
// process, thread/resume → "no rollout found for thread id". This test drives
// the real adapter against a fake app-server that answers the same.
func TestCodexSessionWithoutATurnSurvivesALostProcess(t *testing.T) {
	ctx := context.Background()
	f := newCodexFixture(t, "noRollout")
	reg := NewRegistry()
	reg.Register(f.adapter)
	store := newInternalStore(t)
	svc := NewService(reg, store, nil)
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: f.cwd})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	restarted := NewService(reg, store, nil)
	t.Cleanup(func() { _ = restarted.Shutdown(ctx) })
	if err := restarted.PrepareSession(ctx, sess.ID); err != nil {
		t.Fatalf("PrepareSession after the process was lost: %v", err)
	}
	if got := storedModelChoices(t, restarted, sess.ID); len(got) != 2 {
		t.Fatalf("model choices = %v, want the two the app-server lists", got)
	}
}

// refusingModelsRuntime is a live agent that cannot list its models.
type refusingModelsRuntime struct{ SessionRuntime }

func (refusingModelsRuntime) RefreshConfigOptions(context.Context) error {
	return fmt.Errorf("%w: Codex returned no models", ErrProcessFailed)
}

type refusingModelsAdapter struct{ *MockAdapter }

func (a refusingModelsAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	rt, err := a.MockAdapter.Open(ctx, spec)
	return refusingModelsRuntime{rt}, err
}

// A refused model list used to reach the picker as "Agent process failed.",
// the sentence for a crash. It now says what failed and keeps the cause.
func TestPrepareSessionNamesAModelListThatCouldNotBeRead(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(refusingModelsAdapter{NewMockAdapter()})
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(ctx) })
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.PrepareSession(ctx, sess.ID)
	if !errors.Is(err, ErrModelsUnavailable) || !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want models-unavailable carrying the cause", err)
	}
	if ErrorVendor(err) != AgentTypeMock {
		t.Fatalf("vendor = %q", ErrorVendor(err))
	}
}
