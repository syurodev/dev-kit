package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/identity"
)

// modeRuntime is a mock agent with a permission mode set. It keeps the mode the
// way the vendors' runtimes do and announces a change as current_mode_update.
type modeRuntime struct {
	SessionRuntime
	mu       sync.Mutex
	modes    []acp.SessionMode
	current  string
	refuse   bool
	sets     []string
	closed   bool
	silent   bool // simulate a runtime that changes mode without announcing it
	handlers RuntimeHandlers
}

func (r *modeRuntime) SetHandlers(h RuntimeHandlers) {
	r.SessionRuntime.SetHandlers(h)
	r.mu.Lock()
	r.handlers = h
	r.mu.Unlock()
}

func (r *modeRuntime) Modes() *acp.SessionModeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &acp.SessionModeState{CurrentModeID: r.current, AvailableModes: r.modes}
}

func (r *modeRuntime) SetMode(_ context.Context, id string) error {
	r.mu.Lock()
	r.sets = append(r.sets, id)
	if r.refuse {
		r.mu.Unlock()
		return ErrInvalidConfigOption
	}
	r.current = id
	h := r.handlers
	r.mu.Unlock()
	if !r.silent && h.OnUpdate != nil {
		body, _ := json.Marshal(map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": id})
		h.OnUpdate(body, nil)
	}
	return nil
}

func (r *modeRuntime) SetConfigOption(context.Context, string, string) error { return nil }

func (r *modeRuntime) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return r.SessionRuntime.Close(ctx)
}

func (r *modeRuntime) state() (current string, sets []string, closed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current, append([]string(nil), r.sets...), r.closed
}

// modeAdapter stands in for one vendor: its type, the modes it offers and the
// mode a fresh session opens in.
type modeAdapter struct {
	*MockAdapter
	typ     AgentType
	offered []string
	start   string
	refuse  bool // children opened after this is set refuse every set_mode
	mu      sync.Mutex
	opened  []*modeRuntime
}

func (a *modeAdapter) Type() AgentType { return a.typ }

func (a *modeAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	inner, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	rt := &modeRuntime{SessionRuntime: inner, current: a.start, refuse: a.refuse}
	for _, id := range a.offered {
		rt.modes = append(rt.modes, acp.SessionMode{ID: id, Name: id})
	}
	a.mu.Lock()
	a.opened = append(a.opened, rt)
	a.mu.Unlock()
	return rt, nil
}

func (a *modeAdapter) last() *modeRuntime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opened[len(a.opened)-1]
}

var (
	claudeLikeModes = []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions"}
	codexLikeModes  = []string{codexModeAsk, codexModeAuto, codexModeFull}
)

type modeFixture struct {
	svc    *Service
	claude *modeAdapter // Claude-like: modes default…bypassPermissions, opens in auto
	codex  *modeAdapter // Codex-like: ask / auto / bypassPermissions, opens in auto
}

func newModeFixture(t *testing.T) *modeFixture {
	t.Helper()
	f := &modeFixture{
		claude: &modeAdapter{MockAdapter: NewMockAdapter(), typ: AgentTypeClaude, offered: claudeLikeModes, start: "auto"},
		codex:  &modeAdapter{MockAdapter: NewMockAdapter(), typ: AgentTypeCodex, offered: codexLikeModes, start: codexModeAuto},
	}
	reg := NewRegistry()
	reg.Register(f.claude)
	reg.Register(f.codex)
	f.svc = NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = f.svc.Shutdown(context.Background()) })
	return f
}

// parent opens a session the person made, in mode, as the parent of the test.
func (f *modeFixture) parent(t *testing.T, vendor AgentType, mode string) (AgentSession, context.Context) {
	t.Helper()
	ctx := context.Background()
	p, err := f.svc.CreateSession(ctx, CreateInput{AgentType: vendor, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetMode(ctx, p.ID, mode); err != nil {
		t.Fatal(err)
	}
	return p, identity.WithCaller(ctx, identity.Agent(p.ID))
}

func (f *modeFixture) adapter(vendor AgentType) *modeAdapter {
	if vendor == AgentTypeCodex {
		return f.codex
	}
	return f.claude
}

// storedMode is the mode the child's own recorded state says it is in.
func storedMode(t *testing.T, svc *Service, id string) string {
	t.Helper()
	_ = svc.recorder.Flush(context.Background(), id)
	entry, found, err := svc.store.FindTranscriptKind(context.Background(), id, UpdateCurrentMode)
	if err != nil || !found {
		t.Fatalf("no recorded mode for %s: %v", id, err)
	}
	return storedUpdate(entry).Mode
}

func TestAChildStartsInItsSameVendorParentsMode(t *testing.T) {
	for _, tc := range []struct {
		vendor AgentType
		mode   string
	}{
		{AgentTypeCodex, codexModeAsk},
		{AgentTypeCodex, codexModeFull},
		{AgentTypeCodex, codexModeAuto},
		{AgentTypeClaude, "default"},
		{AgentTypeClaude, "plan"},
		{AgentTypeClaude, "acceptEdits"},
		{AgentTypeClaude, "bypassPermissions"},
	} {
		t.Run(string(tc.vendor)+"/"+tc.mode, func(t *testing.T) {
			f := newModeFixture(t)
			parent, ctx := f.parent(t, tc.vendor, tc.mode)
			child, err := f.svc.CreateSession(ctx, CreateInput{AgentType: tc.vendor, Cwd: parent.ProjectPath})
			if err != nil {
				t.Fatal(err)
			}
			if got, _, closed := f.adapter(tc.vendor).last().state(); got != tc.mode || closed {
				t.Fatalf("child runtime is in %q (closed %v), want %q", got, closed, tc.mode)
			}
			if got := storedMode(t, f.svc, child.ID); got != tc.mode {
				t.Fatalf("child's recorded mode = %q, want %q", got, tc.mode)
			}
		})
	}
}

// The handshake said "auto"; the person (or the agent's own plan-mode exit)
// moved the parent afterwards. The child follows where the parent is now.
func TestAChildFollowsAParentWhoseModeChangedAfterTheHandshake(t *testing.T) {
	f := newModeFixture(t)
	parent, ctx := f.parent(t, AgentTypeClaude, "auto")
	if err := f.svc.SetMode(context.Background(), parent.ID, "default"); err != nil {
		t.Fatal(err)
	}
	child, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeClaude, Cwd: parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	if got, sets, _ := f.claude.last().state(); got != "default" || len(sets) != 1 {
		t.Fatalf("child is in %q after %v, want default (the parent's mode now, not the handshake's auto)", got, sets)
	}
	if got := storedMode(t, f.svc, child.ID); got != "default" {
		t.Fatalf("child's recorded mode = %q", got)
	}
}

func TestAChildAcrossVendorsTakesTheMatchingMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		parentVendor AgentType
		parentMode   string
		childVendor  AgentType
		want         string // the child's mode after creation
		wantSet      bool   // whether the service had to change it
	}{
		{"auto stays auto", AgentTypeClaude, "auto", AgentTypeCodex, codexModeAuto, false},
		{"claude bypass is codex full", AgentTypeClaude, "bypassPermissions", AgentTypeCodex, codexModeFull, true},
		{"codex full is claude bypass", AgentTypeCodex, codexModeFull, AgentTypeClaude, "bypassPermissions", true},
		{"claude default is codex ask", AgentTypeClaude, "default", AgentTypeCodex, codexModeAsk, true},
		{"codex ask is claude default", AgentTypeCodex, codexModeAsk, AgentTypeClaude, "default", true},
		// Plan has no exact match; acceptEdits → auto is the product mapping.
		{"claude plan gets codex ask", AgentTypeClaude, "plan", AgentTypeCodex, codexModeAsk, true},
		{"claude acceptEdits gets codex auto", AgentTypeClaude, "acceptEdits", AgentTypeCodex, codexModeAuto, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newModeFixture(t)
			parent, ctx := f.parent(t, tc.parentVendor, tc.parentMode)
			if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: tc.childVendor, Cwd: parent.ProjectPath}); err != nil {
				t.Fatal(err)
			}
			got, sets, _ := f.adapter(tc.childVendor).last().state()
			if got != tc.want || (len(sets) > 0) != tc.wantSet {
				t.Fatalf("child is in %q after %v, want %q (set: %v)", got, sets, tc.want, tc.wantSet)
			}
		})
	}
}

func TestAnUnmappedModeLeavesTheChildOnItsDefaultOnlyWhenThatIsNoMorePermissive(t *testing.T) {
	// A vendor with no asking mode at all.
	f := newModeFixture(t)
	f.codex.offered = []string{codexModeAuto, codexModeFull}

	// Unknown modes stay unranked and cannot select a cross-vendor mode.
	// Only an Ask / default / plan parent fails closed on a missing mapping.
	parent, ctx := f.parent(t, AgentTypeClaude, "futureMode")
	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
		t.Fatalf("unranked parent: %v", err)
	}
	if got, sets, _ := f.codex.last().state(); got != codexModeAuto || len(sets) != 0 {
		t.Fatalf("child is in %q after %v, want its untouched default", got, sets)
	}

	// Ask has no equal here, and the child would open more permissive: refused.
	strict, strictCtx := f.parent(t, AgentTypeClaude, "default")
	_, err := f.svc.CreateSession(strictCtx, CreateInput{AgentType: AgentTypeCodex, Cwd: strict.ProjectPath})
	if !errors.Is(err, ErrModeNotInherited) {
		t.Fatalf("default parent: err = %v, want ErrModeNotInherited", err)
	}
	if _, _, closed := f.codex.last().state(); !closed {
		t.Fatal("the refused child's agent was left running")
	}
}

func TestARefusedModeNeverLeavesAnAskParentsChildMorePermissive(t *testing.T) {
	f := newModeFixture(t)
	parent, ctx := f.parent(t, AgentTypeCodex, codexModeAsk)
	f.codex.refuse = true
	_, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath})
	if !errors.Is(err, ErrModeNotInherited) {
		t.Fatalf("err = %v, want ErrModeNotInherited", err)
	}
	if _, sets, closed := f.codex.last().state(); !closed || len(sets) != 1 {
		t.Fatalf("closed = %v after sets %v: the child was not shut down", closed, sets)
	}
	sessions, err := f.svc.store.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("%d sessions stored, want only the parent", len(sessions))
	}
}

func TestARefusedModeNeverLeavesAPlanParentsChildMorePermissive(t *testing.T) {
	f := newModeFixture(t)
	parent, ctx := f.parent(t, AgentTypeClaude, "plan")
	f.codex.refuse = true
	_, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath})
	if !errors.Is(err, ErrModeNotInherited) {
		t.Fatalf("err = %v, want ErrModeNotInherited", err)
	}
	if _, _, closed := f.codex.last().state(); !closed {
		t.Fatal("the refused Plan parent's child was left running")
	}
}

func TestARefusedModeDoesNotFailAChildOfALessStrictParent(t *testing.T) {
	f := newModeFixture(t)
	parent, ctx := f.parent(t, AgentTypeCodex, codexModeFull)
	f.codex.refuse = true
	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
		t.Fatalf("a Full auto parent's child must still start on the default: %v", err)
	}
	if got, _, closed := f.codex.last().state(); got != codexModeAuto || closed {
		t.Fatalf("child is in %q (closed %v), want the untouched default", got, closed)
	}
}

// A refusal is no reason to fail a child that is already as strict.
func TestARefusedModeIsFineWhenTheChildAlreadyStartsAsStrict(t *testing.T) {
	f := newModeFixture(t)
	f.codex.start = codexModeAsk
	parent, ctx := f.parent(t, AgentTypeCodex, codexModeAsk)
	f.codex.refuse = true
	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
		t.Fatal(err)
	}
}

// Whatever a person creates keeps the vendor's default, parent or no parent.
func TestASessionAPersonCreatesKeepsTheVendorDefault(t *testing.T) {
	f := newModeFixture(t)
	parent, _ := f.parent(t, AgentTypeCodex, codexModeAsk)
	ctx := context.Background()

	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got, sets, _ := f.codex.last().state(); got != codexModeAuto || len(sets) != 0 {
		t.Fatalf("a fresh session is in %q after %v, want the default", got, sets)
	}
	// Even one the person puts under a parent.
	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath, ParentSessionID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	if got, sets, _ := f.codex.last().state(); got != codexModeAuto || len(sets) != 0 {
		t.Fatalf("a person's sub-session is in %q after %v, want the default", got, sets)
	}
}

// A parent whose vendor has no modes has nothing to hand on.
func TestAParentWithoutModesAddsNoModeToItsChild(t *testing.T) {
	f := newModeFixture(t)
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	reg.Register(f.codex)
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	parent, err := svc.CreateSession(context.Background(), CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent(parent.ID))
	if _, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
		t.Fatal(err)
	}
	if got, sets, _ := f.codex.last().state(); got != codexModeAuto || len(sets) != 0 {
		t.Fatalf("child is in %q after %v", got, sets)
	}
}

func TestChildPrefersLiveModeOverStaleTranscript(t *testing.T) {
	f := newModeFixture(t)
	parent, ctx := f.parent(t, AgentTypeClaude, "default")
	rt := f.claude.last()
	rt.silent = true
	if err := f.svc.SetMode(ctx, parent.ID, "bypassPermissions"); err != nil {
		t.Fatal(err)
	}
	if got := storedMode(t, f.svc, parent.ID); got != "default" {
		t.Fatalf("test needs stale recorded default, got %q", got)
	}
	if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
		t.Fatal(err)
	}
	if got, sets, _ := f.codex.last().state(); got != codexModeFull || len(sets) != 1 {
		t.Fatalf("child mode %q after %v, want live parent's bypassPermissions", got, sets)
	}
}

func TestChildUsesStoredModeWithoutLiveParentAndAfterResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "no runtime", true: "resumed"}[resume], func(t *testing.T) {
			f := newModeFixture(t)
			parent, ctx := f.parent(t, AgentTypeClaude, "bypassPermissions")
			_ = f.svc.recorder.Flush(ctx, parent.ID)
			f.svc.mu.Lock()
			old := f.svc.runtimes[parent.ID]
			delete(f.svc.runtimes, parent.ID)
			f.svc.mu.Unlock()
			_ = old.Close(ctx)
			if resume {
				if _, err := f.svc.resumeRuntime(ctx, parent); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeCodex, Cwd: parent.ProjectPath}); err != nil {
				t.Fatal(err)
			}
			if got, sets, _ := f.codex.last().state(); got != codexModeFull || len(sets) != 1 {
				t.Fatalf("child mode %q after %v, want stored parent's bypassPermissions", got, sets)
			}
		})
	}
}

func TestCrossVendorModeTable(t *testing.T) {
	claudeModes := make([]acp.SessionMode, 0, len(claudeLikeModes))
	for _, id := range claudeLikeModes {
		if modeRank(id) < 0 {
			t.Fatalf("advertised Claude mode %q is unranked", id)
		}
		claudeModes = append(claudeModes, acp.SessionMode{ID: id})
	}
	for _, vendor := range []AgentType{AgentTypeCodex, AgentTypeOpenCode} {
		otherModes := []acp.SessionMode{{ID: "ask"}, {ID: "auto"}, {ID: "bypassPermissions"}}
		for from, want := range map[string]string{
			"plan": "ask", "default": "ask", "acceptEdits": "auto", "auto": "auto",
			"bypassPermissions": "bypassPermissions", "dontAsk": "ask",
		} {
			got, why := inheritedModeID(parentMode{Vendor: AgentTypeClaude, ID: from}, vendor, otherModes)
			if got != want || why != "" {
				t.Errorf("Claude %s → %s = %q (%s), want %q", from, vendor, got, why, want)
			}
		}
		for from, want := range map[string]string{"ask": "default", "auto": "auto", "bypassPermissions": "bypassPermissions"} {
			got, why := inheritedModeID(parentMode{Vendor: vendor, ID: from}, AgentTypeClaude, claudeModes)
			if got != want || why != "" {
				t.Errorf("%s %s → Claude = %q (%s), want %q", vendor, from, got, why, want)
			}
		}
		got, _ := inheritedModeID(parentMode{Vendor: vendor, ID: "auto"}, AgentTypeClaude, []acp.SessionMode{{ID: "acceptEdits"}, {ID: "default"}})
		if got != "acceptEdits" {
			t.Errorf("%s auto → older Claude = %q", vendor, got)
		}
	}
	if modeRank("futureMode") != -1 {
		t.Fatal("unknown modes must stay unranked")
	}
}
