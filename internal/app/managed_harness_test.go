package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/harness"
)

// The adapter is exercised over the composed graph (real Agent Hub, real
// provider and stores, real MCP endpoints); only the vendors, the person's
// answers and the ledger store are fakes.

// The ownership check is reused, not copied.
var _ managedWriterResolver = (*managedOwnershipHost)(nil)

func TestManagedHarnessSourceSchemaMatchesTheHarness(t *testing.T) {
	if agentwork.HarnessSourceSchemaSupported != harness.SnapshotSchema {
		t.Fatalf("agentwork reads schema %d, the harness writes %d", agentwork.HarnessSourceSchemaSupported, harness.SnapshotSchema)
	}
}

// overrideSpy records the session ids whose override the provider reads.
type overrideSpy struct {
	harnessPersistence
	mu  sync.Mutex
	ids []string
}

func (s *overrideSpy) GetOverride(ctx context.Context, sessionID, workspaceID string) (*harness.Override, harness.Stamp, error) {
	s.mu.Lock()
	s.ids = append(s.ids, sessionID)
	s.mu.Unlock()
	return s.harnessPersistence.GetOverride(ctx, sessionID, workspaceID)
}

func (s *overrideSpy) reset() {
	s.mu.Lock()
	s.ids = nil
	s.mu.Unlock()
}

func (s *overrideSpy) read() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...)
}

// fakeOwnership stands in for the ownership check (tasks board, git worktree and
// folder scope, which this rig does not build); the adapter must call it.
type fakeOwnership struct {
	err error
	// onCall runs inside the check, after it is counted.
	onCall func()
	calls  atomic.Int32
	last   atomic.Value // agentwork.Attempt
}

func (f *fakeOwnership) ResolveManagedWriter(_ context.Context, a agentwork.Attempt) (agentwork.ManagedWriterBinding, error) {
	f.calls.Add(1)
	f.last.Store(a)
	if f.onCall != nil {
		f.onCall()
	}
	return agentwork.ManagedWriterBinding{}, f.err
}

// modeHub overlays the live hub with scripted permission modes and served tools.
type modeHub struct {
	managedHarnessHub
	modes      map[string]string // absent: the session has no mode concept
	unreadable map[string]bool   // the session's mode cannot be read
	served     func(id string) ([]string, bool)
}

func (h modeHub) ReadPermissionMode(_ context.Context, id string) (string, agenthub.ModeRead) {
	if h.unreadable[id] {
		return "", agenthub.ModeUnreadable
	}
	if m, ok := h.modes[id]; ok {
		return m, agenthub.ModeKnown
	}
	return "", agenthub.ModeNone
}

func (h modeHub) PermissionModeOf(_ context.Context, id string) (string, bool) {
	m, ok := h.modes[id]
	return m, ok
}

func (h modeHub) ServedTools(id string) ([]string, bool) {
	if h.served != nil {
		return h.served(id)
	}
	return h.managedHarnessHub.ServedTools(id)
}

type mhRig struct {
	*compRig
	spy   *overrideSpy
	fault *fault
	own   *fakeOwnership
	host  *managedHarnessHost
	coord agenthub.AgentSession
}

func newMHRig(t *testing.T, opt compOptions) *mhRig {
	t.Helper()
	m := &mhRig{own: &fakeOwnership{}}
	opt.wrapStore = func(p harnessPersistence) harnessPersistence {
		m.fault = &fault{harnessPersistence: p}
		m.spy = &overrideSpy{harnessPersistence: m.fault}
		return m.spy
	}
	m.compRig = newCompRig(t, opt)
	m.host = newManagedHarnessHost(&Container{AgentHub: m.agents}, m.own)
	m.coord, _ = m.create(agenthub.AgentTypeClaude)
	return m
}

func (m *mhRig) newChild(cfg map[string]string) agenthub.AgentSession {
	m.t.Helper()
	ctx := m.agentCtx(m.coord.ID)
	if cfg != nil {
		ctx = m.ui() // the person's own create path: it does not check an agent's model choice
	}
	child, err := m.agents.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeClaude, Cwd: m.root, ParentSessionID: m.coord.ID, InitialConfig: cfg})
	if err != nil {
		m.t.Fatalf("create child: %v", err)
	}
	return child
}

func (m *mhRig) scope() agentwork.PackageScope {
	return agentwork.PackageScope{WorkspaceID: m.wsID, BoardID: "b1", ParentTaskID: "p1", GroupID: "g1", WorkItemID: "i1", TaskID: "t1", AttemptID: "a1"}
}

func (m *mhRig) attempt(sessionID string) agentwork.Attempt {
	return agentwork.Attempt{ID: "a1", WorkItemID: "i1", GroupID: "g1", WorkspaceID: m.wsID, BoardID: "b1", ParentTaskID: "p1",
		SessionID: sessionID, Role: "implementer", State: "session_created", Generation: 1, Revision: 2}
}

func (m *mhRig) intended(role string) (agentwork.HarnessResolution, error) {
	return m.host.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: role})
}

func refusalOf(t *testing.T, err error) *agentwork.HarnessRefusal {
	t.Helper()
	var r *agentwork.HarnessRefusal
	if !errors.As(err, &r) {
		t.Fatalf("want a harness refusal, got %v", err)
	}
	return r
}

func wantCode(t *testing.T, err error, code string) *agentwork.HarnessRefusal {
	t.Helper()
	r := refusalOf(t, err)
	if r.Code != code {
		t.Fatalf("code = %s (groups %v), want %s", r.Code, r.Groups, code)
	}
	return r
}

// memHarnessStore is the ledger side as one attempt in memory.
type memHarnessStore struct {
	mu       sync.Mutex
	attempt  agentwork.Attempt
	item     agentwork.WorkItem
	group    agentwork.GroupState
	pkg      agentwork.StoredContextPackage
	bindings map[string]agentwork.HarnessBinding
}

func newMemStore(wsID string) *memHarnessStore {
	return &memHarnessStore{
		group: agentwork.GroupState{ID: "g1", WorkspaceID: wsID, BoardID: "b1", ParentTaskID: "p1", CoordinatorID: "c", Enabled: true, SchemaVersion: agentwork.ManagedSchemaVersion},
		item: agentwork.WorkItem{ID: "i1", GroupID: "g1", WorkspaceID: wsID, BoardID: "b1", ParentTaskID: "p1", TaskID: "t1", Role: "implementer",
			RequirementsRevision: "v2:r", CurrentAttemptID: "a1", Revision: 2, SchemaVersion: agentwork.ManagedSchemaVersion},
		attempt: agentwork.Attempt{ID: "a1", WorkItemID: "i1", GroupID: "g1", WorkspaceID: wsID, BoardID: "b1", ParentTaskID: "p1", Role: "implementer",
			State: "reserved", DispatchState: "reserved", Generation: 1, Revision: 1, RequirementsRevision: "v2:r", SchemaVersion: agentwork.ManagedSchemaVersion},
		pkg: agentwork.StoredContextPackage{ContextPackage: agentwork.ContextPackage{ID: "pk1", WorkspaceID: wsID, BoardID: "b1", ParentTaskID: "p1", GroupID: "g1",
			WorkItemID: "i1", TaskID: "t1", AttemptID: "a1", Role: "implementer", RequirementsRevision: "v2:r", Generation: 1}},
		bindings: map[string]agentwork.HarnessBinding{},
	}
}

func (s *memHarnessStore) openChild(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempt.State, s.attempt.SessionID, s.attempt.Revision = "session_created", sessionID, 2
}

func (s *memHarnessStore) GetManagedAttempt(context.Context, string) (agentwork.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempt, nil
}
func (s *memHarnessStore) GetCurrentManagedAttempt(context.Context, string) (agentwork.Attempt, error) {
	return s.GetManagedAttempt(context.Background(), "")
}
func (s *memHarnessStore) GetWorkItem(context.Context, string) (agentwork.WorkItem, error) {
	return s.item, nil
}
func (s *memHarnessStore) GetWorkGroup(context.Context, string) (agentwork.GroupState, error) {
	return s.group, nil
}
func (s *memHarnessStore) GetContextPackage(context.Context, string) (agentwork.StoredContextPackage, error) {
	return s.pkg, nil
}
func (s *memHarnessStore) InsertHarnessBinding(_ context.Context, b agentwork.HarnessBinding, rev int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rev != s.attempt.Revision {
		return agentwork.ErrStaleRevision
	}
	key := b.AttemptID + "/" + b.Phase
	if _, ok := s.bindings[key]; ok {
		return agentwork.ErrHarnessBindingExists
	}
	s.bindings[key] = b
	return nil
}
func (s *memHarnessStore) GetHarnessBinding(_ context.Context, attemptID, phase string) (agentwork.HarnessBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bindings[attemptID+"/"+phase]
	if !ok {
		return agentwork.HarnessBinding{}, agentwork.ErrManagedRecordNotFound
	}
	return b, nil
}

// bound runs the intended phase, opens a real child and binds it, returning the
// service so a test can edit the configuration and verify again.
func (m *mhRig) bound(t *testing.T) (*agentwork.ManagedHarnessService, *memHarnessStore, agenthub.AgentSession) {
	t.Helper()
	store := newMemStore(m.wsID)
	svc := agentwork.NewManagedHarnessService(store, m.host)
	ctx := context.Background()
	if _, err := svc.BindIntended(ctx, "pk1", agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: "implementer"}, 1); err != nil {
		t.Fatalf("bind intended: %v", err)
	}
	child := m.newChild(nil)
	store.openChild(child.ID)
	if _, err := svc.BindActual(ctx, "a1", 2); err != nil {
		t.Fatalf("bind actual: %v", err)
	}
	return svc, store, child
}

func TestIntendedUsesAThrowawaySessionSoParentOverrideIsNeverRead(t *testing.T) {
	m := newMHRig(t, compOptions{})
	m.workspaceOff("vault")
	if _, err := m.apply(m.coord.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if o, _ := m.override(m.coord.ID); o == nil {
		t.Fatal("the coordinator needs an S for this test")
	}
	m.spy.reset()
	m.host.probeID = func() string { return "probe-session-1" }
	res, err := m.intended("implementer")
	if err != nil {
		t.Fatal(err)
	}
	ids := m.spy.read()
	if !reflect.DeepEqual(ids, []string{"probe-session-1"}) {
		t.Fatalf("overrides read for %v: only the throwaway id may be read", ids)
	}
	if res.Facts.SessionRevision != nil {
		t.Fatalf("the intended configuration has no S: %+v", res.Facts)
	}
	for _, g := range res.Groups {
		if g == "vault" {
			t.Fatal("the coordinator's S leaked into the intended groups")
		}
	}
}

func TestFreshChildDoesNotInheritParentOverrideThroughCreateSession(t *testing.T) {
	m := newMHRig(t, compOptions{})
	m.workspaceOff("vault")
	if _, err := m.apply(m.coord.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	child := m.newChild(nil)
	res, err := m.host.ResolveActual(context.Background(), m.attempt(child.ID))
	if err != nil {
		t.Fatal(err)
	}
	if res.Facts.SessionRevision != nil {
		t.Fatalf("a fresh child has no S: %+v", res.Facts)
	}
	for _, g := range res.Groups {
		if g == "vault" {
			t.Fatal("the child serves the parent's S group")
		}
	}
	if m.own.calls.Load() != 1 {
		t.Fatalf("the ownership check ran %d times", m.own.calls.Load())
	}
	// The same child with an S of its own is what the service refuses.
	if _, err = m.apply(child.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	res, err = m.host.ResolveActual(context.Background(), m.attempt(child.ID))
	if err != nil {
		t.Fatal(err)
	}
	if res.Facts.SessionRevision == nil {
		t.Fatalf("a live S on the child must be reported: %+v", res.Facts)
	}
}

func TestAbsentRequiredGroupBlocksBeforeAnyPrompt(t *testing.T) {
	m := newMHRig(t, compOptions{})
	m.workspaceOff("git")
	vendor := m.vendors[agenthub.AgentTypeClaude]
	var prompts atomic.Int32
	vendor.mu.Lock()
	vendor.onPrompt = func() { prompts.Add(1) }
	vendor.mu.Unlock()
	opensBefore, asked := vendor.opens.Load(), len(m.prompter.prompts)
	svc := agentwork.NewManagedHarnessService(newMemStore(m.wsID), m.host)
	_, err := svc.PreflightIntended(context.Background(), "a1", agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: "implementer"})
	r := wantCode(t, err, agentwork.HarnessRequiredGroupOff)
	if !reflect.DeepEqual(r.Groups, []string{"git"}) || !agentwork.ConfigurationNeeded(err) {
		t.Fatalf("groups = %v", r.Groups)
	}
	if prompts.Load() != 0 || vendor.opens.Load() != opensBefore || len(m.prompter.prompts) != asked {
		t.Fatalf("a refused preflight must start nothing: prompts=%d opens=%d->%d", prompts.Load(), opensBefore, vendor.opens.Load())
	}
	if o, _ := m.override(m.coord.ID); o != nil {
		t.Fatal("the preflight must not enable a group")
	}
	// The same configuration does not stop a role that does not need git.
	if _, err = m.intended("reviewer"); err != nil {
		t.Fatalf("a reviewer needs no git: %v", err)
	}
}

func TestWorkspaceEditDuringChildOpenIsConfigChanged(t *testing.T) {
	m := newMHRig(t, compOptions{})
	store := newMemStore(m.wsID)
	svc := agentwork.NewManagedHarnessService(store, m.host)
	ctx := context.Background()
	child := agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: "implementer"}
	if _, err := svc.BindIntended(ctx, "pk1", child, 1); err != nil {
		t.Fatal(err)
	}
	m.workspaceOff("vault") // W edited between the preflight and the child opening
	opened := m.newChild(nil)
	store.openChild(opened.ID)
	_, err := svc.BindActual(ctx, "a1", 2)
	wantCode(t, err, agentwork.HarnessConfigChanged)
	if _, ok := store.bindings["a1/actual"]; ok {
		t.Fatal("a changed configuration must not be bound")
	}
}

func TestVendorCustomizationEditDuringChildOpenIsConfigChanged(t *testing.T) {
	m := newMHRig(t, compOptions{})
	svc, store, child := m.bound(t)
	if err := svc.VerifyForEnqueue(context.Background(), "a1"); err != nil {
		t.Fatalf("an unchanged child verifies: %v", err)
	}
	c, _ := m.prefs.AgentCustomization("claude")
	c.DisabledMCP = []string{"slack"}
	if _, err := m.prefs.SetAgentCustomization("claude", c); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.VerifyForEnqueue(context.Background(), "a1"), agentwork.HarnessConfigChanged)
	if st, err := svc.CheckBinding(context.Background(), "a1"); err != nil || st.State != agentwork.HarnessStateStale || !st.RebuildRequired {
		t.Fatalf("check = %+v %v", st, err)
	}
	// An edit before the child opens is the other path.
	m2 := newMHRig(t, compOptions{})
	store2 := newMemStore(m2.wsID)
	svc2 := agentwork.NewManagedHarnessService(store2, m2.host)
	if _, err := svc2.BindIntended(context.Background(), "pk1", agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: "implementer"}, 1); err != nil {
		t.Fatal(err)
	}
	c, _ = m2.prefs.AgentCustomization("claude")
	c.DisabledMCP = []string{"slack"}
	if _, err := m2.prefs.SetAgentCustomization("claude", c); err != nil {
		t.Fatal(err)
	}
	store2.openChild(m2.newChild(nil).ID)
	_, err := svc2.BindActual(context.Background(), "a1", 2)
	wantCode(t, err, agentwork.HarnessConfigChanged)
	_ = store
	_ = child
}

func TestSameModelDifferentHarnessIsDistinguishable(t *testing.T) {
	m := newMHRig(t, compOptions{})
	cfg := map[string]string{"model": "mock-fast"}
	first := m.newChild(cfg)
	a, err := m.host.ResolveActual(context.Background(), m.attempt(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	m.workspaceOff("vault") // a different workspace profile, the same model
	second := m.newChild(cfg)
	b, err := m.host.ResolveActual(context.Background(), m.attempt(second.ID))
	if err != nil {
		t.Fatal(err)
	}
	if a.Facts.Fingerprint == b.Facts.Fingerprint || reflect.DeepEqual(a.Facts, b.Facts) {
		t.Fatalf("a different harness must read as different: %+v vs %+v", a.Facts, b.Facts)
	}
}

func TestDifferentModelSameHarnessStaysCurrent(t *testing.T) {
	m := newMHRig(t, compOptions{})
	svc, store, _ := m.bound(t)
	_ = store
	// Another child of the same harness with another model has the same facts.
	one := m.newChild(map[string]string{"model": "mock-fast"})
	two := m.newChild(nil)
	a, err := m.host.ResolveActual(context.Background(), m.attempt(one.ID))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.host.ResolveActual(context.Background(), m.attempt(two.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Facts, b.Facts) {
		t.Fatalf("the model is not part of the harness facts: %+v vs %+v", a.Facts, b.Facts)
	}
	if st, err := svc.CheckBinding(context.Background(), "a1"); err != nil || st.State != agentwork.HarnessStateCurrent {
		t.Fatalf("creating more children must not stale the binding: %+v %v", st, err)
	}
}

func TestStaleEndpointIsNotLive(t *testing.T) {
	ctx := context.Background()
	for name, disturb := range map[string]func(m *mhRig, id string){
		"paused": func(m *mhRig, id string) {
			if err := m.agents.CoordinateServing(ctx, id, func(tx *agenthub.ServingTxn) error { return tx.Quarantine() }); err != nil {
				t.Fatal(err)
			}
		},
		"pending": func(m *mhRig, id string) {
			if err := m.agents.CoordinateServing(ctx, id, func(tx *agenthub.ServingTxn) error { tx.MarkPending(); return nil }); err != nil {
				t.Fatal(err)
			}
		},
		"closed": func(m *mhRig, id string) {
			if err := m.agents.CloseSession(m.ui(), id); err != nil {
				t.Fatal(err)
			}
		},
	} {
		m := newMHRig(t, compOptions{})
		child := m.newChild(nil)
		if _, err := m.host.ResolveActual(ctx, m.attempt(child.ID)); err != nil {
			t.Fatalf("%s: a fresh child is live: %v", name, err)
		}
		disturb(m, child.ID)
		_, err := m.host.ResolveActual(ctx, m.attempt(child.ID))
		r := refusalOf(t, err)
		want := agentwork.HarnessEndpointNotLive
		if name == "closed" {
			// A closed session has no live runtime and nothing recorded by a scripted
			// vendor, so its mode is unreadable and the mode rule (which runs first)
			// refuses it: fail closed, still not a configuration to fix.
			want = agentwork.HarnessChildMode
		}
		if r.Code != want || agentwork.ConfigurationNeeded(err) {
			t.Fatalf("%s: code = %s, want %s", name, r.Code, want)
		}
	}
}

func TestRevokedGroupAfterLaunchIsRefusedAtEnqueue(t *testing.T) {
	ctx := context.Background()
	// The endpoint stops serving a required group: seen from the served surface.
	m := newMHRig(t, compOptions{})
	svc, _, child := m.bound(t)
	real := m.agents
	m.host.hub = func() managedHarnessHub {
		return modeHub{managedHarnessHub: real, served: func(id string) ([]string, bool) {
			tools, live := real.ServedTools(id)
			var kept []string
			for _, tool := range tools {
				if harness.Group(tool) != "git" {
					kept = append(kept, tool)
				}
			}
			return kept, live
		}}
	}
	r := wantCode(t, svc.VerifyForEnqueue(ctx, "a1"), agentwork.HarnessRequiredGroupOff)
	if !reflect.DeepEqual(r.Groups, []string{"git"}) {
		t.Fatalf("groups = %v", r.Groups)
	}
	_ = child
	// A saved workspace edit that drops the group after launch is a reload-required
	// change: refused as changed, and nothing was started to learn it.
	m2 := newMHRig(t, compOptions{})
	svc2, _, _ := m2.bound(t)
	m2.workspaceOff("git")
	wantCode(t, svc2.VerifyForEnqueue(ctx, "a1"), agentwork.HarnessConfigChanged)
}

func TestConfigReadFailureFailsClosedNotDefault(t *testing.T) {
	m := newMHRig(t, compOptions{})
	child := m.newChild(nil)
	m.fault.failReads.Store(true)
	_, err := m.intended("implementer")
	wantCode(t, err, agentwork.HarnessUnavailable)
	if agentwork.ConfigurationNeeded(err) {
		t.Fatal("an unreadable configuration is not a configuration to fix")
	}
	_, err = m.host.ResolveActual(context.Background(), m.attempt(child.ID))
	wantCode(t, err, agentwork.HarnessUnavailable)
	// Through the service the preflight is refused, never passed as a default.
	svc := agentwork.NewManagedHarnessService(newMemStore(m.wsID), m.host)
	_, err = svc.PreflightIntended(context.Background(), "a1", agentwork.IntendedChild{Vendor: string(agenthub.AgentTypeClaude), Role: "implementer"})
	wantCode(t, err, agentwork.HarnessUnavailable)
	// An unsupported vendor is the same refusal.
	m.fault.failReads.Store(false)
	_, err = m.host.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: "no-such-vendor", Role: "implementer"})
	wantCode(t, err, agentwork.HarnessUnavailable)
}

func TestNilHarnessReportsNotConfigured(t *testing.T) {
	// Production today: Agent Hub has no harness attached.
	m := newMHRig(t, compOptions{noAttach: true})
	if m.agents.Harness != nil {
		t.Fatal("the rig must leave the harness unattached")
	}
	_, err := m.intended("implementer")
	wantCode(t, err, agentwork.HarnessNotConfigured)
	child := m.newChild(nil)
	_, err = m.host.ResolveActual(context.Background(), m.attempt(child.ID))
	wantCode(t, err, agentwork.HarnessNotConfigured)
	if !agentwork.ConfigurationNeeded(err) {
		t.Fatal("not configured is reported as configuration-needed")
	}
	// No container, no hub: the same answer, never a panic.
	bare := newManagedHarnessHost(&Container{}, &fakeOwnership{})
	_, err = bare.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: "claude", Role: "implementer"})
	wantCode(t, err, agentwork.HarnessNotConfigured)
	_, err = bare.ResolveActual(context.Background(), m.attempt(child.ID))
	wantCode(t, err, agentwork.HarnessNotConfigured)
	nilContainer := newManagedHarnessHost(nil, &fakeOwnership{})
	_, err = nilContainer.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: "claude", Role: "implementer"})
	wantCode(t, err, agentwork.HarnessNotConfigured)
	// Reading at call time: attaching later is seen without rewiring.
	late := newManagedHarnessHost(&Container{AgentHub: m.agents}, &fakeOwnership{})
	_, err = late.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: "claude", Role: "implementer"})
	wantCode(t, err, agentwork.HarnessNotConfigured)
	if err = m.asm.Attach(m.agents); err != nil {
		t.Fatal(err)
	}
	if _, err = late.ResolveIntended(context.Background(), m.scope(), agentwork.IntendedChild{Vendor: "claude", Role: "implementer"}); err != nil {
		t.Fatalf("attached later, resolved without rewiring: %v", err)
	}
}

func TestUnknownRoleGroupNameIsUnavailable(t *testing.T) {
	m := newMHRig(t, compOptions{})
	m.host.roles = map[string][]string{"implementer": {"agenthub", "nosuchgroup"}}
	_, err := m.intended("implementer")
	wantCode(t, err, agentwork.HarnessUnavailable)
	if _, err = m.intended("reviewer"); err == nil {
		t.Fatal("a role missing from the table must not pass with an empty requirement")
	}
	wantCode(t, err, agentwork.HarnessUnavailable)
	svc := agentwork.NewManagedHarnessService(newMemStore(m.wsID), m.host)
	_, err = svc.PreflightIntended(context.Background(), "a1", agentwork.IntendedChild{Vendor: "claude", Role: "implementer"})
	wantCode(t, err, agentwork.HarnessUnavailable)
}

func TestChildScopeAndGrantsReuseTheOwnershipChecks(t *testing.T) {
	m := newMHRig(t, compOptions{})
	child := m.newChild(nil)
	attempt := m.attempt(child.ID)
	m.own.err = agentwork.ErrManagedLeaseScope // another workspace, wider grants, another parent...
	_, err := m.host.ResolveActual(context.Background(), attempt)
	wantCode(t, err, agentwork.HarnessChildScope)
	got, _ := m.own.last.Load().(agentwork.Attempt)
	if m.own.calls.Load() != 1 || got.ID != attempt.ID || got.SessionID != child.ID {
		t.Fatalf("the ownership check must see the attempt: calls=%d %+v", m.own.calls.Load(), got)
	}
	m.own.err = errors.New("git: not a repository /private/path")
	_, err = m.host.ResolveActual(context.Background(), attempt)
	if r := wantCode(t, err, agentwork.HarnessChildScope); r.Groups != nil {
		t.Fatalf("no detail may travel with the refusal: %+v", r)
	}
	m.own.err = agentwork.ErrManagedHostUnavailable
	_, err = m.host.ResolveActual(context.Background(), attempt)
	wantCode(t, err, agentwork.HarnessUnavailable)
	// A session that is not a child of anyone is not a managed child.
	m.own.err = nil
	_, err = m.host.ResolveActual(context.Background(), m.attempt(m.coord.ID))
	wantCode(t, err, agentwork.HarnessChildScope)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	m.own.err = cancelled.Err()
	if _, err = m.host.ResolveActual(cancelled, attempt); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context is returned as it is: %v", err)
	}
}

func TestChildModeWiderThanCoordinatorIsRefused(t *testing.T) {
	m := newMHRig(t, compOptions{})
	child := m.newChild(nil)
	real := m.agents
	use := func(modes map[string]string) {
		m.host.hub = func() managedHarnessHub { return modeHub{managedHarnessHub: real, modes: modes} }
	}
	attempt := m.attempt(child.ID)
	ctx := context.Background()
	use(map[string]string{m.coord.ID: "default", child.ID: "bypassPermissions"})
	_, err := m.host.ResolveActual(ctx, attempt)
	wantCode(t, err, agentwork.HarnessChildMode)
	use(map[string]string{m.coord.ID: "default"}) // the child has no mode concept while its coordinator has one
	_, err = m.host.ResolveActual(ctx, attempt)
	wantCode(t, err, agentwork.HarnessChildMode)
	use(map[string]string{m.coord.ID: "auto", child.ID: "default"})
	if _, err = m.host.ResolveActual(ctx, attempt); err != nil {
		t.Fatalf("a stricter child passes: %v", err)
	}
	use(map[string]string{}) // neither side has a mode concept
	if _, err = m.host.ResolveActual(ctx, attempt); err != nil {
		t.Fatalf("no mode concept on either side passes: %v", err)
	}
	if agentwork.ConfigurationNeeded(&agentwork.HarnessRefusal{Code: agentwork.HarnessChildMode}) {
		t.Fatal("a mode refusal is not fixed by a harness setting")
	}
}

func TestRoleRequirementsAreNotEveryGroup(t *testing.T) {
	m := newMHRig(t, compOptions{})
	reg, err := m.inventory()
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	for _, tool := range reg.Tools {
		all[harness.Group(tool.Name)] = true
	}
	for _, role := range []string{"coordinator", "implementer", "reviewer", "integrator"} {
		groups, err := m.host.RequiredGroups(context.Background(), role)
		if err != nil || len(groups) == 0 {
			t.Fatalf("%s: %v %v", role, groups, err)
		}
		if len(groups) >= len(all) {
			t.Fatalf("%s requires every group (%d of %d)", role, len(groups), len(all))
		}
		for _, g := range groups {
			if !all[g] {
				t.Fatalf("%s requires %q which is not a registry group", role, g)
			}
			if g == "harness" {
				t.Fatalf("%s must not require the always-served proposal tool", role)
			}
		}
	}
	if len(managedRoleGroups) != 4 {
		t.Fatalf("the table must cover exactly the four ledger roles, has %d", len(managedRoleGroups))
	}
	if _, err = m.host.RequiredGroups(context.Background(), "nobody"); err == nil {
		t.Fatal("an unknown role has no requirement")
	}
}

func TestManagedHarnessFactsNeverCarryTheSessionOrWorkspaceDefinition(t *testing.T) {
	m := newMHRig(t, compOptions{})
	m.workspaceOff("vault")
	res, err := m.intended("implementer")
	if err != nil {
		t.Fatal(err)
	}
	if res.Facts.WorkspaceID != m.wsID || res.Facts.Vendor != "claude" || res.Facts.WorkspaceRevision == nil || *res.Facts.WorkspaceRevision < 1 {
		t.Fatalf("facts = %+v", res.Facts)
	}
	if res.Facts.Default || res.Facts.FactsComplete || res.Facts.SourceSchema != harness.SnapshotSchema {
		t.Fatalf("a profiled workspace is not the default and production facts are incomplete: %+v", res.Facts)
	}
}
