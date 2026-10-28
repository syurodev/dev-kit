package harnessconfig

import (
	"reflect"
	"strings"
	"testing"

	"devkit/internal/harness"
)

func TestInspectStaleOverrideFindsOnlyUnregisteredGroupsAndRepairKeepsTheRest(t *testing.T) {
	o := &harness.Override{Schema: harness.SchemaVersion, Revision: 4, Groups: []string{"pdf", "git", "pdf", "ghost", "tasks"}}
	got := InspectStaleOverride(o, registry())
	if want := []string{"ghost", "pdf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale = %v, want %v", got, want)
	}
	rep := RepairedOverride(o, got)
	if rep == nil || !reflect.DeepEqual(rep.Groups, []string{"git", "tasks"}) || rep.Schema != harness.SchemaVersion {
		t.Fatalf("repaired = %+v", rep)
	}
	if len(o.Groups) != 5 {
		t.Fatal("the stored value was modified in place")
	}
	// Removal only: the repaired set is a subset of the stored one.
	for _, g := range rep.Groups {
		found := false
		for _, s := range o.Groups {
			found = found || s == g
		}
		if !found {
			t.Fatalf("%q was added", g)
		}
	}
}

func TestARepairLeavingNothingDeletesTheOverrideAndNothingStaleIsNothing(t *testing.T) {
	o := &harness.Override{Schema: harness.SchemaVersion, Groups: []string{"pdf"}}
	if RepairedOverride(o, []string{"pdf"}) != nil {
		t.Fatal("an empty override must be deleted, as an ordinary revoke leaves it")
	}
	if got := InspectStaleOverride(&harness.Override{Schema: harness.SchemaVersion, Groups: []string{"git"}}, registry()); len(got) != 0 {
		t.Fatalf("stale = %v", got)
	}
	if InspectStaleOverride(nil, registry()) != nil || RepairedOverride(nil, nil) != nil {
		t.Fatal("no override is nothing")
	}
}

// The review is host text around a bounded, printable name and says what it does:
// a removal, with nothing turned on.
func TestTheSessionRepairReviewListsEveryRemovalAndStatesAMereRemoval(t *testing.T) {
	v, err := buildSessionRepairView([]string{"pdf", "bad\x00‮name"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Target != TargetSessionRepair || v.Tier != "narrowing" || !v.NeedsConfirmation || len(v.Changes) != 2 {
		t.Fatalf("view = %+v", v)
	}
	for _, want := range []string{`"pdf"`, "removal only", "Nothing is turned on", "not started or reloaded now"} {
		if !strings.Contains(v.Summary, want) {
			t.Errorf("summary lacks %q: %s", want, v.Summary)
		}
	}
	if strings.ContainsAny(v.Summary, "\x00‮") {
		t.Fatalf("a stored name reached the review unescaped: %q", v.Summary)
	}
}

func TestTheStaleOverrideRefusalNamesTheWayOutAndNoStoredName(t *testing.T) {
	e := harness.ErrStaleSessionOverride
	if e.Code != harness.CodeSnapshotUnavailable {
		t.Fatalf("code = %s", e.Code)
	}
	for _, want := range []string{"Revoke", "Tool groups", "open the session again", "Closing the session"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message lacks %q: %s", want, e.Message)
		}
	}
}
