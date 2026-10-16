package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"devkit/internal/identity"
	"devkit/internal/preferences"
)

// choiceRuntime is a mock agent that keeps a model and an effort and
// announces both, whole, the way Codex's runtime does.
type choiceRuntime struct {
	SessionRuntime
	mu       sync.Mutex
	values   map[string]string
	handlers RuntimeHandlers
}

func (r *choiceRuntime) SetHandlers(h RuntimeHandlers) {
	r.SessionRuntime.SetHandlers(h)
	r.mu.Lock()
	r.handlers = h
	r.mu.Unlock()
	r.announce()
}

func (r *choiceRuntime) SetMode(context.Context, string) error { return nil }

func (r *choiceRuntime) SetConfigOption(_ context.Context, id, value string) error {
	r.mu.Lock()
	r.values[id] = value
	r.mu.Unlock()
	r.announce()
	return nil
}

func (r *choiceRuntime) value(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[id]
}

func (r *choiceRuntime) announce() {
	r.mu.Lock()
	h := r.handlers
	body, _ := json.Marshal(map[string]any{"sessionUpdate": "config_option_update", "configOptions": []map[string]any{
		{"id": "model", "type": "select", "currentValue": r.values["model"]},
		{"id": "reasoning_effort", "type": "select", "currentValue": r.values["reasoning_effort"]},
	}})
	r.mu.Unlock()
	if h.OnUpdate != nil {
		h.OnUpdate(body, nil)
	}
}

// choiceAdapter is the mock vendor with a model catalog it counts the reads of.
type choiceAdapter struct {
	*MockAdapter
	reads   atomic.Int32
	failing atomic.Bool
	mu      sync.Mutex
	opened  []*choiceRuntime
}

func (a *choiceAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	inner, err := a.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	rt := &choiceRuntime{SessionRuntime: inner, values: map[string]string{"model": "m-mid", "reasoning_effort": "medium"}}
	a.mu.Lock()
	a.opened = append(a.opened, rt)
	a.mu.Unlock()
	return rt, nil
}

func (a *choiceAdapter) last() *choiceRuntime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opened[len(a.opened)-1]
}

func (a *choiceAdapter) ConfigCatalog(context.Context, OpenSpec) ([]ConfigOption, error) {
	a.reads.Add(1)
	if a.failing.Load() {
		return nil, ErrAgentAuthRequired
	}
	level := func(names ...string) []ConfigSelectOption {
		var out []ConfigSelectOption
		for _, n := range names {
			out = append(out, ConfigSelectOption{Value: n})
		}
		return out
	}
	return []ConfigOption{{ID: "model", Category: "model", Type: "select", DefaultValue: "m-mid", CurrentValue: json.RawMessage(`"m-mid"`), Options: []ConfigSelectOption{
		{Value: "m-small", Name: "Small", DefaultEffort: "low", SupportedEfforts: level("low", "medium")},
		{Value: "m-mid", Name: "Mid", DefaultEffort: "medium", SupportedEfforts: level("low", "medium", "high")},
		{Value: "m-big", Name: "Big", DefaultEffort: "high", SupportedEfforts: level("medium", "high", "xhigh")},
	}}}, nil
}

type choiceFixture struct {
	svc     *Service
	adapter *choiceAdapter
	parent  AgentSession
	ctx     context.Context // the parent agent's calls
	profile *preferences.AgentModelProfiles
}

func newChoiceFixture(t *testing.T) *choiceFixture {
	t.Helper()
	ctx := context.Background()
	adapter := &choiceAdapter{MockAdapter: NewMockAdapter()}
	reg := NewRegistry()
	reg.Register(adapter)
	svc := NewService(reg, newInternalStore(t), nil)
	t.Cleanup(func() { _ = svc.Shutdown(ctx) })
	f := &choiceFixture{svc: svc, adapter: adapter, profile: &preferences.AgentModelProfiles{}}
	svc.ModelProfiles = func() (preferences.AgentModelProfiles, error) { return *f.profile, nil }
	parent, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	f.parent = parent
	f.ctx = identity.WithCaller(ctx, identity.Agent(parent.ID))
	return f
}

func (f *choiceFixture) plan(t *testing.T, config map[string]string, profile string) (ConfigPlan, error) {
	t.Helper()
	return f.svc.PlanModelChoice(f.ctx, ModelChoiceInput{Vendor: AgentTypeMock, Config: config, Profile: profile})
}

func TestModelCatalogIsCachedAndFailuresAreShortLived(t *testing.T) {
	f := newChoiceFixture(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if got := f.svc.ModelCatalog(ctx, AgentTypeMock); got.Status != ModelsOK || len(got.Models) != 3 || got.DefaultModel != "m-mid" {
			t.Fatalf("catalog = %+v", got)
		}
	}
	if n := f.adapter.reads.Load(); n != 1 {
		t.Fatalf("three calls started %d throw-away agents, want 1", n)
	}
	f.svc.ForgetModelCatalog(AgentTypeMock)
	f.svc.ModelCatalog(ctx, AgentTypeMock)
	if n := f.adapter.reads.Load(); n != 2 {
		t.Fatalf("after a sign-in the list was not read again (reads = %d)", n)
	}

	f.svc.ForgetModelCatalog(AgentTypeMock)
	f.adapter.failing.Store(true)
	for i := 0; i < 3; i++ {
		if got := f.svc.ModelCatalog(ctx, AgentTypeMock); got.Status != ModelsSignInRequired || got.Reason == "" {
			t.Fatalf("failure = %+v", got)
		}
	}
	if n := f.adapter.reads.Load(); n != 3 {
		t.Fatalf("a signed-out vendor was respawned %d times in a row", n-2)
	}
}

func TestListModelsNamesTheCallersOwnChoice(t *testing.T) {
	f := newChoiceFixture(t)
	listing, err := f.svc.ListModels(f.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Vendors) != 1 || listing.Vendors[0].Vendor != AgentTypeMock {
		t.Fatalf("vendors = %+v", listing.Vendors)
	}
	if listing.Current == nil || listing.Current.Model != "m-mid" || listing.Current.Effort != "medium" {
		t.Fatalf("current = %+v, want the parent's m-mid / medium", listing.Current)
	}
	if _, err := f.svc.ListModels(f.ctx, "nope"); !errors.Is(err, ErrVendorUnknown) {
		t.Fatalf("unknown vendor: %v", err)
	}
}

func TestAChildInheritsItsParentsModelAndEffort(t *testing.T) {
	f := newChoiceFixture(t)
	plan, err := f.plan(t, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Inherited || plan.NeedsApproval || plan.Config["model"] != "m-mid" || plan.Config["reasoning_effort"] != "medium" {
		t.Fatalf("plan = %+v", plan)
	}
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath})
	if err != nil {
		t.Fatal(err)
	}
	if rt := f.adapter.last(); child.ParentSessionID == nil || rt.value("model") != "m-mid" || rt.value("reasoning_effort") != "medium" {
		t.Fatalf("child runs %q / %q", rt.value("model"), rt.value("reasoning_effort"))
	}
}

// The policy: nothing above the parent without the person; effort has an
// order, models have none, so any other model asks.
func TestWhatAnAgentMayChooseWithoutAsking(t *testing.T) {
	f := newChoiceFixture(t)
	for _, tc := range []struct {
		name   string
		config map[string]string
		ask    bool
	}{
		{"same model, lower effort", map[string]string{"model": "m-mid", "reasoning_effort": "low"}, false},
		{"same model, same effort", map[string]string{"model": "m-mid", "reasoning_effort": "medium"}, false},
		{"same model, higher effort", map[string]string{"model": "m-mid", "reasoning_effort": "high"}, true},
		{"effort alone, lower", map[string]string{"reasoning_effort": "low"}, false},
		{"effort alone, higher", map[string]string{"reasoning_effort": "high"}, true},
		{"a cheaper model is still another model", map[string]string{"model": "m-small"}, true},
		{"a dearer model", map[string]string{"model": "m-big", "reasoning_effort": "high"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := f.plan(t, tc.config, "")
			if err != nil {
				t.Fatal(err)
			}
			if plan.NeedsApproval != tc.ask || (tc.ask && len(plan.Why) == 0) {
				t.Fatalf("NeedsApproval = %v (%v), want %v", plan.NeedsApproval, plan.Why, tc.ask)
			}
		})
	}
}

func TestAWrongModelOrEffortSaysWhatWouldWork(t *testing.T) {
	f := newChoiceFixture(t)
	_, err := f.plan(t, map[string]string{"model": "gpt-9"}, "")
	if !errors.Is(err, ErrInvalidConfigOption) || !strings.Contains(err.Error(), "m-small, m-mid, m-big") {
		t.Fatalf("unknown model: %v", err)
	}
	_, err = f.plan(t, map[string]string{"model": "m-small", "reasoning_effort": "xhigh"}, "")
	if !errors.Is(err, ErrInvalidConfigOption) || !strings.Contains(err.Error(), "low, medium") {
		t.Fatalf("effort the model does not take: %v", err)
	}
	f.adapter.failing.Store(true)
	f.svc.ForgetModelCatalog(AgentTypeMock)
	if _, err = f.plan(t, map[string]string{"model": "m-mid"}, ""); !errors.Is(err, ErrInvalidConfigOption) || !strings.Contains(err.Error(), "signed in") {
		t.Fatalf("vendor that cannot be asked: %v", err)
	}
}

func TestAProfileIsThePersonsOwnChoiceAndNeedsNoApproval(t *testing.T) {
	f := newChoiceFixture(t)
	f.profile.Profiles = []preferences.AgentModelProfile{
		{ID: "review", Name: "Code review", Vendor: "mock", Model: "m-small", Effort: "low"},
		{ID: "old", Name: "Old", Vendor: "mock", Model: "m-gone"},
		{ID: "other", Name: "Elsewhere", Vendor: "codex", Model: "x"},
	}
	plan, err := f.plan(t, nil, "code review")
	if err != nil {
		t.Fatal(err)
	}
	if plan.NeedsApproval || plan.Profile != "Code review" || plan.Config["model"] != "m-small" || plan.Config["reasoning_effort"] != "low" {
		t.Fatalf("plan = %+v", plan)
	}
	for name, tc := range map[string]struct {
		config  map[string]string
		profile string
		want    string
	}{
		"unknown":       {nil, "nothing", "Code review"},
		"model beside":  {map[string]string{"model": "m-big"}, "review", "not both"},
		"other vendor":  {nil, "other", "codex"},
		"model is gone": {nil, "old", "m-gone"},
	} {
		_, err := f.plan(t, tc.config, tc.profile)
		if !errors.Is(err, ErrModelProfileUnknown) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it to mention %q", name, err, tc.want)
		}
	}
	if got := f.svc.CheckModelProfile(context.Background(), f.profile.Profiles[1]); got.Status != "unknownModel" {
		t.Errorf("status of a profile whose model is gone = %+v", got)
	}
	if got := f.svc.CheckModelProfile(context.Background(), f.profile.Profiles[2]); got.Status != "unknownVendor" {
		t.Errorf("status of a profile for another agent = %+v", got)
	}
	if got := f.svc.CheckModelProfile(context.Background(), f.profile.Profiles[0]); got.Status != "ok" {
		t.Errorf("status of a good profile = %+v", got)
	}
}

// The check is in CreateSession itself, so a caller that skipped the question
// still cannot start a session above its parent.
func TestCreateSessionRefusesAnUnapprovedChoice(t *testing.T) {
	f := newChoiceFixture(t)
	before, _ := f.svc.ListSessions(context.Background())
	_, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath, InitialConfig: map[string]string{"model": "m-big"}})
	if !errors.Is(err, ErrModelChoiceNeedsApproval) {
		t.Fatalf("err = %v, want ErrModelChoiceNeedsApproval", err)
	}
	if after, _ := f.svc.ListSessions(context.Background()); len(after) != len(before) {
		t.Fatal("a session was created anyway")
	}
	child, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath, InitialConfig: map[string]string{"model": "m-big"}, ModelChoiceApproved: true})
	if err != nil {
		t.Fatal(err)
	}
	if rt := f.adapter.last(); child.ID == "" || rt.value("model") != "m-big" {
		t.Fatalf("approved child runs %q", rt.value("model"))
	}
}

// What the choice rules must not loosen: the child is the caller's own, in
// the caller's workspace and folder.
func TestAModelChoiceDoesNotWidenWhereTheChildMayRun(t *testing.T) {
	f := newChoiceFixture(t)
	_, err := f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath, WorkspaceID: "someone-elses"})
	if !errors.Is(err, ErrWorkspaceMismatch) {
		t.Fatalf("err = %v", err)
	}
	_, err = f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir(), InitialConfig: map[string]string{"reasoning_effort": "low"}})
	if !errors.Is(err, ErrCwdOutsideParent) {
		t.Fatalf("a folder outside the parent: %v", err)
	}
}
