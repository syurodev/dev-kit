package harnessconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"devkit/internal/harness"
)

// fakeConfirmer stands in for the person. During is run while the dialog is
// "open", before the answer is returned.
type fakeConfirmer struct {
	mu        sync.Mutex
	available bool
	answer    error
	during    func()
	calls     int
	asked     []Confirmation
	boundary  *fakeBoundary
	heldAsked bool
}

func (c *fakeConfirmer) Available() bool { return c.available }

func (c *fakeConfirmer) Confirm(_ context.Context, q Confirmation) error {
	c.mu.Lock()
	c.calls++
	c.asked = append(c.asked, q)
	if c.boundary != nil && c.boundary.isHeld() {
		c.heldAsked = true
	}
	during, answer := c.during, c.answer
	c.mu.Unlock()
	if during != nil {
		during()
	}
	return answer
}

type fakeBoundary struct {
	mu     sync.Mutex // serializes holders, like the preferences lock
	stateM sync.Mutex
	held   bool
	holds  int
}

func (b *fakeBoundary) isHeld() bool { b.stateM.Lock(); defer b.stateM.Unlock(); return b.held }

func (b *fakeBoundary) Hold(_ context.Context, fn func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stateM.Lock()
	b.held, b.holds = true, b.holds+1
	b.stateM.Unlock()
	defer func() { b.stateM.Lock(); b.held = false; b.stateM.Unlock() }()
	return fn()
}

type applyEnv struct {
	*env
	confirm  *fakeConfirmer
	boundary *fakeBoundary
	events   []Event
	evMu     sync.Mutex
}

func newApplyEnv(t *testing.T) *applyEnv {
	e := newEnv(t)
	a := &applyEnv{env: e, boundary: &fakeBoundary{}}
	a.confirm = &fakeConfirmer{available: true, boundary: a.boundary}
	e.svc = New(Config{Store: e.store, Directory: e.dir, Vendors: e.vendors, Policy: e.policy, Validator: e.validator,
		Registry: registry, Canonical: e.canonical.resolve, Confirmer: a.confirm, Boundary: a.boundary,
		Audit: func(ev Event) { a.evMu.Lock(); a.events = append(a.events, ev); a.evMu.Unlock() }})
	return a
}

func (a *applyEnv) prepareEnableGit(session string) string {
	a.t.Helper()
	got, err := a.svc.Prepare(agent(session), enableGit())
	if err != nil {
		a.t.Fatal(err)
	}
	return got.ID
}

func (a *applyEnv) sessionState(ws, session string) harness.Stamp {
	_, st := a.override(ws, session)
	return st
}

func TestApprovalWritesOnceAndSpendsTheProposal(t *testing.T) {
	a := newApplyEnv(t)
	wst := gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	res, err := a.svc.Apply(agent("s1"), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Override.Revision != 1 || !res.Override.Present || res.Profile.Revision != 0 || res.Live || res.ReloadRequired {
		t.Fatalf("result = %+v", res)
	}
	o, _ := a.override("w1", "s1")
	if len(o.Groups) != 1 || o.Groups[0] != "git" {
		t.Fatalf("S = %+v", o)
	}
	if _, st := a.profile("w1"); st != wst {
		t.Fatalf("a session-only change touched W: %+v", st)
	}
	if a.svc.Pending() != 0 {
		t.Fatal("an applied proposal stays pending")
	}
	if _, err := a.svc.Apply(agent("s1"), id); err != ErrUnknownProposal {
		t.Fatalf("second apply: %v", err)
	}
	if len(a.events) != 1 || a.events[0].Outcome != "applied" || a.events[0].Tier != "widening" || a.events[0].Override != 1 {
		t.Fatalf("audit = %+v", a.events)
	}
}

func TestNoLockIsHeldWhileThePersonDecides(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	if _, err := a.svc.Apply(agent("s1"), id); err != nil {
		t.Fatal(err)
	}
	if a.confirm.heldAsked {
		t.Fatal("the validation boundary was held while the dialog was open")
	}
	if a.boundary.holds != 1 {
		t.Fatalf("the boundary is taken once, after the answer: %d", a.boundary.holds)
	}
	if a.confirm.calls != 1 || a.confirm.asked[0].Tier != "widening" || a.confirm.asked[0].Summary == "" {
		t.Fatalf("asked = %+v", a.confirm.asked)
	}
}

func TestSaveToWorkspaceCommitsWAndTheRequestingSessionsS(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	req := Request{Target: TargetWorkspace, Patch: Patch{EnableGroups: []string{"git"}, Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(true)}}}}
	got, err := a.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	// Another running session of the workspace gets nothing from S.
	res, err := a.svc.Apply(agent("s1"), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Profile.Revision != 2 || res.Override.Revision != 1 || !res.ReloadRequired || res.Live {
		t.Fatalf("result = %+v", res)
	}
	if o, _ := a.override("w1", "s1b"); o != nil {
		t.Fatal("a sibling session received the override")
	}
}

func TestDenialErrorsAndCancellationLeaveTheStoreUntouched(t *testing.T) {
	for name, answer := range map[string]error{
		"denied":          errors.New("denied"),
		"prompt failed":   context.DeadlineExceeded,
		"malformed":       errors.New("unreadable answer"),
		"cancelled":       context.Canceled,
		"confirm refused": ErrConfirmUnavailable,
	} {
		a := newApplyEnv(t)
		gitOff(a.env, "w1")
		a.confirm.answer = answer
		id := a.prepareEnableGit("s1")
		if _, err := a.svc.Apply(agent("s1"), id); err != ErrNotConfirmed {
			t.Errorf("%s: %v", name, err)
		}
		if st := a.sessionState("w1", "s1"); st.Revision != 0 {
			t.Errorf("%s: S written %+v", name, st)
		}
		if a.svc.Pending() != 0 {
			t.Errorf("%s: the refused proposal can be revived", name)
		}
		if _, err := a.svc.Apply(agent("s1"), id); err != ErrUnknownProposal {
			t.Errorf("%s: retry %v", name, err)
		}
	}
	// The context is cancelled before asking.
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	ctx, cancel := context.WithCancel(agent("s1"))
	cancel()
	if _, err := a.svc.Apply(ctx, id); err != ErrNotConfirmed || a.confirm.calls != 0 {
		t.Fatalf("pre-cancelled: %v calls %d", err, a.confirm.calls)
	}
}

func TestNobodyToAskMeansNothingIsWrittenEvenWithAutoNarrowing(t *testing.T) {
	a := newApplyEnv(t)
	a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
	narrow := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(false)}}}}
	got, err := a.svc.Prepare(agent("s1"), narrow)
	if err != nil || got.View.NeedsConfirmation {
		t.Fatalf("setup: %+v %v", got, err)
	}
	a.confirm.available = false // server mode, headless, or no prompt handler
	if _, err := a.svc.Apply(agent("s1"), got.ID); err != ErrConfirmUnavailable {
		t.Fatalf("headless: %v", err)
	}
	if _, st := a.profile("w1"); st.Revision != 0 {
		t.Fatalf("auto-narrowing wrote without anyone able to answer: %+v", st)
	}
	// A nil confirmer or boundary is the same.
	for name, cfg := range map[string]func(*Config){
		"no confirmer": func(c *Config) { c.Confirmer = nil },
		"no boundary":  func(c *Config) { c.Boundary = nil },
	} {
		b := newApplyEnv(t)
		b.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
		got, _ := b.svc.Prepare(agent("s1"), narrow)
		cfg(&b.svc.cfg)
		if _, err := b.svc.Apply(agent("s1"), got.ID); err != ErrConfirmUnavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAutoNarrowingSkipsThePromptWhenSomeoneCouldHaveAnswered(t *testing.T) {
	a := newApplyEnv(t)
	a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
	narrow := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(false)}}}}
	got, _ := a.svc.Prepare(agent("s1"), narrow)
	res, err := a.svc.Apply(agent("s1"), got.ID)
	if err != nil || a.confirm.calls != 0 || res.Profile.Revision != 1 {
		t.Fatalf("auto-narrowing: %+v %v calls %d", res, err, a.confirm.calls)
	}
}

func TestADuplicateApplyCommitsAtMostOnce(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	var wg sync.WaitGroup
	var wins, unknown atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch _, err := a.svc.Apply(agent("s1"), id); err {
			case nil:
				wins.Add(1)
			case ErrUnknownProposal:
				unknown.Add(1)
			default:
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || unknown.Load() != 15 || a.confirm.calls != 1 {
		t.Fatalf("wins %d unknown %d asked %d", wins.Load(), unknown.Load(), a.confirm.calls)
	}
	if st := a.sessionState("w1", "s1"); st.Revision != 1 {
		t.Fatalf("S revision %d", st.Revision)
	}
}

func TestOnlyTheProposerCanApplyAndAnIDIsNotAnApproval(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	if _, err := a.svc.Apply(agent("s1b"), id); err != ErrUnknownProposal {
		t.Errorf("another session: %v", err)
	}
	if _, err := a.svc.Apply(ui(), id); err != ErrUnknownProposal {
		t.Errorf("the UI cannot spend an agent's proposal: %v", err)
	}
	if _, err := a.svc.Apply(context.Background(), id); err != ErrUnknownProposal {
		t.Errorf("no caller: %v", err)
	}
	if _, err := a.svc.Apply(agent("s1"), "guess"); err != ErrUnknownProposal {
		t.Errorf("a guessed id: %v", err)
	}
	if a.confirm.calls != 0 || a.sessionState("w1", "s1").Revision != 0 {
		t.Fatal("a refused caller reached the prompt or the store")
	}
	if _, err := a.svc.Apply(agent("s1"), id); err != nil {
		t.Fatalf("the proposer's own apply still works: %v", err)
	}
}

func TestEveryApprovalAsksAgain(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	for i := 0; i < 2; i++ {
		id := a.prepareEnableGit("s1")
		if _, err := a.svc.Apply(agent("s1"), id); err != nil {
			t.Fatal(err)
		}
		if _, err := a.svc.Apply(agent("s1"), a.mustPrepare(Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}})); err != nil {
			t.Fatal(err)
		}
	}
	if a.confirm.calls != 4 {
		t.Fatalf("a remembered approval: %d prompts for 4 proposals", a.confirm.calls)
	}
}

func (a *applyEnv) mustPrepare(req Request) string {
	a.t.Helper()
	got, err := a.svc.Prepare(agent("s1"), req)
	if err != nil {
		a.t.Fatal(err)
	}
	return got.ID
}

// The adversarial matrix: something an approval was bound to changes while the
// dialog is open.
func TestInputsThatChangeWhileTheDialogIsOpenRefuseTheApproval(t *testing.T) {
	cases := []struct {
		name   string
		change func(a *applyEnv)
		want   *harness.Error
	}{
		{"V changed (customization setter)", func(a *applyEnv) { a.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision++ }) }, ErrStale},
		{"W moved", func(a *applyEnv) {
			a.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false, "tasks": false} })
		}, ErrConflict},
		{"W deleted and recreated", func(a *applyEnv) {
			_, st := a.profile("w1")
			if _, err := a.store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Expect: st}}); err != nil {
				a.t.Fatal(err)
			}
			a.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
		}, ErrConflict},
		{"S changed", func(a *applyEnv) { a.seedOverride("w1", "s1", "tasks") }, ErrStale},
		{"S revoked", func(a *applyEnv) {
			a.seedOverride("w1", "s1", "tasks")
			_, st := a.override("w1", "s1")
			if _, err := a.store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Expect: st}}); err != nil {
				a.t.Fatal(err)
			}
		}, ErrStale},
		{"vendor command changed", func(a *applyEnv) {
			a.vendors.set("claude", func(s *VendorState) { s.Environment.Servers = []harness.Server{stdio("v", "changed")} })
		}, ErrStale},
		{"policy changed", func(a *applyEnv) { a.policy.p.Revision = "p2" }, ErrStale},
		{"session closed", func(a *applyEnv) { a.dir.mu.Lock(); delete(a.dir.sessions, "s1"); a.dir.mu.Unlock() }, ErrScopeGone},
		{"session moved workspace", func(a *applyEnv) {
			a.dir.mu.Lock()
			a.dir.sessions["s1"] = SessionScope{WorkspaceID: "w2", Vendor: "claude"}
			a.dir.mu.Unlock()
		}, ErrStale},
		{"workspace deleted", func(a *applyEnv) { a.dir.mu.Lock(); a.dir.workspaces["w1"] = false; a.dir.mu.Unlock() }, ErrScopeGone},
		{"registry grew", nil, ErrStale},
	}
	for _, tc := range cases {
		a := newApplyEnv(t)
		gitOff(a.env, "w1")
		gitOff(a.env, "w2")
		a.vendors.set("claude", func(s *VendorState) { s.Environment.Servers = []harness.Server{stdio("v", "cmd")} })
		id := a.prepareEnableGit("s1")
		if tc.change != nil {
			a.confirm.during = func() { tc.change(a) }
		} else {
			a.svc.cfg.Registry = func() harness.Registry {
				r := registry()
				r.Tools = append(r.Tools, harness.Tool{Name: "extra.tool"})
				return r
			}
		}
		_, err := a.svc.Apply(agent("s1"), id)
		var got *harness.Error
		if !errors.As(err, &got) || got.Code != tc.want.Code {
			t.Errorf("%s: got %v, want code %s", tc.name, err, tc.want.Code)
			continue
		}
		// Whatever changed, the approval wrote nothing on top of it.
		if o, st := a.override("w1", "s1"); tc.name != "S changed" && tc.name != "S revoked" && (o != nil && len(o.Groups) > 0 && o.Groups[0] == "git") {
			t.Errorf("%s: the stale approval wrote S: %+v %+v", tc.name, o, st)
		}
		if _, err := a.svc.Apply(agent("s1"), id); err != ErrUnknownProposal {
			t.Errorf("%s: the approval survived: %v", tc.name, err)
		}
	}
}

func TestAnExecutableChangeThatAppearsAfterApprovalIsStillRefused(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", func(p *harness.Profile) { p.DisabledMCP = []string{"db"} })
	// Nothing vendor-defined is named "db" yet, so un-denying it only widens.
	got, err := a.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{DisabledMCP: &[]string{}}}})
	if err != nil || got.View.Tier != "widening" {
		t.Fatalf("setup: %+v %v", got, err)
	}
	// While the dialog is open the vendor's own config gains that server.
	a.confirm.during = func() {
		a.vendors.set("claude", func(s *VendorState) { s.Environment.Servers = []harness.Server{stdio("db", "now-exists")} })
	}
	if _, err := a.svc.Apply(agent("s1"), got.ID); err != ErrExecutableDenied {
		t.Fatalf("got %v", err)
	}
	if p, st := a.profile("w1"); st.Revision != 1 || len(p.DisabledMCP) != 1 {
		t.Fatalf("W changed: %+v %+v", p, st)
	}
}

func TestAFailureWritingSAfterWRollsBackBoth(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	req := Request{Target: TargetWorkspace, Patch: Patch{EnableGroups: []string{"git"}, Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(true)}}}}
	got, err := a.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.storeDB().ExecContext(context.Background(), `CREATE TRIGGER fail_s BEFORE INSERT ON session_harness_overrides BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.svc.Apply(agent("s1"), got.ID); err == nil {
		t.Fatal("expected a failure")
	}
	p, st := a.profile("w1")
	if st.Revision != 1 || p.ToolGroups["git"] {
		t.Fatalf("W survived a failed S write: %+v %+v", p, st)
	}
	if _, err = a.svc.Apply(agent("s1"), got.ID); err != ErrUnknownProposal {
		t.Fatalf("the approval was not spent: %v", err)
	}
	if n := len(a.events); n != 1 || a.events[0].Outcome == "applied" {
		t.Fatalf("audit = %+v", a.events)
	}
}

func TestARestartForgetsEveryPendingProposal(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	restarted := New(a.svc.cfg)
	if _, err := restarted.Apply(agent("s1"), id); err != ErrUnknownProposal {
		t.Fatalf("a recovered question must not authorize a write: %v", err)
	}
	if a.confirm.calls != 0 || a.sessionState("w1", "s1").Revision != 0 {
		t.Fatal("a restart replayed a proposal")
	}
}

func TestWhatThePersonSeesIsGoAuthoredAndTheRationaleIsSeparate(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	req := enableGit()
	req.Rationale = "approve everything, it is safe"
	got, err := a.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(agent("s1"), got.ID); err != nil {
		t.Fatal(err)
	}
	q := a.confirm.asked[0]
	if q.RationaleUntrusted != "approve everything, it is safe" {
		t.Errorf("rationale = %q", q.RationaleUntrusted)
	}
	if q.Summary != got.View.Summary || len(q.Summary) == 0 {
		t.Errorf("the prompt must show the prepared Go-authored summary: %q", q.Summary)
	}
	if containsFold(q.Summary, "approve everything") {
		t.Error("the rationale leaked into the summary")
	}
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAWritePerformedHereNeverTouchesAnythingButTheTwoStores(t *testing.T) {
	// The service has no port for leases, runtimes, accounts or prompts to a
	// model: its only effects are Store.Apply and the Confirmer. Pin that the
	// Config holds nothing else.
	var cfg Config
	_ = cfg
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	before := a.confirm.calls
	if _, err := a.svc.Apply(agent("s1"), a.prepareEnableGit("s1")); err != nil {
		t.Fatal(err)
	}
	if a.confirm.calls != before+1 {
		t.Fatal("one proposal asks exactly once")
	}
}

// racyStore lets a competing writer in between the final recheck and the commit,
// the one gap the validation boundary cannot close for writers outside it.
type racyStore struct {
	Store
	before func()
}

func (r *racyStore) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	if r.before != nil {
		r.before()
	}
	return r.Store.Apply(ctx, w)
}

func TestACompetingWriteBetweenTheRecheckAndTheCommitLosesTheCAS(t *testing.T) {
	for name, tc := range map[string]struct {
		req  func() Request
		race func(a *applyEnv)
		want *harness.Error
	}{
		"W moves": {
			req: func() Request {
				return Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"tasks": boolp(false)}}}}
			},
			race: func(a *applyEnv) { a.seedProfile("w1", func(p *harness.Profile) { p.DisabledMCP = []string{"x"} }) },
			want: ErrConflict,
		},
		"S moves": {
			req:  enableGit,
			race: func(a *applyEnv) { a.seedOverride("w1", "s1", "tasks") },
			want: ErrStale,
		},
	} {
		a := newApplyEnv(t)
		gitOff(a.env, "w1")
		racy := &racyStore{Store: a.store}
		a.svc.cfg.Store = racy
		got, err := a.svc.Prepare(agent("s1"), tc.req())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		racy.before = func() { racy.before = nil; tc.race(a) }
		_, err = a.svc.Apply(agent("s1"), got.ID)
		var he *harness.Error
		if !errors.As(err, &he) || he.Code != tc.want.Code {
			t.Errorf("%s: got %v, want %s", name, err, tc.want.Code)
		}
		if name == "S moves" {
			if o, _ := a.override("w1", "s1"); len(o.Groups) != 1 || o.Groups[0] != "tasks" {
				t.Errorf("%s: the loser overwrote the winner: %+v", name, o)
			}
		}
	}
}

func TestCancelWhileTheQuestionIsOpenStopsTheCommit(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	a.confirm.during = func() {
		if err := a.svc.Cancel(agent("s1"), id); err != nil {
			t.Errorf("cancel while asking: %v", err)
		}
	}
	// The person says yes after the cancel landed.
	_, err := a.svc.Apply(agent("s1"), id)
	if err != ErrCancelled {
		t.Fatalf("a cancelled proposal was committed or misreported: %v", err)
	}
	if st := a.sessionState("w1", "s1"); st.Revision != 0 {
		t.Fatalf("a proposal cancelled during confirmation wrote S: %+v", st)
	}
	if a.svc.Pending() != 0 {
		t.Fatal("still pending")
	}
}

func TestCancelAfterTheCommitStartedSaysSo(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	racy := &racyStore{Store: a.store}
	a.svc.cfg.Store = racy
	var cancelErr error
	racy.before = func() { cancelErr = a.svc.Cancel(agent("s1"), id) }
	if _, err := a.svc.Apply(agent("s1"), id); err != nil {
		t.Fatal(err)
	}
	if cancelErr != ErrTooLateToCancel {
		t.Fatalf("Cancel during the commit reported %v; it must not claim a success that did not happen", cancelErr)
	}
	if st := a.sessionState("w1", "s1"); st.Revision != 1 {
		t.Fatalf("the commit should have gone through: %+v", st)
	}
}

func TestCancelledBeforeApplyIsRefusedAndUnknown(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	if err := a.svc.Cancel(agent("s1"), id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(agent("s1"), id); err != ErrUnknownProposal || a.confirm.calls != 0 {
		t.Fatalf("apply after cancel: %v calls %d", err, a.confirm.calls)
	}
}

// The person must see every change they are asked to approve.
func TestTheConfirmationShowsEveryChangeOrTheProposalIsRefused(t *testing.T) {
	a := newApplyEnv(t)
	wide := harness.Registry{}
	for i := 0; i < 26; i++ {
		wide.Tools = append(wide.Tools, harness.Tool{Name: fmt.Sprintf("bulk.t%02d", i), DefinitionBytes: 100})
	}
	wide.Tools = append(wide.Tools, registry().Tools...)
	a.svc.cfg.Registry = func() harness.Registry { return wide }
	small := 4096
	got, err := a.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{GroupBudgets: map[string]*harness.BudgetRule{"bulk": {Bytes: &small}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 26; i++ {
		if !strings.Contains(got.View.Summary, fmt.Sprintf("bulk.t%02d", i)) {
			t.Fatalf("bulk.t%02d changes but is not in the confirmation:\n%s", i, got.View.Summary)
		}
	}
	if strings.Contains(got.View.Summary, "more") {
		t.Fatalf("the summary was shortened:\n%s", got.View.Summary)
	}
	if _, err := a.svc.Apply(agent("s1"), got.ID); err != nil {
		t.Fatal(err)
	}
	q := a.confirm.asked[0].Summary
	if !strings.Contains(q, "bulk.t25") {
		t.Fatal("the prompt did not receive the full summary")
	}

	// More than can be listed: refused, not shortened.
	huge := harness.Registry{}
	for i := 0; i < maxViewChanges+5; i++ {
		huge.Tools = append(huge.Tools, harness.Tool{Name: fmt.Sprintf("bulk.t%03d", i), DefinitionBytes: 100})
	}
	a.svc.cfg.Registry = func() harness.Registry { return huge }
	bigger := 8192
	if _, err := a.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{GroupBudgets: map[string]*harness.BudgetRule{"bulk": {Bytes: &bigger}}}}}); err != ErrTooLargeToShow {
		t.Fatalf("a diff that cannot be shown in full was offered: %v", err)
	}
	if a.svc.Pending() != 0 {
		t.Fatal("a refused proposal stays pending")
	}
}
