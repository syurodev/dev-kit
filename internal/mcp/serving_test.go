package mcp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/capabilities"
	"devkit/internal/harness"
	"devkit/internal/usagemetrics"
	"devkit/internal/vault"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// These tests drive the real SDK client against real SessionHost sockets (and
// in-memory transports where a socket adds nothing). The vendor is never
// involved and nothing reads a database: the server is built from stubs.

// probeCaller counts what reaches the capability gateway, can hold one call,
// and is safe under -race.
type probeCaller struct {
	calls   atomic.Int32
	mu      sync.Mutex
	block   chan struct{}
	entered chan struct{}
}

func (p *probeCaller) Call(ctx context.Context, _, _, _ string, _ map[string]any, handler capabilities.Handler) (any, error) {
	p.calls.Add(1)
	p.mu.Lock()
	block, entered := p.block, p.entered
	p.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
	return handler(ctx)
}

func (p *probeCaller) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return p.Call(ctx, r.SubjectID, r.Capability, r.Scope, r.Metadata, r.Handler)
}

func (p *probeCaller) PublishError(_ context.Context, _ string, err error) error { return err }

// hold makes the next gateway call wait until release is called.
func (p *probeCaller) hold() (entered <-chan struct{}, release func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.block, p.entered = make(chan struct{}), make(chan struct{}, 1)
	block := p.block
	var once sync.Once
	return p.entered, func() { once.Do(func() { close(block) }) }
}

type servingRig struct {
	t       *testing.T
	srv     *Server
	host    *SessionHost
	caller  *probeCaller
	ctx     context.Context
	dir     string
	mu      sync.Mutex
	records []usagemetrics.Observation
	scope   usagemetrics.Scope
}

const sidA = "aaaaaaaa-2222-3333-4444-555555555555"
const sidB = "bbbbbbbb-2222-3333-4444-555555555555"

func newServingRig(t *testing.T, mutate func(*Server)) *servingRig {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "h4a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r := &servingRig{t: t, caller: &probeCaller{}, dir: dir, scope: usagemetrics.Scope{Vendor: "claude", WorkspaceID: "w1"}}
	deps := fullDeps()
	deps.Caller = r.caller
	deps.Vault = &fakeVault{state: vault.StateUnlocked}
	deps.UsageScope = func(sessionID, token string) (usagemetrics.Scope, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.scope, token != ""
	}
	deps.RecordUsage = func(o usagemetrics.Observation) { r.mu.Lock(); r.records = append(r.records, o); r.mu.Unlock() }
	deps.BeginUsage = func() bool { return true }
	deps.EndUsage = func() {}
	deps.DropUsage = func() {}
	r.srv = NewServer(deps)
	if mutate != nil {
		mutate(r.srv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.ctx = ctx
	r.host = NewSessionHost(ctx, r.srv, dir, "/usr/bin/true")
	t.Cleanup(r.host.Close)
	return r
}

func (r *servingRig) inventory() Inventory {
	r.t.Helper()
	inv, err := r.srv.Inventory()
	if err != nil {
		r.t.Fatal(err)
	}
	return inv
}

// resolve resolves a policy against the server's own inventory.
func (r *servingRig) resolve(mutate func(*harness.Inputs)) harness.Effective {
	r.t.Helper()
	in := harness.Inputs{Registry: r.inventory().Registry(), Vendor: harness.Vendor{Revision: 1}}
	if mutate != nil {
		mutate(&in)
	}
	eff, err := harness.Resolve(in)
	if err != nil {
		r.t.Fatalf("Resolve: %v", err)
	}
	return eff
}

func groupOff(group string, rev int64) func(*harness.Inputs) {
	return func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: rev, ToolGroups: map[string]bool{group: false}}
	}
}

func (r *servingRig) client(path string) *sdk.ClientSession {
	r.t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		r.t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(r.ctx, newRWTransport(conn), nil)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { cs.Close() })
	return cs
}

func listNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names = append(names, tool.Name)
	}
	return names
}

func resultText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func namesOf(inv Inventory, keep func(InventoryTool) bool) []string {
	var out []string
	for _, t := range inv.Tools {
		if keep(t) {
			out = append(out, t.Name)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

// ---- inventory -----------------------------------------------------------

func TestInventoryIsWhatAClientSees(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	listed := listAllTools(t)
	if len(inv.Tools) != len(listed) || len(inv.Tools) < 50 {
		t.Fatalf("inventory has %d tools, a client lists %d", len(inv.Tools), len(listed))
	}
	for i, tool := range listed {
		got := inv.Tools[i]
		raw, _ := json.Marshal(tool)
		if got.Name != tool.Name || got.Group != harness.Group(tool.Name) || got.DefinitionBytes != len(raw) {
			t.Errorf("inventory[%d] = %+v, tool %s is %d bytes", i, got, tool.Name, len(raw))
		}
		reason, exempt := budgetExempt[tool.Name]
		if got.BudgetExempt != exempt || got.ExemptReason != reason {
			t.Errorf("%s exemption = %v %q, want %v %q", tool.Name, got.BudgetExempt, got.ExemptReason, exempt, reason)
		}
	}
	if !inv.Has("vault.status") || inv.Has("nope.nothing") {
		t.Error("Has does not follow the registry")
	}
}

func TestInventoryReflectsConditionalRegistration(t *testing.T) {
	full := newServingRig(t, nil).inventory()
	deps := fullDeps()
	deps.PDFDocuments = nil
	lean, err := NewServer(deps).Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(lean.Tools) >= len(full.Tools) {
		t.Fatalf("a server without PDF documents lists %d tools, the full one %d", len(lean.Tools), len(full.Tools))
	}
	for _, tl := range lean.Tools {
		if strings.HasPrefix(tl.Name, "pdf.") {
			t.Errorf("%s is in the inventory without the module", tl.Name)
		}
	}
}

// ---- default behaviour ---------------------------------------------------

func TestPolicyOfAnUnconfiguredSessionServesTheLegacyList(t *testing.T) {
	r := newServingRig(t, nil)
	legacy, err := r.host.Open(sidA)
	if err != nil {
		t.Fatal(err)
	}
	want := listNames(t, r.client(legacy.SocketPath))
	legacy.Close()

	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(nil))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	got := listNames(t, cs)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("all-on list differs from the legacy list:\n got %v\nwant %v", got, want)
	}
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("vault.status = %v %v", res, err)
	}
	if got, want := strings.Join(ep.PreApprovedTools, ","), strings.Join(r.srv.PreApprovedTools(), ","); got != want {
		t.Errorf("pre-approved %q, want today's %q", got, want)
	}
}

func TestTerminalServerIsUnfiltered(t *testing.T) {
	r := newServingRig(t, nil)
	if _, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 1))); err != nil {
		t.Fatal(err)
	}
	// A session's policy is its own: the shared terminal server still lists everything.
	if got := len(listAllTools(t)); got != len(r.inventory().Tools) {
		t.Fatalf("terminal server lists %d, inventory %d", got, len(r.inventory().Tools))
	}
}

// ---- filtering -----------------------------------------------------------

func TestEveryGroupOffHidesExactlyThatGroup(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	groups := map[string]bool{}
	for _, tl := range inv.Tools {
		groups[tl.Group] = true
	}
	if len(groups) < 10 {
		t.Fatalf("only %d groups", len(groups))
	}
	i := 0
	for group := range groups {
		i++
		sid := sidA
		if i%2 == 0 {
			sid = sidB // two sessions, two policies, never mixed up
		}
		ep, err := r.host.OpenWithPolicy(sid, r.resolve(groupOff(group, int64(i))))
		if err != nil {
			t.Fatalf("%s: %v", group, err)
		}
		cs := r.client(ep.SocketPath)
		got := listNames(t, cs)
		want := namesOf(inv, func(tl InventoryTool) bool { return tl.Group != group })
		if !sameSet(got, want) {
			t.Errorf("group %s off: lists %d tools, want %d", group, len(got), len(want))
		}
		ep.Close()
	}
}

func TestAHiddenToolIsRefusedBeforeItsHandlerEvenWhenAClientCachedIt(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 4)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	var hidden string
	for _, tl := range inv.Tools {
		if tl.Group == "git" {
			hidden = tl.Name
			break
		}
	}
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: hidden, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("a hidden call is a tool error, not a protocol failure: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(resultText(res), harness.CodeGroupOff+":") {
		t.Fatalf("hidden call = %v %q", res.IsError, resultText(res))
	}
	if n := r.caller.calls.Load(); n != 0 {
		t.Fatalf("the handler reached the gateway %d times for a hidden tool", n)
	}
	// An enabled tool still goes through the gateway as before.
	if res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil || res.IsError {
		t.Fatalf("vault.status = %v %v", res, err)
	}
	if r.caller.calls.Load() != 1 {
		t.Fatal("an enabled tool did not reach the gateway")
	}
}

func TestUnknownAndEmptyToolNamesKeepTheNormalAnswer(t *testing.T) {
	r := newServingRig(t, nil)
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 1)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	legacy, err := r.host.Open(sidB)
	if err != nil {
		t.Fatal(err)
	}
	cl := r.client(legacy.SocketPath)
	for _, name := range []string{"nothing.here", "", "git", "git."} {
		got, gotErr := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		want, wantErr := cl.CallTool(r.ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%q: policy endpoint err=%v, legacy err=%v", name, gotErr, wantErr)
			continue
		}
		if gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Errorf("%q: %q vs %q", name, gotErr, wantErr)
		}
		if got != nil && want != nil && resultText(got) != resultText(want) {
			t.Errorf("%q: %q vs %q", name, resultText(got), resultText(want))
		}
		if gotErr == nil && got != nil && strings.Contains(resultText(got), harness.CodeGroupOff) {
			t.Errorf("%q was answered as a hidden tool", name)
		}
	}
}

func TestPaginationNeverLeaksAHiddenTool(t *testing.T) {
	r := newServingRig(t, func(s *Server) { s.pageSize = 7 })
	inv := r.inventory()
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 2)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	want := namesOf(inv, func(tl InventoryTool) bool { return tl.Group != "git" })
	var got []string
	pages := 0
	cursor := ""
	for {
		res, err := cs.ListTools(r.ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if res.NextCursor != "" && len(res.Tools) != 7 {
			t.Fatalf("page %d has %d tools: a filtered page must still be full", pages, len(res.Tools))
		}
		for _, tool := range res.Tools {
			if harness.Group(tool.Name) == "git" {
				t.Fatalf("page %d leaks %s", pages, tool.Name)
			}
			got = append(got, tool.Name)
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if pages < 3 || !sameSet(got, want) || len(got) != len(want) {
		t.Fatalf("%d pages, %d tools, want %d", pages, len(got), len(want))
	}
}

func TestAPolicyFromAnotherRegistryIsRefusedBeforeAnythingListens(t *testing.T) {
	r := newServingRig(t, nil)
	eff, err := harness.Resolve(harness.Inputs{Registry: harness.Registry{Tools: []harness.Tool{{Name: "git.status", DefinitionBytes: 10}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.host.OpenWithPolicy(sidA, eff); err == nil {
		t.Fatal("a policy that does not know the registered tools was accepted")
	}
	entries, _ := os.ReadDir(r.dir)
	if len(entries) != 0 {
		t.Fatalf("a refused open left %d files", len(entries))
	}
}

// H1 reserves one exact tool name as served whatever its group does; its
// implementation belongs to H5. The rule must hold if it is registered, and
// nothing is invented while it is not.
func TestTheBootstrapToolSurvivesItsGroupBeingOffAndSiblingsDoNot(t *testing.T) {
	register := func(s *Server) {
		s.extraTools = func(ms *sdk.Server) {
			for _, name := range []string{harness.ProposeTool, "harness.profile.get"} {
				name := name
				sdk.AddTool(ms, &sdk.Tool{Name: name}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok " + name}}}, nil, nil
				})
			}
		}
	}
	r := newServingRig(t, register)
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("harness", 3)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	got := listNames(t, cs)
	has := func(n string) bool {
		for _, g := range got {
			if g == n {
				return true
			}
		}
		return false
	}
	if !has(harness.ProposeTool) || has("harness.profile.get") {
		t.Fatalf("with the harness group off the list has propose=%v get=%v", has(harness.ProposeTool), has("harness.profile.get"))
	}
	if res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: harness.ProposeTool, Arguments: map[string]any{}}); err != nil || res.IsError {
		t.Fatalf("propose = %v %v", res, err)
	}
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "harness.profile.get", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.HasPrefix(resultText(res), harness.CodeGroupOff) {
		t.Fatalf("sibling = %v %v", res, err)
	}
	// Not registered here: nothing is invented.
	plain := newServingRig(t, nil)
	if plain.inventory().Has(harness.ProposeTool) {
		t.Fatal("the propose tool is registered before H5")
	}
}

// ---- lifecycle -----------------------------------------------------------

func TestClosingAnEndpointEndsItsConnectionsAndPolicy(t *testing.T) {
	r := newServingRig(t, nil)
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 1)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	if _, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	ep.Close()
	deadline := time.After(5 * time.Second)
	for {
		_, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
		if err != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("an accepted connection kept dispatching after Close")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if r.caller.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d", r.caller.calls.Load())
	}
	if _, err := os.Stat(ep.SocketPath); !os.IsNotExist(err) {
		t.Fatalf("socket left behind: %v", err)
	}
}

func TestACloseOfTheOldEndpointCannotTouchItsReplacement(t *testing.T) {
	r := newServingRig(t, nil)
	old, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 1)))
	if err != nil {
		t.Fatal(err)
	}
	oldClient := r.client(old.SocketPath)
	old.Close()
	fresh, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("tasks", 2)))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.UsageToken == old.UsageToken || fresh.SocketPath != old.SocketPath {
		t.Fatalf("replacement should have a new token on the same path")
	}
	old.Close() // late and repeated
	old.Close()
	if _, err := os.Stat(fresh.SocketPath); err != nil {
		t.Fatalf("the replacement's socket was removed by the old Close: %v", err)
	}
	cs := r.client(fresh.SocketPath)
	names := listNames(t, cs)
	for _, n := range names {
		if harness.Group(n) == "tasks" {
			t.Fatalf("replacement serves %s", n)
		}
	}
	if len(names) == 0 {
		t.Fatal("replacement serves nothing")
	}
	// The old connection is dead; it does not adopt the replacement's policy or token.
	if _, err := oldClient.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err == nil {
		t.Fatal("the old connection still dispatches")
	}
	r.host.mu.Lock()
	n := len(r.host.endpoints)
	r.host.mu.Unlock()
	if n != 1 {
		t.Fatalf("host tracks %d endpoints, want the replacement only", n)
	}
	fresh.Close()
	fresh.Close()
}

// ---- one snapshot per call ----------------------------------------------

func TestACallKeepsTheSnapshotItEnteredWith(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	first := r.resolve(func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 7}
		in.Session = &harness.Override{Schema: 1, Revision: 11}
	})
	ep, err := r.host.OpenWithPolicy(sidA, first)
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	entered, release := r.caller.hold()
	done := make(chan *sdk.CallToolResult, 1)
	go func() {
		res, _ := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
		done <- res
	}()
	<-entered
	// The group is revoked and the revisions change while the call is inside its handler.
	r.host.mu.Lock()
	holder := r.host.endpoints[sidA].serving
	r.host.mu.Unlock()
	second := r.resolve(func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 7, ToolGroups: map[string]bool{"vault": false}}
		in.Session = &harness.Override{Schema: 1, Revision: 12}
	})
	if !holder.replace(second) {
		t.Fatal("replace refused on an open endpoint")
	}
	release()
	res := <-done
	if res == nil || res.IsError {
		t.Fatalf("the admitted call was not allowed to finish: %v", res)
	}
	r.mu.Lock()
	rec := r.records[len(r.records)-1]
	r.mu.Unlock()
	if rec.Scope.ProfileRevision == nil || *rec.Scope.ProfileRevision != 7 || rec.Scope.OverrideRevision == nil || *rec.Scope.OverrideRevision != 11 {
		t.Fatalf("observed under revisions %v/%v, want the entry snapshot's 7/11", rec.Scope.ProfileRevision, rec.Scope.OverrideRevision)
	}
	// The next call, and a client that listed the tool earlier, see the new policy.
	again, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}})
	if err != nil || !again.IsError || !strings.HasPrefix(resultText(again), harness.CodeGroupOff) {
		t.Fatalf("after revoke = %v %v", again, err)
	}
	_ = inv
}

func TestObservationsCarryTheSnapshotsRevisionsNotTheHostsScope(t *testing.T) {
	r := newServingRig(t, nil)
	wrong := int64(99)
	r.scope.ProfileRevision, r.scope.OverrideRevision = &wrong, &wrong // what the host would say is current
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 3}
	}))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	if _, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	rec := r.records[len(r.records)-1]
	r.mu.Unlock()
	if rec.Scope.ProfileRevision == nil || *rec.Scope.ProfileRevision != 3 || rec.Scope.OverrideRevision != nil {
		t.Fatalf("scope = %v/%v, want W=3 and no S", rec.Scope.ProfileRevision, rec.Scope.OverrideRevision)
	}
	if rec.Scope.Vendor != "claude" || rec.Scope.WorkspaceID != "w1" {
		t.Fatalf("vendor/workspace were not kept: %+v", rec.Scope)
	}
}

// ---- budgets -------------------------------------------------------------

func bigTools(sizes map[string]int) func(*Server) {
	return func(s *Server) {
		s.extraTools = func(ms *sdk.Server) {
			for name, size := range sizes {
				name, size := name, size
				sdk.AddTool(ms, &sdk.Tool{Name: name}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x\n", size/2)}}}, nil, nil
				})
			}
		}
	}
}

func TestResolvedBudgetsApplyThroughTheExistingMiddleware(t *testing.T) {
	r := newServingRig(t, bigTools(map[string]int{"bulk.one": 40000, "bulk.two": 40000, "pdf.readText": 0}))
	// Default policy: today's 24 KiB.
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(nil))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	call := func(name string) string {
		res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		return resultText(res)
	}
	if got := len(call("bulk.one")); got > ResultBudget || got < ResultBudget/2 {
		t.Fatalf("default budget cut to %d bytes", got)
	}
	ep.Close()

	// A group budget of 4 KiB with a tool override of 8 KiB beside it.
	ep2, err := r.host.OpenWithPolicy(sidA, r.resolve(func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 1, ResultBudget: &harness.BudgetProfile{
			Groups: map[string]harness.BudgetRule{"bulk": {Bytes: intp(4096)}},
			Tools:  map[string]harness.BudgetRule{"bulk.two": {Bytes: intp(8192)}}}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	cs = r.client(ep2.SocketPath)
	if got := len(call("bulk.one")); got > 4096 || got < 2048 {
		t.Fatalf("group budget cut to %d bytes", got)
	}
	if got := len(call("bulk.two")); got > 8192 || got < 4096 {
		t.Fatalf("tool budget cut to %d bytes", got)
	}
	r.mu.Lock()
	var cut bool
	for _, o := range r.records {
		cut = cut || (o.Tool == "bulk.one" && o.Cut && o.After < o.Before && o.After <= 4096)
	}
	r.mu.Unlock()
	if !cut {
		t.Error("the measured before/after bytes did not describe the policy's cut")
	}
}

// A fixed exemption is the server's, not the profile's: a budget aimed at an
// exempt tool changes nothing about its result.
func TestFixedExemptionsSurviveAnyBudget(t *testing.T) {
	reg := harness.Registry{Tools: []harness.Tool{
		{Name: "pdf.readText", DefinitionBytes: 100, BudgetExempt: true},
		{Name: "pdf.other", DefinitionBytes: 100},
	}}
	eff, err := harness.Resolve(harness.Inputs{Registry: reg, Workspace: &harness.Profile{Schema: 1, Revision: 1,
		ResultBudget: &harness.BudgetProfile{Groups: map[string]harness.BudgetRule{"pdf": {Bytes: intp(2048)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ms.AddReceivingMiddleware(withMeasuredResultBudget(ResultBudget, nil, newServingHolder(eff)))
	for _, name := range []string{"pdf.readText", "pdf.other"} {
		sdk.AddTool(ms, &sdk.Tool{Name: name}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x\n", 20000)}}}, nil, nil
		})
	}
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for name, want := range map[string]func(int) bool{
		"pdf.readText": func(n int) bool { return n == 40000 },
		"pdf.other":    func(n int) bool { return n <= 2048 },
	} {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		if n := len(resultText(res)); !want(n) {
			t.Errorf("%s came back %d bytes", name, n)
		}
	}
}

func intp(n int) *int { return &n }

// ---- no hot-path reads ---------------------------------------------------

func TestListingAndRefusingNeverReachTheGatewayOrScope(t *testing.T) {
	r := newServingRig(t, nil)
	var scopeReads atomic.Int32
	r.srv.deps.UsageScope = func(string, string) (usagemetrics.Scope, bool) {
		scopeReads.Add(1)
		return usagemetrics.Scope{Vendor: "claude"}, true
	}
	ep, err := r.host.OpenWithPolicy(sidA, r.resolve(groupOff("git", 1)))
	if err != nil {
		t.Fatal(err)
	}
	cs := r.client(ep.SocketPath)
	listNames(t, cs)
	if _, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "git.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if r.caller.calls.Load() != 0 {
		t.Fatal("listing or refusing reached the capability gateway")
	}
	if scopeReads.Load() != 1 {
		t.Fatalf("scope read %d times for one call, want once at entry", scopeReads.Load())
	}
}

// A call that is already inside the middleware when its endpoint closes meets a
// closed policy: it is refused, never served from a stale snapshot.
func TestAClosedPolicyAnswersNothing(t *testing.T) {
	reg := harness.Registry{Tools: []harness.Tool{{Name: "a.one", DefinitionBytes: 10}}}
	eff, err := harness.Resolve(harness.Inputs{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	holder := newServingHolder(eff)
	var ran atomic.Bool
	ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ms.AddReceivingMiddleware(withMeasuredResultBudget(ResultBudget, nil, holder))
	sdk.AddTool(ms, &sdk.Tool{Name: "a.one"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		ran.Store(true)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil, nil
	})
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	holder.close()
	if holder.replace(eff) {
		t.Fatal("a closed holder accepted a new policy")
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "a.one", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.HasPrefix(resultText(res), harness.CodeSnapshotUnavailable) || ran.Load() {
		t.Fatalf("closed policy = %v %v ran=%v", res, err, ran.Load())
	}
}

// The old endpoint's cleanup runs between the replacement's bind and its
// publication. It must not find itself still registered and remove the new
// socket: bind/publish and cleanup are serialized.
func TestAnOldCloseCannotRemoveTheSocketOfAReplacementBeingPublished(t *testing.T) {
	r := newServingRig(t, nil)
	old, err := r.host.OpenWithPolicy(sidA, r.resolve(nil))
	if err != nil {
		t.Fatal(err)
	}
	paused, resume := make(chan struct{}), make(chan struct{})
	oldDone := make(chan struct{})
	r.host.closeHook = func() { close(paused); <-resume } // the old listener is closed; its cleanup has not run
	go func() { old.Close(); close(oldDone) }()
	<-paused
	r.host.closeHook = nil

	r.host.bindHook = func() { // the replacement is bound, not yet published
		r.host.bindHook = nil
		close(resume)
		select {
		case <-oldDone: // an unserialized cleanup would finish here, having removed the socket
		case <-time.After(300 * time.Millisecond):
		}
	}
	fresh, err := r.host.OpenWithPolicy(sidA, r.resolve(nil))
	if err != nil {
		t.Fatal(err)
	}
	<-oldDone
	if _, err := os.Stat(fresh.SocketPath); err != nil {
		t.Fatalf("the replacement's socket is gone: %v", err)
	}
	cs := r.client(fresh.SocketPath)
	if _, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("the replacement does not serve: %v", err)
	}
	r.host.mu.Lock()
	owner := r.host.endpoints[sidA]
	r.host.mu.Unlock()
	if owner == nil || owner.path != fresh.SocketPath {
		t.Fatal("the replacement is not the registered endpoint")
	}
}

// A policy resolved against another registry cannot grant itself an exemption
// the server does not own.
func TestAPolicyCannotAddABudgetExemption(t *testing.T) {
	r := newServingRig(t, nil)
	inv := r.inventory()
	forged := inv.Registry()
	for i := range forged.Tools {
		forged.Tools[i].BudgetExempt = true // same names, every tool claims an exemption
	}
	eff, err := harness.Resolve(harness.Inputs{Registry: forged})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.host.OpenWithPolicy(sidA, eff); err == nil {
		t.Fatal("a policy whose exemptions differ from the server's was accepted")
	}
	if entries, _ := os.ReadDir(r.dir); len(entries) != 0 {
		t.Fatalf("a refused open left %d files", len(entries))
	}
	// And if one ever reached the middleware, the cut still applies: only the
	// server's own table exempts a tool.
	ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ms.AddReceivingMiddleware(withMeasuredResultBudget(ResultBudget, nil, newServingHolder(eff)))
	name := inv.Tools[0].Name
	if inv.Tools[0].BudgetExempt {
		t.Fatalf("pick another tool: %s is exempt", name)
	}
	sdk.AddTool(ms, &sdk.Tool{Name: name}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x\n", 20000)}}}, nil, nil
	})
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(resultText(res)); n > ResultBudget {
		t.Fatalf("a forged exemption let %d bytes through a %d byte budget", n, ResultBudget)
	}
}
