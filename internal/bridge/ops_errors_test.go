package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/ops"
	"devkit/internal/vault"
)

func TestOperationPreparationErrorsUsePublicErrorFor(t *testing.T) {
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	const canary = "CANARY-private-body"
	type input struct {
		Query string `json:"query"`
	}
	for _, kind := range []identity.Kind{identity.KindUI, identity.KindAgent, identity.KindUserTerminal} {
		for _, stage := range []string{"json", "target", "scope", "desktop"} {
			t.Run(string(kind)+"/"+stage, func(t *testing.T) {
				container := app.NewContainer()
				service := NewAppService(container)
				rt := service.notesRuntime()
				rt.ServerMode = stage == "desktop"
				rt.Origin = "mcp"
				ctx := identity.WithCaller(context.Background(), identity.Caller{Kind: kind, SessionID: "session"})
				resolverCalled, handlerCalled := false, false
				op := ops.Op[input, string]{Name: "test.prepare", Subject: "builtin.notes", Capability: "notes:read", Exposure: ops.Both,
					Scope: func(context.Context, input) (ops.Scope, error) {
						resolverCalled = true
						return ops.FixedScope("note:list"), nil
					},
					Handle: func(context.Context, ops.Deps, input) (string, error) { handlerCalled = true; return "unexpected", nil },
				}
				raw := []byte(`{"query":"` + canary + `"}`)
				var cause error
				switch stage {
				case "json":
					raw = []byte(`{"query":["` + canary + `"]}`)
					var in input
					cause = json.Unmarshal(raw, &in)
				case "target":
					cause = fmt.Errorf("%w: %s", vault.ErrLocked, canary)
					op.Target = &ops.Target{Resolve: func(context.Context, ops.Deps, any) (context.Context, string, error) { return nil, "", cause }}
				case "scope":
					cause = errors.New(canary)
					op.Scope = func(context.Context, input) (ops.Scope, error) {
						resolverCalled = true
						return ops.Scope{Value: canary, Meta: map[string]any{"query": canary}}, cause
					}
				case "desktop":
					op.Desktop = "Requires the desktop app"
					cause = apperrors.New("validation_failed", op.Desktop, false)
				}
				_, err := op.InvokeJSON(ctx, rt, raw)
				var got *apperrors.AppError
				if !errors.As(err, &got) {
					t.Fatalf("unpublished error: %T", err)
				}
				want := publicErrorFor(ctx, op.Subject, cause)
				if got.Code != want.Code || got.Message != want.Message || got.Recoverable != want.Recoverable {
					t.Fatalf("got=%+v want=%+v", got, want)
				}
				if strings.Contains(err.Error(), canary) {
					t.Fatal("input reached public error")
				}
				if handlerCalled {
					t.Fatal("handler ran after preparation failed")
				}
				if stage != "scope" && resolverCalled {
					t.Fatal("scope ran before preparation failure")
				}
				events := container.Audit.Events()
				if stage != "desktop" {
					if len(events) != 0 {
						t.Fatal("preparation error invented an authorization event")
					}
					return
				}
				if len(events) != 1 || events[0].Outcome != audit.OutcomeDenied || events[0].Scope != "operation:test.prepare" {
					t.Fatalf("desktop denial: %+v", events)
				}
				encoded, _ := json.Marshal(events)
				if strings.Contains(string(encoded), canary) {
					t.Fatal("input reached denial audit")
				}
			})
		}
	}
}
