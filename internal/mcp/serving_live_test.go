package mcp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Live installation, driven against real SessionHost sockets and the real SDK
// client. The vendor is never involved.

func (r *servingRig) openLive(sid string, eff harness.Effective) agenthub.MCPEndpoint {
	r.t.Helper()
	ep, err := r.host.OpenWithPolicy(sid, eff)
	if err != nil {
		r.t.Fatal(err)
	}
	if ep.Live == nil {
		r.t.Fatal("a policy endpoint must carry its publication handle")
	}
	return ep
}

// swap does what the coordinator does for one change: quarantine, install,
// and nothing else.
func swap(t *testing.T, ep agenthub.MCPEndpoint, eff harness.Effective) (agenthub.InstallReport, error) {
	t.Helper()
	tk, err := ep.Live.Quarantine()
	if err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	return ep.Live.Install(tk, eff, harness.ServingIdentity{Fingerprint: eff.Fingerprint})
}

func vaultNames(inv Inventory) []string {
	return namesOf(inv, func(tl InventoryTool) bool { return tl.Group == "vault" })
}

func (r *servingRig) callVault(cs *sdk.ClientSession) *sdk.CallToolResult {
	r.t.Helper()
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
	if err != nil {
		r.t.Fatalf("call: %v", err)
	}
	return res
}

func TestInstallBringsBackAHiddenGroupOnThatEndpointOnly(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	a := r.openLive(sidA, r.resolve(groupOff("vault", 1)))
	b := r.openLive(sidB, r.resolve(groupOff("vault", 1)))
	ca, cb := r.client(a.SocketPath), r.client(b.SocketPath)
	if got := listNames(t, ca); sameSet(got, namesOf(inv, func(InventoryTool) bool { return true })) {
		t.Fatal("vault should start hidden")
	}
	rep, err := swap(t, a, r.resolve(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ToolsChanged || !rep.NotificationScheduled {
		t.Fatalf("report = %+v", rep)
	}
	all := namesOf(inv, func(InventoryTool) bool { return true })
	if got := listNames(t, ca); !sameSet(got, all) {
		t.Fatalf("A lists %d tools, want all %d", len(got), len(all))
	}
	// The re-registered handler really works: it goes through the gateway.
	before := r.caller.calls.Load()
	if res := r.callVault(ca); strings.HasPrefix(resultText(res), harness.CodeGroupOff) {
		t.Fatalf("vault should be served after install: %s", resultText(res))
	}
	if r.caller.calls.Load() != before+1 {
		t.Fatal("an enabled tool must reach the gateway")
	}
	// B, a different endpoint of a different session, is untouched.
	if got := listNames(t, cb); len(got) != len(all)-len(vaultNames(inv)) {
		t.Fatalf("B lists %d tools; a publication must not reach another endpoint", len(got))
	}
	if res := r.callVault(cb); !strings.HasPrefix(resultText(res), harness.CodeGroupOff) {
		t.Fatalf("B must still refuse vault: %s", resultText(res))
	}
}

func TestInstallHidesAGroupAndRefusesACachedCall(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	ep := r.openLive(sidA, r.resolve(nil))
	cs := r.client(ep.SocketPath)
	if _, err := swap(t, ep, r.resolve(groupOff("vault", 2))); err != nil {
		t.Fatal(err)
	}
	for _, n := range listNames(t, cs) {
		if harness.Group(n) == "vault" {
			t.Fatalf("%s is still listed", n)
		}
	}
	before := r.caller.calls.Load()
	if res := r.callVault(cs); !strings.HasPrefix(resultText(res), harness.CodeGroupOff) {
		t.Fatalf("a client that cached the tool is refused: %q", resultText(res))
	}
	if r.caller.calls.Load() != before {
		t.Fatal("a refused call must not reach the gateway")
	}
	_ = inv
}

func TestQuarantineRefusesNewWorkButNeverWaitsForARunningCall(t *testing.T) {
	r := newServingRig(t, nil)
	ep := r.openLive(sidA, r.resolve(nil))
	cs := r.client(ep.SocketPath)
	other := r.client(ep.SocketPath)
	entered, release := r.caller.hold()
	defer release()
	done := make(chan *sdk.CallToolResult, 1)
	go func() {
		res, _ := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
		done <- res
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never reached the gateway")
	}
	// A running handler must not stall the quarantine: a proposal can itself be
	// the running call.
	tk := make(chan agenthub.EndpointTicket, 1)
	go func() { t1, _ := ep.Live.Quarantine(); tk <- t1 }()
	var ticket agenthub.EndpointTicket
	select {
	case ticket = <-tk:
	case <-time.After(5 * time.Second):
		t.Fatal("Quarantine waited for a running handler")
	}
	res, err := other.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.HasPrefix(resultText(res), harness.CodeSnapshotUnavailable) {
		t.Fatalf("a call during quarantine = %v %v", res, err)
	}
	if _, err := other.ListTools(r.ctx, nil); err == nil {
		t.Fatal("tools/list must not be answered from a registry being rewritten")
	}
	if st := ep.Live.State(); !st.Paused || st.Closed {
		t.Fatalf("state = %+v", st)
	}
	// The call admitted before the quarantine finishes normally.
	release()
	select {
	case res := <-done:
		if res == nil || res.IsError {
			t.Fatalf("the running call must finish: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the running call never finished")
	}
	if err := ep.Live.Restore(ticket); err != nil {
		t.Fatal(err)
	}
	if res, err := other.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil || res.IsError {
		t.Fatalf("restored admission = %v %v", res, err)
	}
}

func TestOnlyTheCurrentOperationMayRestoreOrInstall(t *testing.T) {
	r := newServingRig(t, nil)
	ep := r.openLive(sidA, r.resolve(nil))
	old, err := ep.Live.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	newer, err := ep.Live.Quarantine() // a reconciliation takes over
	if err != nil {
		t.Fatal(err)
	}
	if err := ep.Live.Restore(old); err == nil {
		t.Fatal("a superseded operation cannot reopen admission")
	}
	if _, err := ep.Live.Install(old, r.resolve(groupOff("vault", 3)), harness.ServingIdentity{}); err == nil {
		t.Fatal("a superseded operation cannot install")
	}
	if !ep.Live.State().Paused {
		t.Fatal("the endpoint must still be paused")
	}
	if _, err := ep.Live.Install(newer, r.resolve(groupOff("vault", 3)), harness.ServingIdentity{}); err != nil {
		t.Fatal(err)
	}
	if ep.Live.State().Paused {
		t.Fatal("the current operation reopens admission")
	}
	// Spent: the same ticket cannot install twice.
	if _, err := ep.Live.Install(newer, r.resolve(nil), harness.ServingIdentity{}); err == nil {
		t.Fatal("a ticket is single use")
	}
}

func TestAFailedInstallLeavesTheEndpointClosedAndTheNextOneRepairsIt(t *testing.T) {
	for _, stage := range []string{"begin", "registered", "hidden", "policy"} {
		t.Run(stage, func(t *testing.T) {
			r := newServingRig(t, nil)
			inv := r.inventory()
			ep := r.openLive(sidA, r.resolve(groupOff("vault", 1))) // vault hidden, the rest served
			cs := r.client(ep.SocketPath)
			// Enable vault and hide env in one change, failing midway.
			next := r.resolve(groupOff("env", 2))
			boom := errors.New("injected")
			r.host.installHook = func(s string) error {
				if s == stage {
					return boom
				}
				return nil
			}
			if _, err := swap(t, ep, next); !errors.Is(err, boom) {
				t.Fatalf("install = %v", err)
			}
			r.host.installHook = nil
			if !ep.Live.State().Paused {
				t.Fatal("a failed install must leave admission closed")
			}
			if _, err := cs.ListTools(r.ctx, nil); err == nil {
				t.Fatal("a half-updated registry must not be listed")
			}
			if res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil || !res.IsError {
				t.Fatalf("calls are refused while paused: %v %v", res, err)
			}
			// Reconciliation: a new operation installs the latest policy exactly.
			if _, err := swap(t, ep, next); err != nil {
				t.Fatal(err)
			}
			want := namesOf(inv, func(tl InventoryTool) bool { return tl.Group != "env" })
			if got := listNames(t, cs); !sameSet(got, want) {
				t.Fatalf("after repair the endpoint lists %d tools, want %d", len(got), len(want))
			}
		})
	}
}

func TestAnOldIncarnationCannotPublishToItsReplacement(t *testing.T) {
	r := newServingRig(t, nil)
	old := r.openLive(sidA, r.resolve(groupOff("vault", 1)))
	oldTicket, err := old.Live.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	repl := r.openLive(sidA, r.resolve(groupOff("vault", 1)))
	if old.Live.Incarnation() == repl.Live.Incarnation() {
		t.Fatal("a replacement is a new incarnation")
	}
	if _, err := old.Live.Install(oldTicket, r.resolve(nil), harness.ServingIdentity{}); err == nil {
		t.Fatal("a closed incarnation cannot install")
	}
	if _, err := old.Live.Quarantine(); err == nil {
		t.Fatal("a closed incarnation cannot be quarantined")
	}
	cs := r.client(repl.SocketPath)
	for _, n := range listNames(t, cs) {
		if harness.Group(n) == "vault" {
			t.Fatal("the replacement must keep its own policy")
		}
	}
	if repl.Live.State().Paused {
		t.Fatal("the replacement was never quarantined")
	}
}

func TestACursorIssuedBeforeARevokeNeverRevealsAHiddenTool(t *testing.T) {
	r := newServingRig(t, func(s *Server) { s.pageSize = 7 })
	ep := r.openLive(sidA, r.resolve(nil))
	cs := r.client(ep.SocketPath)
	first, err := cs.ListTools(r.ctx, nil)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page: %v %q", err, first.NextCursor)
	}
	if _, err := swap(t, ep, r.resolve(groupOff("vault", 2))); err != nil {
		t.Fatal(err)
	}
	cursor := first.NextCursor
	for cursor != "" {
		res, err := cs.ListTools(r.ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if harness.Group(tool.Name) == "vault" {
				t.Fatalf("a stale cursor revealed %s", tool.Name)
			}
		}
		cursor = res.NextCursor
	}
}

func TestAnInitiallyEmptyEndpointAdvertisesListChangeAndLaterEnableIsDiscoverable(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	// Every group off: nothing is registered on the instance at open.
	empty := r.resolve(func(in *harness.Inputs) {
		off := map[string]bool{}
		for _, tl := range inv.Tools {
			off[tl.Group] = false
		}
		in.Workspace = &harness.Profile{Schema: 1, Revision: 1, ToolGroups: off}
	})
	ep := r.openLive(sidA, empty)
	conn, err := net.Dial("unix", ep.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	var changed atomic.Int32
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, &sdk.ClientOptions{
		ToolListChangedHandler: func(_ context.Context, _ *sdk.ToolListChangedRequest) { changed.Add(1) },
	})
	cs, err := client.Connect(r.ctx, newRWTransport(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	caps := cs.InitializeResult().Capabilities
	if caps == nil || caps.Tools == nil || !caps.Tools.ListChanged {
		t.Fatalf("an empty endpoint must still advertise tool list changes: %+v", caps)
	}
	if got := listNames(t, cs); len(got) != 0 {
		t.Fatalf("lists %v", got)
	}
	if _, err := swap(t, ep, r.resolve(nil)); err != nil {
		t.Fatal(err)
	}
	if got := listNames(t, cs); len(got) != len(inv.Tools) {
		t.Fatalf("after enable lists %d, want %d", len(got), len(inv.Tools))
	}
	// The notification is the SDK's debounced attempt; it is observed here only to
	// show that discovery is wired, never as an acknowledgement.
	deadline := time.After(5 * time.Second)
	for changed.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("no list-changed notification arrived")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestInstallRefusesAPolicyFromAnotherRegistry(t *testing.T) {
	r := newServingRig(t, nil)
	ep := r.openLive(sidA, r.resolve(nil))
	other, err := harness.Resolve(harness.Inputs{Registry: harness.Registry{Tools: []harness.Tool{{Name: "x.y", DefinitionBytes: 10}}}})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := ep.Live.Quarantine()
	if _, err := ep.Live.Install(tk, other, harness.ServingIdentity{}); err == nil {
		t.Fatal("a policy that does not cover the registry cannot be installed")
	}
	if !ep.Live.State().Paused {
		t.Fatal("a refused install leaves the endpoint paused for its reconciliation")
	}
}

func TestStateReportsTheInstalledPolicyFingerprint(t *testing.T) {
	r := newServingRig(t, nil)
	first := r.resolve(nil)
	ep := r.openLive(sidA, first)
	if st := ep.Live.State(); st.Fingerprint != first.Fingerprint || st.Paused || st.Closed || st.Incarnation == "" {
		t.Fatalf("state = %+v", st)
	}
	next := r.resolve(groupOff("vault", 5))
	if _, err := swap(t, ep, next); err != nil {
		t.Fatal(err)
	}
	if st := ep.Live.State(); st.Fingerprint != next.Fingerprint {
		t.Fatalf("state = %+v", st)
	}
	ep.Close()
	if st := ep.Live.State(); !st.Closed {
		t.Fatalf("state = %+v", st)
	}
}
