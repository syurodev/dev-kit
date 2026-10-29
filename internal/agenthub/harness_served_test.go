package agenthub_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// gitOffBasis is a snapshot with a pinned serving basis whose workspace turns the
// git group off, as the provider returns one.
func gitOffBasis(t *testing.T, session, ws, vendor string) (harness.Snapshot, error) {
	t.Helper()
	in := harness.Inputs{Registry: tinyRegistry, Vendor: harness.Vendor{Revision: 4}, Workspace: &harness.Profile{Schema: 1, Revision: 1, ToolGroups: map[string]bool{"git": false}}}
	snap := snapshotFor(t, session, ws, vendor, func(i *harness.Inputs) { *i = in })
	basis, err := harness.NewServingBasis(in, snap.Effective, harness.NewCanonRecorder(func(p string) (string, error) { return p, nil }),
		harness.Stamp{Revision: 1, Present: true}, harness.Stamp{})
	if err != nil {
		return harness.Snapshot{}, err
	}
	snap.Basis = basis
	return snap, nil
}

func served(t *testing.T, e *launchEnv, id string) (string, bool) {
	t.Helper()
	tools, live := e.svc.ServedTools(id)
	return strings.Join(tools, ","), live
}

func TestServedToolsReportsTheAcknowledgedSurfaceNotTheLaunch(t *testing.T) {
	lives := &liveLog{}
	e := newLaunchEnv(t, func(ev *events) agenthub.MCPEndpoints {
		return liveEndpoints{policyEndpoints: &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}}, log: lives, opens: &atomic.Int32{}}
	})
	e.harness.set(func(session, ws, vendor string) (harness.Snapshot, error) { return gitOffBasis(t, session, ws, vendor) })
	s := e.mustCreate()
	ctx := context.Background()

	if got, live := served(t, e, s.ID); !live || got != "harness.profile.propose,tasks.list" {
		t.Fatalf("launch surface = %q live=%v", got, live)
	}
	// A live grant (S enabling git) is on the acknowledged surface, not only the launch's.
	grant := func(context.Context) (*harness.Override, harness.Stamp, error) {
		return &harness.Override{Schema: 1, Revision: 1, Groups: []string{"git"}}, harness.Stamp{Revision: 1, Present: true}, nil
	}
	if _, err := e.svc.ReconcileServing(ctx, s.ID, grant); err != nil {
		t.Fatal(err)
	}
	if got, live := served(t, e, s.ID); !live || got != "git.status,harness.profile.propose,tasks.list" {
		t.Fatalf("after the live grant = %q live=%v", got, live)
	}
	// The revoke removes it again: a group enabled by the launch is not the same
	// as a group served now.
	revoke := func(context.Context) (*harness.Override, harness.Stamp, error) {
		return nil, harness.Stamp{Revision: 2}, nil
	}
	if _, err := e.svc.ReconcileServing(ctx, s.ID, revoke); err != nil {
		t.Fatal(err)
	}
	if got, live := served(t, e, s.ID); !live || got != "harness.profile.propose,tasks.list" {
		t.Fatalf("after the live revoke = %q live=%v", got, live)
	}
	// The returned slice is a copy.
	tools, _ := e.svc.ServedTools(s.ID)
	tools[0] = "mutated"
	if got, _ := served(t, e, s.ID); got != "harness.profile.propose,tasks.list" {
		t.Fatalf("the read leaked its backing array: %q", got)
	}
	// A closed endpoint is not live and serves nothing.
	lives.last().close()
	if got, live := served(t, e, s.ID); live || got != "" {
		t.Fatalf("closed endpoint = %q live=%v", got, live)
	}
	if got, live := served(t, e, "no-such-session"); live || got != "" {
		t.Fatalf("unknown session = %q live=%v", got, live)
	}
}

func TestServedToolsIsNotLiveWithoutAnEnforcingEndpoint(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	if got, live := served(t, e, s.ID); live || got != "" {
		t.Fatalf("a legacy endpoint cannot enforce, so nothing is live: %q %v", got, live)
	}
}
