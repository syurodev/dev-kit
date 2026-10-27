package bridge

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/permissions"
	"devkit/internal/preferences"
)

func newPolicyBridge(t *testing.T) *AppService {
	t.Helper()
	svc, _ := newBridgeWithAgentHubAdapter(t, agenthub.NewMockAdapter())
	svc.container.Preferences = preferences.NewService(preferences.NewMemoryRepository())
	return svc
}

// The opt-in is the window's own: off until the person turns it on, refused to an
// agent session and to the terminal on the shared socket, with a conflict (not a
// last-write-wins) when the revision is stale.
func TestTheNarrowingPolicyIsWindowOnlyOffByDefaultAndGuardedByARevision(t *testing.T) {
	svc := newPolicyBridge(t)
	win := context.Background()
	agent := identity.WithCaller(win, identity.Agent("11111111-1111-1111-1111-111111111111"))
	terminal := identity.WithCaller(win, identity.UserTerminal())

	if p, err := svc.AgentHarnessPolicyGet(win); err != nil || p.AutoNarrowing || p.Revision != 0 {
		t.Fatalf("default: %+v %v", p, err)
	}
	for who, caller := range map[string]context.Context{"agent": agent, "terminal": terminal} {
		if _, err := svc.AgentHarnessPolicyGet(caller); publicCode(err) != "permission_denied" {
			t.Errorf("%s read: %v", who, err)
		}
		if _, err := svc.AgentHarnessAutoNarrowingSet(caller, 0, true); publicCode(err) != "permission_denied" {
			t.Errorf("%s write: %v", who, err)
		}
	}
	if p, _ := svc.AgentHarnessPolicyGet(win); p.AutoNarrowing {
		t.Fatal("a refused caller changed the setting")
	}

	on, err := svc.AgentHarnessAutoNarrowingSet(win, 0, true)
	if err != nil || !on.AutoNarrowing || on.Revision != 1 {
		t.Fatalf("the window turns it on: %+v %v", on, err)
	}
	_, err = svc.AgentHarnessAutoNarrowingSet(win, 0, false) // read before the change above
	if publicCode(err) != "validation_failed" || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a stale revision is a conflict the window can act on: %v", err)
	}
	if p, _ := svc.AgentHarnessPolicyGet(win); !p.AutoNarrowing {
		t.Fatal("a refused write changed the setting")
	}
}

func TestTheNarrowingPolicyIsNotReachableAsAGatewayScopeByAnyOtherSubject(t *testing.T) {
	svc := newPolicyBridge(t)
	// The grant belongs to the settings subject and to this scope only.
	for _, g := range []struct{ subject, capability, scope string }{
		{"builtin.harness", "preferences:write", "agent_harness_policy"},
		{"builtin.agenthub", "preferences:write", "agent_harness_policy"},
		{"builtin.settings", "harness:write", "agent_harness_policy"},
		{"builtin.settings", "preferences:write", "agent_harness_policy:*"},
	} {
		if svc.container.Permissions.Check(permissions.Request{SubjectID: g.subject, Capability: g.capability, Scope: g.scope}).Allowed {
			t.Errorf("%s %s on %s is allowed", g.subject, g.capability, g.scope)
		}
	}
}
