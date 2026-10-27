package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"
	"devkit/internal/preferences"
)

// The harness assembly is the one place the harness graph is built: the SQL
// HarnessStore, the vendor customization (V) in preferences, the session and
// workspace directory, the shared interaction hub and V writer boundary, the
// registry of the tools the MCP server really registers, the provider, and
// Agent Hub's per-session coordinator (the H4c publication, reconciliation,
// lifecycle and status owner). Nothing here duplicates a resolver, a registry, an
// approval flow or a launch path: each is the existing owner, reached through its
// port.
//
// Building the assembly constructs services only. Nothing reaches a session until
// Attach is called, and only main.go calls it. A failure leaves nothing attached:
// there is no fallback from a configuration that cannot be built to unfiltered
// serving.

// Fixed, actionable refusals. They carry no path, error text or value.
var (
	// ErrHarnessAssemblyIncomplete: a required collaborator is missing (or is a
	// typed nil), so no part of the graph was attached.
	ErrHarnessAssemblyIncomplete = errors.New("harness assembly: a required service is missing; nothing was attached")
	// ErrHarnessAssemblyRegistry: the MCP server's registry could not be read, or
	// it registers no tools, so a policy cannot be resolved against it.
	ErrHarnessAssemblyRegistry = errors.New("harness assembly: the MCP tool registry is unavailable or empty; nothing was attached")
	// ErrHarnessAssemblyEndpoints: the session host cannot serve a prepared policy,
	// so attaching a provider would run configured sessions unfiltered or refuse
	// them all. Neither is a fallback this assembly will make.
	ErrHarnessAssemblyEndpoints = errors.New("harness assembly: the session host cannot enforce a harness policy; nothing was attached")
	// ErrHarnessAlreadyAttached: Agent Hub already has a provider or lifecycle owner.
	ErrHarnessAlreadyAttached = errors.New("harness assembly: Agent Hub already has a harness attached")
)

// harnessPersistence is the SQL HarnessStore (storage.HarnessStore) as the
// assembly needs it: the proposal service's store port and the lifecycle owner's
// override cleanup. The composition root builds it, so this package never imports
// storage (internal/arch).
type harnessPersistence interface {
	harnessconfig.Store
	overrideClearer
}

// HarnessAssemblyConfig is every input of the graph. Each field is required.
type HarnessAssemblyConfig struct {
	// Store is the migrated application database's HarnessStore (W and S).
	Store harnessPersistence
	// Preferences is the vendor customization (V) and the writer boundary the
	// proposal's final recheck runs inside.
	Preferences *preferences.Service
	// Hub is the shared interaction hub: the same instance the MCP gateway and the
	// bridge use, so a harness approval cannot become a second prompt flow.
	Hub *interaction.Hub
	// Workspaces answers whether a workspace exists.
	Workspaces harnessWorkspaces
	// Agents is the Agent Hub service: trusted session scope, and the per-session
	// coordinator the H4c publication protocol lives in.
	Agents *agenthub.Service
	// Vendors are the vendor types the runtime registry opens, in production
	// Claude, Codex and opencode.
	Vendors []agenthub.AgentType
	// Inventory returns the tools the MCP server actually registered (read after
	// its conditional modules are wired). It is the only source of the registry.
	Inventory func() (harness.Registry, error)
	// Audit receives proposal outcomes (codes, ids, revisions; never values).
	Audit func(harnessconfig.Event)
}

// HarnessAssembly is the built graph. Service is the internal proposal service
// (Prepare, Apply, Reconcile): it is not a public transport and no MCP tool
// exposes it (H5 owns that).
type HarnessAssembly struct {
	Store    harnessPersistence
	Provider *harnessconfig.Provider
	Service  *harnessconfig.Service

	agents *agenthub.Service
}

// NewHarnessAssembly builds the graph or returns a fixed error. It reads the
// registry once and validates every collaborator, typed nils included, before
// constructing anything that could be attached.
func NewHarnessAssembly(cfg HarnessAssemblyConfig) (*HarnessAssembly, error) {
	if isNilInterface(cfg.Store) || cfg.Preferences == nil || cfg.Hub == nil || cfg.Agents == nil || isNilWorkspaces(cfg.Workspaces) ||
		cfg.Inventory == nil || len(cfg.Vendors) == 0 {
		return nil, ErrHarnessAssemblyIncomplete
	}
	registry, err := cfg.Inventory()
	if err != nil || len(registry.Tools) == 0 {
		return nil, ErrHarnessAssemblyRegistry
	}
	// One immutable copy: every resolution in the application uses these facts.
	registry = harness.Registry{Tools: slices.Clone(registry.Tools)}
	registryOf := func() harness.Registry { return harness.Registry{Tools: slices.Clone(registry.Tools)} }

	store := cfg.Store
	vendors := harnessVendors{prefs: cfg.Preferences, supported: slices.Clone(cfg.Vendors)}
	provider := harnessconfig.NewProvider(harnessconfig.ProviderConfig{
		Store: store, Vendors: vendors, Registry: registryOf, Canonical: filepath.EvalSymlinks,
	})
	service := harnessconfig.New(harnessconfig.Config{
		Store:     store,
		Directory: harnessDirectory{sessions: cfg.Agents, workspaces: cfg.Workspaces},
		Vendors:   vendors,
		Policy:    harnessPolicy{},
		Validator: harnessValidator{},
		Registry:  registryOf,
		Canonical: filepath.EvalSymlinks,
		Confirmer: harnessConfirmer{hub: cfg.Hub},
		Boundary:  harnessBoundary{prefs: cfg.Preferences},
		Audit:     cfg.Audit,
		Live:      harnessLive{svc: cfg.Agents},
	})
	return &HarnessAssembly{Store: store, Provider: provider, Service: service, agents: cfg.Agents}, nil
}

// Attach gives Agent Hub its provider and its lifecycle owner, together or not at
// all. It refuses unless the session host can serve a prepared policy, so a
// configured session is never started unfiltered, and refuses a second
// attachment. Live publication is checked on each returned endpoint at launch;
// PolicyEndpoints alone only proves that launch filtering is supported.
// Call it before any session can start.
func (a *HarnessAssembly) Attach(agents *agenthub.Service) error {
	if a == nil || a.Provider == nil || a.Store == nil || agents == nil || agents != a.agents {
		return ErrHarnessAssemblyIncomplete
	}
	if _, ok := agents.MCPEndpoints.(agenthub.PolicyEndpoints); !ok || isNilInterface(agents.MCPEndpoints) {
		return ErrHarnessAssemblyEndpoints
	}
	if agents.Harness != nil || agents.HarnessLifecycle != nil {
		return ErrHarnessAlreadyAttached
	}
	agents.Harness = harnessSnapshots{provider: a.Provider}
	agents.HarnessLifecycle = harnessLifecycle{store: a.Store}
	return nil
}

// harnessSnapshots is Agent Hub's HarnessSnapshots over the provider: one
// resolution per launch, carrying the pinned serving basis.
type harnessSnapshots struct{ provider *harnessconfig.Provider }

func (h harnessSnapshots) Snapshot(ctx context.Context, sessionID, workspaceID, vendor string) (harness.Snapshot, error) {
	return h.provider.Snapshot(ctx, harnessconfig.SnapshotRequest{SessionID: sessionID, WorkspaceID: workspaceID, Vendor: vendor})
}

var _ agenthub.HarnessSnapshots = harnessSnapshots{}

// isNilWorkspaces catches a typed nil stored in the interface.
func isNilWorkspaces(w harnessWorkspaces) bool { return isNilInterface(w) }

func isNilInterface(x any) bool {
	if x == nil {
		return true
	}
	v := reflect.ValueOf(x)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
