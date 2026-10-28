package bridge

import (
	"context"
	"testing"

	"devkit/internal/identity"
	"devkit/internal/permissions"
)

// The idle release is the window's own: on by default, refused to an agent session
// and to the terminal on the shared socket, and a time out of range is a
// validation error that changes nothing.
func TestTheIdleReleaseSettingIsWindowOnlyOnByDefaultAndValidated(t *testing.T) {
	svc := newPolicyBridge(t)
	win := context.Background()
	agent := identity.WithCaller(win, identity.Agent("11111111-1111-1111-1111-111111111111"))
	terminal := identity.WithCaller(win, identity.UserTerminal())

	if p, err := svc.AgentIdleReleaseGet(win); err != nil || !p.Enabled || p.ChildMinutes != 10 || p.TopMinutes != 30 {
		t.Fatalf("default: %+v %v", p, err)
	}
	for who, caller := range map[string]context.Context{"agent": agent, "terminal": terminal} {
		if _, err := svc.AgentIdleReleaseGet(caller); publicCode(err) != "permission_denied" {
			t.Errorf("%s read: %v", who, err)
		}
		if _, err := svc.AgentIdleReleaseSet(caller, false, 5, 5); publicCode(err) != "permission_denied" {
			t.Errorf("%s write: %v", who, err)
		}
	}
	if p, _ := svc.AgentIdleReleaseGet(win); !p.Enabled {
		t.Fatal("a refused caller changed the setting")
	}

	set, err := svc.AgentIdleReleaseSet(win, false, 5, 45)
	if err != nil || set.Enabled || set.ChildMinutes != 5 || set.TopMinutes != 45 {
		t.Fatalf("the window changes it: %+v %v", set, err)
	}
	if _, err := svc.AgentIdleReleaseSet(win, true, 1, 45); publicCode(err) != "validation_failed" {
		t.Fatalf("a time under two minutes is a validation error the window can act on: %v", err)
	}
	if p, _ := svc.AgentIdleReleaseGet(win); p != set {
		t.Fatalf("a refused write changed the setting: %+v", p)
	}
}

func TestTheIdleReleaseSettingIsNotReachableAsAGatewayScopeByAnyOtherSubject(t *testing.T) {
	svc := newPolicyBridge(t)
	for _, g := range []struct{ subject, capability, scope string }{
		{"builtin.harness", "preferences:write", "agent_idle_release"},
		{"builtin.agenthub", "preferences:write", "agent_idle_release"},
		{"builtin.settings", "preferences:write", "agent_idle_release:*"},
	} {
		if svc.container.Permissions.Check(permissions.Request{SubjectID: g.subject, Capability: g.capability, Scope: g.scope}).Allowed {
			t.Errorf("%s %s on %s is allowed", g.subject, g.capability, g.scope)
		}
	}
}
