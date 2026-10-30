package harnessconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

func grantableOf(groups ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return groups, nil }
}

// s1 hands "git" to s1b (same workspace).
func delegation() Delegation {
	return Delegation{WorkspaceID: "w1", ChildSessionID: "s1b", Groups: []string{"git"}, GrantorSessionID: "s1", RequestID: "req-1", Grantable: grantableOf("git", "tasks")}
}

func TestADelegatedGrantWritesTheChildsSOnceWithoutAskingThePerson(t *testing.T) {
	a := newApplyEnv(t)
	wst := gitOff(a.env, "w1")
	res, err := a.svc.ApplyDelegated(agent("s1"), delegation())
	if err != nil {
		t.Fatal(err)
	}
	if res.Override.Revision != 1 || res.Profile.Revision != 0 {
		t.Fatalf("result = %+v", res)
	}
	if o, _ := a.override("w1", "s1b"); o == nil || len(o.Groups) != 1 || o.Groups[0] != "git" {
		t.Fatalf("child's S = %+v", o)
	}
	if o, _ := a.override("w1", "s1"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("the grantor's own S changed: %+v", o)
	}
	if _, st := a.profile("w1"); st != wst {
		t.Fatalf("a delegated grant touched W: %+v", st)
	}
	if a.confirm.calls != 0 {
		t.Fatalf("the person was asked %d times; the parent's grant replaces the question", a.confirm.calls)
	}
	if a.boundary.holds != 1 {
		t.Fatalf("the V boundary was held %d times, want once", a.boundary.holds)
	}
	if len(a.events) != 1 || a.events[0].Outcome != "applied" || a.events[0].Grantor != "s1" || a.events[0].RequestID != "req-1" ||
		a.events[0].SessionID != "s1b" || a.events[0].Caller != "agent:s1" {
		t.Fatalf("audit = %+v", a.events)
	}
	// Repeating it finds nothing to change: the groups are served already.
	if _, err := a.svc.ApplyDelegated(agent("s1"), delegation()); !errors.Is(err, ErrNoChange) {
		t.Fatalf("second grant: %v", err)
	}
}

func TestADelegatedGrantIsRefusedForEveryoneButTheNamedGrantor(t *testing.T) {
	a := newApplyEnv(t)
	a.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false, "tasks": false} })
	cases := map[string]struct {
		ctx context.Context
		mod func(*Delegation)
	}{
		"the child itself":         {agent("s1b"), func(*Delegation) {}},
		"a stranger":               {agent("s2"), func(*Delegation) {}},
		"the window":               {ui(), func(*Delegation) {}},
		"a terminal":               {identity.WithCaller(context.Background(), identity.UserTerminal()), func(*Delegation) {}},
		"no caller":                {context.Background(), func(*Delegation) {}},
		"a grantor named as child": {agent("s1"), func(d *Delegation) { d.ChildSessionID = "s1" }},
		"a group not grantable":    {agent("s1"), func(d *Delegation) { d.Groups = []string{"tasks"}; d.Grantable = grantableOf("git") }},
		"no groups":                {agent("s1"), func(d *Delegation) { d.Groups = nil }},
		"no request id":            {agent("s1"), func(d *Delegation) { d.RequestID = "" }},
		"another workspace":        {agent("s1"), func(d *Delegation) { d.WorkspaceID = "w2" }},
	}
	for name, c := range cases {
		d := delegation()
		c.mod(&d)
		if _, err := a.svc.ApplyDelegated(c.ctx, d); err == nil {
			t.Errorf("%s: granted", name)
		}
	}
	if o, _ := a.override("w1", "s1b"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("a refused grant wrote S: %+v", o)
	}
	if a.confirm.calls != 0 || len(a.events) == 0 {
		t.Fatalf("asked %d times, audit rows %d", a.confirm.calls, len(a.events))
	}
}

func TestADelegatedGrantCannotCarryAnythingButGroups(t *testing.T) {
	// The Delegation type has no budget, W, revoke or executable field; the only
	// patch it builds is EnableGroups. This pins that a delegated grant never
	// writes W even when W would change the outcome.
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	_, wBefore := a.profile("w1")
	if _, err := a.svc.ApplyDelegated(agent("s1"), delegation()); err != nil {
		t.Fatal(err)
	}
	if _, wAfter := a.profile("w1"); wAfter != wBefore {
		t.Fatalf("W moved: %+v -> %+v", wBefore, wAfter)
	}
}

func TestADelegatedGrantFailsClosedWithoutTheBoundary(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	if _, err := e.svc.ApplyDelegated(agent("s1"), delegation()); !errors.Is(err, ErrConfirmUnavailable) {
		t.Fatalf("no boundary: %v", err)
	}
	if o, _ := e.override("w1", "s1b"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("S written without the boundary: %+v", o)
	}
}

func TestADelegatedGrantPublishesToTheChildsEndpointAndOnlyThen(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1b", "w1")
	res, err := l.svc.ApplyDelegated(agent("s1"), delegation())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Live || res.Publication.State != PublicationInstalled || res.Override.Revision != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1b,publish:s1b:1" {
		t.Fatalf("events = %s: the child's endpoint is the one coordinated, never the grantor's", got)
	}
	if l.confirm.calls != 0 {
		t.Fatalf("the person was asked %d times", l.confirm.calls)
	}
}

func TestADelegatedGrantWhosePublicationFailedStillReportsTheCommit(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1b", "w1")
	l.live.publishErr = errors.New("registry update failed")
	res, err := l.svc.ApplyDelegated(agent("s1"), delegation())
	var pe *PublicationError
	if !errors.As(err, &pe) {
		t.Fatalf("want a publication error, got %v", err)
	}
	if res.Live || res.Commit != CommitCommitted || res.Override.Revision != 1 || res.Publication.State != PublicationPending {
		t.Fatalf("a committed change must report its stamps: %+v", res)
	}
	if strings.Contains(err.Error(), "registry update failed") {
		t.Fatalf("the cause reached the caller: %v", err)
	}
	if o, _ := l.override("w1", "s1b"); o == nil || len(o.Groups) != 1 {
		t.Fatalf("the committed write is not durable: %+v", o)
	}
}

func TestADelegatedGrantWithAnUnknownCommitStaysPaused(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1b", "w1")
	l.store.mode = "unknownAfter"
	l.store.failGet = errors.New("db gone")
	res, err := l.svc.ApplyDelegated(agent("s1"), delegation())
	var pe *PublicationError
	if !errors.As(err, &pe) || res.Live || res.Commit != CommitUnknown {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	if !l.live.target("s1b").Quarantined {
		t.Fatal("admission must stay closed after an unknown commit")
	}
}

func TestADelegatedGrantFailsWhenWritingSFails(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1b", "w1")
	l.store.mode = "fail"
	if _, err := l.svc.ApplyDelegated(agent("s1"), delegation()); err == nil {
		t.Fatal("a failed write reported success")
	}
	if got := strings.Join(l.live.events(), ","); got != "quarantine:s1b,restore:s1b" {
		t.Fatalf("a proven rollback must restore admission: %s", got)
	}
}

func (e *env) lock(session, group string) {
	e.t.Helper()
	if _, err := e.db.Exec(`INSERT INTO agent_session_group_locks(session_id,group_name,created_at) VALUES(?,?,'x') ON CONFLICT DO NOTHING`, session, group); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) locks(session string) []string {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT group_name FROM agent_session_group_locks WHERE session_id=? ORDER BY 1`, session)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, g)
	}
	return out
}

// The grantor's right to hand a group over is read again inside the boundary: a
// set read before it is not authority at the commit.
func TestAGrantorThatLosesAGroupBetweenTheEarlyCheckAndTheCommitIsRefused(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	calls := 0
	d := delegation()
	d.Grantable = func(context.Context) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{"git"}, nil // the early check
		}
		return nil, nil // the check inside the boundary: the grantor no longer serves it
	}
	if _, err := a.svc.ApplyDelegated(agent("s1"), d); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
	if calls != 2 {
		t.Fatalf("Grantable was read %d times, want once early and once inside the commit", calls)
	}
	if o, _ := a.override("w1", "s1b"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("S = %+v, want nothing written", o)
	}
	if a.boundary.holds != 1 {
		t.Fatalf("boundary holds = %d", a.boundary.holds)
	}
}

// The person blocks the group after the early check and before the write; the
// store finds the block inside its own transaction.
func TestABlockWrittenAfterTheEarlyCheckStopsADelegatedGrant(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	calls := 0
	d := delegation()
	d.Grantable = func(context.Context) ([]string, error) {
		calls++
		if calls == 2 { // inside the boundary, just before the write
			a.lock("s1b", "git")
		}
		return []string{"git"}, nil
	}
	if _, err := a.svc.ApplyDelegated(agent("s1"), d); !errors.Is(err, ErrGroupLocked) {
		t.Fatalf("err = %v, want ErrGroupLocked", err)
	}
	if o, _ := a.override("w1", "s1b"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("a blocked group was written: %+v", o)
	}
}

// A proposal prepared before the block, approved after it, does not write.
func TestAnApprovedProposalCannotAddAGroupBlockedMeanwhile(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	id := a.prepareEnableGit("s1")
	a.confirm.during = func() { a.lock("s1", "git") } // blocked while the question is open
	if _, err := a.svc.Apply(agent("s1"), id); !errors.Is(err, ErrGroupLocked) {
		t.Fatalf("err = %v, want ErrGroupLocked", err)
	}
	if o, _ := a.override("w1", "s1"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("S = %+v", o)
	}
}

func TestOnlyTheWindowsRevokeCanCarryABlockAndItIsAtomicWithTheRevoke(t *testing.T) {
	a := newApplyEnv(t)
	gitOff(a.env, "w1")
	a.seedOverride("w1", "s1b", "git")
	revoke := Request{WorkspaceID: "w1", SessionID: "s1b", Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}, LockRevoked: true}}

	// An agent cannot write a lock, whoever the target is.
	if _, err := a.svc.Prepare(agent("s1b"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}, LockRevoked: true}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("agent revoke with a block: %v", err)
	}
	// A block alone, or with an enable, is not a revoke.
	for name, p := range map[string]Patch{
		"no revoke": {LockRevoked: true, EnableGroups: []string{"tasks"}},
		"enable":    {EnableGroups: []string{"tasks"}, RevokeGroups: []string{"git"}, LockRevoked: true},
	} {
		if _, err := a.svc.Prepare(ui(), Request{WorkspaceID: "w1", SessionID: "s1b", Target: TargetSession, Patch: p}); err == nil {
			t.Errorf("%s: prepared", name)
		}
	}
	// Declined: nothing is written, no block is left.
	p, err := a.svc.Prepare(ui(), revoke)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.View.Summary, "blocked for this session") {
		t.Fatalf("the review does not say the revoke also blocks: %s", p.View.Summary)
	}
	a.confirm.answer = errors.New("no")
	if _, err := a.svc.Apply(ui(), p.ID); err == nil {
		t.Fatal("a declined revoke reported success")
	}
	if got := a.locks("s1b"); len(got) != 0 {
		t.Fatalf("a declined revoke left blocks %v", got)
	}
	if o, _ := a.override("w1", "s1b"); o == nil || len(o.Groups) != 1 {
		t.Fatalf("a declined revoke changed S: %+v", o)
	}
	// Confirmed: S and the block land together.
	a.confirm.answer = nil
	p, err = a.svc.Prepare(ui(), revoke)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(ui(), p.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.locks("s1b"); len(got) != 1 || got[0] != "git" {
		t.Fatalf("blocks = %v", got)
	}
	if o, _ := a.override("w1", "s1b"); o != nil && len(o.Groups) != 0 {
		t.Fatalf("S = %+v", o)
	}
	// And now nobody adds it back.
	if _, err := a.svc.ApplyDelegated(agent("s1"), delegation()); !errors.Is(err, ErrGroupLocked) {
		t.Fatalf("delegated grant of a blocked group: %v", err)
	}
}

// A revoke whose block cannot be saved is not a revoke: the whole write fails.
func TestARevokeWhoseBlockCannotBeSavedFailsAndChangesNothing(t *testing.T) {
	a := newApplyEnv(t)
	a.seedOverride("w1", "s1b", "git")
	if _, err := a.db.Exec(`CREATE TRIGGER fail_lock BEFORE INSERT ON agent_session_group_locks BEGIN SELECT RAISE(FAIL, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	p, err := a.svc.Prepare(ui(), Request{WorkspaceID: "w1", SessionID: "s1b", Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}, LockRevoked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Apply(ui(), p.ID); err == nil {
		t.Fatal("a revoke whose block failed reported success")
	}
	if o, _ := a.override("w1", "s1b"); o == nil || len(o.Groups) != 1 {
		t.Fatalf("the revoke landed without its block: %+v", o)
	}
}
