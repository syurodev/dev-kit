package harnessconfig

import (
	"errors"
	"strings"
	"testing"

	"devkit/internal/harness"
)

func installed() Publication {
	return Publication{State: PublicationInstalled, Serving: harness.ServingIdentity{Session: harness.Stamp{Revision: 3, Present: true}, Fingerprint: "fp"}}
}

func TestOutcomeNeverClaimsMoreThanTheCommitAndTheEndpointProved(t *testing.T) {
	committed := func(pub Publication, live bool) Result {
		return Result{Profile: harness.Stamp{Revision: 2, Present: true}, Override: harness.Stamp{Revision: 3, Present: true}, Commit: CommitCommitted, Publication: pub, Live: live}
	}
	cases := []struct {
		name   string
		res    Result
		err    error
		status OutcomeStatus
		live   bool
	}{
		{"installed", committed(installed(), true), nil, OutcomeApplied, true},
		{"stored only", committed(Publication{}, false), nil, OutcomeApplied, false},
		{"staged", committed(Publication{State: PublicationStaged}, true), nil, OutcomeStaged, false},
		// A claimed-live result with a pending publication is still not live.
		{"pending claimed live", committed(Publication{State: PublicationPending}, true), &PublicationError{Result: committed(Publication{State: PublicationPending}, true), Cause: errors.New("x")}, OutcomePaused, false},
		{"unknown commit claimed live", Result{Commit: CommitUnknown, Publication: Publication{State: PublicationPending}, Live: true}, &PublicationError{Result: Result{Commit: CommitUnknown, Publication: Publication{State: PublicationPending}, Live: true}, Cause: errors.New("x")}, OutcomeUnknown, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := OutcomeOf(TargetSession, c.res, c.err)
			if err != nil {
				t.Fatal(err)
			}
			if out.Status != c.status || out.Live != c.live {
				t.Fatalf("status %q live %v, want %q %v: %+v", out.Status, out.Live, c.status, c.live, out)
			}
			if (c.status == OutcomePaused || c.status == OutcomeUnknown) && (out.Code != harness.CodeSnapshotUnavailable || out.Recovery == "" || !strings.Contains(strings.ToLower(out.Recovery), "reconcile")) {
				t.Fatalf("a failure outcome carries its public code and a recovery that points to reconcile: %+v", out)
			}
		})
	}
}

func TestOutcomeCarriesStampsAndServingIdentityThroughAFailure(t *testing.T) {
	res := Result{Profile: harness.Stamp{Revision: 2, Present: true}, Override: harness.Stamp{Revision: 3, Present: true}, Commit: CommitCommitted, ReloadRequired: true,
		Publication: Publication{State: PublicationPending, Serving: harness.ServingIdentity{Session: harness.Stamp{Revision: 2, Present: true}, Fingerprint: "old"}}}
	out, err := OutcomeOf(TargetWorkspace, res, &PublicationError{Result: res, Cause: errors.New("disk")})
	if err != nil {
		t.Fatal(err)
	}
	if out.ProfileRevision != 2 || !out.ProfilePresent || out.OverrideRevision != 3 || !out.OverridePresent ||
		out.ServingOverrideRevision != 2 || out.ServingFingerprint != "old" || !out.ReloadRequired || out.Target != "workspace" {
		t.Fatalf("stamps, serving identity and the reload fact must survive a failure: %+v", out)
	}
	if strings.Contains(out.Message+out.Recovery, "disk") {
		t.Fatalf("the cause must not reach the outcome: %+v", out)
	}
}

func TestARefusalBeforeTheWriteIsNotAnOutcome(t *testing.T) {
	for _, err := range []error{ErrStale, ErrNotConfirmed, ErrConflict, errors.New("boom")} {
		if out, got := OutcomeOf(TargetSession, Result{}, err); got != err || out.Status != "" {
			t.Fatalf("a refusal saved nothing, so it is returned as is: %+v %v", out, got)
		}
	}
}

func TestReconcileOutcomes(t *testing.T) {
	ok, err := OutcomeOfReconcile(installed(), nil)
	if err != nil || ok.Status != OutcomeApplied || !ok.Live || ok.Commit != CommitNone {
		t.Fatalf("installed: %+v %v", ok, err)
	}
	none, err := OutcomeOfReconcile(Publication{}, nil)
	if err != nil || none.Status != OutcomeApplied || none.Live {
		t.Fatalf("nothing running is not live: %+v %v", none, err)
	}
	failed, err := OutcomeOfReconcile(Publication{State: PublicationInstalled}, &PublicationError{Cause: errors.New("x")})
	if err != nil || failed.Status != OutcomePaused || failed.Live || failed.Publication != PublicationPending {
		t.Fatalf("a failed reconcile leaves the session paused: %+v %v", failed, err)
	}
	if _, err := OutcomeOfReconcile(Publication{}, ErrScopeGone); err != ErrScopeGone {
		t.Fatalf("a refusal is returned as is: %v", err)
	}
}
