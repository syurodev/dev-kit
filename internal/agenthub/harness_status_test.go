package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

func (e *launchEnv) status(id string) agenthub.HarnessStatus {
	e.t.Helper()
	st, err := e.svc.HarnessStatus(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func TestStatusSeparatesDesiredAppliedAndTheirRelationship(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	st := e.status(s.ID)
	if st.State != agenthub.HarnessCurrent || st.Desired == nil || st.Applied == nil || !st.Desired.Equal(*st.Applied) || st.Live || st.Enforced || !st.AppliedFactsComplete {
		t.Fatalf("freshly opened = %+v", st)
	}

	// W is edited and saved while the session runs: desired moves, applied does not.
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 2} }), nil
	})
	st = e.status(s.ID)
	if st.State != agenthub.HarnessReloadRequired || st.Applied.Revisions.Workspace != nil || *st.Desired.Revisions.Workspace != 2 {
		t.Fatalf("a saved W edit must read as reload-required, not applied: %+v", st)
	}

	// An executable gain from W needs a new session, not a reload.
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) {
			in.Workspace = &harness.Profile{Schema: 1, Revision: 3, Servers: []harness.Server{{Name: "db", Command: "c", Enabled: true}}}
		}), nil
	})
	if st = e.status(s.ID); st.State != agenthub.HarnessNewSessionRequired {
		t.Fatalf("state = %s", st.State)
	}
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("the status must agree with what a reload does: %v", err)
	}

	// The reload that is allowed brings applied back to desired.
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 2} }), nil
	})
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	if st = e.status(s.ID); st.State != agenthub.HarnessCurrent {
		t.Fatalf("after the reload: %+v", st)
	}
}

func TestStatusNeverReadsMissingDataAsApplied(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.svc.Harness = nil
	s := e.mustCreate()
	if st := e.status(s.ID); st.State != agenthub.HarnessUnconfigured || st.Applied != nil || st.Desired != nil {
		t.Fatalf("no provider = %+v", st)
	}

	// A provider appears after the session opened: nothing was applied under it.
	e.svc.Harness = e.harness
	st := e.status(s.ID)
	if st.State != agenthub.HarnessNotApplied || st.Applied != nil || st.Desired == nil {
		t.Fatalf("not applied = %+v", st)
	}

	// The saved configuration cannot be read: unavailable, with the applied record
	// still shown and nothing invented for desired.
	e.mustReloadWith(s.ID)
	e.harness.set(func(string, string, string) (harness.Snapshot, error) {
		return harness.Snapshot{}, errors.New("corrupt")
	})
	st = e.status(s.ID)
	if st.State != agenthub.HarnessUnavailable || st.Desired != nil || st.Applied == nil {
		t.Fatalf("unavailable = %+v", st)
	}
	if _, err := e.svc.HarnessStatus(context.Background(), "nope"); err == nil {
		t.Fatal("an unknown session has no status")
	}
}

func (e *launchEnv) mustReloadWith(id string) {
	e.t.Helper()
	if err := e.reload(id); err != nil {
		e.t.Fatal(err)
	}
}

func TestStatusReportsIncompleteVendorFactsAndEnforcement(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		snap := snapshotFor(t, sid, ws, v, gitOffW(2))
		snap.Ref.FactsComplete = false
		return snap, nil
	})
	s := e.mustCreate()
	st := e.status(s.ID)
	if !st.Enforced || st.AppliedFactsComplete || st.State != agenthub.HarnessCurrent || st.Live {
		t.Fatalf("%+v", st)
	}
}

func TestAFailedOpenChangesNeitherTheAppliedRecordNorTheStatus(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	before := e.status(s.ID)
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 2} }), nil
	})
	e.adapter.setErr(errors.New("spawn failed"))
	if err := e.reload(s.ID); err == nil {
		t.Fatal("expected a failed open")
	}
	after := e.status(s.ID)
	if !after.Applied.Equal(*before.Applied) || after.State != agenthub.HarnessReloadRequired {
		t.Fatalf("a failed open reported progress: before %+v after %+v", before, after)
	}
}

// Attribution: turn rows carry the revisions the runtime actually had at the
// start of the turn, and only when the endpoint enforced them.
func TestTurnRowsCarryTheAppliedRevisionsAtTurnStart(t *testing.T) {
	e := newLaunchEnv(t, policy)
	withRevs := func(w, sess int64) func(string, string, string) (harness.Snapshot, error) {
		return func(sid, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) {
				gitOffW(w)(in)
				in.Session = &harness.Override{Schema: 1, Revision: sess}
			}), nil
		}
	}
	e.harness.set(withRevs(3, 5))
	s := e.mustCreate()
	turn := func(prompt string) {
		if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: prompt}); err != nil {
			t.Fatal(err)
		}
	}
	turn("one")
	// The saved configuration moves while the runtime keeps what it opened with:
	// the next turn is still attributed to what was applied, not to what is saved.
	e.harness.set(withRevs(4, 6))
	turn("two")
	e.mustReloadWith(s.ID)
	turn("three")

	rows, err := agenthub.NewSQLStore(e.db).ListTurns(context.Background(), s.ID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("turns = %d %v", len(rows), err)
	}
	want := [][2]int64{{3, 5}, {3, 5}, {4, 6}}
	for i, row := range rows {
		a := row.Attribution
		if a == nil || a.Facts.ProfileRevision == nil || a.Facts.OverrideRevision == nil || *a.Facts.ProfileRevision != want[i][0] || *a.Facts.OverrideRevision != want[i][1] {
			t.Fatalf("turn %d attribution = %+v, want W%d S%d", i+1, a, want[i][0], want[i][1])
		}
	}
}

func TestTurnRowsStayUnknownWhenTheEndpointDoesNotEnforce(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 9} }), nil
	})
	s := e.mustCreate()
	if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := agenthub.NewSQLStore(e.db).ListTurns(context.Background(), s.ID)
	if len(rows) != 1 || rows[0].Attribution == nil {
		t.Fatalf("rows = %+v", rows)
	}
	if f := rows[0].Attribution.Facts; f.ProfileRevision != nil || f.OverrideRevision != nil {
		t.Fatalf("a revision was stamped for a configuration that was not applied: %+v", f)
	}
}

// Coverage is part of what was applied: losing the vendor facts is a difference
// even when the fingerprint is unchanged.
func TestStatusNoticesAChangeInFactCoverage(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		snap := snapshotFor(t, sid, ws, v, nil)
		snap.Ref.FactsComplete = false
		return snap, nil
	})
	if st := e.status(s.ID); st.State != agenthub.HarnessReloadRequired {
		t.Fatalf("state = %s", st.State)
	}
}
