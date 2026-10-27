package mcp

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/storage"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A real Agent Hub service, the real corrected SessionHost, the real resolver
// and a scripted vendor: the tools a client can list, the value the host
// guidance is rendered from and the launch permission lists must be one set.

type policyProvider struct {
	mu  sync.Mutex
	inv Inventory
	off map[string]bool
}

func (p *policyProvider) set(off ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.off = map[string]bool{}
	for _, g := range off {
		p.off[g] = false
	}
}

func (p *policyProvider) Snapshot(_ context.Context, session, ws, vendor string) (harness.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in := harness.Inputs{Registry: p.inv.Registry(), Vendor: harness.Vendor{Revision: 1}}
	if len(p.off) > 0 {
		in.Workspace = &harness.Profile{Schema: 1, Revision: 2, ToolGroups: p.off}
	}
	eff, err := harness.Resolve(in)
	if err != nil {
		return harness.Snapshot{}, err
	}
	return harness.Snapshot{Vendor: in.Vendor, Effective: eff, Ref: harness.SnapshotRef{
		Schema: harness.SnapshotSchema, WorkspaceID: ws, SessionID: session, Vendor: vendor, Revisions: eff.Revisions,
		Fingerprint: eff.Fingerprint, ExecutableDigest: eff.ExecutableDigest(), ExecutableEntries: eff.ExecutableEntries(),
		WorkspaceOwned: eff.WorkspaceOwned(), WorkspaceSuppressed: eff.WorkspaceSuppressed(), FactsComplete: true, Default: in.Workspace == nil,
	}}, nil
}

func TestEndpointListGuidanceInputAndPermissionListsAgree(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	r := newServingRig(t, nil)
	provider := &policyProvider{inv: r.inventory()}
	mock := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(mock)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	svc.MCPEndpoints = r.host
	svc.Harness = provider

	create := func() agenthub.AgentSession {
		t.Helper()
		sess, err := svc.CreateSession(identityUI(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), WorkspaceID: "w1"})
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	listed := func(spec agenthub.OpenSpec) []string {
		t.Helper()
		conn, err := net.Dial("unix", spec.AllowUnixSockets[0])
		if err != nil {
			t.Fatal(err)
		}
		cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(r.ctx, newRWTransport(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		names := listNames(t, cs)
		sort.Strings(names)
		return names
	}
	subset := func(small, big []string) bool {
		set := map[string]bool{}
		for _, n := range big {
			set[n] = true
		}
		for _, n := range small {
			if !set[n] {
				return false
			}
		}
		return true
	}

	provider.set("pdf", "git")
	create()
	spec := mock.LastSpec()
	names := listed(spec)
	if spec.DevKit == nil || strings.Join(spec.DevKit.Tools, ",") != strings.Join(names, ",") {
		t.Fatalf("guidance input %v differs from the list a client sees %v", spec.DevKit, names)
	}
	for _, n := range names {
		if strings.HasPrefix(n, "pdf.") || strings.HasPrefix(n, "git.") {
			t.Fatalf("%s is served", n)
		}
	}
	for _, lists := range [][]string{spec.PreApprovedMCP["devkit"], spec.GateOwnedMCP["devkit"]} {
		if !subset(lists, names) {
			t.Fatalf("a launch permission list names a hidden tool: %v", lists)
		}
	}
	if len(spec.PreApprovedMCP["devkit"]) == 0 {
		t.Fatal("the agenthub reads should still be pre-approved")
	}

	// A second session of the same process: its own policy, its own value.
	provider.set()
	create()
	second := mock.LastSpec()
	if len(second.DevKit.Tools) <= len(spec.DevKit.Tools) || !subset(second.PreApprovedMCP["devkit"], listed(second)) {
		t.Fatalf("second session: %d served tools vs %d", len(second.DevKit.Tools), len(spec.DevKit.Tools))
	}
	if len(spec.DevKit.Tools) == len(second.DevKit.Tools) {
		t.Fatal("the sessions share a value")
	}
	// A policy that serves nothing is positive, empty and not nil.
	var every []string
	for _, tl := range r.inventory().Tools {
		every = append(every, tl.Group)
	}
	provider.set(every...)
	create()
	if empty := mock.LastSpec().DevKit; empty == nil || len(empty.Tools) != 0 {
		t.Fatalf("all groups off: spec.DevKit = %+v, want a non-nil empty value", empty)
	}
}

func identityUI() context.Context {
	return identity.WithCaller(context.Background(), identity.UI())
}
