package capabilities

import (
	"context"
	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/permissions"
	"errors"
	"testing"
)

func TestExposureIsCheckedAndAuditedByGateway(t *testing.T) {
	for _, kind := range []identity.Kind{identity.KindUI, identity.KindAgent, identity.KindUserTerminal, identity.KindRemoteHuman, ""} {
		for _, exposure := range []Exposure{ExposureBoth, ExposureUI, ExposureAgent} {
			for _, granted := range []bool{false, true} {
				grants := []permissions.Grant{}
				if granted {
					grants = append(grants, permissions.Grant{SubjectID: "notes", Capability: "read", Scope: "*"})
				}
				sink := audit.NewMemorySink()
				g := NewGateway(permissions.NewPolicy(grants), sink)
				called := false
				ctx := identity.WithCaller(context.Background(), identity.Caller{Kind: kind})
				_, err := g.Call(ctx, CallRequest{SubjectID: "notes", Capability: "read", Scope: "note:list", Operation: "notes.list", Exposure: exposure, Handler: func(context.Context) (any, error) { called = true; return nil, nil }})
				want := granted && (exposure == ExposureBoth || exposure == ExposureUI && kind == identity.KindUI || exposure == ExposureAgent && kind != identity.KindUI)
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
			}
		}
	}
}
