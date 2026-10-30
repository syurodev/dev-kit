package agenthub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"devkit/internal/identity"
)

type fakeGroupHost struct {
	mu     sync.Mutex
	known  map[string]bool
	served map[string][]string
	failed map[string]bool
}

func (h *fakeGroupHost) KnownGroup(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.known[name]
}

func (h *fakeGroupHost) ServedGroups(_ context.Context, id string) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failed[id] {
		return nil, errors.New("no live endpoint")
	}
	return append([]string(nil), h.served[id]...), nil
}

func (h *fakeGroupHost) serve(id string, groups ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.served[id] = groups
}

type groupFixture struct {
	*choiceFixture
	host     *fakeGroupHost
	child    AgentSession
	childCtx context.Context
}

func newGroupFixture(t *testing.T) *groupFixture {
	t.Helper()
	f := &groupFixture{choiceFixture: newChoiceFixture(t)}
	f.host = &fakeGroupHost{
		known:  map[string]bool{"git": true, "web": true, "db": true},
		served: map[string][]string{},
		failed: map[string]bool{},
	}
	f.svc.GroupHost = f.host
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	f.child = child
	f.childCtx = identity.WithCaller(context.Background(), identity.Agent(child.ID))
	f.host.serve(f.parent.ID, "git", "web")
	return f
}

// applier records the call and reports a committed write.
type applier struct {
	calls     int
	committed bool
	err       error
	grantable []string
}

func (a *applier) fn(_ context.Context, _ GroupRequest, grantable []string) (GroupApplyResult, error) {
	a.calls++
	a.grantable = grantable
	return GroupApplyResult{Payload: "outcome", Committed: a.committed}, a.err
}

func (f *groupFixture) ask(t *testing.T, route GroupRequestRoute, groups ...string) GroupRequest {
	t.Helper()
	r, err := f.svc.CreateGroupRequest(f.childCtx, groups, route, "need it")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return r
}

func TestAChildAsksAParentAndAnswersAreBoundToTheRow(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "web", "git")
	if r.State != GroupRequestPendingParent || r.Revision != 1 || r.ParentID != f.parent.ID || r.ChildID != f.child.ID || r.Groups[0] != "git" {
		t.Fatalf("request = %+v", r)
	}
	// Asking again for the same groups is the same request, not a second one.
	if again := f.ask(t, GroupRouteParent, "git", "web"); again.ID != r.ID {
		t.Fatalf("second ask made %s, want %s", again.ID, r.ID)
	}
	a := &applier{committed: true}
	next, payload, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn)
	if err != nil || next.State != GroupRequestApproved || next.Resolver != "parent:"+f.parent.ID || next.Outcome != GroupOutcomeApplied || payload != "outcome" || a.calls != 1 {
		t.Fatalf("approve = %+v %v %v calls=%d", next, payload, err, a.calls)
	}
	if got := a.grantable; len(got) != 2 || got[0] != "git" || got[1] != "web" {
		t.Fatalf("grantable = %v", got)
	}
	// A second approval of the same request finds it finished and applies nothing.
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupRequestResolved) || a.calls != 1 {
		t.Fatalf("second approve: %v calls=%d", err, a.calls)
	}
}

func TestOnlyTheDirectParentCanAnswerAndOnlyAtTheRevisionItSaw(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	a := &applier{committed: true}

	stranger, err := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for name, ctx := range map[string]context.Context{
		"the child itself": f.childCtx,
		"a stranger":       identity.WithCaller(context.Background(), identity.Agent(stranger.ID)),
		"the window":       identity.WithCaller(context.Background(), identity.UI()),
		"a terminal":       identity.WithCaller(context.Background(), identity.UserTerminal()),
	} {
		if _, _, err := f.svc.ResolveGroupRequestAsParent(ctx, r.ID, r.Revision, GroupApprove, a.fn); err == nil || a.calls != 0 {
			t.Errorf("%s resolved a request: %v (applier calls %d)", name, err, a.calls)
		}
	}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision+1, GroupApprove, a.fn); !errors.Is(err, ErrGroupRequestStale) {
		t.Errorf("wrong revision: %v", err)
	}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, "no-such-request", 1, GroupApprove, a.fn); !errors.Is(err, ErrGroupRequestNotFound) {
		t.Errorf("unknown request: %v", err)
	}
	if got, _ := f.svc.GetGroupRequest(f.ctx, r.ID); got.State != GroupRequestPendingParent {
		t.Errorf("a refused answer moved the request to %s", got.State)
	}
}

func TestAParentCannotGrantWhatItDoesNotServe(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "db")
	a := &applier{committed: true}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupNotGrantable) || a.calls != 0 {
		t.Fatalf("approve out of scope: %v calls=%d", err, a.calls)
	}
	// It can still deny or forward; forward changes the same row, once.
	fwd, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupForward, a.fn)
	if err != nil || fwd.ID != r.ID || fwd.State != GroupRequestPendingUser || fwd.Route != GroupRouteUser || fwd.Revision != r.Revision+1 {
		t.Fatalf("forward = %+v %v", fwd, err)
	}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, fwd.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupRequestResolved) {
		t.Fatalf("the parent answered a request that waits for the person: %v", err)
	}
	// The parent stops serving a group after the request was made.
	f.host.serve(f.parent.ID, "web")
	r2 := f.ask(t, GroupRouteParent, "web", "git")
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r2.ID, r2.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupNotGrantable) {
		t.Fatalf("approve after the parent lost a group: %v", err)
	}
}

func TestAClosedParentAndAClosedChildLeaveNothingToAnswer(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	if err := f.svc.CloseSession(identity.WithCaller(context.Background(), identity.UI()), f.child.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.GetGroupRequest(identity.WithCaller(context.Background(), identity.UI()), r.ID)
	if err != nil || got.State != GroupRequestInvalidated || got.Outcome != GroupOutcomeSessionClosed {
		t.Fatalf("after the child closed: %+v %v", got, err)
	}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, got.Revision, GroupApprove, (&applier{committed: true}).fn); !errors.Is(err, ErrGroupRequestResolved) {
		t.Fatalf("answering a closed child's request: %v", err)
	}

	g := newGroupFixture(t)
	r2 := g.ask(t, GroupRouteParent, "git")
	ui := identity.WithCaller(context.Background(), identity.UI())
	if err := g.svc.CloseSession(ui, g.parent.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := g.svc.GetGroupRequest(ui, r2.ID); got.State != GroupRequestInvalidated {
		t.Fatalf("after the parent closed: %+v", got)
	}
	// The child can no longer use the parent route, and is not forwarded silently.
	if _, err := g.svc.CreateGroupRequest(g.childCtx, []string{"web"}, GroupRouteParent, ""); !errors.Is(err, ErrGroupRouteUnavailable) {
		t.Fatalf("parent route with a closed parent: %v", err)
	}
}

func TestAPersonsAnswerAndALock(t *testing.T) {
	f := newGroupFixture(t)
	a := &applier{committed: true}

	r := f.ask(t, GroupRouteUser, "db")
	if r.State != GroupRequestPendingUser {
		t.Fatalf("user route state = %s", r.State)
	}
	if _, _, err := f.svc.AnswerGroupRequestAsUser(context.Background(), r.ID, r.Revision+1, UserAnswer{Approve: true}, a.fn); !errors.Is(err, ErrGroupRequestStale) {
		t.Fatalf("stale answer: %v", err)
	}
	// Decline closes this request only: the child may ask again.
	got, _, err := f.svc.AnswerGroupRequestAsUser(context.Background(), r.ID, r.Revision, UserAnswer{}, a.fn)
	if err != nil || got.State != GroupRequestDenied || got.Outcome != GroupOutcomeDeniedUser {
		t.Fatalf("decline = %+v %v", got, err)
	}
	if locks, _ := f.svc.store.(GroupRequestStore).GroupLocks(context.Background(), f.child.ID); len(locks) != 0 {
		t.Fatalf("a plain decline locked %v", locks)
	}
	r2 := f.ask(t, GroupRouteUser, "db")
	if r2.ID == r.ID {
		t.Fatal("a declined request came back")
	}
	// Decline with Block: the lock is durable, and no agent path passes it.
	if _, _, err := f.svc.AnswerGroupRequestAsUser(context.Background(), r2.ID, r2.Revision, UserAnswer{Block: true}, a.fn); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"db"}, GroupRouteUser, ""); !errors.Is(err, ErrGroupBlocked) {
		t.Fatalf("ask for a locked group: %v", err)
	}
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"db"}, GroupRouteParent, ""); !errors.Is(err, ErrGroupBlocked) {
		t.Fatalf("ask the parent for a locked group: %v", err)
	}
	f.host.serve(f.parent.ID, "git", "web", "db")
	if g, err := f.svc.GrantableGroups(f.ctx, f.child); err != nil || containsAll(g, []string{"db"}) {
		t.Fatalf("grantable = %v %v, want db left out", g, err)
	}
	// Only the window writes or lifts a lock.
	ui := identity.WithCaller(context.Background(), identity.UI())
	for name, ctx := range map[string]context.Context{"parent": f.ctx, "child": f.childCtx} {
		if err := f.svc.SetGroupLock(ctx, f.child.ID, "db", false); !errors.Is(err, ErrPermissionDenied) {
			t.Errorf("%s lifted a lock: %v", name, err)
		}
	}
	if err := f.svc.SetGroupLock(ui, f.child.ID, "db", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"db"}, GroupRouteUser, ""); err != nil {
		t.Fatalf("ask after the lock was lifted: %v", err)
	}
}

func TestALockedGroupIsNotApprovableByTheParentEvenIfAskedBefore(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	ui := identity.WithCaller(context.Background(), identity.UI())
	if err := f.svc.SetGroupLock(ui, f.child.ID, "git", true); err != nil {
		t.Fatal(err)
	}
	a := &applier{committed: true}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupBlocked) || a.calls != 0 {
		t.Fatalf("approve a group locked meanwhile: %v calls=%d", err, a.calls)
	}
}

func TestAFailedWriteLeavesTheRequestOpenAndACommittedOneClosesIt(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	boom := errors.New("write failed")
	a := &applier{committed: false, err: boom}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn); !errors.Is(err, boom) {
		t.Fatalf("failed write: %v", err)
	}
	if got, _ := f.svc.GetGroupRequest(f.ctx, r.ID); got.State != GroupRequestPendingParent || got.Revision != r.Revision {
		t.Fatalf("a failed write moved the request: %+v", got)
	}
	// Written but not published: the request is approved, and the caller still
	// receives the publication failure to report.
	pub := errors.New("publication failed")
	a2 := &applier{committed: true, err: pub}
	got, payload, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a2.fn)
	if !errors.Is(err, pub) || got.State != GroupRequestApproved || payload != "outcome" {
		t.Fatalf("committed with failed publication = %+v %v %v", got, payload, err)
	}
}

func TestNothingIsAskedWhenItIsAlreadyServed(t *testing.T) {
	f := newGroupFixture(t)
	f.host.serve(f.child.ID, "git")
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"git"}, GroupRouteParent, ""); !errors.Is(err, ErrGroupNoChange) {
		t.Fatalf("ask for a served group: %v", err)
	}
	f.host.serve(f.child.ID)
	r := f.ask(t, GroupRouteParent, "git")
	f.host.serve(f.child.ID, "git") // the person enabled it meanwhile
	a := &applier{committed: true}
	got, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn)
	if err != nil || got.State != GroupRequestSuperseded || a.calls != 0 {
		t.Fatalf("approve a served group = %+v %v calls=%d", got, err, a.calls)
	}
}

func TestRequestsAreValidatedAndBounded(t *testing.T) {
	f := newGroupFixture(t)
	for name, groups := range map[string][]string{"none": nil, "blank": {" "}, "unknown": {"nope"}, "spaces": {"a b"}} {
		if _, err := f.svc.CreateGroupRequest(f.childCtx, groups, GroupRouteUser, ""); !errors.Is(err, ErrGroupRequestInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"git"}, "elsewhere", ""); !errors.Is(err, ErrGroupRequestInvalid) {
		t.Errorf("bad route: %v", err)
	}
	// Not an agent: nobody else asks on a session's behalf.
	if _, err := f.svc.CreateGroupRequest(context.Background(), []string{"git"}, GroupRouteUser, ""); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("anonymous: %v", err)
	}
	if _, err := f.svc.CreateGroupRequest(identity.WithCaller(context.Background(), identity.UI()), []string{"git"}, GroupRouteUser, ""); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("window: %v", err)
	}
	// A root session has no parent to ask.
	root := identity.WithCaller(context.Background(), identity.Agent(f.parent.ID))
	if _, err := f.svc.CreateGroupRequest(root, []string{"db"}, GroupRouteParent, ""); !errors.Is(err, ErrGroupRouteUnavailable) {
		t.Errorf("parent route of a root session: %v", err)
	}
	f.host.known["g1"], f.host.known["g2"], f.host.known["g3"], f.host.known["g4"], f.host.known["g5"] = true, true, true, true, true
	for _, g := range []string{"g1", "g2", "g3", "g4"} {
		f.ask(t, GroupRouteUser, g)
	}
	if _, err := f.svc.CreateGroupRequest(f.childCtx, []string{"g5"}, GroupRouteUser, ""); !errors.Is(err, ErrGroupRequestLimit) {
		t.Errorf("fifth open request: %v", err)
	}
	long := make([]byte, 2000)
	for i := range long {
		long[i] = 'x'
	}
	f.host.known["g6"] = true
	// The reason is trimmed to its bound.
	g := newGroupFixture(t)
	r, err := g.svc.CreateGroupRequest(g.childCtx, []string{"git"}, GroupRouteUser, string(long))
	if err != nil || len([]rune(r.Reason)) != maxGroupRequestReason {
		t.Fatalf("reason length = %d, %v", len([]rune(r.Reason)), err)
	}
}

func TestAParentRequestExpiresWithoutATimer(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	st := f.svc.store.(GroupRequestStore)
	old := r
	old.CreatedAt = time.Now().UTC().Add(-groupRequestTTL - time.Minute)
	old.Revision, old.UpdatedAt = r.Revision+1, time.Now().UTC()
	if _, err := f.svc.store.(*SQLStore).db.ExecContext(context.Background(), `UPDATE agent_group_requests SET created_at=? WHERE id=?`, formatStoreTime(old.CreatedAt), r.ID); err != nil {
		t.Fatal(err)
	}
	a := &applier{committed: true}
	if _, _, err := f.svc.ResolveGroupRequestAsParent(f.ctx, r.ID, r.Revision, GroupApprove, a.fn); !errors.Is(err, ErrGroupRequestResolved) || a.calls != 0 {
		t.Fatalf("answer after the limit: %v calls=%d", err, a.calls)
	}
	got, _, _ := st.GetGroupRequest(context.Background(), r.ID)
	if got.State != GroupRequestExpired || got.Outcome != GroupOutcomeExpired {
		t.Fatalf("expired request = %+v", got)
	}
	// An expired request is gone from the open list, so the child may ask again.
	if again := f.ask(t, GroupRouteParent, "git"); again.ID == r.ID {
		t.Fatal("an expired request was returned as the open one")
	}
}

func TestARestartClosesEveryOpenRequestAndKeepsTheLocks(t *testing.T) {
	f := newGroupFixture(t)
	parentRoute := f.ask(t, GroupRouteParent, "git")
	userRoute := f.ask(t, GroupRouteUser, "web")
	ui := identity.WithCaller(context.Background(), identity.UI())
	if err := f.svc.SetGroupLock(ui, f.child.ID, "db", true); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecoverStaleSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{parentRoute.ID, userRoute.ID} {
		got, err := f.svc.GetGroupRequest(ui, id)
		if err != nil || got.State != GroupRequestInvalidated || got.Outcome != GroupOutcomeRestart || got.Resolver != "system" {
			t.Fatalf("after restart %s = %+v %v", id, got, err)
		}
	}
	a := &applier{committed: true}
	if _, _, err := f.svc.AnswerGroupRequestAsUser(context.Background(), userRoute.ID, userRoute.Revision+1, UserAnswer{Approve: true}, a.fn); !errors.Is(err, ErrGroupRequestResolved) || a.calls != 0 {
		t.Fatalf("a question from before the restart was answerable: %v calls=%d", err, a.calls)
	}
	if locks, _ := f.svc.GroupLocks(ui, f.child.ID); len(locks) != 1 || locks[0] != "db" {
		t.Fatalf("locks after restart = %v", locks)
	}
}

func TestWhoSeesWhat(t *testing.T) {
	f := newGroupFixture(t)
	r := f.ask(t, GroupRouteParent, "git")
	ui := identity.WithCaller(context.Background(), identity.UI())
	if _, err := f.svc.GetGroupRequest(f.ctx, r.ID); err != nil {
		t.Errorf("parent: %v", err)
	}
	if _, err := f.svc.GetGroupRequest(f.childCtx, r.ID); err != nil {
		t.Errorf("child: %v", err)
	}
	stranger, _ := f.svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if _, err := f.svc.GetGroupRequest(identity.WithCaller(context.Background(), identity.Agent(stranger.ID)), r.ID); !errors.Is(err, ErrGroupRequestNotFound) {
		t.Errorf("stranger: %v", err)
	}
	if list, err := f.svc.ListGroupRequests(f.ctx); err != nil || len(list) != 1 {
		t.Errorf("parent list = %v %v", list, err)
	}
	if list, err := f.svc.ListGroupRequests(ui); err != nil || len(list) != 0 {
		t.Errorf("the window lists only requests waiting for the person: %v %v", list, err)
	}
	u := f.ask(t, GroupRouteUser, "db")
	if list, _ := f.svc.ListGroupRequests(ui); len(list) != 1 || list[0].ID != u.ID {
		t.Errorf("window list = %v", list)
	}
}
