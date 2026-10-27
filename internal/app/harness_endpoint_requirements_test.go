package app

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/mcp"
)

// A host may implement launch filtering without offering live publication.
type launchOnlyHarnessHost struct {
	*mcp.SessionHost
	typedNil bool
}

type nilHarnessLiveHandle struct{ agenthub.ServingEndpoint }

func (h launchOnlyHarnessHost) OpenWithPolicy(id string, policy harness.Effective) (agenthub.MCPEndpoint, error) {
	ep, err := h.SessionHost.OpenWithPolicy(id, policy)
	ep.Live = nil
	if h.typedNil {
		var handle *nilHarnessLiveHandle
		ep.Live = handle
	}
	return ep, err
}

func TestPinnedHarnessRejectsEndpointsWithoutLivePublication(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, typedNil := range []bool{false, true} {
			name := "default"
			if configured {
				name = "configured"
			}
			if typedNil {
				name += "/typed-nil"
			} else {
				name += "/nil"
			}
			t.Run(name, func(t *testing.T) {
				r := newCompRig(t, compOptions{noAttach: true})
				if configured {
					r.workspaceOff("vault")
				}
				r.agents.MCPEndpoints = launchOnlyHarnessHost{SessionHost: r.host, typedNil: typedNil}
				if err := r.asm.Attach(r.agents); err != nil {
					t.Fatal(err)
				}
				_, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, WorkspaceID: r.wsID})
				if !errors.Is(err, agenthub.ErrHarnessNotEnforceable) {
					t.Fatalf("want fail-closed launch, got %v", err)
				}
				if r.vendors[agenthub.AgentTypeClaude].opens.Load() != 0 {
					t.Fatal("vendor started without live publication")
				}
				sessions, err := agenthub.NewSQLStore(r.db).ListSessions(context.Background())
				if err != nil || len(sessions) != 0 {
					t.Fatalf("refused launch left sessions: %v %v", sessions, err)
				}
				entries, err := os.ReadDir(r.sockDir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("staged endpoint leaked: %v %v", entries, err)
				}
			})
		}
	}
}

func TestPinnedHarnessRejectsAHostDowngradeBeforeReload(t *testing.T) {
	for _, typedNil := range []bool{false, true} {
		name := "legacy"
		if typedNil {
			name = "typed-nil"
		}
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{})
			sess, spec := r.create(agenthub.AgentTypeClaude)
			cs := r.connect(spec)
			before := r.agents.ServingTarget(sess.ID)
			if typedNil {
				var host *mcp.SessionHost
				r.agents.MCPEndpoints = host
			} else {
				r.agents.MCPEndpoints = legacyOnlyEndpoints{}
			}
			defer func() { r.agents.MCPEndpoints = r.host }()
			if err := r.agents.ResumeSession(r.ui(), sess.ID, true); !errors.Is(err, agenthub.ErrHarnessNotEnforceable) {
				t.Fatalf("want refusal before closing live endpoint, got %v", err)
			}
			after := r.agents.ServingTarget(sess.ID)
			if after.Incarnation != before.Incarnation || !after.Acknowledged {
				t.Fatalf("running endpoint changed: %+v", after)
			}
			if r.vendors[agenthub.AgentTypeClaude].loads.Load() != 0 {
				t.Fatal("vendor reloaded after preflight refusal")
			}
			if _, err := cs.ListTools(r.ctx, nil); err != nil {
				t.Fatalf("old endpoint closed: %v", err)
			}
		})
	}
}

func TestPinnedHarnessLaunchOnlyReloadPreservesDurableState(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	beforeOverride, beforeStamp := r.override(sess.ID)
	store := agenthub.NewSQLStore(r.db)
	beforeRef, beforeEnforced, beforeFound, err := store.GetHarnessApplied(r.ctx, sess.ID)
	if err != nil || !beforeFound {
		t.Fatalf("missing launch attribution: %v", err)
	}
	r.agents.MCPEndpoints = launchOnlyHarnessHost{SessionHost: r.host}
	if err := r.agents.ResumeSession(r.ui(), sess.ID, true); !errors.Is(err, agenthub.ErrHarnessNotEnforceable) {
		t.Fatalf("want publication refusal, got %v", err)
	}
	if r.vendors[agenthub.AgentTypeClaude].loads.Load() != 0 {
		t.Fatal("vendor loaded without publication")
	}
	afterOverride, afterStamp := r.override(sess.ID)
	if !reflect.DeepEqual(beforeOverride, afterOverride) || beforeStamp != afterStamp {
		t.Fatal("failed reload changed durable S")
	}
	afterRef, afterEnforced, afterFound, err := store.GetHarnessApplied(r.ctx, sess.ID)
	if err != nil || !reflect.DeepEqual(beforeRef, afterRef) || beforeEnforced != afterEnforced || beforeFound != afterFound {
		t.Fatalf("failed reload rewrote historical attribution: %v", err)
	}
	status, err := r.agents.HarnessStatus(r.ctx, sess.ID)
	if err != nil || status.Live {
		t.Fatalf("failed launch claimed live publication: %+v %v", status, err)
	}
	entries, err := os.ReadDir(r.sockDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed reload leaked endpoint: %v %v", entries, err)
	}
}
