package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

// R1a-1: a session override (S) that names a group this build does not register
// locks that one session out. F-C repairs the workspace profile only. The window can
// remove exactly the stale groups from S, confirmed, without closing the session.
// These run the real composed stack; only vendors and the person's answers are
// scripted.

// writeOverride stores S directly, as an earlier build that had the module wired
// would have left it. It is the fixture, not the repair.
func (r *compRig) writeOverride(sessionID string, groups ...string) harness.Stamp {
	r.t.Helper()
	_, st := r.override(sessionID)
	a, err := r.asm.Store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: r.wsID, SessionID: sessionID,
		Override: &harness.OverrideWrite{Expect: st, Value: &harness.Override{Schema: harness.SchemaVersion, Groups: groups}}})
	if err != nil {
		r.t.Fatal(err)
	}
	return a.Override
}

// staleSessionRig is the report's scenario with a clean workspace profile: a session
// whose S enabled pdf while the module was wired, then a build without the module.
func staleSessionRig(t *testing.T, groups ...string) (*compRig, string) {
	t.Helper()
	r := newCompRig(t, compOptions{controls: true})
	sess, _ := r.create(agenthub.AgentTypeClaude)
	r.writeOverride(sess.ID, groups...)
	r.opt.noPDF = true
	r.restart()
	return r, sess.ID
}

func (r *compRig) revokeReview(sessionID string, groups ...string) (harnessconfig.Review, error) {
	return r.asm.Controls().PrepareRevoke(r.ui(), r.wsID, sessionID, groups)
}

func storedGroups(o *harness.Override) string {
	if o == nil {
		return "<none>"
	}
	return strings.Join(o.Groups, ",")
}

// The report's lockout, end to end: the refusal says what is wrong and what to do,
// a declined repair changes nothing, an approved one removes only the unavailable
// group, and the same session then opens, without a Close and serving nothing new.
func TestAStaleSessionOverrideIsRemovedFromTheWindowAndTheSessionOpensAgain(t *testing.T) {
	r, sid := staleSessionRig(t, "pdf")
	r.workspaceOff("vault") // W is clean (vault is registered) and keeps vault off

	// The refusal is truthful and leaks nothing stored.
	err := r.agents.ResumeSession(r.ui(), sid, true)
	if !errors.Is(err, harness.ErrStaleSessionOverride) {
		t.Fatalf("resume = %v, want the stale-override refusal", err)
	}
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeSnapshotUnavailable || strings.Contains(he.Message, "pdf") || !strings.Contains(he.Message, "Revoke") {
		t.Fatalf("refusal = %+v", err)
	}
	// The resolver stays strict: the snapshot is still refused, not made permissive.
	if _, err := r.asm.Provider.Snapshot(context.Background(), harnessconfig.SnapshotRequest{SessionID: sid, WorkspaceID: r.wsID, Vendor: string(agenthub.AgentTypeClaude)}); err == nil {
		t.Fatal("the launch resolver must stay strict")
	}
	refBefore, enforcedBefore, _ := r.agents.HarnessApplied(sid)
	_, wBefore := r.storedProfile()
	oBefore, sBefore := r.override(sid)

	// The card lists the unavailable group, and Revoke prepares the repair.
	view, err := r.asm.Controls().SessionProfile(r.ui(), r.wsID, sid)
	if err != nil || view.Session == nil || strings.Join(view.Session.Overrides, ",") != "pdf" {
		t.Fatalf("session view = %+v, %v", view.Session, err)
	}
	rev, err := r.revokeReview(sid, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if rev.Target != string(harnessconfig.TargetSessionRepair) || rev.Tier != "narrowing" || !rev.NeedsConfirmation || len(rev.Changes) != 1 ||
		rev.SessionID != sid || !strings.Contains(rev.Summary, `"pdf"`) || !strings.Contains(rev.Summary, "removal only") {
		t.Fatalf("review = %+v", rev)
	}

	// Declined: nothing changes and the session still cannot open.
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "deny"}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
		t.Fatal("a denied repair must be refused")
	}
	if o, s := r.override(sid); s != sBefore || storedGroups(o) != storedGroups(oBefore) {
		t.Fatalf("a denial changed S: %+v", o)
	}
	if err := r.agents.ResumeSession(r.ui(), sid, true); !errors.Is(err, harness.ErrStaleSessionOverride) {
		t.Fatalf("still stale after a denial: %v", err)
	}

	// Approved.
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	if rev, err = r.revokeReview(sid, "pdf"); err != nil {
		t.Fatal(err)
	}
	out, err := r.asm.Controls().Apply(r.ui(), rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != harnessconfig.OutcomeApplied || out.Live || out.ReloadRequired || out.Target != string(harnessconfig.TargetSessionRepair) ||
		out.Commit != harnessconfig.CommitCommitted || out.OverrideRevision != sBefore.Revision+1 {
		t.Fatalf("outcome = %+v", out)
	}
	if o, s := r.override(sid); o != nil || s.Revision != sBefore.Revision+1 {
		t.Fatalf("S after = %+v %+v, want removed in one revision step", o, s)
	}
	if _, w := r.storedProfile(); w != wBefore {
		t.Fatal("the workspace profile must not change")
	}
	if ref, enforced, _ := r.agents.HarnessApplied(sid); ref.Fingerprint != refBefore.Fingerprint || ref.ExecutableDigest != refBefore.ExecutableDigest || enforced != enforcedBefore {
		t.Fatal("the applied reference must not change by the repair")
	}

	// The same session opens, with no Close, and serves only what is registered and on.
	if err := r.agents.ResumeSession(r.ui(), sid, true); err != nil {
		t.Fatalf("resume after the repair: %v", err)
	}
	names := r.listed(r.connect(r.vendors[agenthub.AgentTypeClaude].LastSpec()))
	if hasGroup(names, "pdf") || hasGroup(names, "vault") || !hasGroup(names, "git") {
		t.Fatalf("a repaired session must serve what is registered and on, nothing more: %v", names)
	}
	// The window sees a healthy session again.
	if d := r.stale(); d.State != harnessconfig.StaleNone {
		t.Fatalf("diagnosis: %+v", d)
	}
}

// Only the stale group goes: a group S legitimately holds is kept, and a still
// registered name is not removable through this path.
func TestOnlyTheUnavailableGroupLeavesTheOverrideAndARegisteredNameIsNeverRemovedThroughIt(t *testing.T) {
	r, sid := staleSessionRig(t, "pdf", "vault")
	r.workspaceOff("vault")
	_, sBefore := r.override(sid)

	// A registered name takes the ordinary revoke, which cannot evaluate a stale S: it
	// is refused and nothing is written.
	if _, err := r.revokeReview(sid, "vault"); err == nil {
		t.Fatal("a registered group must not be removed through the stale path")
	}
	// A mix of a registered and an unavailable name is refused.
	if _, err := r.revokeReview(sid, "vault", "pdf"); err == nil {
		t.Fatal("a mixed request must be refused")
	}
	// A name S does not hold, and one nobody has, are the unknown group they always were.
	for _, name := range []string{"ghost", "gone"} {
		if _, err := r.revokeReview(sid, name); err == nil || !strings.Contains(err.Error(), harness.CodeGroupUnknown) {
			t.Fatalf("%s = %v", name, err)
		}
	}
	if r.asm.Service.Pending() != 0 {
		t.Fatal("a refused request left a prepared proposal behind")
	}
	if o, s := r.override(sid); s != sBefore || storedGroups(o) != "pdf,vault" {
		t.Fatalf("a refusal changed S: %+v", o)
	}

	rev, err := r.revokeReview(sid, "pdf")
	if err != nil || len(rev.Changes) != 1 {
		t.Fatalf("review = %+v, %v", rev, err)
	}
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	if o, s := r.override(sid); storedGroups(o) != "vault" || s.Revision != sBefore.Revision+1 {
		t.Fatalf("S after = %q %+v", storedGroups(o), s)
	}
	// Removal only: what the person had turned on stays on, and nothing else is on.
	if err := r.agents.ResumeSession(r.ui(), sid, true); err != nil {
		t.Fatal(err)
	}
	names := r.listed(r.connect(r.vendors[agenthub.AgentTypeClaude].LastSpec()))
	if !hasGroup(names, "vault") || hasGroup(names, "pdf") {
		t.Fatalf("served = %v", names)
	}
}

// A healthy S with a name the registry lacks is not a repair, and a still-registered
// group is revoked by the ordinary path exactly as before.
func TestARevokeOnAHealthySessionIsUnchangedAndAnUnknownNameStaysUnknown(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.revokeReview(sess.ID, "pdfx"); err == nil || !strings.Contains(err.Error(), harness.CodeGroupUnknown) {
		t.Fatalf("an unknown name on a healthy S = %v", err)
	}
	rev, err := r.revokeReview(sess.ID, "vault")
	if err != nil {
		t.Fatal(err)
	}
	if rev.Target != string(harnessconfig.TargetSession) {
		t.Fatalf("the ordinary revoke must stay ordinary: %+v", rev)
	}
	_ = r.asm.Controls().Cancel(r.ui(), rev.ID)
}

// Window only, enforced in Go at each owner: the facade, the service and Apply. An
// agent, a terminal or an unknown origin gets nothing, and a prepared id is the
// preparer's alone.
func TestOnlyTheWindowCanRepairAStaleSessionOverride(t *testing.T) {
	r, sid := staleSessionRig(t, "pdf")
	ctl := r.asm.Controls()
	_, sBefore := r.override(sid)
	for name, ctx := range map[string]context.Context{
		"agent of the session": r.agentCtx(sid),
		"terminal":             identity.WithCaller(r.ctx, identity.UserTerminal()),
		"unknown":              r.ctx,
	} {
		if _, err := ctl.PrepareRevoke(ctx, r.wsID, sid, []string{"pdf"}); !errors.Is(err, harnessconfig.ErrDenied) {
			t.Errorf("%s through the facade = %v", name, err)
		}
		if _, err := r.asm.Service.PrepareSessionRepair(ctx, r.wsID, sid, []string{"pdf"}); !errors.Is(err, harnessconfig.ErrDenied) {
			t.Errorf("%s through the service = %v", name, err)
		}
	}
	// Another session's agent is not the owner either.
	other, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.asm.Service.PrepareSessionRepair(r.agentCtx(other.ID), r.wsID, sid, []string{"pdf"}); !errors.Is(err, harnessconfig.ErrDenied) {
		t.Errorf("another session's agent = %v", err)
	}
	// The window cannot reach a session through another workspace.
	if _, err := r.asm.Service.PrepareSessionRepair(r.ui(), "no-such-workspace", sid, []string{"pdf"}); err == nil {
		t.Error("a wrong workspace must be refused")
	}
	if r.asm.Service.Pending() != 0 {
		t.Fatal("a refusal prepared something")
	}

	// A prepared id cannot be applied, cancelled or listed by anyone but the window.
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	rev, err := ctl.PrepareRevoke(r.ui(), r.wsID, sid, []string{"pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.Apply(r.agentCtx(sid), rev.ID); !errors.Is(err, harnessconfig.ErrDenied) {
		t.Errorf("an agent applying through the facade = %v", err)
	}
	if _, err := r.asm.Service.Apply(r.agentCtx(sid), rev.ID); !errors.Is(err, harnessconfig.ErrUnknownProposal) {
		t.Errorf("an agent applying through the service = %v", err)
	}
	if _, err := r.asm.Service.Apply(identity.WithCaller(r.ctx, identity.UserTerminal()), rev.ID); !errors.Is(err, harnessconfig.ErrUnknownProposal) {
		t.Errorf("a terminal applying through the service = %v", err)
	}
	if err := ctl.Cancel(r.agentCtx(sid), rev.ID); !errors.Is(err, harnessconfig.ErrDenied) {
		t.Errorf("an agent cancelling = %v", err)
	}
	if o, s := r.override(sid); s != sBefore || storedGroups(o) != "pdf" {
		t.Fatalf("nothing may be written: %+v", o)
	}
	// And the agent's own request tool still cannot revoke, repair or see any of it.
	if _, err := r.asm.Service.Prepare(r.agentCtx(sid), harnessconfig.Request{Target: harnessconfig.Target("sessionRepair")}); err == nil {
		t.Error("no request may carry the repair target")
	}
	if _, err := r.asm.Service.Prepare(identity.WithCaller(r.ctx, identity.UI()), harnessconfig.Request{WorkspaceID: r.wsID, SessionID: sid, Target: harnessconfig.TargetSessionRepair}); err == nil {
		t.Error("no ordinary request may carry the repair target")
	}
}

// A replayed, cancelled or overtaken repair does nothing.
func TestAReplayedCancelledOrOvertakenSessionRepairChangesNothing(t *testing.T) {
	t.Run("replay and cancel", func(t *testing.T) {
		r, sid := staleSessionRig(t, "pdf")
		r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
		rev, err := r.revokeReview(sid, "pdf")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.asm.Controls().Cancel(r.ui(), rev.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
			t.Fatal("a cancelled id must not apply")
		}
		if o, _ := r.override(sid); storedGroups(o) != "pdf" {
			t.Fatalf("S = %q", storedGroups(o))
		}

		rev, err = r.revokeReview(sid, "pdf")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
			t.Fatal(err)
		}
		_, after := r.override(sid)
		if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
			t.Fatal("a spent id must not apply again")
		}
		if _, now := r.override(sid); now != after {
			t.Fatal("a replay wrote")
		}
		// And a repair with nothing stale left is refused, not an empty write.
		if _, err := r.revokeReview(sid, "pdf"); err == nil {
			t.Fatal("nothing is stale any more")
		}
	})

	// While the question is open anything the approval was bound to may move; the
	// old removal must never erase what is newer.
	cases := map[string]func(r *compRig, sid string){
		"the override gains another group": func(r *compRig, sid string) { r.writeOverride(sid, "pdf", "vault") },
		"the override gains another stale group": func(r *compRig, sid string) {
			r.writeOverride(sid, "pdf", "ghost")
		},
		"the override is rewritten": func(r *compRig, sid string) { r.writeOverride(sid, "pdf") },
		"the workspace profile changes": func(r *compRig, sid string) {
			r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"vault": false} })
		},
		"the workspace profile goes stale": func(r *compRig, sid string) {
			r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
		},
		"a vendor edit": func(r *compRig, sid string) {
			c, _ := r.prefs.AgentCustomization("claude")
			c.DisabledMCP = []string{"slack"}
			if _, err := r.prefs.SetAgentCustomization("claude", c); err != nil {
				r.t.Error(err)
			}
		},
		"policy off then on": func(r *compRig, sid string) {
			p, _ := r.prefs.AgentHarnessPolicy()
			on, err := r.prefs.SetAgentHarnessAutoNarrowing(p.Revision, true)
			if err != nil {
				r.t.Error(err)
				return
			}
			if _, err := r.prefs.SetAgentHarnessAutoNarrowing(on.Revision, false); err != nil {
				r.t.Error(err)
			}
		},
		"the session is closed": func(r *compRig, sid string) {
			if err := r.agents.CloseSession(r.ui(), sid); err != nil {
				r.t.Error(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, sid := staleSessionRig(t, "pdf")
			rev, err := r.revokeReview(sid, "pdf")
			if err != nil {
				t.Fatal(err)
			}
			r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
			r.prompter.mu.Lock()
			r.prompter.during = func() { change(r, sid) }
			r.prompter.mu.Unlock()
			_, apply := r.asm.Controls().Apply(r.ui(), rev.ID)
			if apply == nil {
				t.Fatal("the repair must be stale")
			}
			o, _ := r.override(sid)
			switch name {
			case "the session is closed":
				if o != nil {
					t.Fatalf("a close clears S and the repair must not resurrect it: %q", storedGroups(o))
				}
			case "the override gains another group":
				if storedGroups(o) != "pdf,vault" {
					t.Fatalf("the newer S was overwritten: %q", storedGroups(o))
				}
			case "the override gains another stale group":
				if storedGroups(o) != "ghost,pdf" && storedGroups(o) != "pdf,ghost" {
					t.Fatalf("the newer S was overwritten: %q", storedGroups(o))
				}
			case "the workspace profile changes", "the workspace profile goes stale", "the override is rewritten", "a vendor edit", "policy off then on":
				if storedGroups(o) != "pdf" {
					t.Fatalf("a stale repair wrote: %q", storedGroups(o))
				}
			}
		})
	}
}

// Whatever the auto-narrowing setting and whatever was said before, the repair asks
// every time; with nobody to ask it writes nothing.
func TestASessionRepairAlwaysAsksAndHeadlessWritesNothing(t *testing.T) {
	r, sid := staleSessionRig(t, "pdf")
	p, _ := r.prefs.AgentHarnessPolicy()
	if _, err := r.prefs.SetAgentHarnessAutoNarrowing(p.Revision, true); err != nil {
		t.Fatal(err)
	}
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow_always"}
	rev, err := r.revokeReview(sid, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	asked := 0
	for _, k := range kinds(r.prompter) {
		if k == interaction.KindHarness {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("the person was asked %d times, want 1 (allow-always and auto-narrowing never skip it)", asked)
	}

	r.writeOverride(sid, "pdf")
	rev, err = r.revokeReview(sid, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, before := r.override(sid)
	r.hub.Headless = true
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); !errors.Is(err, harnessconfig.ErrConfirmUnavailable) {
		t.Fatalf("headless = %v", err)
	}
	if _, now := r.override(sid); now != before {
		t.Fatal("headless must write nothing")
	}
}

// If the workspace profile is stale too, it is the first thing to repair; the
// refusal says so, and nothing about S is written.
func TestAStaleWorkspaceProfileIsRepairedBeforeTheSessionOverride(t *testing.T) {
	r, sid := staleSessionRig(t, "pdf")
	r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
	if err := r.agents.ResumeSession(r.ui(), sid, true); !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("with both stale the generic refusal stands, got %v", err)
	}
	if _, err := r.revokeReview(sid, "pdf"); !errors.Is(err, harnessconfig.ErrSessionRepairNeedsProfile) {
		t.Fatalf("revoke = %v", err)
	}
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	wrev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), wrev.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.agents.ResumeSession(r.ui(), sid, true); !errors.Is(err, harness.ErrStaleSessionOverride) {
		t.Fatalf("after the workspace repair the session says what is left: %v", err)
	}
	rev, err := r.revokeReview(sid, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.agents.ResumeSession(r.ui(), sid, true); err != nil {
		t.Fatal(err)
	}
}

// A session with a running enforcing endpoint is not touched: S moving under a
// served policy is the ordinary publication path, not this repair.
func TestARunningSessionIsNotRepairedUnderItsEndpoint(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, spec := r.create(agenthub.AgentTypeClaude)
	r.connect(spec)
	r.writeOverride(sess.ID, "ghost") // not registered; the endpoint is running
	rev, err := r.revokeReview(sess.ID, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	_, before := r.override(sess.ID)
	servingBefore := r.agents.ServingTarget(sess.ID)
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); !errors.Is(err, harnessconfig.ErrLiveUnavailable) {
		t.Fatalf("apply = %v", err)
	}
	if o, now := r.override(sess.ID); now != before || storedGroups(o) != "ghost" {
		t.Fatal("a running session's S was written")
	}
	if got := r.agents.ServingTarget(sess.ID); got.Incarnation != servingBefore.Incarnation || got.Epoch != servingBefore.Epoch || got.Quarantined {
		t.Fatalf("the running endpoint was touched: %+v -> %+v", servingBefore, got)
	}
}

// An ambiguous commit is reported as uncertain, never retried, and a fresh read
// shows what is really stored.
func TestAnAmbiguousSessionRepairCommitIsReportedAsUncertain(t *testing.T) {
	var f *fault
	r := newCompRig(t, compOptions{controls: true, wrapStore: func(p harnessPersistence) harnessPersistence {
		f = &fault{harnessPersistence: p}
		return f
	}})
	sess, _ := r.create(agenthub.AgentTypeClaude)
	r.writeOverride(sess.ID, "pdf")
	r.opt.noPDF = true
	r.restart()
	rev, err := r.revokeReview(sess.ID, "pdf")
	if err != nil {
		t.Fatal(err)
	}
	f.mode.Store("unknown")
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	out, err := r.asm.Controls().Apply(r.ui(), rev.ID)
	if err != nil {
		t.Fatalf("an uncertain commit is an outcome, not an error: %v", err)
	}
	if out.Status != harnessconfig.OutcomeUnknown || out.Live || out.Target != string(harnessconfig.TargetSessionRepair) || strings.Contains(strings.ToLower(out.Message+out.Recovery), "reconcile") {
		t.Fatalf("outcome = %+v", out)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
		t.Fatal("the same id must not be applied again")
	}
	f.failReads.Store(false)
	f.mode.Store("")
	if o, _ := r.override(sess.ID); o != nil {
		t.Fatalf("a fresh read shows the write: %q", storedGroups(o))
	}
}

// R1a-2: a session repair whose review could not show every removal in full is
// refused with the repair's own sentence (a repair cannot be split), not the
// ordinary editor's "split it into smaller changes", and nothing is written.
func TestASessionRepairTooLongToShowInFullIsRefusedWithoutAdviceToSplitIt(t *testing.T) {
	// The window may name only a short group (it is a selector); the removal is every
	// stale group stored, and the long ones are what the review cannot hold.
	names := []string{"gone"}
	for i := 1; i < harnessconfig.MaxStaleEntries; i++ {
		names = append(names, fmt.Sprintf("gone%03d-", i)+strings.Repeat("x", 120))
	}
	r, sid := staleSessionRig(t, names...)
	_, sBefore := r.override(sid)
	_, err := r.revokeReview(sid, names[0])
	if err == nil || strings.Contains(err.Error(), "Split") || !strings.Contains(err.Error(), "cannot be split") {
		t.Fatalf("revoke = %v", err)
	}
	if _, sAfter := r.override(sid); sAfter != sBefore {
		t.Fatal("a refused repair wrote")
	}
}
