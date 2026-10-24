package ops

import (
	"context"
	"devkit/internal/audit"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/permissions"
	"errors"
	"testing"
)

type gatewayCaller struct{ g *capabilities.Gateway }

func (c gatewayCaller) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return c.g.Call(ctx, r)
}
func TestInvokePolicyBeforeGuardsConfirmationAndHandler(t *testing.T) {
	for _, allow := range []bool{false, true} {
		sink := audit.NewMemorySink()
		grants := []permissions.Grant{}
		if allow {
			grants = append(grants, permissions.Grant{SubjectID: "s", Capability: "read", Scope: "*"})
		}
		rt := Runtime{Caller: gatewayCaller{capabilities.NewGateway(permissions.NewPolicy(grants), sink)}}
		guardCalled, handlerCalled := false, false
		op := Op[struct{}, string]{Name: "test.read", Subject: "s", Capability: "read", Scope: func(context.Context, struct{}) (Scope, error) { return FixedScope("fixed"), nil }, Guards: []Guard{func(context.Context, Deps, any) error {
			guardCalled = true
			if len(sink.Events()) != 1 {
				t.Fatal("guard before audit")
			}
			return nil
		}}, Handle: func(context.Context, Deps, struct{}) (string, error) { handlerCalled = true; return "ok", nil }}
		_, err := Invoke(identity.WithCaller(context.Background(), identity.Agent("a")), rt, op, struct{}{})
		if guardCalled != allow || handlerCalled != allow || (err == nil) != allow {
			t.Fatalf("allow=%v guard=%v handler=%v err=%v", allow, guardCalled, handlerCalled, err)
		}
		if !allow && !errors.Is(err, capabilities.ErrPermissionDenied) {
			t.Fatal(err)
		}
	}
}
func TestInvokeRequiresCallerStamp(t *testing.T) {
	_, err := Invoke(context.Background(), Runtime{}, Op[struct{}, string]{Name: "test"}, struct{}{})
	if err == nil {
		t.Fatal("unstamped caller accepted")
	}
}

func TestConfirmationDecision(t *testing.T) {
	for _, kind := range []identity.Kind{identity.KindUI, identity.KindAgent, identity.KindUserTerminal} {
		for _, policy := range []ConfirmPolicy{Never, EnvTag, AskOnce, Always, Custom} {
			for _, blocks := range []bool{false, true} {
				ctx := identity.WithCaller(context.Background(), identity.Caller{Kind: kind})
				c := Confirm{Policy: policy, Custom: func(context.Context, Deps, any) (interaction.ConfirmRequest, bool, error) {
					return interaction.ConfirmRequest{}, true, nil
				}}
				err := decide(ctx, Runtime{EnvBlocks: func(string) bool { return blocks }}, c, "test", "dev", nil)
				asks := kind != identity.KindUI && policy != Never && (policy != EnvTag || blocks)
				if asks != errors.Is(err, interaction.ErrConfirmationRequired) {
					t.Fatalf("kind=%s policy=%d blocks=%v err=%v", kind, policy, blocks, err)
				}
			}
		}
	}
}
