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
	return agenthub.NormalizedUsage{}, nil
}
func (a refusingAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (a refusingAdapter) Extensions() []string { return nil }

type refusingRuntime struct{ err error }

func (r *refusingRuntime) ACPSessionID() string                 { return "acp-refusing" }
func (r *refusingRuntime) SetHandlers(agenthub.RuntimeHandlers) {}
func (r *refusingRuntime) Cancel(context.Context) error         { return nil }
func (r *refusingRuntime) Close(context.Context) error          { return nil }
func (r *refusingRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (agenthub.PromptOutcome, error) {
	return agenthub.PromptOutcome{}, r.err
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
	}{
		{"usage limit", fmt.Errorf("%w: try again at 3:10 PM", agenthub.ErrAgentUsageLimit), agenthub.SessionStatusIdle},
		{"turn failed", fmt.Errorf("%w: context window", agenthub.ErrTurnFailed), agenthub.SessionStatusIdle},
		{"process failed", fmt.Errorf("%w: codex exited", agenthub.ErrProcessFailed), agenthub.SessionStatusError},
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
			reg.Register(refusingAdapter{rt: &refusingRuntime{err: tc.err}})
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
		})
	}
}
