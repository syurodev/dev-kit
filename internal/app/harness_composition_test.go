package app

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/preferences"
	"devkit/internal/storage"
	"devkit/internal/workspace"
)

// ---- D1: one assembly boundary ------------------------------------------------

type assemblyParts struct {
	db     *sql.DB
	prefs  *preferences.Service
	hub    *interaction.Hub
	ws     *workspace.Service
	agents *agenthub.Service
}

func newAssemblyParts(t *testing.T) assemblyParts {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	reg := agenthub.NewRegistry()
	return assemblyParts{
		db: db, prefs: preferences.NewService(preferences.NewMemoryRepository()), hub: interaction.NewHub(interaction.NoopPrompter{}),
		ws: workspace.NewService(workspace.NewMemoryRepository()), agents: agenthub.NewService(reg, agenthub.NewSQLStore(db), nil),
	}
}

func (p assemblyParts) config() HarnessAssemblyConfig {
	return HarnessAssemblyConfig{
		Store: storage.NewHarnessStore(p.db), Preferences: p.prefs, Hub: p.hub, Workspaces: p.ws, Agents: p.agents, Vendors: productionVendors,
		Inventory: func() (harness.Registry, error) {
			return harness.Registry{Tools: []harness.Tool{{Name: "git.status", DefinitionBytes: 900}}}, nil
		},
	}
}

func TestAnIncompleteGraphIsRefusedAndAttachesNothing(t *testing.T) {
	var nilWorkspaces *workspace.Service // the typed-nil trap
	cases := map[string]func(c *HarnessAssemblyConfig){
		"no store":        func(c *HarnessAssemblyConfig) { c.Store = nil },
		"typed nil store": func(c *HarnessAssemblyConfig) { var s *storage.HarnessStore; c.Store = s },
		"no preferences":  func(c *HarnessAssemblyConfig) { c.Preferences = nil },
		"no hub":          func(c *HarnessAssemblyConfig) { c.Hub = nil },
		"no agent hub":    func(c *HarnessAssemblyConfig) { c.Agents = nil },
		"no workspaces":   func(c *HarnessAssemblyConfig) { c.Workspaces = nil },
		"typed nil":       func(c *HarnessAssemblyConfig) { c.Workspaces = nilWorkspaces },
		"no vendors":      func(c *HarnessAssemblyConfig) { c.Vendors = nil },
		"no inventory":    func(c *HarnessAssemblyConfig) { c.Inventory = nil },
		"unreadable inventory": func(c *HarnessAssemblyConfig) {
			c.Inventory = func() (harness.Registry, error) { return harness.Registry{}, errors.New("boom") }
		},
		"empty inventory": func(c *HarnessAssemblyConfig) {
			c.Inventory = func() (harness.Registry, error) { return harness.Registry{}, nil }
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := newAssemblyParts(t)
			cfg := p.config()
			mutate(&cfg)
			asm, err := NewHarnessAssembly(cfg)
			if err == nil || asm != nil {
				t.Fatalf("an incomplete graph must be refused, got %v", err)
			}
			if strings.Contains(err.Error(), "boom") {
				t.Fatalf("a cause leaked into the fixed error: %v", err)
			}
			if p.agents.Harness != nil || p.agents.HarnessLifecycle != nil {
				t.Fatal("nothing may be attached after a refusal")
			}
		})
	}
}

func TestAttachNeedsAHostThatCanEnforceAndAttachesOnce(t *testing.T) {
	p := newAssemblyParts(t)
	asm, err := NewHarnessAssembly(p.config())
	if err != nil {
		t.Fatal(err)
	}
	// No endpoint host at all, or one that can only serve today's default: a
	// provider would run configured sessions unfiltered or refuse every launch.
	if err := asm.Attach(p.agents); !errors.Is(err, ErrHarnessAssemblyEndpoints) {
		t.Fatalf("want an endpoints refusal, got %v", err)
	}
	if p.agents.Harness != nil || p.agents.HarnessLifecycle != nil {
		t.Fatal("a refused attach must leave Agent Hub legacy")
	}
	p.agents.MCPEndpoints = legacyOnlyEndpoints{}
	if err := asm.Attach(p.agents); !errors.Is(err, ErrHarnessAssemblyEndpoints) {
		t.Fatalf("a legacy-only host cannot enforce: %v", err)
	}
	var nilHost *mcp.SessionHost
	p.agents.MCPEndpoints = nilHost
	if err := asm.Attach(p.agents); !errors.Is(err, ErrHarnessAssemblyEndpoints) {
		t.Fatalf("a typed-nil host must be refused: %v", err)
	}
	if p.agents.Harness != nil || p.agents.HarnessLifecycle != nil {
		t.Fatal("a typed-nil refusal must attach nothing")
	}
	// Another Agent Hub is not this graph's.
	other := agenthub.NewService(agenthub.NewRegistry(), agenthub.NewSQLStore(p.db), nil)
	if err := asm.Attach(other); !errors.Is(err, ErrHarnessAssemblyIncomplete) {
		t.Fatalf("a foreign Agent Hub must be refused: %v", err)
	}
	if err := asm.Attach(nil); !errors.Is(err, ErrHarnessAssemblyIncomplete) {
		t.Fatalf("nil: %v", err)
	}
}

type legacyOnlyEndpoints struct{}

func (legacyOnlyEndpoints) Open(string) (agenthub.MCPEndpoint, error) {
	return agenthub.MCPEndpoint{}, errors.New("legacy")
}

func TestTheComposedGraphAttachesTogetherAndOnce(t *testing.T) {
	r := newCompRig(t, compOptions{})
	if r.agents.Harness == nil || r.agents.HarnessLifecycle == nil {
		t.Fatal("the provider and the lifecycle owner attach together")
	}
	if err := r.asm.Attach(r.agents); !errors.Is(err, ErrHarnessAlreadyAttached) {
		t.Fatalf("a second attach must be refused, got %v", err)
	}
}

// ---- D2: vendor coverage -----------------------------------------------------

func TestAnOpenCodeCustomizationReachesTheLaunchUnchanged(t *testing.T) {
	custom := preferences.AgentCustomization{
		SkillFolders:   nil,
		DisabledSkills: nil,
		DisabledMCP:    []string{"slack"},
		Servers:        []preferences.AgentMCPConfig{{Name: "db", Command: "/bin/db", Args: []string{"--ro"}, EnvKeys: []string{"TOKEN"}, Enabled: true}},
	}
	launched := func(attach bool) preferences.AgentCustomization {
		r := newCompRig(t, compOptions{noAttach: !attach})
		if _, err := r.prefs.SetAgentCustomization("opencode", custom); err != nil {
			t.Fatal(err)
		}
		_, spec := r.create(agenthub.AgentTypeOpenCode)
		return spec.Customizations["opencode"]
	}
	legacy, composed := launched(false), launched(true)
	if composed.Revision != 1 || len(composed.Servers) != 1 || composed.Servers[0].Name != "db" || composed.Servers[0].Args[0] != "--ro" || composed.DisabledMCP[0] != "slack" {
		t.Fatalf("the composed launch lost the stored opencode customization: %+v", composed)
	}
	if composed.Revision != legacy.Revision || len(composed.Servers) != len(legacy.Servers) {
		t.Fatalf("composed %+v differs from the legacy launch %+v", composed, legacy)
	}
}

func TestAnUnsupportedVendorIsRefusedAtLaunchAndLeavesNothingBehind(t *testing.T) {
	r := newCompRig(t, compOptions{extraTypes: []agenthub.AgentType{"gemini"}})
	_, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: "gemini", Cwd: r.root, WorkspaceID: r.wsID})
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeSnapshotUnavailable {
		t.Fatalf("an unsupported vendor must be refused with a published code, got %v", err)
	}
	if r.vendors["gemini"].opens.Load() != 0 {
		t.Fatal("no vendor process may start for a refused launch")
	}
	sessions, _ := agenthub.NewSQLStore(r.db).ListSessions(context.Background())
	if len(sessions) != 0 {
		t.Fatalf("a refused launch left %d session rows", len(sessions))
	}
}

// ---- D3: create, compatibility ----------------------------------------------

func TestAFreshDefaultSessionIsCoherentAndMatchesTheLegacyLaunch(t *testing.T) {
	legacy := newCompRig(t, compOptions{noAttach: true})
	_, lspec := legacy.create(agenthub.AgentTypeClaude)

	r := newCompRig(t, compOptions{})
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	names := r.listed(cs)
	inv, _ := r.inventory()
	if len(names) != len(inv.Tools) {
		t.Fatalf("a default session serves the whole registry: %d of %d", len(names), len(inv.Tools))
	}
	if spec.DevKit == nil || len(spec.DevKit.Tools) != len(names) {
		t.Fatalf("host guidance input must be the served surface: %+v", spec.DevKit)
	}
	// Launch permission lists are what the legacy launch gives (nothing narrowed
	// because nothing is off); a configured policy only ever narrows them.
	if strings.Join(spec.PreApprovedMCP["devkit"], ",") != strings.Join(lspec.PreApprovedMCP["devkit"], ",") ||
		strings.Join(spec.GateOwnedMCP["devkit"], ",") != strings.Join(lspec.GateOwnedMCP["devkit"], ",") {
		t.Fatal("a default composed launch must grant exactly what the legacy launch grants")
	}
	st, err := r.agents.HarnessStatus(context.Background(), sess.ID)
	if err != nil || !st.Live || st.State != agenthub.HarnessCurrent || !st.Enforced {
		t.Fatalf("status = %+v %v", st, err)
	}
	// The unprofiled shared (terminal) socket is a different surface: nothing
	// here touches it.
	if lspec.DevKit != nil {
		t.Fatal("a legacy launch carries no served surface")
	}
}

func TestAConfiguredWorkspaceNarrowsListingGuidanceAndPermissionsTogether(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	names := r.listed(cs)
	if hasGroup(names, "vault") {
		t.Fatal("vault must not be listed")
	}
	if contains(spec.DevKit.Tools, "vault.status") {
		t.Fatal("guidance must not name a hidden tool")
	}
	for _, list := range [][]string{spec.PreApprovedMCP["devkit"], spec.GateOwnedMCP["devkit"]} {
		if hasGroup(list, "vault") {
			t.Fatalf("a launch permission list names a hidden tool: %v", list)
		}
	}
	before := r.gate.handled.Load()
	if got := r.call(cs, "vault.status"); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("a hidden direct call must be refused: %q", got)
	}
	if r.gate.handled.Load() != before {
		t.Fatal("a refused call must never reach the gateway")
	}
}

var _ = identity.UI
