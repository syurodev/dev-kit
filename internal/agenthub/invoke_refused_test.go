package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// refusingAdapter's runtime ends every turn with err.
type refusingAdapter struct{ rt *refusingRuntime }

func (a refusingAdapter) Type() agenthub.AgentType { return agenthub.AgentType("refusing") }
func (a refusingAdapter) Open(context.Context, agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return a.rt, nil
}
func (a refusingAdapter) NormalizeUsage(json.RawMessage) (agenthub.NormalizedUsage, error) {
	var meta struct {
		Quota struct {
			TokenCount struct {
				Input  int `json:"inputTokens"`
				Cached int `json:"cachedInputTokens"`
			} `json:"token_count"`
		} `json:"quota"`
	}
	_ = json.Unmarshal(a.rt.out.Meta, &meta)
	cached := meta.Quota.TokenCount.Cached
	return agenthub.NormalizedUsage{InputTokens: meta.Quota.TokenCount.Input, CachedInputTokens: &cached, RawMeta: append(json.RawMessage(nil), a.rt.out.Meta...)}, nil
}
func (a refusingAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a refusingAdapter) Extensions() []string { return nil }

type refusingRuntime struct {
	err error
	out agenthub.PromptOutcome
}

func (r *refusingRuntime) ACPSessionID() string                 { return "acp-refusing" }
func (r *refusingRuntime) SetHandlers(agenthub.RuntimeHandlers) {}
func (r *refusingRuntime) Cancel(context.Context) error         { return nil }
func (r *refusingRuntime) Close(context.Context) error          { return nil }
func (r *refusingRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	return r.out, r.err
}

// A turn the vendor refused leaves a usable session behind, so it ends idle;
// only a turn that broke the agent ends in error. "error" is what draws the
// "stopped … Restart" card, and it used to be drawn over a Codex session that
// had only hit its usage limit.
func TestInvokeRefusedTurnLeavesTheSessionIdle(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want agenthub.SessionStatus
		out  agenthub.PromptOutcome
	}{
		{"usage limit", fmt.Errorf("%w: try again at 3:10 PM", agenthub.ErrAgentUsageLimit), agenthub.SessionStatusIdle, agenthub.PromptOutcome{Meta: json.RawMessage(`{"quota":{"token_count":{"inputTokens":14,"cachedInputTokens":9}}}`)}},
		{"turn failed", fmt.Errorf("%w: context window", agenthub.ErrTurnFailed), agenthub.SessionStatusIdle, agenthub.PromptOutcome{}},
		{"process failed", fmt.Errorf("%w: codex exited", agenthub.ErrProcessFailed), agenthub.SessionStatusError, agenthub.PromptOutcome{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if err := storage.Migrate(ctx, db); err != nil {
				t.Fatal(err)
			}
			store := agenthub.NewSQLStore(db)
			reg := agenthub.NewRegistry()
			reg.Register(refusingAdapter{rt: &refusingRuntime{err: tc.err, out: tc.out}})
			svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})

			sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: "refusing", Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "go"}); !errors.Is(err, tc.err) {
				t.Fatalf("Invoke = %v, want %v", err, tc.err)
			}
			got, err := store.GetSession(ctx, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("status = %s, want %s", got.Status, tc.want)
			}
			if tc.name == "usage limit" && (got.Stop == nil || got.Stop.Kind != agenthub.StopUsageLimit) {
				t.Fatalf("stop = %+v, want usage_limit", got.Stop)
			}
			if tc.name == "usage limit" {
				turns, err := store.ListTurns(ctx, sess.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(turns) != 1 || string(turns[0].RawMeta) != string(tc.out.Meta) {
					t.Fatalf("turns = %+v, want failed turn with raw quota metadata", turns)
				}
				a := turns[0].Attribution
				if a == nil || a.ContextVersion != 1 || a.StartedAt.IsZero() || a.Facts.AccountID != "refusing-default" || a.Facts.AccountGeneration != "builtin" {
					t.Fatalf("failed billed turn attribution = %+v", a)
				}
				var usage agenthub.NormalizedUsage
				if err := json.Unmarshal(turns[0].UsageJSON, &usage); err != nil {
					t.Fatal(err)
				}
				if usage.InputTokens != 14 || usage.CachedInputTokens == nil || *usage.CachedInputTokens != 9 {
					t.Fatalf("usage = %+v", usage)
				}
			}
		})
	}
}
