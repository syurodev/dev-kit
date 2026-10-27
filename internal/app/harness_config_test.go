package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/preferences"
	"devkit/internal/storage"
	"devkit/internal/workspace"
)

type scopeStub map[string]struct {
	workspace string
	vendor    agenthub.AgentType
}

func (s scopeStub) HarnessScope(_ context.Context, id string) (string, agenthub.AgentType, error) {
	if v, ok := s[id]; ok {
		return v.workspace, v.vendor, nil
	}
	return "", "", agenthub.ErrSessionScopeUnavailable
}

type workspacesStub []workspace.Workspace

func (w workspacesStub) ListWorkspaces() ([]workspace.Workspace, error) { return w, nil }

// completingPrompter answers the interaction hub like a person would, after
// running an optional action while the dialog is open.
type completingPrompter struct {
	mu     sync.Mutex
	hub    *interaction.Hub
	answer string
	// answers overrides answer for a kind of prompt (the scope selection is
	// answered "session" or "workspace", the confirmation "allow").
	answers map[interaction.Kind]string
	during  func()
	prompts []interaction.Pending
}

func (p *completingPrompter) Prompt(pending interaction.Pending) error {
	p.mu.Lock()
	p.prompts = append(p.prompts, pending)
	during, answer := p.during, p.answer
	if a, ok := p.answers[pending.Kind]; ok {
		answer = a
	}
	p.mu.Unlock()
	go func() {
		if during != nil {
			during()
		}
		_ = p.hub.Complete(pending.ID, answer)
	}()
	return nil
}

type harnessApp struct {
	t        *testing.T
	svc      *harnessconfig.Service
	prefs    *preferences.Service
	hub      *interaction.Hub
	prompter *completingPrompter
	store    *storage.HarnessStore
	events   []harnessconfig.Event
}

func newHarnessApp(t *testing.T) *harnessApp {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one')`,
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id) VALUES('s1','claude','/one','idle',0,'x','x','w1')`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	a := &harnessApp{t: t, prefs: preferences.NewService(preferences.NewMemoryRepository()), store: storage.NewHarnessStore(db)}
	a.prompter = &completingPrompter{answer: "allow"}
	a.hub = interaction.NewHub(a.prompter)
	a.prompter.hub = a.hub
	a.svc = harnessconfig.New(harnessconfig.Config{
		Store: a.store,
		Directory: harnessDirectory{
			sessions:   scopeStub{"s1": {"w1", agenthub.AgentTypeClaude}},
			workspaces: workspacesStub{{ID: "w1"}},
		},
		Vendors:   harnessVendors{prefs: a.prefs, supported: productionVendors},
		Policy:    harnessPolicy{prefs: a.prefs},
		Validator: harnessValidator{},
		Registry: func() harness.Registry {
			return harness.Registry{Tools: []harness.Tool{{Name: "git.status", DefinitionBytes: 900}, {Name: "tasks.list", DefinitionBytes: 800}, {Name: harness.ProposeTool, DefinitionBytes: 600}}}
		},
		Confirmer: harnessConfirmer{hub: a.hub},
		Boundary:  harnessBoundary{prefs: a.prefs},
		Audit:     func(e harnessconfig.Event) { a.events = append(a.events, e) },
	})
	return a
}

func (a *harnessApp) gitOff() {
	a.t.Helper()
	_, st, err := a.store.GetProfile(context.Background(), "w1")
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err = a.store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Expect: st,
		Value: &harness.Profile{Schema: harness.SchemaVersion, ToolGroups: map[string]bool{"git": false}}}}); err != nil {
		a.t.Fatal(err)
	}
}

func agentCtx() context.Context {
	ctx, cancel := context.WithTimeout(identity.WithCaller(context.Background(), identity.Agent("s1")), 10*time.Second)
	_ = cancel
	return ctx
}

var enableGit = harnessconfig.Request{Target: harnessconfig.TargetSession, Patch: harnessconfig.Patch{EnableGroups: []string{"git"}}}

func TestARealApprovalThroughTheHubWritesOnceAndOffersNoAlways(t *testing.T) {
	a := newHarnessApp(t)
	a.gitOff()
	got, err := a.svc.Prepare(agentCtx(), enableGit)
	if err != nil {
		t.Fatal(err)
	}
	// "allow_always" from a card is treated as one allow and remembers nothing.
	a.prompter.answer = "allow_always"
	if _, err = a.svc.Apply(agentCtx(), got.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.prompter.prompts) != 1 {
		t.Fatalf("prompts = %d", len(a.prompter.prompts))
	}
	p := a.prompter.prompts[0]
	if p.Kind != interaction.KindHarness || p.Tool != harness.ProposeTool || p.AllowAlwaysOffered {
		t.Fatalf("the prompt must be a harness confirmation with no always option: %+v", p)
	}
	o, st, _ := a.store.GetOverride(context.Background(), "s1", "w1")
	if o == nil || st.Revision != 1 {
		t.Fatalf("S = %+v %+v", o, st)
	}
	// A second identical request asks again.
	if _, err = a.svc.Apply(agentCtx(), mustPrepare(t, a.svc, harnessconfig.Request{Target: harnessconfig.TargetSession, Patch: harnessconfig.Patch{RevokeGroups: []string{"git"}}})); err != nil {
		t.Fatal(err)
	}
	if len(a.prompter.prompts) != 2 {
		t.Fatalf("a remembered answer: %d prompts", len(a.prompter.prompts))
	}
}

func mustPrepare(t *testing.T, svc *harnessconfig.Service, req harnessconfig.Request) string {
	t.Helper()
	got, err := svc.Prepare(agentCtx(), req)
	if err != nil {
		t.Fatal(err)
	}
	return got.ID
}

func TestADenyOrAnUnreadableAnswerWritesNothing(t *testing.T) {
	for _, answer := range []string{"deny", "", "maybe"} {
		a := newHarnessApp(t)
		a.gitOff()
		id := mustPrepare(t, a.svc, enableGit)
		a.prompter.answer = answer
		if _, err := a.svc.Apply(agentCtx(), id); !errors.Is(err, harnessconfig.ErrNotConfirmed) && err != harnessconfig.ErrNotConfirmed {
			t.Errorf("%q: %v", answer, err)
		}
		if o, _, _ := a.store.GetOverride(context.Background(), "s1", "w1"); o != nil {
			t.Errorf("%q wrote S", answer)
		}
	}
}

func TestHeadlessAndPromptlessHubsCannotApprove(t *testing.T) {
	for name, hub := range map[string]*interaction.Hub{
		"headless":  {Headless: true},
		"no prompt": interaction.NewHub(nil),
		"headless+given": func() *interaction.Hub {
			h := interaction.NewHub(interaction.NoopPrompter{})
			h.Headless = true
			return h
		}(),
	} {
		cfg := harnessConfirmer{hub: hub}
		if cfg.Available() {
			t.Errorf("%s: Available", name)
		}
	}
	if (harnessConfirmer{}).Available() {
		t.Error("a nil hub cannot ask")
	}
	// Through the service: nothing is written.
	a := newHarnessApp(t)
	a.gitOff()
	id := mustPrepare(t, a.svc, enableGit)
	a.hub.Headless = true
	if _, err := a.svc.Apply(agentCtx(), id); err != harnessconfig.ErrConfirmUnavailable {
		t.Fatalf("server mode: %v", err)
	}
	if o, _, _ := a.store.GetOverride(context.Background(), "s1", "w1"); o != nil {
		t.Fatal("a headless hub let a write through")
	}
}

// The existing Settings setter edits V while the dialog is open.
func TestACustomizationEditMadeThroughTheRealSetterStalesTheApproval(t *testing.T) {
	a := newHarnessApp(t)
	a.gitOff()
	id := mustPrepare(t, a.svc, enableGit)
	a.prompter.during = func() {
		c, _ := a.prefs.AgentCustomization("claude")
		c.DisabledMCP = []string{"slack"}
		if _, err := a.prefs.SetAgentCustomization("claude", c); err != nil {
			t.Error(err)
		}
	}
	_, err := a.svc.Apply(agentCtx(), id)
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeProposalStale {
		t.Fatalf("got %v", err)
	}
	if o, _, _ := a.store.GetOverride(context.Background(), "s1", "w1"); o != nil {
		t.Fatal("the stale approval wrote S")
	}
}

// While the final recheck and commit run, the setter must wait: the boundary is
// the same lock, so V cannot change between the recheck and the write.
func TestTheBoundaryExcludesTheCustomizationSetter(t *testing.T) {
	a := newHarnessApp(t)
	b := harnessBoundary{prefs: a.prefs}
	inside, done := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = b.Hold(context.Background(), func() error { close(inside); <-release; return nil })
	}()
	<-inside
	go func() {
		c, _ := a.prefs.AgentCustomization("claude")
		_, _ = a.prefs.SetAgentCustomization("claude", c)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("the setter ran while the boundary was held")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the setter never ran after the boundary was released")
	}
	// A cancelled context never runs the body.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	if err := b.Hold(ctx, func() error { ran = true; return nil }); err == nil || ran {
		t.Fatalf("cancelled hold ran=%v err=%v", ran, err)
	}
}

func TestNoLockIsHeldWhileTheDialogIsOpen(t *testing.T) {
	a := newHarnessApp(t)
	a.gitOff()
	id := mustPrepare(t, a.svc, enableGit)
	setterRan := make(chan struct{})
	a.prompter.during = func() {
		c, _ := a.prefs.AgentCustomization("claude")
		_, _ = a.prefs.SetAgentCustomization("claude", c) // would block if the boundary were held
		close(setterRan)
	}
	_, _ = a.svc.Apply(agentCtx(), id)
	select {
	case <-setterRan:
	default:
		t.Fatal("the customization setter was blocked by the open dialog")
	}
}

func TestVendorFactsAreUnavailableSoServerAndDenyProposalsFailClosed(t *testing.T) {
	a := newHarnessApp(t)
	for name, patch := range map[string]*harnessconfig.ProfilePatch{
		"deny list": {DisabledMCP: &[]string{"x"}},
		"servers":   {Servers: &[]harness.Server{{Name: "db", Command: "cmd", Enabled: true}}},
	} {
		_, err := a.svc.Prepare(agentCtx(), harnessconfig.Request{Target: harnessconfig.TargetWorkspace, Patch: harnessconfig.Patch{Workspace: patch}})
		if err != harnessconfig.ErrFactsUnavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A group change needs none.
	a.gitOff()
	if _, err := a.svc.Prepare(agentCtx(), enableGit); err != nil {
		t.Errorf("group change: %v", err)
	}
}

func TestTheVendorAdapterMapsPreferencesFaithfully(t *testing.T) {
	prefs := preferences.NewService(preferences.NewMemoryRepository())
	if _, err := prefs.SetAgentCustomization("claude", preferences.AgentCustomization{
		DisabledMCP: []string{"slack"},
		Servers:     []preferences.AgentMCPConfig{{Name: "db", Command: "cmd", Args: []string{"-x"}, EnvKeys: []string{"K"}, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	vendors := harnessVendors{prefs: prefs, supported: productionVendors}
	st, err := vendors.State(context.Background(), "claude")
	if err != nil || st.FactsComplete || st.Vendor.Revision != 1 || len(st.Vendor.Servers) != 1 || st.Vendor.Servers[0].Args[0] != "-x" || st.Vendor.DisabledMCP[0] != "slack" {
		t.Fatalf("%+v %v", st, err)
	}
}

// opencode is a supported vendor: a stored customization and its revision reach
// the snapshot exactly. Reading it as an empty V would change the launch the
// moment a provider is enabled.
func TestTheVendorAdapterPreservesAnOpenCodeCustomization(t *testing.T) {
	prefs := preferences.NewService(preferences.NewMemoryRepository())
	if _, err := prefs.SetAgentCustomization("opencode", preferences.AgentCustomization{
		SkillFolders: []string{"/skills/one"},
		DisabledMCP:  []string{"slack"},
		Servers:      []preferences.AgentMCPConfig{{Name: "db", Command: "cmd", Args: []string{"--ro"}, EnvKeys: []string{"TOKEN"}, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	st, err := harnessVendors{prefs: prefs, supported: productionVendors}.State(context.Background(), "opencode")
	if err != nil || st.Vendor.Revision != 1 || st.FactsComplete {
		t.Fatalf("%+v %v", st, err)
	}
	if len(st.Vendor.SkillFolders) != 1 || st.Vendor.SkillFolders[0] != "/skills/one" || st.Vendor.DisabledMCP[0] != "slack" ||
		len(st.Vendor.Servers) != 1 || st.Vendor.Servers[0].Command != "cmd" || st.Vendor.Servers[0].Args[0] != "--ro" || st.Vendor.Servers[0].EnvKeys[0] != "TOKEN" {
		t.Fatalf("an opencode customization was not preserved: %+v", st.Vendor)
	}
}

// A vendor the runtime does not open is refused explicitly. Returning an empty V
// would read as "no customization" and let a configured launch through.
func TestAnUnsupportedVendorIsRefusedNotReadAsEmpty(t *testing.T) {
	prefs := preferences.NewService(preferences.NewMemoryRepository())
	v := harnessVendors{prefs: prefs, supported: productionVendors}
	_, err := v.State(context.Background(), "gemini")
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeSnapshotUnavailable {
		t.Fatalf("want a refusal with a published code, got %v", err)
	}
	if got := strings.Join(v.Names(), ","); got != "claude,codex,opencode" {
		t.Fatalf("names = %s", got)
	}
	if _, err := (harnessVendors{supported: productionVendors}).State(context.Background(), "claude"); err == nil {
		t.Fatal("a vendor adapter without preferences must not answer")
	}
}

var productionVendors = []agenthub.AgentType{agenthub.AgentTypeClaude, agenthub.AgentTypeCodex, agenthub.AgentTypeOpenCode}

func TestTheValidatorAppliesTheExistingCustomizationRules(t *testing.T) {
	v := harnessValidator{}
	if err := v.Validate(context.Background(), harness.Profile{Schema: 1, Servers: []harness.Server{{Name: "db", Command: "cmd", Enabled: true}}}); err != nil {
		t.Fatalf("a valid server: %v", err)
	}
	for name, p := range map[string]harness.Profile{
		"credentials in a URL": {Schema: 1, Servers: []harness.Server{{Name: "db", URL: "https://u:p@host.test/x", Enabled: true}}},
		"plain http remote":    {Schema: 1, Servers: []harness.Server{{Name: "db", URL: "http://example.com", Enabled: true}}},
		"env value, not name":  {Schema: 1, Servers: []harness.Server{{Name: "db", Command: "c", EnvKeys: []string{"K=secret"}, Enabled: true}}},
		"reserved name":        {Schema: 1, Servers: []harness.Server{{Name: "devkit", Command: "c", Enabled: true}}},
		"relative folder":      {Schema: 1, SkillFolders: []string{"skills/a"}},
	} {
		err := v.Validate(context.Background(), p)
		var he *harness.Error
		if !errors.As(err, &he) || he.Code != harness.CodeProfileInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheDirectoryResolvesFromHostData(t *testing.T) {
	d := harnessDirectory{sessions: scopeStub{"s1": {"w1", agenthub.AgentTypeCodex}}, workspaces: workspacesStub{{ID: "w1"}}}
	sc, err := d.SessionScope(context.Background(), "s1")
	if err != nil || sc.WorkspaceID != "w1" || sc.Vendor != "codex" {
		t.Fatalf("%+v %v", sc, err)
	}
	if _, err := d.SessionScope(context.Background(), "gone"); err == nil {
		t.Fatal("an unknown session resolved")
	}
	if ok, _ := d.WorkspaceExists(context.Background(), "w1"); !ok {
		t.Fatal("w1 exists")
	}
	if ok, _ := d.WorkspaceExists(context.Background(), "w9"); ok {
		t.Fatal("w9 does not")
	}
}
