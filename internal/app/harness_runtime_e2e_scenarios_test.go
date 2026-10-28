package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// setBudget stores a saved-workspace result budget for one tool (head cut), the
// state a later session launch pins.
func (c *e2eCase) setBudget(tool string, bytes *int) {
	c.t.Helper()
	r := c.r
	w, st, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		c.t.Fatal(err)
	}
	next := harness.Profile{Schema: harness.SchemaVersion}
	if w != nil {
		next = *w
	}
	if bytes == nil {
		next.ResultBudget = nil
	} else {
		next.ResultBudget = &harness.BudgetProfile{Tools: map[string]harness.BudgetRule{tool: {Bytes: bytes, Cut: harness.CutHead}}}
	}
	if _, err := r.asm.Store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: r.wsID,
		Profile: &harness.ProfileWrite{Expect: st, Value: &next}}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *e2eCase) clearBudget() { c.setBudget("", nil) }

func (c *e2eCase) faultPending() { c.fault.mode.Store("pending") }

func (c *e2eCase) faultClear() {
	c.fault.failReads.Store(false)
	c.fault.mode.Store("")
}

func runR2(t *testing.T, name string) {
	c := newE2ECase(t, name)
	r := c.r
	r.prompter.answers = answers("session")

	// No prompt is sent before the jail is shown to have no way out.
	proof := e2eProveNoNetwork(t, c.v.runner)
	c.row("proof-no-network", map[string]any{"probe": proof})

	vaultStatus := e2eStep{Tool: "vault_status"}
	// Valid arguments: a runtime that validates input (opencode) would otherwise refuse before any call is sent.
	probeWrite := e2eStep{Tool: "workspace_memory_create", Args: map[string]any{"kind": "knowledge", "title": "r2 probe", "summary": "r2 probe", "body": "r2 probe"}}

	// Row 1a: default (no workspace profile) launch.
	s1 := c.create()
	t1 := c.turn(s1, "done", vaultStatus)
	c.row("1a-default-launch", c.summary(t1))
	if t1.Status != "completed" {
		t.Fatalf("default launch turn = %q (%s)", t1.Status, t1.Err)
	}
	// A vendor that surfaces none of DevKit's tools would let every later row "pass" while proving
	// nothing (opencode 2.x did until the fixture learned its Code Mode). Fail instead.
	if offeredDevkit(t1.offered()) == 0 {
		t.Fatalf("%s offered no DevKit tools on a default launch (offered %d tool names): the rows below would prove nothing", name, len(t1.offered()))
	}
	c.row("launches", map[string]any{"launches": c.v.runner.Launches()})

	// Row 1b: a narrowed workspace profile, a fresh session.
	r.workspaceOff("vault", "workspace")
	s2 := c.create()
	t2 := c.turn(s2, "done", vaultStatus)
	c.row("1b-narrowed-discovery", c.summary(t2))

	// Rows 3 and 6: the actual wire delegation of propose; then what the runtime did about the change.
	t3 := c.turn(s2, "done", e2eStep{Tool: "harness_profile_propose", Args: map[string]any{"groups": []string{"vault", "workspace"}, "rationale": "r2 evidence"}})
	c.row("3-propose-wire", c.summary(t3))
	t4 := c.turn(s2, "done", vaultStatus)
	c.row("6a-no-reload-discovery", mergeFacts(c.summary(t4), map[string]any{
		"serverListChangedNotifications": t3.tapMethods("s2c")["notifications/tools/list_changed"] + t4.tapMethods("s2c")["notifications/tools/list_changed"],
		"clientToolsListAfterEnable":     t3.tapMethods("c2s")["tools/list"] + t4.tapMethods("c2s")["tools/list"],
		"vaultOfferedAfterEnable":        len(devkitOffered(t4.offered(), "vault")),
	}))

	// Explicit Reload (the contract's fallback): the S just saved is served from launch.
	if err := r.agents.ResumeSession(r.ui(), s2.ID, true); err != nil {
		t.Fatalf("reload: %v", err)
	}
	t5 := c.turn(s2, "done", vaultStatus)
	c.row("6b-after-explicit-reload", c.summary(t5))

	// Row 3b: a tool enabled by S, outside the fixed launch pre-approval lists, goes through the
	// vendor's own permission flow. The answer is a refusal, so nothing runs whatever the server would do.
	// Both asks are refused: the vendor's own (Codex) and DevKit's gate after dispatch (opencode has no vendor ask).
	c.perms.setDeny(true)
	c.gates.setDeny(true)
	t6 := c.turn(s2, "done", probeWrite)
	c.perms.setDeny(false)
	c.gates.setDeny(false)
	callsW, _ := t6.tapCalls("workspace.memory.create")
	c.row("3b-newly-enabled-write-tool", mergeFacts(c.summary(t6), map[string]any{"serverSawWriteCalls": callsW, "memoryCreateHandlerRan": r.memCreates.Load()}))

	// Rows 2 and 4a: a hidden tool the vendor still holds. s3 is served vault from its launch;
	// the vendor uses it; a committed revoke follows; the vendor is asked to call it again.
	s3 := c.create()
	if _, err := r.apply(s3.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.agents.ResumeSession(r.ui(), s3.ID, true); err != nil {
		t.Fatalf("reload s3: %v", err)
	}
	u1 := c.turn(s3, "done", vaultStatus)
	c.row("4a-served-before-revoke", c.summary(u1))
	if _, err := r.apply(s3.ID, nil, []string{"vault"}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	u2 := c.turn(s3, "done", vaultStatus)
	calls, errs := u2.tapCalls("vault.status")
	c.row("2-4b-revoke-then-cached-call", mergeFacts(c.summary(u2), map[string]any{"serverSawVaultCalls": calls, "serverRefusals": errs}))

	// Row 4c: the call-start snapshot. A call is held inside its handler while the group is revoked.
	if _, err := r.apply(s3.ID, []string{"vault"}, nil); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	// The held call needs a runtime that has the tool again. Read what it offers first and, if it has
	// not refreshed yet, use the explicit Reload, so a runtime that is merely slow is not read as a result.
	pre := c.turn(s3, "done", vaultStatus)
	reloaded := len(devkitOffered(pre.offered(), "vault")) == 0
	if reloaded {
		if err := r.agents.ResumeSession(r.ui(), s3.ID, true); err != nil {
			t.Fatalf("reload s3 before the held call: %v", err)
		}
	}
	c.row("4c-pre-discovery", mergeFacts(c.summary(pre), map[string]any{"reloadedBeforeHeldCall": reloaded}))
	entered, release := r.gate.holdNext()
	c.v.model.script("done", vaultStatus)
	a0, t0, p0, k0, h0, g0 := c.snapshot()
	type invoked struct {
		res agenthub.InvokeResult
		err error
	}
	done := make(chan invoked, 1)
	go func() {
		ctx, cancel := context.WithTimeout(r.ui(), 3*time.Minute)
		defer cancel()
		res, err := r.agents.Invoke(ctx, agenthub.InvokeInput{SessionID: s3.ID, Prompt: "r2 fixture turn"})
		done <- invoked{res, err}
	}()
	var midRevoke map[string]any
	select {
	case <-entered:
		_, err := r.apply(s3.ID, nil, []string{"vault"})
		midRevoke = map[string]any{"revokeDuringHeldCallErr": fmt.Sprint(err)}
	case <-time.After(60 * time.Second):
		midRevoke = map[string]any{"heldCallNeverReachedHandler": true}
	}
	release()
	inv := <-done
	time.Sleep(300 * time.Millisecond)
	u3 := c.delta(a0, t0, p0, k0, h0, g0, inv.res.Status, inv.err)
	c.row("4c-call-start-snapshot", mergeFacts(c.summary(u3), midRevoke))

	// Row 4d: result budgets observed from the server. A saved W caps harness.profile.get at its minimum.
	wb := 2 * 1024
	c.setBudget("harness.profile.get", &wb)
	s4 := c.create()
	b1 := c.turn(s4, "done", e2eStep{Tool: "harness_profile_get", Args: map[string]any{}})
	c.row("4d-result-budget", mergeFacts(c.summary(b1), map[string]any{"configuredBytes": wb}))
	c.clearBudget()
	b0 := c.turn(s1, "done", e2eStep{Tool: "harness_profile_get", Args: map[string]any{}})
	c.row("4d-result-budget-default-control", c.summary(b0))

	// Row 5a: restart (the host and Agent Hub are rebuilt over the same database), then the durable S.
	r.host.Close()
	if err := r.agents.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.v.runner.KillAll() // the app exit: no vendor process survives it
	r.restart()
	if err := r.agents.ResumeSession(r.ui(), s2.ID, false); err != nil {
		t.Fatalf("resume after restart: %v", err)
	}
	v1 := c.turn(s2, "done", vaultStatus)
	o, st := r.override(s2.ID)
	facts := map[string]any{"overrideStored": o != nil}
	if o != nil {
		facts["overrideGroups"], facts["overrideRevision"] = o.Groups, st.Revision
	}
	c.row("5a-restart-durable-S", mergeFacts(c.summary(v1), facts))

	// Row 5b: a committed change whose publication fails, through the real adapter path.
	r.workspaceOff("vault")
	s5 := c.create()
	c.faultPending()
	f1 := c.turn(s5, "done", e2eStep{Tool: "harness_profile_propose", Args: map[string]any{"groups": []string{"vault"}, "rationale": "r2 failed publication"}})
	c.row("5b-failed-publication", c.summary(f1))
	c.faultClear()
	f2 := c.turn(s5, "done", vaultStatus)
	c.row("5c-paused-endpoint-refuses", c.summary(f2))
	rec, err := r.port.Reconcile(r.ui(), r.wsID, s5.ID)
	c.row("5d-reconcile", map[string]any{"status": rec.Status, "live": rec.Live, "publication": rec.Publication, "err": fmt.Sprint(err)})
	f3 := c.turn(s5, "done", vaultStatus)
	c.row("5e-after-reconcile", c.summary(f3))
	// The reconcile reports the endpoint live; whether the runtime has the group yet is a separate
	// observation, so the explicit Reload is read after it instead of being assumed.
	if err := r.agents.ResumeSession(r.ui(), s5.ID, true); err != nil {
		t.Fatalf("reload after reconcile: %v", err)
	}
	f4 := c.turn(s5, "done", vaultStatus)
	c.row("5f-after-reconcile-and-reload", c.summary(f4))
}
