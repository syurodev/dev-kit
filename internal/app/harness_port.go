package app

import (
	"context"
	"errors"
	"sync"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

// The construction order has a knot in it. The MCP server's registry must contain
// harness.profile.get and harness.profile.propose before its inventory is read,
// because the assembly resolves every policy against that inventory; but the
// handlers need the assembly, which needs the inventory. HarnessPort cuts the knot
// without a second registry, a late registration or a global: it is created first
// and handed to the MCP server as the tools' backing, and ComposeHarness binds it,
// once, to the facade of the very assembly built from that server's inventory. Until
// then every operation refuses with a fixed sentence and does nothing.

var (
	// ErrHarnessNotReady: the controls were asked before the graph was bound.
	ErrHarnessNotReady = &harness.Error{Code: harness.CodeNotAvailable, Message: "Tool-group settings are not available yet."}
	// ErrHarnessPortBound: the port already has its controls; it never rebinds.
	ErrHarnessPortBound = errors.New("harness port: already bound")
)

// HarnessPort is the startup-only handle to the public controls.
type HarnessPort struct {
	mu  sync.RWMutex
	ctl harnessconfig.Controls
}

var _ harnessconfig.Controls = (*HarnessPort)(nil)

// NewHarnessPort returns an unbound port. Pass it to mcp.Deps.Harness and to the
// application container before the MCP server is built.
func NewHarnessPort() *HarnessPort { return &HarnessPort{} }

// Bind attaches the controls, once. A nil (or typed-nil) value is refused.
func (p *HarnessPort) Bind(c harnessconfig.Controls) error {
	if p == nil || isNilInterface(c) {
		return ErrHarnessAssemblyIncomplete
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctl != nil {
		return ErrHarnessPortBound
	}
	p.ctl = c
	return nil
}

// Bound reports whether the controls are attached. The window shows its entry
// points from this, so nothing is offered that cannot answer.
func (p *HarnessPort) Bound() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ctl != nil
}

func (p *HarnessPort) get() (harnessconfig.Controls, error) {
	if p == nil {
		return nil, ErrHarnessNotReady
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.ctl == nil {
		return nil, ErrHarnessNotReady
	}
	return p.ctl, nil
}

func (p *HarnessPort) Profile(ctx context.Context) (harnessconfig.ProfileView, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.Profile(ctx)
}

func (p *HarnessPort) Request(ctx context.Context, req harnessconfig.GroupRequest) (harnessconfig.RequestResult, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	return c.Request(ctx, req)
}

func (p *HarnessPort) WorkspaceProfile(ctx context.Context, workspaceID string) (harnessconfig.ProfileView, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.WorkspaceProfile(ctx, workspaceID)
}

func (p *HarnessPort) SessionProfile(ctx context.Context, workspaceID, sessionID string) (harnessconfig.ProfileView, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.SessionProfile(ctx, workspaceID, sessionID)
}

func (p *HarnessPort) PrepareWorkspaceGroups(ctx context.Context, workspaceID string, edits []harnessconfig.GroupEdit) (harnessconfig.Review, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.PrepareWorkspaceGroups(ctx, workspaceID, edits)
}

func (p *HarnessPort) PrepareRevoke(ctx context.Context, workspaceID, sessionID string, groups []string) (harnessconfig.Review, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.PrepareRevoke(ctx, workspaceID, sessionID, groups)
}

func (p *HarnessPort) ProfileWithBudgets(ctx context.Context, f harnessconfig.BudgetFilter) (harnessconfig.ProfileView, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.ProfileWithBudgets(ctx, f)
}

func (p *HarnessPort) WorkspaceBudgets(ctx context.Context, workspaceID string, f harnessconfig.BudgetFilter) (harnessconfig.BudgetPage, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.BudgetPage{}, err
	}
	return c.WorkspaceBudgets(ctx, workspaceID, f)
}

func (p *HarnessPort) PrepareBudgetEdits(ctx context.Context, workspaceID string, edits []harnessconfig.BudgetEdit) (harnessconfig.Review, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.PrepareBudgetEdits(ctx, workspaceID, edits)
}

func (p *HarnessPort) Apply(ctx context.Context, id string) (harnessconfig.Outcome, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.Outcome{}, err
	}
	return c.Apply(ctx, id)
}

func (p *HarnessPort) Cancel(ctx context.Context, id string) error {
	c, err := p.get()
	if err != nil {
		return err
	}
	return c.Cancel(ctx, id)
}

func (p *HarnessPort) Reconcile(ctx context.Context, workspaceID, sessionID string) (harnessconfig.Outcome, error) {
	c, err := p.get()
	if err != nil {
		return harnessconfig.Outcome{}, err
	}
	return c.Reconcile(ctx, workspaceID, sessionID)
}

// ComposeHarness builds the graph from the MCP server's real inventory, attaches
// it to Agent Hub and binds the port, as one step: either all three happen or
// none, and a failure leaves nothing reachable. It is the only call a composition
// root makes for the harness. It must run after the session host is installed on
// Agent Hub (Attach checks) and before any listener, session or window is
// admitted.
func ComposeHarness(port *HarnessPort, cfg HarnessAssemblyConfig, agents *agenthub.Service) (*HarnessAssembly, error) {
	if port == nil || port.Bound() {
		return nil, ErrHarnessAlreadyAttached
	}
	asm, err := NewHarnessAssembly(cfg)
	if err != nil {
		return nil, err
	}
	if err := asm.Attach(agents); err != nil {
		return nil, err
	}
	if err := port.Bind(asm.Controls()); err != nil {
		// Unreachable unless the port was bound concurrently: undo the attachment
		// rather than leave Agent Hub filtering with no controls to recover it.
		agents.Harness, agents.HarnessLifecycle = nil, nil
		return nil, err
	}
	return asm, nil
}
