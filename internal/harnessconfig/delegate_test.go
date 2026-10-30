package harnessconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// s1 hands "git" to s1b (same workspace).
func delegation() Delegation {
	return Delegation{WorkspaceID: "w1", ChildSessionID: "s1b", Groups: []string{"git"}, GrantorSessionID: "s1", RequestID: "req-1", Grantable: []string{"git", "tasks"}}
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
		"a group not grantable":    {agent("s1"), func(d *Delegation) { d.Groups = []string{"tasks"}; d.Grantable = []string{"git"} }},
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
