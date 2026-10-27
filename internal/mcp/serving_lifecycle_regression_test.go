package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

type closeLifecycleFunc func(context.Context, string) error

func (f closeLifecycleFunc) SessionClosed(ctx context.Context, id string) error { return f(ctx, id) }

func TestCloseFencesReopenUntilOverrideCleanupFinishes(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	var reopenErr error
	r.svc.HarnessLifecycle = closeLifecycleFunc(func(ctx context.Context, id string) error {
		// The durable closed status is already written. Opening must still wait
		// for the tombstone, rather than preparing from the old override.
		reopenErr = r.svc.ResumeSession(identityUI(), id, false)
		_, err := r.hstore.ClearOverride(ctx, id)
		return err
	})
	if err := r.svc.CloseSession(identityUI(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reopenErr, agenthub.ErrHarnessCleanupFailed) {
		t.Fatalf("reopen during cleanup: %v", reopenErr)
	}
	_, stamp := r.override(sess.ID)
	if stamp.Present || stamp.Revision != 2 {
		t.Fatalf("override: %+v", stamp)
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := callStatus(r, r.connect(r.mock.LastSpec())); !strings.Contains(got, "harness.groupOff") {
		t.Fatalf("reopened endpoint ignored the Close tombstone: %s", got)
	}
}

func TestFailedCloseCleanupKeepsReopenBlockedUntilRetry(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	r.svc.HarnessLifecycle = closeLifecycleFunc(func(context.Context, string) error { return errors.New("injected cleanup failure") })
	if err := r.svc.CloseSession(identityUI(), sess.ID); !errors.Is(err, agenthub.ErrHarnessCleanupFailed) {
		t.Fatal(err)
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, false); !errors.Is(err, agenthub.ErrHarnessCleanupFailed) {
		t.Fatalf("failed cleanup must not reopen with old S: %v", err)
	}
	r.svc.HarnessLifecycle = liveClearer{r.hstore}
	if err := r.svc.CloseSession(identityUI(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := callStatus(r, r.connect(r.mock.LastSpec())); !strings.Contains(got, "harness.groupOff") {
		t.Fatal(got)
	}
}

type heldAppliedStore struct {
	*agenthub.SQLStore
	beforeWrite func()
}

func (s *heldAppliedStore) SetHarnessApplied(ctx context.Context, id string, ref harness.SnapshotRef, enforced bool, at time.Time) error {
	if s.beforeWrite != nil {
		s.beforeWrite()
	}
	return s.SQLStore.SetHarnessApplied(ctx, id, ref, enforced, at)
}

// Use the same real SQL/endpoint/proposal stack, decorating only the durable
// applied-reference write to hold the exact old check-to-publication gap.
func useAppliedStore(r *liveRig, st *heldAppliedStore) {
	reg := agenthub.NewRegistry()
	reg.Register(r.loader)
	r.svc = agenthub.NewService(reg, st, func(string, json.RawMessage) {})
	r.svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	r.svc.MCPEndpoints, r.svc.Harness, r.svc.HarnessLifecycle = r.host, r.snaps, liveClearer{r.hstore}
	r.hsvc = harnessconfig.New(harnessconfig.Config{Store: r.faults, Directory: liveDirectory{r.svc}, Vendors: liveVendors{},
		Policy: livePolicy{}, Validator: liveValidator{}, Registry: func() harness.Registry { return r.inventory().Registry() },
		Confirmer: r.confirm, Boundary: r.boundary, Live: liveLive{r.svc}})
}

func TestFinalLaunchPublicationSerializesWithCloseAndApply(t *testing.T) {
	for _, operation := range []string{"close", "apply"} {
		t.Run(operation, func(t *testing.T) {
			r := newLiveRig(t)
			st := &heldAppliedStore{SQLStore: agenthub.NewSQLStore(r.db)}
			useAppliedStore(r, st)
			r.workspaceOff("vault")
			sess, _ := r.create()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			st.beforeWrite = func() { close(entered); <-release }
			resumeDone := make(chan error, 1)
			go func() { resumeDone <- r.svc.ResumeSession(identityUI(), sess.ID, true) }()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("resume did not reach applied-reference write")
			}
			mutationDone := make(chan error, 1)
			go func() {
				if operation == "close" {
					mutationDone <- r.svc.CloseSession(identityUI(), sess.ID)
					return
				}
				p, err := r.propose(sess.ID, []string{"vault"}, nil)
				if err == nil {
					_, err = r.hsvc.Apply(r.agent(sess.ID), p.ID)
				}
				mutationDone <- err
			}()
			// This is a blocking assertion, not ordering by a sleep: neither
			// mutation may complete while the durable write is explicitly held.
			select {
			case err := <-mutationDone:
				unblock()
				<-resumeDone
				t.Fatalf("%s crossed the final publication boundary: %v", operation, err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			select {
			case err := <-resumeDone:
				// Close may win immediately after publication; completeResume
				// must then refuse to change the closed row back to idle.
				if err != nil && !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("resume did not settle")
			}
			select {
			case err := <-mutationDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("mutation did not settle")
			}
			if operation == "close" {
				row, err := st.GetSession(context.Background(), sess.ID)
				if err != nil || row.Status != agenthub.SessionStatusClosed || r.status(sess.ID).Live {
					t.Fatalf("Close lost to resume: %+v %v", row, err)
				}
			} else if got := callStatus(r, r.connect(r.mock.LastSpec())); strings.Contains(got, "harness.groupOff") {
				t.Fatalf("post-publication Apply was lost: %s", got)
			}
		})
	}
}

func TestStagedEndpointDoesNotClaimLiveAndPublicationFailureStaysNotLive(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	r.loader.onLoad = func() {
		if r.status(sess.ID).Live {
			t.Error("staged endpoint is not a published vendor runtime")
		}
		res, err := r.apply(sess.ID, []string{"vault"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Live || res.Publication.State != harnessconfig.PublicationStaged {
			t.Errorf("staged result: %+v", res)
		}
		if got := r.svc.ReplacementGuidance(sess.ID, "vault", agenthub.AgentTypeMock); got != "" {
			t.Error("staged publication must not return live replacement guidance")
		}
	}
	err := r.svc.ResumeSession(identityUI(), sess.ID, true)
	r.loader.onLoad = nil
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatal(err)
	}
	if r.status(sess.ID).Live {
		t.Fatal("discarded launch is not live")
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, false); err != nil {
		t.Fatal(err)
	}
	if !r.status(sess.ID).Live {
		t.Fatal("successfully published replacement should be live")
	}
}
