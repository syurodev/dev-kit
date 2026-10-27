package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/storage"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The whole path, with nothing mocked but the vendor: a real H2 proposal service,
// a real SQLite HarnessStore, a real Agent Hub service (launch fences,
// coordinator), the real SessionHost and the real SDK client. The scripted
// "mock" vendor is the only stand-in; no model, account or network is involved.

type liveSnapshots struct{ p *harnessconfig.Provider }

func (l liveSnapshots) Snapshot(ctx context.Context, session, ws, vendor string) (harness.Snapshot, error) {
	return l.p.Snapshot(ctx, harnessconfig.SnapshotRequest{SessionID: session, WorkspaceID: ws, Vendor: vendor})
}

// hookedSnapshots lets a test act between a launch reading S and publishing.
type hookedSnapshots struct {
	liveSnapshots
	mu    sync.Mutex
	after func(session string)
	reads int
}

func (h *hookedSnapshots) Snapshot(ctx context.Context, session, ws, vendor string) (harness.Snapshot, error) {
	snap, err := h.liveSnapshots.Snapshot(ctx, session, ws, vendor)
	h.mu.Lock()
	h.reads++
	after := h.after
	h.mu.Unlock()
	if after != nil {
		after(session)
	}
	return snap, err
}

// loadingMock is the scripted vendor with a resumable session: Load reopens the
// conversation in a new scripted process, as a vendor's own resume does.
type loadingMock struct {
	*agenthub.MockAdapter
	mu     sync.Mutex
	onLoad func()
}

func (l *loadingMock) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	return l.Open(ctx, spec)
}

// Open runs the test's hook first: the vendor is starting and the endpoint is
// already registered. A session nobody has prompted starts over through Open.
func (l *loadingMock) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	l.mu.Lock()
	hook := l.onLoad
	l.mu.Unlock()
	if hook != nil {
		hook()
	}
	return l.MockAdapter.Open(ctx, spec)
}

type liveClearer struct{ s *storage.HarnessStore }

func (l liveClearer) SessionClosed(ctx context.Context, id string) error {
	_, err := l.s.ClearOverride(ctx, id)
	return err
}

type liveVendors struct{}

func (liveVendors) Names() []string { return []string{"mock"} }
func (liveVendors) State(context.Context, string) (harnessconfig.VendorState, error) {
	return harnessconfig.VendorState{Vendor: harness.Vendor{Revision: 1}, FactsComplete: true}, nil
}

type liveDirectory struct{ svc *agenthub.Service }

func (d liveDirectory) SessionScope(ctx context.Context, id string) (harnessconfig.SessionScope, error) {
	ws, vendor, err := d.svc.HarnessScope(ctx, id)
	if err != nil {
		return harnessconfig.SessionScope{}, err
	}
	return harnessconfig.SessionScope{WorkspaceID: ws, Vendor: string(vendor)}, nil
}
func (liveDirectory) WorkspaceExists(_ context.Context, id string) (bool, error) {
	return id == "w1", nil
}

type livePolicy struct{}

func (livePolicy) Policy(context.Context) (harnessconfig.Policy, error) {
	return harnessconfig.Policy{Revision: "p"}, nil
}

type liveValidator struct{}

func (liveValidator) Validate(context.Context, harness.Profile) error { return nil }

type liveConfirm struct {
	mu     sync.Mutex
	asked  []harnessconfig.Confirmation
	during func()
}

func (*liveConfirm) Available() bool { return true }
func (c *liveConfirm) Confirm(_ context.Context, q harnessconfig.Confirmation) error {
	c.mu.Lock()
	c.asked = append(c.asked, q)
	during := c.during
	c.mu.Unlock()
	if during != nil {
		during()
	}
	return nil
}

type liveBoundary struct{ mu sync.Mutex }

func (b *liveBoundary) Hold(_ context.Context, fn func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fn()
}

// liveFaults decorates the real store with the failures a test scripts.
type liveFaults struct {
	harnessconfig.Store
	mu       sync.Mutex
	mode     string // "", "conflict", "unknownAfter"
	failRead error  // GetOverride error once a write has landed
	wrote    bool
	// durable runs once the real write has committed, before Apply returns: the
	// moment between "the new S is durable" and "the endpoint serves it".
	durable func()
}

func (f *liveFaults) set(mode string, failRead error) {
	f.mu.Lock()
	f.mode, f.failRead, f.wrote = mode, failRead, false
	f.mu.Unlock()
}

func (f *liveFaults) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	f.mu.Lock()
	mode := f.mode
	f.mu.Unlock()
	if mode == "conflict" {
		return harness.Applied{}, &harness.ConflictError{Target: "override"}
	}
	a, err := f.Store.Apply(ctx, w)
	if err != nil {
		return a, err
	}
	f.mu.Lock()
	f.wrote = true
	during := f.durable
	f.mu.Unlock()
	if during != nil {
		during()
	}
	if mode == "unknownAfter" {
		return harness.Applied{}, fmt.Errorf("%w: connection lost", harness.ErrStoreCommitUnknown)
	}
	return a, nil
}

func (f *liveFaults) GetOverride(ctx context.Context, session, ws string) (*harness.Override, harness.Stamp, error) {
	f.mu.Lock()
	fail := f.wrote && f.failRead != nil
	err := f.failRead
	f.mu.Unlock()
	if fail {
		return nil, harness.Stamp{}, err
	}
	return f.Store.GetOverride(ctx, session, ws)
}

// liveLive is the composition root's adapter over Agent Hub's coordinator, as
// internal/app wires it.
type liveLive struct{ svc *agenthub.Service }

func (l liveLive) Target(_ context.Context, id string) (harnessconfig.LiveTarget, error) {
	return toLiveTarget(l.svc.ServingTarget(id)), nil
}
func (l liveLive) Coordinate(ctx context.Context, id string, fn func(harnessconfig.LiveTxn) error) error {
	return l.svc.CoordinateServing(ctx, id, func(t *agenthub.ServingTxn) error { return fn(liveTx{t}) })
}
func (l liveLive) Reconcile(ctx context.Context, id string, read func(context.Context) (*harness.Override, harness.Stamp, error)) (harnessconfig.Publication, error) {
	p, err := l.svc.ReconcileServing(ctx, id, read)
	return toPublication(p), err
}

func toLiveTarget(v agenthub.ServingView) harnessconfig.LiveTarget {
	return harnessconfig.LiveTarget{Active: v.Active, Incarnation: v.Incarnation, Epoch: v.Epoch, Basis: v.Basis, Serving: v.Serving, Quarantined: v.Quarantined}
}

func toPublication(p agenthub.ServingPublication) harnessconfig.Publication {
	out := harnessconfig.Publication{Serving: p.Serving, ToolsChanged: p.ToolsChanged, NotificationScheduled: p.NotificationScheduled}
	switch {
	case p.Installed:
		out.State = harnessconfig.PublicationInstalled
	case p.Pending:
		out.State = harnessconfig.PublicationPending
	}
	return out
}

type liveTx struct{ t *agenthub.ServingTxn }

func (x liveTx) Target() harnessconfig.LiveTarget { return toLiveTarget(x.t.Target()) }
func (x liveTx) Quarantine() error                { return x.t.Quarantine() }
func (x liveTx) Restore() error                   { return x.t.Restore() }
func (x liveTx) MarkPending()                     { x.t.MarkPending() }
func (x liveTx) Publish(s harness.Stamp, o *harness.Override) (harnessconfig.Publication, error) {
	p, err := x.t.Publish(s, o)
	return toPublication(p), err
}

type liveRig struct {
	*servingRig
	db       *sql.DB
	hstore   *storage.HarnessStore
	faults   *liveFaults
	mock     *agenthub.MockAdapter
	loader   *loadingMock
	svc      *agenthub.Service
	snaps    *hookedSnapshots
	hsvc     *harnessconfig.Service
	confirm  *liveConfirm
	boundary *liveBoundary
}

func newLiveRig(t *testing.T) *liveRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	r := &liveRig{servingRig: newServingRig(t, nil), db: db, hstore: storage.NewHarnessStore(db), mock: agenthub.NewMockAdapter(),
		confirm: &liveConfirm{}, boundary: &liveBoundary{}}
	registry := func() harness.Registry { return r.inventory().Registry() }
	provider := harnessconfig.NewProvider(harnessconfig.ProviderConfig{Store: r.hstore, Vendors: liveVendors{}, Registry: registry})
	reg := agenthub.NewRegistry()
	r.loader = &loadingMock{MockAdapter: r.mock}
	reg.Register(r.loader)
	r.svc = agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
	r.svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	r.svc.MCPEndpoints = r.host
	r.snaps = &hookedSnapshots{liveSnapshots: liveSnapshots{provider}}
	r.svc.Harness = r.snaps
	r.svc.HarnessLifecycle = liveClearer{r.hstore}
	r.faults = &liveFaults{Store: r.hstore}
	r.hsvc = harnessconfig.New(harnessconfig.Config{Store: r.faults, Directory: liveDirectory{r.svc}, Vendors: liveVendors{},
		Policy: livePolicy{}, Validator: liveValidator{}, Registry: registry, Confirmer: r.confirm, Boundary: r.boundary, Live: liveLive{r.svc}})
	return r
}

// workspaceOff stores a workspace profile with the groups off, as the person
// would before a session launches.
func (r *liveRig) workspaceOff(groups ...string) harness.Stamp {
	r.t.Helper()
	off := map[string]bool{}
	for _, g := range groups {
		off[g] = false
	}
	_, st, err := r.hstore.GetProfile(context.Background(), "w1")
	if err != nil {
		r.t.Fatal(err)
	}
	a, err := r.hstore.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1",
		Profile: &harness.ProfileWrite{Expect: st, Value: &harness.Profile{Schema: harness.SchemaVersion, ToolGroups: off}}})
	if err != nil {
		r.t.Fatal(err)
	}
	return a.Profile
}

func (r *liveRig) create() (agenthub.AgentSession, agenthub.OpenSpec) {
	r.t.Helper()
	sess, err := r.svc.CreateSession(identityUI(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: r.t.TempDir(), WorkspaceID: "w1"})
	if err != nil {
		r.t.Fatal(err)
	}
	return sess, r.mock.LastSpec()
}

func (r *liveRig) agent(id string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(id))
}

func (r *liveRig) propose(sid string, enable, revoke []string) (harnessconfig.Prepared, error) {
	return r.hsvc.Prepare(r.agent(sid), harnessconfig.Request{Target: harnessconfig.TargetSession, Patch: harnessconfig.Patch{EnableGroups: enable, RevokeGroups: revoke}})
}

func (r *liveRig) apply(sid string, enable, revoke []string) (harnessconfig.Result, error) {
	r.t.Helper()
	p, err := r.propose(sid, enable, revoke)
	if err != nil {
		r.t.Fatalf("Prepare: %v", err)
	}
	return r.hsvc.Apply(r.agent(sid), p.ID)
}

func (r *liveRig) connect(spec agenthub.OpenSpec) *sdk.ClientSession {
	r.t.Helper()
	conn, err := net.Dial("unix", spec.AllowUnixSockets[0])
	if err != nil {
		r.t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(r.ctx, newRWTransport(conn), nil)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { cs.Close() })
	return cs
}

func (r *liveRig) override(sid string) (*harness.Override, harness.Stamp) {
	r.t.Helper()
	o, st, err := r.hstore.GetOverride(context.Background(), sid, "w1")
	if err != nil {
		r.t.Fatal(err)
	}
	return o, st
}

func hasVault(names []string) bool {
	for _, n := range names {
		if harness.Group(n) == "vault" {
			return true
		}
	}
	return false
}

func callStatus(r *liveRig, cs *sdk.ClientSession) string {
	r.t.Helper()
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
	if err != nil {
		r.t.Fatalf("call: %v", err)
	}
	return resultText(res)
}

func TestLiveEnableReachesTheRunningSessionAndKeepsTheLaunchReference(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	other, otherSpec := r.create()
	cs, cs2 := r.connect(spec), r.connect(otherSpec)
	if hasVault(listNames(t, cs)) {
		t.Fatal("vault should start hidden by the workspace profile")
	}
	refBefore, enforced, _ := r.svc.HarnessApplied(sess.ID)
	if !enforced || refBefore.Revisions.Session != nil {
		t.Fatalf("launch reference = %+v enforced=%v", refBefore, enforced)
	}

	res, err := r.apply(sess.ID, []string{"vault"}, nil)
	if err != nil || !res.Live || res.Publication.State != harnessconfig.PublicationInstalled || res.Commit != harnessconfig.CommitCommitted {
		t.Fatalf("apply = %+v %v", res, err)
	}
	if !hasVault(listNames(t, cs)) {
		t.Fatal("the running session's client must now see the vault tools")
	}
	if got := callStatus(r, cs); strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("the enabled tool must work: %s", got)
	}
	// Another running session, same workspace: its own endpoint, untouched.
	if hasVault(listNames(t, cs2)) || !strings.HasPrefix(callStatus(r, cs2), harness.CodeGroupOff) {
		t.Fatal("another session must keep its own policy")
	}
	if v := r.svc.ServingTarget(other.ID); v.Epoch != 0 || v.Serving.Session.Revision != 0 {
		t.Fatalf("other session's coordinator moved: %+v", v)
	}
	// The launch-applied reference and executable baseline do not move.
	refAfter, _, _ := r.svc.HarnessApplied(sess.ID)
	if !refAfter.Equal(refBefore) || refAfter.ExecutableDigest != refBefore.ExecutableDigest {
		t.Fatalf("a live S publication must not rewrite the launch reference:\n%+v\n%+v", refBefore, refAfter)
	}
	v := r.svc.ServingTarget(sess.ID)
	if !v.Acknowledged || v.Serving.Session != (harness.Stamp{Revision: 1, Present: true}) || v.Quarantined {
		t.Fatalf("serving = %+v", v)
	}
	// A new launch of the same session adopts the stored S.
	if err := r.svc.ResumeSession(identityUI(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if !contains(r.mock.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("a reload must resolve the latest durable S")
	}
	if ref, _, _ := r.svc.HarnessApplied(sess.ID); ref.Revisions.Session == nil || *ref.Revisions.Session != 1 {
		t.Fatalf("the reopened launch records S: %+v", ref.Revisions)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestLiveRevokeUsesThePinnedWorkspaceProfileNotTheSavedOne(t *testing.T) {
	r := newLiveRig(t)
	wst := r.workspaceOff("vault")
	sess, spec := r.create()
	cs := r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if !hasVault(listNames(t, cs)) {
		t.Fatal("vault should be on")
	}
	// The person edits the workspace profile afterwards: vault is now allowed by
	// the SAVED W. The session still runs under the W it opened with.
	if _, err := r.hstore.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1",
		Profile: &harness.ProfileWrite{Expect: wst, Value: &harness.Profile{Schema: harness.SchemaVersion}}}); err != nil {
		t.Fatal(err)
	}
	// Stored: revoking S changes nothing (saved W allows vault). Live: it removes
	// the tools. That is a real change and must not be rejected as a no-op.
	p, err := r.propose(sess.ID, nil, []string{"vault"})
	if err != nil {
		t.Fatalf("a live-only narrowing must be offered: %v", err)
	}
	if p.View.Tier != "narrowing" {
		t.Fatalf("tier = %s", p.View.Tier)
	}
	res, err := r.hsvc.Apply(r.agent(sess.ID), p.ID)
	if err != nil || !res.Live {
		t.Fatalf("apply = %+v %v", res, err)
	}
	if hasVault(listNames(t, cs)) {
		t.Fatal("the running session must lose vault: its pinned W disables it")
	}
	if got := callStatus(r, cs); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("a call to a revoked tool is refused: %s", got)
	}
	_, st := r.override(sess.ID)
	if st.Present || st.Revision != 2 {
		t.Fatalf("S should be a tombstone at revision 2, got %+v", st)
	}
}

func TestACallAdmittedBeforeTheCommitFinishesAndNoLaterCallUsesTheOldPolicy(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	cs, caller2 := r.connect(spec), r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	entered, release := r.caller.hold()
	defer release()
	done := make(chan string, 1)
	go func() { done <- callStatus(r, cs) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never reached the gateway")
	}
	// The revoke is committed and published while that call is still running: a
	// proposal can itself be a running call, so nothing waits for handlers.
	applied := make(chan error, 1)
	go func() { _, err := r.apply(sess.ID, nil, []string{"vault"}); applied <- err }()
	select {
	case err := <-applied:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Apply waited for a running handler")
	}
	before := r.caller.calls.Load()
	if got := callStatus(r, caller2); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("after the commit no call may use the old policy: %s", got)
	}
	if r.caller.calls.Load() != before {
		t.Fatal("a refused call reached the gateway")
	}
	release()
	select {
	case got := <-done:
		if strings.HasPrefix(got, harness.CodeGroupOff) || strings.HasPrefix(got, harness.CodeSnapshotUnavailable) {
			t.Fatalf("the call admitted before the change finishes under its entry policy: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the running call never finished")
	}
}

func TestNoCallIsAdmittedUnderTheOldPolicyOnceTheRevokeIsDurable(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	cs, probe := r.connect(spec), r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	// Between the durable write of the revoke and the endpoint serving it, the old
	// policy (vault on) is still the one the endpoint holds. Admission must already
	// be closed: a call here is refused, never served.
	var during string
	var listErr error
	r.faults.durable = func() {
		during = callStatus(r, probe)
		_, listErr = probe.ListTools(r.ctx, nil)
	}
	before := r.caller.calls.Load()
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	r.faults.durable = nil
	if !strings.HasPrefix(during, harness.CodeSnapshotUnavailable) || r.caller.calls.Load() != before {
		t.Fatalf("a call after the durable write was admitted under the old policy: %q", during)
	}
	if listErr == nil {
		t.Fatal("the tool list must not be offered while the registry is being rewritten")
	}
	if hasVault(listNames(t, cs)) {
		t.Fatal("after the publication vault is gone")
	}
}

func TestProvenRollbackReopensAdmissionUnderTheOldPolicy(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	cs := r.connect(spec)
	r.faults.set("conflict", nil)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
		t.Fatal("the lost compare-and-swap must fail the apply")
	}
	v := r.svc.ServingTarget(sess.ID)
	if v.Quarantined || !v.Acknowledged || v.Serving.Session.Revision != 0 {
		t.Fatalf("the prior ready state must be restored: %+v", v)
	}
	if hasVault(listNames(t, cs)) {
		t.Fatal("nothing was committed, vault stays hidden")
	}
	if got := callStatus(r, cs); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("admission is back under the old policy: %s", got)
	}
	if o, _ := r.override(sess.ID); o != nil {
		t.Fatalf("S = %+v", o)
	}
}

func TestAnAmbiguousCommitStaysClosedUntilReconciledAgainstTheLatestS(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	other, otherSpec := r.create()
	cs, cs2 := r.connect(spec), r.connect(otherSpec)
	r.faults.set("unknownAfter", errors.New("db gone"))
	res, err := r.apply(sess.ID, []string{"vault"}, nil)
	var pe *harnessconfig.PublicationError
	if !errors.As(err, &pe) {
		t.Fatalf("want a publication error, got %v", err)
	}
	if res.Live || res.Commit != harnessconfig.CommitUnknown {
		t.Fatalf("result = %+v", res)
	}
	// Durable truth: the write did land. The endpoint must not serve anything
	// until it is reconciled.
	if _, st := r.override(sess.ID); st.Revision != 1 || !st.Present {
		t.Fatalf("stored = %+v", st)
	}
	if !r.svc.ServingTarget(sess.ID).Quarantined {
		t.Fatal("admission must stay closed")
	}
	if got := callStatus(r, cs); !strings.HasPrefix(got, harness.CodeSnapshotUnavailable) {
		t.Fatalf("calls are refused while paused: %s", got)
	}
	if _, err := cs.ListTools(r.ctx, nil); err == nil {
		t.Fatal("no tool list while paused")
	}
	// The other session never noticed.
	if v := r.svc.ServingTarget(other.ID); v.Quarantined || hasVault(listNames(t, cs2)) {
		t.Fatalf("the other session was affected: %+v", v)
	}
	// Reconcile reads the latest durable S and installs it; twice is a no-op.
	r.faults.set("", nil)
	pub, err := r.hsvc.Reconcile(r.agent(sess.ID), sess.ID)
	if err != nil || pub.State != harnessconfig.PublicationInstalled {
		t.Fatalf("reconcile = %+v %v", pub, err)
	}
	if !hasVault(listNames(t, cs)) {
		t.Fatal("after reconciliation the session serves the stored S")
	}
	if v := r.svc.ServingTarget(sess.ID); !v.Acknowledged || v.Serving.Session != (harness.Stamp{Revision: 1, Present: true}) {
		t.Fatalf("serving = %+v", v)
	}
	if pub, err := r.hsvc.Reconcile(r.agent(sess.ID), sess.ID); err != nil || pub.State != harnessconfig.PublicationInstalled {
		t.Fatalf("a second reconcile must be a harmless no-op: %+v %v", pub, err)
	}
}

func TestACommitFollowedByAFailedInstallReportsStampsAndReconciles(t *testing.T) {
	// "hidden" cannot happen when a change only adds tools.
	for _, stage := range []string{"begin", "registered", "policy"} {
		t.Run(stage, func(t *testing.T) {
			r := newLiveRig(t)
			r.workspaceOff("vault")
			sess, spec := r.create()
			cs := r.connect(spec)
			boom := errors.New("injected")
			r.host.installHook = func(s string) error {
				if s == stage {
					return boom
				}
				return nil
			}
			res, err := r.apply(sess.ID, []string{"vault"}, nil)
			var pe *harnessconfig.PublicationError
			if !errors.As(err, &pe) || strings.Contains(err.Error(), "injected") {
				t.Fatalf("want a publication error without the cause, got %v", err)
			}
			if res.Live || res.Override != (harness.Stamp{Revision: 1, Present: true}) || res.Commit != harnessconfig.CommitCommitted {
				t.Fatalf("a committed change reports its stamps: %+v", res)
			}
			if !r.svc.ServingTarget(sess.ID).Quarantined {
				t.Fatal("a partial publication leaves the endpoint closed")
			}
			if _, err := cs.ListTools(r.ctx, nil); err == nil {
				t.Fatal("a half-updated registry must never be listed")
			}
			// Retrying publication is reconciliation, not another proposal.
			r.host.installHook = nil
			if pub, err := r.hsvc.Reconcile(r.agent(sess.ID), sess.ID); err != nil || pub.State != harnessconfig.PublicationInstalled {
				t.Fatalf("reconcile = %+v %v", pub, err)
			}
			if !hasVault(listNames(t, cs)) {
				t.Fatal("the committed S must be served after reconciliation")
			}
			if got := callStatus(r, cs); strings.HasPrefix(got, harness.CodeGroupOff) {
				t.Fatalf("%s", got)
			}
		})
	}
}

func TestAReconcileDoesNotReplayAnOlderCandidate(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, spec := r.create()
	cs := r.connect(spec)
	// A failed publication of "enable" ...
	r.host.installHook = func(string) error { return errors.New("injected") }
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
		t.Fatal("expected a publication error")
	}
	r.host.installHook = nil
	// ... then the revoke is committed while the endpoint is still paused. The
	// endpoint is paused so the proposal is refused; the person can still revoke
	// through the stored path (the lifecycle owner), which is what a close does.
	if _, err := r.propose(sess.ID, nil, []string{"vault"}); err == nil {
		t.Fatal("a paused session cannot be judged against, so it is refused")
	}
	if _, err := r.hstore.ClearOverride(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := r.hsvc.Reconcile(r.agent(sess.ID), sess.ID)
	if err != nil || pub.State != harnessconfig.PublicationInstalled {
		t.Fatalf("reconcile = %+v %v", pub, err)
	}
	if hasVault(listNames(t, cs)) {
		t.Fatal("the latest durable S is a tombstone: the older enable must not be replayed")
	}
	if v := r.svc.ServingTarget(sess.ID); v.Serving.Session != (harness.Stamp{Revision: 2}) {
		t.Fatalf("serving must name the tombstone: %+v", v.Serving)
	}
}

func TestACommittedChangeDuringALaunchMakesItStaleAndItIsPreparedAgain(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	first := true
	r.snaps.after = func(session string) {
		if session != sess.ID || !first {
			return
		}
		first = false
		// S is committed after the launch read it and before it published.
		if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
			t.Errorf("apply during launch: %v", err)
		}
	}
	if err := r.svc.ResumeSession(identityUI(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if r.snaps.reads < 3 {
		t.Fatalf("snapshot read %d times; the stale launch should have been prepared again", r.snaps.reads)
	}
	spec := r.mock.LastSpec()
	if !contains(spec.DevKit.Tools, "vault.status") {
		t.Fatal("the published launch must carry the S committed during it")
	}
	cs := r.connect(spec)
	if !hasVault(listNames(t, cs)) {
		t.Fatal("the endpoint must serve it too")
	}
}

func TestAnSChangeWhileTheVendorStartsReachesTheStagedEndpointAndDiscardsTheLaunch(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	var during harnessconfig.Result
	var duringErr error
	var staged bool
	r.loader.onLoad = func() {
		// The endpoint is registered with the coordinator before the vendor exists,
		// so a commit now reaches it like a published one.
		staged = r.svc.ServingTarget(sess.ID).Active
		during, duringErr = r.apply(sess.ID, []string{"vault"}, nil)
	}
	err := r.svc.ResumeSession(identityUI(), sess.ID, true)
	r.loader.onLoad = nil
	if duringErr != nil || !staged || !during.Live {
		t.Fatalf("the staged endpoint must be updated like a published one: staged=%v %+v %v", staged, during, duringErr)
	}
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("the launch read S before the commit and must be discarded, got %v", err)
	}
	if v := r.svc.ServingTarget(sess.ID); v.Active {
		t.Fatalf("the discarded launch left its endpoint behind: %+v", v)
	}
	// The next open resolves the latest durable S.
	if err := r.svc.ResumeSession(identityUI(), sess.ID, false); err != nil {
		t.Fatal(err)
	}
	if !contains(r.mock.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("the reopened launch must carry the committed S")
	}
}

func TestALaunchThatKeepsGoingStaleIsRefusedNotPublished(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	var mu sync.Mutex
	enable := true
	r.snaps.after = func(session string) {
		if session != sess.ID {
			return
		}
		mu.Lock()
		on := enable
		enable = !enable
		mu.Unlock()
		var err error
		if on {
			_, err = r.apply(sess.ID, []string{"vault"}, nil)
		} else {
			_, err = r.apply(sess.ID, nil, []string{"vault"})
		}
		if err != nil {
			t.Errorf("apply during launch: %v", err)
		}
	}
	err := r.svc.ResumeSession(identityUI(), sess.ID, true)
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("want a stale-launch refusal after the bounded attempts, got %v", err)
	}
	if v := r.svc.ServingTarget(sess.ID); v.Active {
		t.Fatalf("no endpoint may be left behind: %+v", v)
	}
}

func TestCloseAndApplyAreOrderedByTheCoordinator(t *testing.T) {
	t.Run("apply then close clears S with a tombstone", func(t *testing.T) {
		r := newLiveRig(t)
		r.workspaceOff("vault")
		sess, _ := r.create()
		if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := r.svc.CloseSession(identityUI(), sess.ID); err != nil {
			t.Fatal(err)
		}
		if o, st := r.override(sess.ID); o != nil || st.Revision != 2 || st.Present {
			t.Fatalf("S after close = %+v %+v", o, st)
		}
		if v := r.svc.ServingTarget(sess.ID); v.Active {
			t.Fatalf("a closed session has no live endpoint: %+v", v)
		}
		// The applied reference stays for the reopen guard.
		if _, _, ok := r.svc.HarnessApplied(sess.ID); !ok {
			t.Fatal("close must keep the applied reference")
		}
	})
	t.Run("close then a prepared apply writes nothing", func(t *testing.T) {
		r := newLiveRig(t)
		r.workspaceOff("vault")
		sess, _ := r.create()
		p, err := r.propose(sess.ID, []string{"vault"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.svc.CloseSession(identityUI(), sess.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.hsvc.Apply(r.agent(sess.ID), p.ID); err == nil {
			t.Fatal("an approval for a closed session must be refused")
		}
		if o, st := r.override(sess.ID); o != nil || st.Present {
			t.Fatalf("S = %+v %+v", o, st)
		}
	})
}

func TestAReplacementEndpointNeedsAFreshProposal(t *testing.T) {
	r := newLiveRig(t)
	r.workspaceOff("vault")
	sess, _ := r.create()
	p, err := r.propose(sess.ID, []string{"vault"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := r.svc.ServingTarget(sess.ID).Incarnation
	// The session is reloaded while the dialog is "open": same policy, new
	// incarnation. The approval does not carry over.
	if err := r.svc.ResumeSession(identityUI(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if now := r.svc.ServingTarget(sess.ID).Incarnation; now == old || now == "" {
		t.Fatalf("a reload must create a new incarnation (%q -> %q)", old, now)
	}
	_, err = r.hsvc.Apply(r.agent(sess.ID), p.ID)
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeProposalStale {
		t.Fatalf("want a stale proposal, got %v", err)
	}
	if o, _ := r.override(sess.ID); o != nil {
		t.Fatal("nothing may be written")
	}
}
