package app

import (
	"context"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
)

// R1b probe (written by the independent reviewer): the budget setters and reads
// are the window's. TestOnlyTheAgentItselfCanAskAndOnlyTheWindowCanEdit covers the
// group setters, Apply, Cancel and Reconcile for an agent and two of them for a
// terminal, but nothing exercised the two budget methods with a non-UI caller, nor
// the group/revoke setters with a terminal or an unidentified context.
func TestNoBudgetOrScopeSetterAnswersACallerThatIsNotTheWindow(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, _ := r.create(agenthub.AgentTypeClaude)
	callers := map[string]context.Context{
		"agent":    r.agentCtx(sess.ID),
		"terminal": identity.WithCaller(r.ctx, identity.UserTerminal()),
		"remote":   identity.WithCaller(r.ctx, identity.Caller{Kind: identity.KindRemoteHuman}),
		"none":     r.ctx,
	}
	edit := []harnessconfig.BudgetEdit{{Kind: harnessconfig.BudgetTool, Name: "vault.status", Bytes: kibp(8)}}
	_, before := r.storedProfile()
	for name, ctx := range callers {
		calls := map[string]func() error{
			"workspace budgets": func() error {
				_, err := r.port.WorkspaceBudgets(ctx, r.wsID, harnessconfig.BudgetFilter{Limit: 5})
				return err
			},
			"prepare budgets": func() error { _, err := r.port.PrepareBudgetEdits(ctx, r.wsID, edit); return err },
			"prepare groups": func() error {
				_, err := r.port.PrepareWorkspaceGroups(ctx, r.wsID, []harnessconfig.GroupEdit{{Group: "vault", Setting: harnessconfig.SettingOff}})
				return err
			},
			"prepare revoke": func() error { _, err := r.port.PrepareRevoke(ctx, r.wsID, sess.ID, []string{"vault"}); return err },
			"apply":          func() error { _, err := r.port.Apply(ctx, "x"); return err },
			"cancel":         func() error { return r.port.Cancel(ctx, "x") },
			"reconcile":      func() error { _, err := r.port.Reconcile(ctx, r.wsID, sess.ID); return err },
		}
		for what, call := range calls {
			if err := call(); !isCode(err, harness.CodeProfileDenied) {
				t.Errorf("%s / %s: only the window may do this, got %v", name, what, err)
			}
		}
	}
	if _, now := r.storedProfile(); now != before {
		t.Fatal("a refused caller wrote the workspace profile")
	}
}
