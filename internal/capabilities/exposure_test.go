package capabilities

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/permissions"
)

func TestExposureIsCheckedAndAuditedByGateway(t *testing.T) {
	for _, kind := range []identity.Kind{identity.KindUI, identity.KindAgent, identity.KindUserTerminal, identity.KindRemoteHuman, "", "future-kind"} {
		for _, exposure := range []Exposure{ExposureBoth, ExposureUI, ExposureAgent, Exposure(255)} {
			for _, granted := range []bool{false, true} {
				t.Run(fmt.Sprintf("caller=%s/exposure=%d/granted=%v", kind, exposure, granted), func(t *testing.T) {
					grants := []permissions.Grant{}
					if granted {
						grants = append(grants, permissions.Grant{SubjectID: "notes", Capability: "read", Scope: "*"})
					}
					sink := audit.NewMemorySink()
					g := NewGateway(permissions.NewPolicy(grants), sink)
					called := false
					ctx := identity.WithCaller(context.Background(), identity.Caller{Kind: kind})
					_, err := g.Call(ctx, CallRequest{SubjectID: "notes", Capability: "read", Scope: "note:list", Operation: "notes.list", Exposure: exposure, Handler: func(context.Context) (any, error) { called = true; return nil, nil }})
					want := granted && (exposure == ExposureBoth || exposure == ExposureUI && kind == identity.KindUI || exposure == ExposureAgent && (kind == identity.KindAgent || kind == identity.KindUserTerminal))
					if called != want || (err == nil) != want {
						t.Fatalf("kind=%s exposure=%d granted=%v: called=%v err=%v", kind, exposure, granted, called, err)
					}
					if !want && !errors.Is(err, ErrPermissionDenied) {
						t.Fatalf("not permission denial: %v", err)
					}
					events := sink.Events()
					if len(events) != 1 {
						t.Fatal(events)
					}
					if (events[0].Outcome == audit.OutcomeAllowed) != want {
						t.Fatal(events)
					}
				})
			}
		}
	}
}

func TestPreflightRefusalIsAuditedWithoutRunningHandler(t *testing.T) {
	for _, granted := range []bool{false, true} {
		var grants []permissions.Grant
		if granted {
			grants = []permissions.Grant{{SubjectID: "s", Capability: "read", Scope: "*"}}
		}
		sink := audit.NewMemorySink()
		g := NewGateway(permissions.NewPolicy(grants), sink)
		refusal := errors.New("desktop app only")
		called := false
		_, err := g.Call(context.Background(), CallRequest{SubjectID: "s", Capability: "read", Scope: "operation:test.desktop", Refusal: refusal, Handler: func(context.Context) (any, error) { called = true; return nil, nil }})
		if !errors.Is(err, refusal) || called {
			t.Fatalf("granted=%v err=%v called=%v", granted, err, called)
		}
		events := sink.Events()
		if len(events) != 1 || events[0].Outcome != audit.OutcomeDenied {
			t.Fatal(events)
		}
	}
}
