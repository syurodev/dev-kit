package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/harness"
)

// An unreadable coordinator mode is not "no mode concept": a bypassPermissions
// child must not pass because the coordinator's read failed.
func TestUnreadableCoordinatorModeFailsClosed(t *testing.T) {
	m := newMHRig(t, compOptions{})
	child := m.newChild(nil)
	real := m.agents
	attempt := m.attempt(child.ID)
	ctx := context.Background()
	m.host.hub = func() managedHarnessHub {
		return modeHub{managedHarnessHub: real, modes: map[string]string{child.ID: "bypassPermissions"}, unreadable: map[string]bool{m.coord.ID: true}}
	}
	_, err := m.host.ResolveActual(ctx, attempt)
	wantCode(t, err, agentwork.HarnessChildMode)
	// An unreadable child mode is refused too, whatever the coordinator has.
	m.host.hub = func() managedHarnessHub {
		return modeHub{managedHarnessHub: real, modes: map[string]string{m.coord.ID: "auto"}, unreadable: map[string]bool{child.ID: true}}
	}
	_, err = m.host.ResolveActual(ctx, attempt)
	wantCode(t, err, agentwork.HarnessChildMode)
	// A coordinator with no mode concept (and a child that has none) still passes.
	m.host.hub = func() managedHarnessHub { return modeHub{managedHarnessHub: real} }
	if _, err = m.host.ResolveActual(ctx, attempt); err != nil {
		t.Fatalf("no mode concept on either side passes: %v", err)
	}
	// The real hub: the scripted vendors have no mode concept, so it passes unchanged.
	m.host = newManagedHarnessHost(&Container{AgentHub: m.agents}, m.own)
	if _, err = m.host.ResolveActual(ctx, attempt); err != nil {
		t.Fatalf("the real hub with no mode concept passes: %v", err)
	}
}

// The service's fixed order decides, so the adapter must hand it every fact:
// a W edit after open plus a required group revoked live is a required-group
// refusal, and an S that is saved but not live is an override, not a change.
func TestActualOrderRequiredGroupOffBeatsConfigChangedAndOverrideBeatsBoth(t *testing.T) {
	ctx := context.Background()
	m := newMHRig(t, compOptions{})
	svc, _, child := m.bound(t)
	real := m.agents
	m.workspaceOff("vault") // W edited after the child opened: reload-required
	if st, err := m.agents.HarnessStatus(ctx, child.ID); err != nil || st.State != agenthub.HarnessReloadRequired {
		t.Fatalf("the test needs a reload-required child: %+v %v", st, err)
	}
	wantCode(t, svc.VerifyForEnqueue(ctx, "a1"), agentwork.HarnessConfigChanged)
	m.host.hub = func() managedHarnessHub {
		return modeHub{managedHarnessHub: real, served: func(id string) ([]string, bool) {
			tools, live := real.ServedTools(id)
			var kept []string
			for _, tool := range tools {
				if harness.Group(tool) != "git" {
					kept = append(kept, tool)
				}
			}
			return kept, live
		}}
	}
	r := wantCode(t, svc.VerifyForEnqueue(ctx, "a1"), agentwork.HarnessRequiredGroupOff)
	if !reflect.DeepEqual(r.Groups, []string{"git"}) {
		t.Fatalf("groups = %v", r.Groups)
	}

	// An S committed to the child's store but never published to its endpoint.
	m2 := newMHRig(t, compOptions{})
	svc2, _, child2 := m2.bound(t)
	m2.workspaceOff("vault")
	_, stamp, err := m2.asm.Store.GetOverride(ctx, child2.ID, m2.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m2.asm.Store.Apply(ctx, harness.StoreWrite{WorkspaceID: m2.wsID, SessionID: child2.ID,
		Override: &harness.OverrideWrite{Expect: stamp, Value: &harness.Override{Schema: harness.SchemaVersion, Groups: []string{"vault"}}}}); err != nil {
		t.Fatal(err)
	}
	if st, err := m2.agents.HarnessStatus(ctx, child2.ID); err != nil || st.Serving.Pending || !st.Serving.Acknowledged {
		t.Fatalf("the test needs an S that is saved but not pending on the endpoint: %+v %v", st.Serving, err)
	}
	wantCode(t, svc2.VerifyForEnqueue(ctx, "a1"), agentwork.HarnessOverridePresent)
}

// A cancelled context is the caller's, never a verdict about the child.
func TestCancelledContextIsNotAChildScopeRefusal(t *testing.T) {
	m := newMHRig(t, compOptions{})
	child := m.newChild(nil)
	attempt := m.attempt(child.ID)
	ctx, cancel := context.WithCancel(context.Background())
	m.own.onCall = cancel // the ownership check passes, then the caller gives up
	_, err := m.host.ResolveActual(ctx, attempt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during the child lookup = %v, want context.Canceled", err)
	}
	var r *agentwork.HarnessRefusal
	if errors.As(err, &r) {
		t.Fatalf("a cancelled context must not be a refusal: %v", r.Code)
	}
}
