package app

import (
	"context"
	"encoding/json"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"
)

// The window's side of the controls, through the real SQLite store, the shared
// hub and the H4c coordinator. The person is scripted (the prompter answers);
// nothing here calls a vendor or a model.

func (r *compRig) storeW(w *harness.Profile) harness.Stamp {
	r.t.Helper()
	_, st, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	a, err := r.asm.Store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: r.wsID, Profile: &harness.ProfileWrite{Expect: st, Value: w}})
	if err != nil {
		r.t.Fatal(err)
	}
	return a.Profile
}

func TestTheEditorChangesOnlyGroupEntriesAndKeepsEveryOtherFieldOfW(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	bytes := 4096
	r.storeW(&harness.Profile{
		Schema:       harness.SchemaVersion,
		ToolGroups:   map[string]bool{"vault": false, "db": false},
		ResultBudget: &harness.BudgetProfile{Tools: map[string]harness.BudgetRule{"workspace.list": {Bytes: &bytes}}, Groups: map[string]harness.BudgetRule{"env": {Cut: harness.CutHead}}},
	})
	_, beforeSt, _ := r.asm.Store.GetProfile(context.Background(), r.wsID)
	before, _, _ := r.asm.Store.GetProfile(context.Background(), r.wsID)
	beforeJSON, _ := json.Marshal(before)

	rev, err := r.port.PrepareWorkspaceGroups(r.ui(), r.wsID, []harnessconfig.GroupEdit{
		{Group: "vault", Setting: harnessconfig.SettingOn},
		{Group: "db", Setting: harnessconfig.SettingInherit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev.ID == "" || rev.Target != "workspace" || !rev.NeedsConfirmation || len(rev.Changes) == 0 || rev.WorkspaceID != r.wsID {
		t.Fatalf("expected a host-authored review that needs approval: %+v", rev)
	}
	stored := false
	for _, c := range rev.Changes {
		stored = stored || c.Scope == harnessconfig.ScopeStored
	}
	if !stored {
		t.Fatalf("the review must show the stored effect: %+v", rev.Changes)
	}
	if _, st, _ := r.asm.Store.GetProfile(context.Background(), r.wsID); st != beforeSt {
		t.Fatal("Prepare must write nothing")
	}

	out, err := r.port.Apply(r.ui(), rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != harnessconfig.OutcomeApplied || out.Commit != harnessconfig.CommitCommitted || out.Target != "workspace" || out.ProfileRevision != beforeSt.Revision+1 || !out.ReloadRequired {
		t.Fatalf("expected a committed workspace change: %+v", out)
	}
	if k := kinds(r.prompter); len(k) != 1 || k[0] != interaction.KindHarness {
		t.Fatalf("a person-triggered change still asks once through the shared hub: %v", k)
	}

	after, _, _ := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if !after.ToolGroups["vault"] {
		t.Fatalf("vault should now be on: %+v", after.ToolGroups)
	}
	if _, ok := after.ToolGroups["db"]; ok {
		t.Fatalf("inherit removes the entry: %+v", after.ToolGroups)
	}
	// Everything the editor did not touch is the same in meaning.
	after.ToolGroups, before.ToolGroups = nil, nil
	after.Revision, before.Revision = 0, 0
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if string(a) != string(b) {
		t.Fatalf("an unedited field changed:\n before %s\n after  %s\n (was %s)", b, a, beforeJSON)
	}
}

func TestAStaleWorkspaceEditIsRefusedAndWritesNothing(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	rev, err := r.port.PrepareWorkspaceGroups(r.ui(), r.wsID, []harnessconfig.GroupEdit{{Group: "vault", Setting: harnessconfig.SettingOn}})
	if err != nil {
		t.Fatal(err)
	}
	// Someone else edits W between the review and the approval.
	moved := r.workspaceOff("vault", "db")
	_, err = r.port.Apply(r.ui(), rev.ID)
	if !isCode(err, harness.CodeProfileConflict) && !isCode(err, harness.CodeProposalStale) {
		t.Fatalf("a stale review must be refused, got %v", err)
	}
	if _, st, _ := r.asm.Store.GetProfile(context.Background(), r.wsID); st != moved {
		t.Fatalf("a refused apply must not write: %+v vs %+v", st, moved)
	}
	// The proposal is spent: no blind retry with the same id.
	if _, err := r.port.Apply(r.ui(), rev.ID); !isCode(err, harness.CodeProposalStale) {
		t.Fatalf("a spent proposal is not pending: %v", err)
	}
}

func TestCancellingAReviewDropsItAndSpendsTheID(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("vault")
	rev, err := r.port.PrepareWorkspaceGroups(r.ui(), r.wsID, []harnessconfig.GroupEdit{{Group: "vault", Setting: harnessconfig.SettingOn}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.port.Cancel(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	if r.asm.Service.Pending() != 0 {
		t.Fatal("cancel must drop the preparation")
	}
	if _, err := r.port.Apply(r.ui(), rev.ID); !isCode(err, harness.CodeProposalStale) {
		t.Fatalf("a cancelled proposal cannot be applied: %v", err)
	}
	if len(kinds(r.prompter)) != 0 {
		t.Fatal("nobody should have been asked")
	}
}

func TestEditsAreBoundedAndNamedAgainstTheRegistry(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	for name, edits := range map[string][]harnessconfig.GroupEdit{
		"none":      nil,
		"unknown":   {{Group: "nonesuch", Setting: harnessconfig.SettingOn}},
		"duplicate": {{Group: "vault", Setting: harnessconfig.SettingOn}, {Group: "vault", Setting: harnessconfig.SettingOff}},
		"bad value": {{Group: "vault", Setting: "maybe"}},
	} {
		if _, err := r.port.PrepareWorkspaceGroups(r.ui(), r.wsID, edits); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	if _, err := r.port.PrepareWorkspaceGroups(r.ui(), "no-such-workspace", []harnessconfig.GroupEdit{{Group: "vault", Setting: harnessconfig.SettingOn}}); !isCode(err, harness.CodeProfileDenied) {
		t.Errorf("an unknown workspace is gone: %v", err)
	}
	if r.asm.Service.Pending() != 0 {
		t.Fatal("a refused edit must not leave a preparation")
	}
}

func TestRevokingASessionGroupGoesThroughReviewAndApproval(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})).Outcome; out == nil || !out.Live {
		t.Fatalf("setup: the group should be live: %+v", out)
	}
	view, err := r.port.SessionProfile(r.ui(), r.wsID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Session == nil || len(view.Session.Overrides) != 1 || !rowOf(view, "vault").Session {
		t.Fatalf("the session card should list its active override: %+v", view.Session)
	}

	r.prompter.mu.Lock()
	r.prompter.prompts = nil
	r.prompter.mu.Unlock()
	rev, err := r.port.PrepareRevoke(r.ui(), r.wsID, sess.ID, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if !rev.NeedsConfirmation {
		t.Fatal("a person-triggered revoke is not exempt from review")
	}
	running := false
	for _, c := range rev.Changes {
		running = running || c.Scope == harnessconfig.ScopeRunning
	}
	if !running {
		t.Fatalf("the review should show the running effect separately: %+v", rev.Changes)
	}
	out, err := r.port.Apply(r.ui(), rev.ID)
	if err != nil || out.Status != harnessconfig.OutcomeApplied || !out.Live || out.Target != "session" {
		t.Fatalf("revoke: %+v %v", out, err)
	}
	if hasGroup(r.listed(cs), "vault") {
		t.Fatal("after the revoke the endpoint no longer serves the group")
	}
	// The next call is refused by the endpoint itself, not merely hidden.
	if res := r.callRaw(cs, "vault.status", map[string]any{}); !res.IsError {
		t.Fatalf("a direct call after the revoke must be refused: %+v", res)
	}
	after, _ := r.port.SessionProfile(r.ui(), r.wsID, sess.ID)
	if rowOf(after, "vault").Running != harnessconfig.AvailableOff || len(after.Session.Overrides) != 0 {
		t.Fatalf("running state should follow: %+v", rowOf(after, "vault"))
	}
}

func TestASessionOfAnotherWorkspaceCannotBeNamedFromThisOne(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, _ := r.create(agenthub.AgentTypeClaude)
	other, _, err := r.ws.AddWorkspace("two", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.port.SessionProfile(r.ui(), other.ID, sess.ID); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("the window must name the session's own workspace: %v", err)
	}
	if _, err := r.port.PrepareRevoke(r.ui(), other.ID, sess.ID, []string{"vault"}); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("revoke through a foreign workspace: %v", err)
	}
	if _, err := r.port.Reconcile(r.ui(), other.ID, sess.ID); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("reconcile through a foreign workspace: %v", err)
	}
}

func TestAnUnsupportedVendorSessionIsRefusedWithAFixedError(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, _ := r.create(agenthub.AgentTypeClaude)
	// A session whose vendor the harness does not cover (a mock type): the row says
	// so, and neither the agent nor the window gets a projection for it.
	h := &harnessControls{dir: r.asm.controls.dir, vendors: harnessVendors{prefs: r.prefs, supported: []agenthub.AgentType{agenthub.AgentTypeCodex}}}
	if _, err := h.uiSession(r.ui(), r.wsID, sess.ID); !isCode(err, harness.CodeNotAvailable) {
		t.Fatalf("an uncovered vendor is not available: %v", err)
	}
}
