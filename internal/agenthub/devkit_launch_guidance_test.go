package agenthub_test

import (
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// What the model is told is decided once, by the prepared launch, and every
// path that opens a runtime goes through it.

func servedOf(e *launchEnv) *agenthub.DevKitServing { return e.adapter.lastSpec().DevKit }

func TestAPreparedLaunchCarriesWhatItsEndpointServes(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, gitOffW(3)), nil
	})
	s := e.mustCreate()
	got := servedOf(e)
	if got == nil || strings.Join(got.Tools, ",") != "harness.profile.propose,tasks.list" {
		t.Fatalf("spec.DevKit = %+v, want the endpoint's served tools", got)
	}
	// Reload: prepared again, from the then-current policy, never the old text.
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Workspace = &harness.Profile{Schema: 1, Revision: 4, ToolGroups: map[string]bool{"tasks": false}}
		}), nil
	})
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	if got := servedOf(e); got == nil || strings.Join(got.Tools, ",") != "git.status,harness.profile.propose" {
		t.Fatalf("after reload spec.DevKit = %+v", got)
	}
}

func TestALegacyLaunchHasNoServedValue(t *testing.T) {
	e := newLaunchEnv(t, plain) // a default snapshot on an endpoint that cannot enforce: today's behaviour
	e.mustCreate()
	if got := servedOf(e); got != nil {
		t.Fatalf("a legacy launch got %+v: it would render a prepared-looking prompt", got)
	}
	e2 := newLaunchEnv(t, policy)
	e2.svc.Harness = nil
	e2.mustCreate()
	if got := servedOf(e2); got != nil {
		t.Fatalf("no harness: spec.DevKit = %+v", got)
	}
}

func TestAnEndpointThatDoesNotSayWhatItServesFailsPreparation(t *testing.T) {
	e := newLaunchEnv(t, func(ev *events) agenthub.MCPEndpoints {
		return &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}, noServed: true}
	})
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, gitOffW(3)), nil
	})
	_, err := e.create()
	if !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("error = %v, want the harness-unavailable refusal", err)
	}
	if e.ev.count("adapter.open") != 0 {
		t.Fatal("a runtime opened with guidance that could not be rendered")
	}
	if e.ev.count("endpoint.close") != e.ev.count("endpoint.openWithPolicy") {
		t.Fatalf("an endpoint was left open: %v", e.ev.all())
	}
	if list, _ := e.svc.ListSessions(t.Context()); len(list) != 0 {
		t.Fatalf("%d sessions were stored", len(list))
	}
}

// Two sessions of one process get their own value; changing one value changes
// nothing in the other.
func TestTwoSessionsKeepTheirOwnServedValue(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, gitOffW(1)), nil
	})
	e.mustCreate()
	first := servedOf(e)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, nil), nil
	})
	e.mustCreate()
	second := servedOf(e)
	if first == second || len(first.Tools) == len(second.Tools) {
		t.Fatalf("first %v second %v", first.Tools, second.Tools)
	}
	first.Tools[0] = "mutated"
	if servedOf(e).Tools[0] == "mutated" {
		t.Fatal("sessions share the served slice")
	}
}
