package harnessconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"devkit/internal/harness"
)

// fakeLive scripts the running-endpoint port. It models the coordinator as one
// mutex per session and records every effect it is asked to cause, in order.
type fakeLive struct {
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	targets map[string]*LiveTarget
	log     []string

	quarantineErr, publishErr error
	onQuarantine              func(session string)
	onPublish                 func(session string)
}

func newFakeLive() *fakeLive {
	return &fakeLive{locks: map[string]*sync.Mutex{}, targets: map[string]*LiveTarget{}}
}

func (f *fakeLive) set(session string, lt LiveTarget) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := lt
	f.targets[session] = &c
}

func (f *fakeLive) target(session string) LiveTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.targets[session]; t != nil {
		return *t
	}
	return LiveTarget{}
}

func (f *fakeLive) edit(session string, fn func(*LiveTarget)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.targets[session]
	if t == nil {
		t = &LiveTarget{}
		f.targets[session] = t
	}
	fn(t)
}

func (f *fakeLive) record(format string, args ...any) {
	f.mu.Lock()
	f.log = append(f.log, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

func (f *fakeLive) events() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fakeLive) Target(_ context.Context, session string) (LiveTarget, error) {
	return f.target(session), nil
}

func (f *fakeLive) Coordinate(_ context.Context, session string, fn func(LiveTxn) error) error {
	f.mu.Lock()
	l := f.locks[session]
	if l == nil {
		l = &sync.Mutex{}
		f.locks[session] = l
	}
	f.mu.Unlock()
	l.Lock()
	defer l.Unlock()
	return fn(&fakeTxn{f: f, session: session})
}

func (f *fakeLive) Reconcile(_ context.Context, session string, read func(context.Context) (*harness.Override, harness.Stamp, error)) (Publication, error) {
	f.record("reconcile:%s", session)
	return Publication{}, nil
}

type fakeTxn struct {
	f       *fakeLive
	session string
}

func (t *fakeTxn) Target() LiveTarget { return t.f.target(t.session) }

func (t *fakeTxn) Quarantine() error {
	if t.f.quarantineErr != nil {
		return t.f.quarantineErr
	}
	t.f.record("quarantine:%s", t.session)
	t.f.edit(t.session, func(l *LiveTarget) { l.Quarantined = true })
	if t.f.onQuarantine != nil {
		t.f.onQuarantine(t.session)
	}
	return nil
}

func (t *fakeTxn) Restore() error {
	t.f.record("restore:%s", t.session)
	t.f.edit(t.session, func(l *LiveTarget) { l.Quarantined = false })
	return nil
}

func (t *fakeTxn) MarkPending() {
	t.f.record("pending:%s", t.session)
	t.f.edit(t.session, func(l *LiveTarget) { l.Epoch++ })
}

func (t *fakeTxn) Publish(stamp harness.Stamp, durable *harness.Override) (Publication, error) {
	lt := t.f.target(t.session)
	if t.f.onPublish != nil {
		t.f.onPublish(t.session)
	}
	if !lt.Active {
		t.f.record("moved:%s:%d", t.session, stamp.Revision)
		t.f.edit(t.session, func(l *LiveTarget) { l.Epoch++ })
		return Publication{}, nil
	}
	if t.f.publishErr != nil {
		t.f.record("publish-failed:%s:%d", t.session, stamp.Revision)
		t.f.edit(t.session, func(l *LiveTarget) { l.Epoch++ })
		return Publication{State: PublicationPending}, t.f.publishErr
	}
	eff, err := lt.Basis.Resolve(overrideWithRevision(durable, stamp.Revision))
	if err != nil {
		return Publication{State: PublicationPending}, err
	}
	id := lt.Basis.Identity(stamp, eff)
	t.f.record("publish:%s:%d", t.session, stamp.Revision)
	t.f.edit(t.session, func(l *LiveTarget) { l.Serving, l.Quarantined, l.Epoch = id, false, l.Epoch+1 })
	return Publication{State: PublicationInstalled, Serving: id, ToolsChanged: true}, nil
}

// faultStore decorates the real SQLite store with the failures a test scripts.
type faultStore struct {
	Store
	mode     string // "", "conflict", "unknownAfter", "unknownBefore", "fail"
	afterApp func() // runs once the real write is durable
	failGet  error  // GetOverride error after the first Apply
	applied  bool
}

func (s *faultStore) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	switch s.mode {
	case "conflict":
		return harness.Applied{}, &harness.ConflictError{Target: "override"}
	case "fail":
		return harness.Applied{}, errors.New("disk i/o")
	case "unknownBefore":
		return harness.Applied{}, fmt.Errorf("%w: lost", harness.ErrStoreCommitUnknown)
	}
	a, err := s.Store.Apply(ctx, w)
	if err != nil {
		return a, err
	}
	s.applied = true
	if s.afterApp != nil {
		s.afterApp()
	}
	if s.mode == "unknownAfter" {
		return harness.Applied{}, fmt.Errorf("%w: lost ack", harness.ErrStoreCommitUnknown)
	}
	return a, nil
}

func (s *faultStore) GetOverride(ctx context.Context, session, ws string) (*harness.Override, harness.Stamp, error) {
	if s.applied && s.failGet != nil {
		return nil, harness.Stamp{}, s.failGet
	}
	return s.Store.GetOverride(ctx, session, ws)
}

type liveEnv struct {
	*applyEnv
	live  *fakeLive
	store *faultStore
}

func newLiveEnv(t *testing.T) *liveEnv {
	a := newApplyEnv(t)
	l := &liveEnv{applyEnv: a, live: newFakeLive(), store: &faultStore{Store: a.store}}
	a.svc = New(Config{Store: l.store, Directory: a.dir, Vendors: a.vendors, Policy: a.policy, Validator: a.validator,
		Registry: registry, Canonical: a.canonical.resolve, Confirmer: a.confirm, Boundary: a.boundary, Live: l.live,
		Audit: func(ev Event) { a.evMu.Lock(); a.events = append(a.events, ev); a.evMu.Unlock() }})
	return l
}

// activate gives the session a running endpoint pinned to what its launch
// resolves to right now, as the real adapter does at open.
func (l *liveEnv) activate(session, workspace string) {
	l.t.Helper()
	snap, err := l.provider().Snapshot(context.Background(), snapReq(session, workspace, "claude"))
	if err != nil {
		l.t.Fatal(err)
	}
	l.live.set(session, LiveTarget{Active: true, Incarnation: "inc-" + session, Epoch: 1, Basis: snap.Basis,
		Serving: snap.Basis.Identity(snap.Basis.LaunchSession(), snap.Effective)})
}

func (l *liveEnv) storedOnly() *Service {
	return New(Config{Store: l.env.store, Directory: l.dir, Vendors: l.vendors, Policy: l.policy, Validator: l.validator,
		Registry: registry, Canonical: l.canonical.resolve, Confirmer: l.confirm, Boundary: l.boundary})
}

func (l *liveEnv) prepare(session string, req Request) Prepared {
	l.t.Helper()
	got, err := l.svc.Prepare(agent(session), req)
	if err != nil {
		l.t.Fatalf("Prepare: %v", err)
	}
	return got
}

func offW(groups ...string) func(*harness.Profile) {
	return func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{}
		for _, g := range groups {
			p.ToolGroups[g] = false
		}
	}
}

func TestLiveOnlyWideningSurvivesAStoredNoChange(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git")) // the session launches with git off
	l.activate("s1", "w1")
	l.seedProfile("w1", func(*harness.Profile) {}) // saved W now allows git

	if _, err := l.storedOnly().Prepare(agent("s1"), enableGit()); !errors.Is(err, ErrNoChange) && err != ErrNoChange {
		t.Fatalf("without a running endpoint the saved W makes this a no-op, got %v", err)
	}
	got := l.prepare("s1", enableGit())
	if got.View.Tier != "widening" || !got.View.NeedsConfirmation {
		t.Fatalf("a live-only widening must ask: %+v", got.View)
	}
	var stored, running int
	for _, c := range got.View.Changes {
		switch c.Scope {
		case ScopeStored:
			stored++
		case ScopeRunning:
			running++
		}
	}
	if stored != 0 || running != 1 {
		t.Fatalf("changes = %+v", got.View.Changes)
	}
	for _, want := range []string{"Saved configuration: no difference in effect.", "workspace profile revision 1", "tool group git: unavailable → available (2 tools"} {
		if !strings.Contains(got.View.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, got.View.Summary)
		}
	}
	if !strings.Contains(got.View.Summary, "does not pre-approve") {
		t.Errorf("enabling a group must say it adds no pre-approval:\n%s", got.View.Summary)
	}
}

func TestARevokeThatChangesNothingLiveIsNotPromisedAsALiveDisable(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", func(*harness.Profile) {}) // pinned W: git on
	l.seedOverride("w1", "s1", "git")
	l.activate("s1", "w1")
	l.seedProfile("w1", offW("git")) // saved W later turns git off
	got := l.prepare("s1", Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}})
	if got.View.Tier != "narrowing" {
		t.Fatalf("tier = %s", got.View.Tier)
	}
	if !strings.Contains(got.View.Summary, "no change to what it can use") || strings.Contains(got.View.Summary, "available → unavailable") {
		t.Fatalf("the running session keeps git (its own W allows it):\n%s", got.View.Summary)
	}
	if !strings.Contains(got.View.Summary, "deny") && !strings.Contains(got.View.Summary, "turn off tool group git") {
		t.Fatalf("the stored effect must still be shown:\n%s", got.View.Summary)
	}
}

func TestAutoNarrowingCannotSkipALiveWidening(t *testing.T) {
	l := newLiveEnv(t)
	l.policy.p.AutoNarrowing = true
	l.seedProfile("w1", offW("git", "tasks"))
	l.seedOverride("w1", "s1", "git")
	l.activate("s1", "w1") // live: git on, tasks off
	l.seedProfile("w1", offW("git"))
	// Stored: git on (S) -> off is a narrowing; tasks is already on. Live: git
	// off but tasks on, a widening.
	req := Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}, EnableGroups: []string{"tasks"}}}
	got := l.prepare("s1", req)
	if got.View.Tier != "widening" || !got.View.NeedsConfirmation {
		t.Fatalf("the strictest of stored and live decides, auto-narrowing included: %+v", got.View)
	}
	// Control: the same stored narrowing with no running endpoint is auto-applied.
	plain := l.storedOnly()
	plain.cfg.Policy = l.policy
	p2, err := plain.Prepare(agent("s1"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}})
	if err != nil || p2.View.NeedsConfirmation {
		t.Fatalf("control: %v %+v", err, p2.View)
	}
}

func TestOppositeStoredAndLiveEffectsAreShownSeparately(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git", "tasks"))
	l.seedOverride("w1", "s1", "git")
	l.activate("s1", "w1")
	l.seedProfile("w1", offW("git"))
	got := l.prepare("s1", Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}, EnableGroups: []string{"tasks"}}})
	s := got.View.Summary
	stored := strings.Index(s, "Saved configuration")
	running := strings.Index(s, "This running session")
	if stored < 0 || running < stored {
		t.Fatalf("both sections, stored first:\n%s", s)
	}
	if !strings.Contains(s[stored:running], "turn off tool group git") || !strings.Contains(s[running:], "tool group tasks: unavailable → available") ||
		!strings.Contains(s[running:], "tool group git: available → unavailable") {
		t.Fatalf("effects were merged or lost:\n%s", s)
	}
}

func TestACommittedChangeIsQuarantinedThenPublishedThenLive(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	l.activate("s1b", "w1")
	id := l.prepare("s1", enableGit()).ID
	res, err := l.svc.Apply(agent("s1"), id)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Live || res.Commit != CommitCommitted || res.Publication.State != PublicationInstalled || res.Override.Revision != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := l.live.events(); strings.Join(got, ",") != "quarantine:s1,publish:s1:1" {
		t.Fatalf("events = %v", got)
	}
	if lt := l.live.target("s1b"); lt.Epoch != 1 || lt.Quarantined || lt.Serving.Session.Revision != 0 {
		t.Fatalf("another running session must be untouched: %+v", lt)
	}
	l.evMu.Lock()
	defer l.evMu.Unlock()
	if ev := l.events[len(l.events)-1]; ev.Outcome != "applied" || ev.Publication != "installed" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestApplyWithoutAnEndpointStoresOnly(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	res, err := l.svc.Apply(agent("s1"), l.prepare("s1", enableGit()).ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Live || res.Publication.State != PublicationNone || res.Commit != CommitCommitted {
		t.Fatalf("an inactive session is stored-only: %+v", res)
	}
	if got := strings.Join(l.live.events(), ","); got != "moved:s1:1" {
		t.Fatalf("the coordinator still learns S moved: %v", got)
	}
}

func TestStaleLiveBindingIsRefusedBeforeAnythingChanges(t *testing.T) {
	type drift struct {
		do   func(l *liveEnv)
		code string
	}
	stale, unavailable := harness.CodeProposalStale, harness.CodeNotAvailable
	cases := map[string]drift{
		"another S commit moved the epoch": {func(l *liveEnv) { l.live.edit("s1", func(t *LiveTarget) { t.Epoch++ }) }, stale},
		"a replacement endpoint, same policy": {func(l *liveEnv) {
			l.live.edit("s1", func(t *LiveTarget) { t.Incarnation = "inc-replacement" })
		}, stale},
		"the endpoint went away": {func(l *liveEnv) { l.live.set("s1", LiveTarget{}) }, stale},
		// A paused endpoint, or one not serving the stored S, cannot be judged
		// against at all: unavailable, never a synthetic default.
		"the endpoint is paused": {func(l *liveEnv) { l.live.edit("s1", func(t *LiveTarget) { t.Quarantined = true }) }, unavailable},
		"it serves another S":    {func(l *liveEnv) { l.live.edit("s1", func(t *LiveTarget) { t.Serving.Session.Revision = 9 }) }, unavailable},
		// V changes the vendor facts the stored evaluation is bound to.
		"vendor edit (V boundary)": {func(l *liveEnv) { l.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision = 9 }) }, stale},
	}
	for name, d := range cases {
		drift := d.do
		t.Run(name, func(t *testing.T) {
			l := newLiveEnv(t)
			l.seedProfile("w1", offW("git"))
			l.activate("s1", "w1")
			id := l.prepare("s1", enableGit()).ID
			drift(l)
			_, err := l.svc.Apply(agent("s1"), id)
			if err == nil || code(t, err) != d.code {
				t.Fatalf("want %s, got %v", d.code, err)
			}
			if o, st := l.override("w1", "s1"); o != nil || st.Revision != 0 {
				t.Fatalf("nothing may be written: %+v %+v", o, st)
			}
			if ev := l.live.events(); len(ev) != 0 {
				t.Fatalf("nothing may be quarantined or published: %v", ev)
			}
		})
	}
}

func TestAnInactiveSessionThatBecomesActiveBeforeCommitIsStale(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	id := l.prepare("s1", enableGit()).ID
	l.activate("s1", "w1")
	if _, err := l.svc.Apply(agent("s1"), id); err == nil || code(t, err) != harness.CodeProposalStale {
		t.Fatalf("want stale, got %v", err)
	}
	if ev := l.live.events(); len(ev) != 0 {
		t.Fatalf("events = %v", ev)
	}
}

func TestWorkspaceMovementDuringTheDialogIsAProfileConflict(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.confirm.during = func() { l.seedProfile("w1", offW("git", "tasks")) }
	_, err := l.svc.Apply(agent("s1"), id)
	if err == nil || code(t, err) != harness.CodeProfileConflict {
		t.Fatalf("want profileConflict, got %v", err)
	}
	if ev := l.live.events(); len(ev) != 0 {
		t.Fatalf("events = %v", ev)
	}
}

func TestRejectedApprovalWritesAndPublishesNothing(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	l.confirm.answer = errors.New("no")
	id := l.prepare("s1", enableGit()).ID
	if _, err := l.svc.Apply(agent("s1"), id); err == nil || code(t, err) != harness.CodeProfileDenied {
		t.Fatalf("want denied, got %v", err)
	}
	if ev := l.live.events(); len(ev) != 0 {
		t.Fatalf("events = %v", ev)
	}
	if o, _ := l.override("w1", "s1"); o != nil {
		t.Fatalf("S = %+v", o)
	}
}

func TestProvenRollbackReopensTheSameAdmission(t *testing.T) {
	for mode, wantErr := range map[string]string{"conflict": harness.CodeProposalStale, "fail": harness.CodeProposalStale} {
		t.Run(mode, func(t *testing.T) {
			l := newLiveEnv(t)
			l.seedProfile("w1", offW("git"))
			l.activate("s1", "w1")
			id := l.prepare("s1", enableGit()).ID
			l.store.mode = mode
			before := l.live.target("s1")
			_, err := l.svc.Apply(agent("s1"), id)
			if err == nil || code(t, err) != wantErr {
				t.Fatalf("want %s, got %v", wantErr, err)
			}
			if got := strings.Join(l.live.events(), ","); got != "quarantine:s1,restore:s1" {
				t.Fatalf("events = %s", got)
			}
			after := l.live.target("s1")
			if after.Quarantined || after.Serving != before.Serving {
				t.Fatalf("the prior ready state must be back: %+v", after)
			}
		})
	}
}

func TestACancelAfterQuarantineButBeforeTheCommitWinsAndRestores(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.live.onQuarantine = func(string) {
		if err := l.svc.Cancel(agent("s1"), id); err != nil {
			t.Errorf("Cancel: %v", err)
		}
	}
	_, err := l.svc.Apply(agent("s1"), id)
	if err == nil || err != ErrCancelled {
		t.Fatalf("want cancelled, got %v", err)
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1,restore:s1" {
		t.Fatalf("events = %s", got)
	}
	if o, _ := l.override("w1", "s1"); o != nil {
		t.Fatal("a cancelled proposal must not write")
	}
}

func TestACommitAckedAmbiguouslyIsReadBackAndPublished(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.store.mode = "unknownAfter" // durable, but the store says it does not know
	res, err := l.svc.Apply(agent("s1"), id)
	if err != nil || !res.Live || res.Commit != CommitCommitted || res.Override.Revision != 1 {
		t.Fatalf("read-back proves the commit: %+v %v", res, err)
	}
}

func TestACommitThatReadBackProvesAbsentIsARollback(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.store.mode = "unknownBefore"
	_, err := l.svc.Apply(agent("s1"), id)
	if err == nil || code(t, err) != harness.CodeProposalStale {
		t.Fatalf("want stale, got %v", err)
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1,restore:s1" {
		t.Fatalf("events = %s", got)
	}
}

func TestAnAmbiguousCommitThatCannotBeReadBackStaysPaused(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.store.mode = "unknownAfter"
	l.store.failGet = errors.New("db gone")
	res, err := l.svc.Apply(agent("s1"), id)
	var pe *PublicationError
	if !errors.As(err, &pe) || code(t, err) != harness.CodeSnapshotUnavailable {
		t.Fatalf("want a publication error, got %v", err)
	}
	if res.Live || res.Commit != CommitUnknown || pe.Result.Commit != CommitUnknown || res.Publication.State != PublicationPending {
		t.Fatalf("result = %+v", res)
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1,pending:s1" {
		t.Fatalf("an unknown outcome must not restore admission: %s", got)
	}
	if !l.live.target("s1").Quarantined {
		t.Fatal("admission must stay closed")
	}
	// The spent proposal is not retried.
	if _, err := l.svc.Apply(agent("s1"), id); err == nil || code(t, err) != harness.CodeProposalStale {
		t.Fatalf("a spent proposal is refused, got %v", err)
	}
}

func TestCommittedButPublicationFailedReturnsTheCommittedStamps(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	l.live.publishErr = errors.New("registry update failed")
	res, err := l.svc.Apply(agent("s1"), id)
	var pe *PublicationError
	if !errors.As(err, &pe) {
		t.Fatalf("want a publication error, got %v", err)
	}
	if res.Live || res.Override.Revision != 1 || !res.Override.Present || res.Commit != CommitCommitted || res.Publication.State != PublicationPending {
		t.Fatalf("a committed change must report its stamps: %+v", res)
	}
	if pe.Result.Override != res.Override {
		t.Fatalf("the error retains the result: %+v", pe.Result)
	}
	if strings.Contains(err.Error(), "registry update failed") {
		t.Fatalf("the cause must not reach the caller: %v", err)
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1,publish-failed:s1:1" {
		t.Fatalf("a committed write must never be rolled back to admission: %s", got)
	}
	l.evMu.Lock()
	defer l.evMu.Unlock()
	if ev := l.events[len(l.events)-1]; ev.Outcome != "applied" || ev.Publication != "pending" {
		t.Fatalf("the audit must not say nothing was applied: %+v", ev)
	}
}

func TestACancelledRequesterStillFinishesTheCommittedPublication(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	id := l.prepare("s1", enableGit()).ID
	ctx, cancel := context.WithCancel(agent("s1"))
	l.store.afterApp = cancel // the requester goes away the moment the write lands
	res, err := l.svc.Apply(ctx, id)
	if err != nil || !res.Live {
		t.Fatalf("post-commit cancellation must not turn a write into a denial: %+v %v", res, err)
	}
	if lt := l.live.target("s1"); lt.Quarantined || lt.Serving.Session.Revision != 1 {
		t.Fatalf("the endpoint must serve the committed S: %+v", lt)
	}
}

func TestWorkspaceOnlyChangeNeverTouchesARunningEndpoint(t *testing.T) {
	l := newLiveEnv(t)
	l.activate("s1", "w1")
	req := Request{WorkspaceID: "w1", Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(false)}}}}
	got, err := l.svc.Prepare(ui(), req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.svc.Apply(ui(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Live || !res.ReloadRequired || res.Profile.Revision != 1 || res.Publication.State != PublicationNone {
		t.Fatalf("result = %+v", res)
	}
	if ev := l.live.events(); len(ev) != 0 {
		t.Fatalf("a workspace write leaves running sessions on their pinned W: %v", ev)
	}
}
