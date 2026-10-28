package agenthub_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/checkpoint"
	"devkit/internal/harness"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

type events struct {
	mu  sync.Mutex
	log []string
}

func (e *events) add(format string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, fmt.Sprintf(format, args...))
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.log...)
}

func (e *events) count(prefix string) (n int) {
	for _, l := range e.all() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (e *events) index(prefix string) int {
	for i, l := range e.all() {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}

// launchAdapter records every open and lets a test fail the next one.
type launchAdapter struct {
	*agenthub.MockAdapter
	ev      *events
	mu      sync.Mutex
	openErr error
	// openErrFor, when set, answers per open: what that account's open does.
	openErrFor func(agenthub.OpenSpec) error
	specs      []agenthub.OpenSpec
	last       *mortal
	// wrapRT, when set, wraps every runtime the adapter makes (under the mortal).
	wrapRT func(agenthub.SessionRuntime) agenthub.SessionRuntime
}

// mortal is a runtime whose process a test can declare dead.
type mortal struct {
	agenthub.SessionRuntime
	dead atomic.Bool
}

func (m *mortal) Dead() (bool, error) { return m.dead.Load(), nil }

func (a *launchAdapter) wrap(rt agenthub.SessionRuntime) agenthub.SessionRuntime {
	if a.wrapRT != nil {
		rt = a.wrapRT(rt)
	}
	m := &mortal{SessionRuntime: rt}
	a.mu.Lock()
	a.last = m
	a.mu.Unlock()
	return m
}

func (a *launchAdapter) kill() { a.mu.Lock(); a.last.dead.Store(true); a.mu.Unlock() }

func (a *launchAdapter) record(spec agenthub.OpenSpec, kind string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ev.add("adapter.%s", kind)
	a.specs = append(a.specs, spec)
	if a.openErrFor != nil {
		if err := a.openErrFor(spec); err != nil {
			return err
		}
	}
	return a.openErr
}

func (a *launchAdapter) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	if err := a.record(spec, "open"); err != nil {
		return nil, err
	}
	rt, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return a.wrap(rt), nil
}

func (a *launchAdapter) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	if err := a.record(spec, "load"); err != nil {
		return nil, err
	}
	rt, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return a.wrap(rt), nil
}

func (a *launchAdapter) lastSpec() agenthub.OpenSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.specs[len(a.specs)-1]
}

func (a *launchAdapter) setErr(err error) { a.mu.Lock(); a.openErr = err; a.mu.Unlock() }

// fakeHarness answers Snapshot from a function the test swaps.
type fakeHarness struct {
	ev    *events
	calls atomic.Int32
	mu    sync.Mutex
	fn    func(session, ws, vendor string) (harness.Snapshot, error)
}

func (f *fakeHarness) Snapshot(_ context.Context, session, ws, vendor string) (harness.Snapshot, error) {
	f.calls.Add(1)
	f.ev.add("snapshot")
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()
	return fn(session, ws, vendor)
}

func (f *fakeHarness) set(fn func(session, ws, vendor string) (harness.Snapshot, error)) {
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}

// plainEndpoints serves today's default only.
type plainEndpoints struct {
	ev     *events
	mu     sync.Mutex
	n      int
	closed []string
}

func (p *plainEndpoints) Open(sessionID string) (agenthub.MCPEndpoint, error) {
	return p.open(sessionID, "endpoint.open")
}

func (p *plainEndpoints) open(sessionID, kind string) (agenthub.MCPEndpoint, error) {
	p.mu.Lock()
	p.n++
	token := fmt.Sprintf("token-%d", p.n)
	p.mu.Unlock()
	p.ev.add("%s", kind)
	return agenthub.MCPEndpoint{
		UsageToken: token, SocketPath: "/tmp/" + sessionID[:8] + ".sock",
		Server: acp.McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/x.sock"}, Env: []acp.EnvVariable{}},
		Close:  func() { p.mu.Lock(); p.closed = append(p.closed, token); p.mu.Unlock(); p.ev.add("endpoint.close") },
	}, nil
}

// policyEndpoints can serve a prepared policy.
type policyEndpoints struct {
	plainEndpoints
	policies []harness.Effective
	// noServed makes the endpoint omit the inventory it serves (a broken port).
	noServed bool
}

func (p *policyEndpoints) OpenWithPolicy(sessionID string, e harness.Effective) (agenthub.MCPEndpoint, error) {
	p.mu.Lock()
	p.policies = append(p.policies, e)
	p.mu.Unlock()
	ep, err := p.open(sessionID, "endpoint.openWithPolicy")
	if err != nil || p.noServed {
		return ep, err
	}
	var names []string
	for _, t := range tinyRegistry.Tools {
		if e.Served(t.Name) {
			names = append(names, t.Name)
		}
	}
	ep.Served = agenthub.NewDevKitServing(names)
	return ep, nil
}

var tinyRegistry = harness.Registry{Tools: []harness.Tool{
	{Name: "git.status", DefinitionBytes: 900}, {Name: "tasks.list", DefinitionBytes: 800}, {Name: harness.ProposeTool, DefinitionBytes: 600},
}}

// snapshotFor resolves a snapshot with the real resolver, as the provider does.
func snapshotFor(t *testing.T, session, ws, vendor string, mutate func(*harness.Inputs)) harness.Snapshot {
	t.Helper()
	in := harness.Inputs{Registry: tinyRegistry, Vendor: harness.Vendor{Revision: 4}}
	if mutate != nil {
		mutate(&in)
	}
	eff, err := harness.Resolve(in)
	if err != nil {
		t.Fatal(err)
	}
	return harness.Snapshot{
		Vendor: in.Vendor,
		Ref: harness.SnapshotRef{Schema: harness.SnapshotSchema, WorkspaceID: ws, SessionID: session, Vendor: vendor, Revisions: eff.Revisions,
			Fingerprint: eff.Fingerprint, ExecutableDigest: eff.ExecutableDigest(), ExecutableEntries: eff.ExecutableEntries(), WorkspaceOwned: eff.WorkspaceOwned(), WorkspaceSuppressed: eff.WorkspaceSuppressed(), FactsComplete: true, Default: in.Workspace == nil && in.Session == nil},
		Effective: eff,
	}
}

func gitOffW(rev int64) func(*harness.Inputs) {
	return func(in *harness.Inputs) {
		in.Workspace = &harness.Profile{Schema: 1, Revision: rev, ToolGroups: map[string]bool{"git": false}}
	}
}

type launchEnv struct {
	t       *testing.T
	db      *sql.DB
	svc     *agenthub.Service
	adapter *launchAdapter
	harness *fakeHarness
	ev      *events
}

func newLaunchEnv(t *testing.T, endpoints func(*events) agenthub.MCPEndpoints) *launchEnv {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	ev := &events{}
	adapter := &launchAdapter{MockAdapter: agenthub.NewMockAdapter(), ev: ev}
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(string, json.RawMessage) {})
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	e := &launchEnv{t: t, db: db, svc: svc, adapter: adapter, ev: ev, harness: &fakeHarness{ev: ev}}
	e.harness.set(func(session, ws, vendor string) (harness.Snapshot, error) {
		return snapshotFor(t, session, ws, vendor, nil), nil
	})
	if endpoints != nil {
		svc.MCPEndpoints = endpoints(ev)
	}
	svc.Harness = e.harness
	return e
}

func (e *launchEnv) create() (agenthub.AgentSession, error) {
	return e.svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: e.t.TempDir(), WorkspaceID: "w1"})
}

func (e *launchEnv) mustCreate() agenthub.AgentSession {
	e.t.Helper()
	s, err := e.create()
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func plain(ev *events) agenthub.MCPEndpoints { return &plainEndpoints{ev: ev} }
func policy(ev *events) agenthub.MCPEndpoints {
	return &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}}
}

func (e *launchEnv) reload(id string) error {
	return e.svc.ResumeSession(context.Background(), id, true)
}

func TestWithoutAHarnessNothingIsResolvedOrRecorded(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.svc.Harness = nil
	s := e.mustCreate()
	if _, _, ok := e.svc.HarnessApplied(s.ID); ok || e.harness.calls.Load() != 0 {
		t.Fatal("a service with no harness recorded one")
	}
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
}

func TestADefaultSnapshotLaunchesLikeTodayWithTheVendorCustomizationItWasResolvedFrom(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Vendor = harness.Vendor{Revision: 7, DisabledMCP: []string{"zeta", "alpha"}, Servers: []harness.Server{{Name: "z", Command: "c", Enabled: true}, {Name: "a", Command: "d", Enabled: true}}}
		}), nil
	})
	// Settings is edited after the snapshot was resolved and before the launch
	// reads its sandbox settings: the runtime must still get the resolved V.
	e.svc.SandboxExtras = func() preferences.AgentSandboxSettings {
		s := preferences.DefaultAgentSandbox()
		s.Customizations = map[string]preferences.AgentCustomization{"mock": {Revision: 8, DisabledMCP: []string{"edited"}}}
		return s
	}
	s := e.mustCreate()
	ref, enforced, ok := e.svc.HarnessApplied(s.ID)
	if !ok || !ref.Default || enforced || ref.SessionID != s.ID || ref.WorkspaceID != "w1" || ref.Revisions.Vendor != 7 {
		t.Fatalf("applied = %+v enforced=%v ok=%v", ref, enforced, ok)
	}
	got := e.adapter.lastSpec().Customizations["mock"]
	if got.Revision != 7 || len(got.DisabledMCP) != 2 || got.DisabledMCP[0] != "zeta" || len(got.Servers) != 2 || got.Servers[0].Name != "z" {
		t.Fatalf("the applied reference says V=7 but the runtime got %+v", got)
	}
	if e.ev.count("endpoint.open") != 1 || e.ev.index("snapshot") > e.ev.index("endpoint.open") {
		t.Fatalf("the snapshot is resolved before the endpoint opens: %v", e.ev.all())
	}
}

func TestAProfileThatCannotBeEnforcedIsRefusedBeforeAnythingOpens(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) { return snapshotFor(t, s, ws, v, gitOffW(1)), nil })
	if _, err := e.create(); !errors.Is(err, agenthub.ErrHarnessNotEnforceable) {
		t.Fatalf("create: %v", err)
	}
	if got := e.ev.all(); len(got) != 1 || got[0] != "snapshot" {
		t.Fatalf("a refused launch opened something: %v", got)
	}
	// An existing session is refused on resume as well, with no process started.
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) { return snapshotFor(t, s, ws, v, nil), nil })
	s := e.mustCreate()
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) { return snapshotFor(t, s, ws, v, gitOffW(1)), nil })
	opens, endpoints := e.ev.count("adapter."), e.ev.count("endpoint.")
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNotEnforceable) {
		t.Fatalf("reload: %v", err)
	}
	if e.ev.count("adapter.") != opens || e.ev.count("endpoint.") != endpoints {
		t.Fatal("a refused reload started a process or touched the live endpoint")
	}
	// The running session still answers from the runtime it opened with.
	if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "still there?"}); err != nil {
		t.Fatalf("a refused reload took the running session down: %v", err)
	}
	if e.ev.count("adapter.") != opens {
		t.Fatal("the session needed a new process after a refused reload")
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); !ref.Default {
		t.Fatal("a refused reload changed the applied record")
	}
}

func TestAnUnresolvableConfigurationRefusesTheLaunchAndPublishesNothing(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.harness.set(func(string, string, string) (harness.Snapshot, error) {
		return harness.Snapshot{}, errors.New("corrupt")
	})
	if _, err := e.create(); !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("create: %v", err)
	}
	if got := e.ev.all(); len(got) != 1 {
		t.Fatalf("something opened beside the failed snapshot: %v", got)
	}
	sessions, _ := e.svc.ListSessions(context.Background())
	if len(sessions) != 0 {
		t.Fatal("a session row exists for a launch that was refused")
	}
}

func TestAnEnforcingEndpointIsStagedWithThePolicyBeforeTheAdapterOpens(t *testing.T) {
	e := newLaunchEnv(t, policy)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			gitOffW(3)(in)
			in.Session = &harness.Override{Schema: 1, Revision: 5}
		}), nil
	})
	s := e.mustCreate()
	if e.ev.index("endpoint.openWithPolicy") < 0 || e.ev.index("endpoint.openWithPolicy") > e.ev.index("adapter.open") {
		t.Fatalf("the policy must be staged before the vendor can ask: %v", e.ev.all())
	}
	eps := e.svc.MCPEndpoints.(*policyEndpoints)
	if len(eps.policies) != 1 || eps.policies[0].Served("git.status") || !eps.policies[0].Served("tasks.list") {
		t.Fatalf("the endpoint was not given the snapshot's policy")
	}
	ref, enforced, ok := e.svc.HarnessApplied(s.ID)
	if !ok || !enforced || ref.Default || *ref.Revisions.Workspace != 3 || *ref.Revisions.Session != 5 {
		t.Fatalf("applied = %+v %v", ref, enforced)
	}
	// Usage attribution carries the revisions because the endpoint enforces them,
	// and only under this runtime's own token.
	scope, ok := e.svc.ToolUsageScope(s.ID, "token-1")
	if !ok || scope.ProfileRevision == nil || *scope.ProfileRevision != 3 || *scope.OverrideRevision != 5 {
		t.Fatalf("scope = %+v %v", scope, ok)
	}
	if _, ok := e.svc.ToolUsageScope(s.ID, "token-0"); ok {
		t.Fatal("a foreign token adopted the runtime's facts")
	}
	// Reload: a new endpoint and token; the old token no longer adopts anything.
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			gitOffW(3)(in)
			in.Session = &harness.Override{Schema: 1, Revision: 6}
		}), nil
	})
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.ToolUsageScope(s.ID, "token-1"); ok {
		t.Fatal("a late callback from the old endpoint adopted the new runtime")
	}
	if scope, ok := e.svc.ToolUsageScope(s.ID, "token-2"); !ok || *scope.OverrideRevision != 6 {
		t.Fatalf("scope after reload = %+v %v", scope, ok)
	}
}

func TestRevisionsAreNotStatedForAnEndpointThatDoesNotEnforce(t *testing.T) {
	e := newLaunchEnv(t, plain)
	// W exists but serves the default: launchable without enforcement, and no
	// revision may be claimed in attribution.
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 9} }), nil
	})
	s := e.mustCreate()
	ref, enforced, _ := e.svc.HarnessApplied(s.ID)
	if enforced || ref.Revisions.Workspace == nil {
		t.Fatalf("applied = %+v %v", ref, enforced)
	}
	scope, ok := e.svc.ToolUsageScope(s.ID, "token-1")
	if !ok || scope.ProfileRevision != nil || scope.OverrideRevision != nil {
		t.Fatalf("a revision was stamped for a configuration the endpoint does not enforce: %+v", scope)
	}
}

func TestLaunchConfigurationComesFromTheSnapshotOnlyWhenProfiled(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.svc.SandboxExtras = func() preferences.AgentSandboxSettings {
		s := preferences.DefaultAgentSandbox()
		s.Customizations = map[string]preferences.AgentCustomization{"mock": {Revision: 4, DisabledMCP: []string{"old"}}}
		return s
	}
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Vendor.DisabledMCP = []string{"old"}
			in.Workspace = &harness.Profile{Schema: 1, Revision: 2, DisabledMCP: []string{"slack"},
				Servers: []harness.Server{{Name: "db", Command: "cmd", Args: []string{"-x"}, EnvKeys: []string{"K"}, Enabled: true}}}
		}), nil
	})
	e.mustCreate()
	got := e.adapter.lastSpec().Customizations["mock"]
	if len(got.DisabledMCP) != 2 || got.DisabledMCP[0] != "old" || got.DisabledMCP[1] != "slack" {
		t.Fatalf("deny list = %v", got.DisabledMCP)
	}
	if len(got.Servers) != 1 || got.Servers[0].Name != "db" || got.Servers[0].Args[0] != "-x" || got.Revision != 4 {
		t.Fatalf("servers = %+v", got)
	}
}

func TestAFailedOpenKeepsThePreviousAppliedRecordAndClosesTheEndpoint(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	first, _, _ := e.svc.HarnessApplied(s.ID)
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) { in.Workspace = &harness.Profile{Schema: 1, Revision: 2} }), nil
	})
	e.adapter.setErr(errors.New("spawn failed"))
	if err := e.reload(s.ID); err == nil {
		t.Fatal("expected the open to fail")
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); !ref.Equal(first) {
		t.Fatalf("a failed open published %+v", ref)
	}
	eps := e.svc.MCPEndpoints.(*plainEndpoints)
	if len(eps.closed) < 2 {
		t.Fatalf("the staged endpoint was not torn down: %v", eps.closed)
	}
	e.adapter.setErr(nil)
	if err := e.reload(s.ID); err != nil {
		t.Fatal(err)
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); ref.Equal(first) || *ref.Revisions.Workspace != 2 {
		t.Fatalf("a successful open must replace the record: %+v", ref)
	}
}

func TestANewExecutableSetAfterAMovedProfileNeedsANewSession(t *testing.T) {
	e := newLaunchEnv(t, plain)
	withServer := func(rev int64, cmd string) func(string, string, string) (harness.Snapshot, error) {
		return func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				in.Workspace = &harness.Profile{Schema: 1, Revision: rev, Servers: []harness.Server{{Name: "db", Command: cmd, Enabled: true}}}
			}), nil
		}
	}
	e.harness.set(withServer(1, "a"))
	s := e.mustCreate()
	first, _, _ := e.svc.HarnessApplied(s.ID)

	e.harness.set(withServer(2, "b")) // W moved and the executable changed
	opens := e.ev.count("adapter.")
	endpoints := e.ev.count("endpoint.")
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("reload adopting a changed executable: %v", err)
	}
	if e.ev.count("adapter.") != opens || e.ev.count("endpoint.") != endpoints {
		t.Fatal("the refusal came after something was opened")
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); !ref.Equal(first) {
		t.Fatal("the applied record moved")
	}

	e.harness.set(withServer(2, "a")) // W moved, same executables: fine
	if err := e.reload(s.ID); err != nil {
		t.Fatalf("a non-executable W change must reload: %v", err)
	}

	// V edited in Settings while W stays put: a reload has always adopted that.
	e.harness.set(func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Vendor.Servers = []harness.Server{{Name: "vendor-only", Command: "c", Enabled: true}}
			in.Workspace = &harness.Profile{Schema: 1, Revision: 2, Servers: []harness.Server{{Name: "db", Command: "a", Enabled: true}}}
		}), nil
	})
	if err := e.reload(s.ID); err != nil {
		t.Fatalf("a V-only executable change must reload as before: %v", err)
	}
	// A brand-new fresh child is a new session: the gate does not apply.
	e.harness.set(withServer(9, "z"))
	e.mustCreate()
}

func TestAReleaseSealWinsBeforeAnythingIsResolved(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	end, err := e.svc.SealWriterAdmission(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	before := e.ev.all()
	if err := e.reload(s.ID); err == nil {
		t.Fatal("a sealed session was reloaded")
	}
	if got := e.ev.all(); len(got) != len(before) {
		t.Fatalf("a sealed start resolved or opened something: %v", got[len(before):])
	}
}

func TestTwoSimultaneousResumeCallersPrepareOnce(t *testing.T) {
	e := newLaunchEnv(t, plain)
	s := e.mustCreate()
	calls := e.harness.calls.Load()
	e.adapter.kill() // the process died; the next turn must resume it
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "x"})
		}()
	}
	wg.Wait()
	if got := e.harness.calls.Load() - calls; got != 1 {
		t.Fatalf("snapshot resolved %d times for one resume", got)
	}
}

func TestASessionWithoutAWorkspaceHasNoSnapshot(t *testing.T) {
	e := newLaunchEnv(t, plain)
	e.svc.MatchingWorkspaces = func(string) ([]string, error) { return nil, nil }
	s, err := e.svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if e.harness.calls.Load() != 0 {
		t.Fatal("a snapshot was resolved for a session with no workspace")
	}
	if _, _, ok := e.svc.HarnessApplied(s.ID); ok {
		t.Fatal("a session with no workspace has a harness record")
	}
}

// Rewind has its own opens (Open and Fork); they use the same prepared launch.
func TestRewindOpensThroughTheSharedLaunchPipeline(t *testing.T) {
	git := checkpoint.Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO workspaces (id, name, root) VALUES ('w1', 'w1', '')"); err != nil {
		t.Fatal(err)
	}
	store := agenthub.NewSQLStore(db)
	project := t.TempDir()
	ev := &events{}
	mock := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(replyingAdapter{MockAdapter: mock, closed: &atomic.Bool{}, writeDuringTurn: func() {}})
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	svc.SetCheckpoints(agenthub.NewEngineCheckpoints(checkpoint.New(filepath.Join(t.TempDir(), "checkpoints"), git, checkpoint.Limits{}), store, store))
	svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	svc.MCPEndpoints = &policyEndpoints{plainEndpoints: plainEndpoints{ev: ev}}
	fh := &fakeHarness{ev: ev}
	current := func(rev int64) {
		fh.set(func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				gitOffW(rev)(in)
			}), nil
		})
	}
	current(1)
	svc.Harness = fh

	ctx := context.Background()
	session, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: project, WorkspaceID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: fmt.Sprintf("prompt %d", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	records := waitForCheckpoint(t, store, session.ID, 2)
	before, _, _ := svc.HarnessApplied(session.ID)

	// W moves to revision 2 with the same executables: the rewind adopts it,
	// through the same prepared launch, and the old token stops adopting facts.
	current(2)
	preview, err := svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: records[1].PromptMessageID, Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	after, enforced, ok := svc.HarnessApplied(session.ID)
	if !ok || !enforced || *after.Revisions.Workspace != 2 || after.Equal(before) {
		t.Fatalf("applied after rewind = %+v enforced=%v", after, enforced)
	}
	if scope, ok := svc.ToolUsageScope(session.ID, "token-2"); !ok || *scope.ProfileRevision != 2 {
		t.Fatalf("the rewound runtime's facts = %+v %v", scope, ok)
	}
	if _, ok := svc.ToolUsageScope(session.ID, "token-1"); ok {
		t.Fatal("the replaced runtime's token still adopts facts")
	}
	if ev.index("endpoint.openWithPolicy") < 0 || ev.count("endpoint.openWithPolicy") != 2 {
		t.Fatalf("rewind did not stage the endpoint with the policy: %v", ev.all())
	}
}

// Review: the gate must look at what changed and why, not at W's revision.
func TestAReopenAdoptsOnlyWhatTheWorkspaceContentReallyChanged(t *testing.T) {
	target := "/real/a"
	snapWith := func(rev int64, mutate func(*harness.Profile)) func(string, string, string) (harness.Snapshot, error) {
		return func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				in.Canonical = func(p string) (string, error) {
					if p == "/link/skills" {
						return target, nil
					}
					return p, nil
				}
				w := &harness.Profile{Schema: 1, Revision: rev, Servers: []harness.Server{{Name: "db", Command: "a", Enabled: true}}, SkillFolders: []string{"/link/skills"}}
				if mutate != nil {
					mutate(w)
				}
				in.Workspace = w
			}), nil
		}
	}
	e := newLaunchEnv(t, plain)
	e.harness.set(snapWith(1, nil))
	s := e.mustCreate()

	// The symlink inside W now points somewhere else; the revision did not move.
	target = "/real/b"
	e.harness.set(snapWith(1, nil))
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("a retargeted skill symlink in W was adopted as if nothing changed: %v", err)
	}
	target = "/real/a"

	// W gets a deny entry that removes its server: narrowing, so it reloads.
	e.harness.set(snapWith(2, func(w *harness.Profile) { w.DisabledMCP = []string{"db"} }))
	if err := e.reload(s.ID); err != nil {
		t.Fatalf("a narrowing W change must not need a new session: %v", err)
	}
	// And dropping a server entirely.
	e.harness.set(snapWith(3, func(w *harness.Profile) { w.Servers = nil }))
	if err := e.reload(s.ID); err != nil {
		t.Fatalf("removing a W server must not need a new session: %v", err)
	}
	// Putting an executable back is a gain again.
	e.harness.set(snapWith(4, nil))
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("re-adding a server after narrowing: %v", err)
	}
}

// Review: the vendor customization changes a server's command in the same reload
// in which the workspace only turns a tool group off.
func TestAVendorCommandChangeBesideAWorkspaceNarrowingStillReloads(t *testing.T) {
	e := newLaunchEnv(t, policy)
	snap := func(vendorCmd string, wRev int64, gitOn bool) func(string, string, string) (harness.Snapshot, error) {
		return func(sid, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) {
				in.Vendor.Servers = []harness.Server{{Name: "vendor-db", Command: vendorCmd, Enabled: true}}
				in.Workspace = &harness.Profile{Schema: 1, Revision: wRev, ToolGroups: map[string]bool{"git": gitOn}}
			}), nil
		}
	}
	e.harness.set(snap("old-command", 1, true))
	s := e.mustCreate()
	e.harness.set(snap("new-command", 2, false))
	if err := e.reload(s.ID); err != nil {
		t.Fatalf("a vendor command change must not be blamed on a workspace that only narrows: %v", err)
	}
	if ref, _, _ := e.svc.HarnessApplied(s.ID); *ref.Revisions.Workspace != 2 {
		t.Fatalf("the reload did not apply the new configuration: %+v", ref)
	}
	// The workspace adding its own server in the same breath is its doing.
	e.harness.set(func(sid, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, sid, ws, v, func(in *harness.Inputs) {
			in.Vendor.Servers = []harness.Server{{Name: "vendor-db", Command: "newer", Enabled: true}}
			in.Workspace = &harness.Profile{Schema: 1, Revision: 3, ToolGroups: map[string]bool{"git": false}, Servers: []harness.Server{{Name: "w", Command: "x", Enabled: true}}}
		}), nil
	})
	if err := e.reload(s.ID); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("got %v", err)
	}
}
