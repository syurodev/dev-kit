package app

import (
	"context"
	"errors"
	"strings"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"
	"devkit/internal/preferences"
	"devkit/internal/workspace"
)

// The adapters below bind internal/harnessconfig's ports to the real services.
// They add no behavior of their own. No production code builds the service yet:
// that needs a registry source and a public transport (H4/H5), and until then
// nothing can reach a harness write.

// harnessVendors reads V from preferences. The vendor-defined servers and skills
// are not readable with commands and arguments today (EnvironmentReport carries
// names only), so FactsComplete is false: any proposal that depends on them is
// refused as facts-unavailable instead of being judged against an empty
// environment.
type harnessVendors struct{ prefs *preferences.Service }

func (harnessVendors) Names() []string {
	return []string{string(agenthub.AgentTypeClaude), string(agenthub.AgentTypeCodex)}
}

func (v harnessVendors) State(_ context.Context, vendor string) (harnessconfig.VendorState, error) {
	state := harnessconfig.VendorState{FactsComplete: false}
	if vendor != string(agenthub.AgentTypeClaude) && vendor != string(agenthub.AgentTypeCodex) {
		return state, nil
	}
	c, err := v.prefs.AgentCustomization(vendor)
	if err != nil {
		return harnessconfig.VendorState{}, err
	}
	state.Vendor = harness.Vendor{
		Revision:       c.Revision,
		SkillFolders:   c.SkillFolders,
		DisabledSkills: c.DisabledSkills,
		DisabledMCP:    c.DisabledMCP,
		Servers:        harnessServers(c.Servers),
	}
	return state, nil
}

func harnessServers(in []preferences.AgentMCPConfig) []harness.Server {
	out := make([]harness.Server, 0, len(in))
	for _, s := range in {
		out = append(out, harness.Server{Name: s.Name, Command: s.Command, Args: s.Args, URL: s.URL, EnvKeys: s.EnvKeys, Enabled: s.Enabled})
	}
	return out
}

// harnessBoundary excludes the existing customization setter, so a vendor
// customization edit cannot land between the final recheck and the commit.
type harnessBoundary struct{ prefs *preferences.Service }

func (b harnessBoundary) Hold(ctx context.Context, fn func() error) error {
	return b.prefs.HoldAgentCustomization(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	})
}

// harnessConfirmer asks through the existing interaction hub. EnvBlocks makes
// the hub offer no "always" answer, ignore a remembered one and never remember
// this one; Kind and Tool name the request. Anything but an explicit allow is an
// error, and a hub with nobody to ask is not Available.
type harnessConfirmer struct{ hub *interaction.Hub }

func (c harnessConfirmer) Available() bool { return c.hub.CanAsk() }

func (c harnessConfirmer) Confirm(ctx context.Context, q harnessconfig.Confirmation) error {
	summary := q.Summary
	if q.RationaleUntrusted != "" {
		summary += "\n\nNote from the proposer (untrusted text, not checked by DevKit): " + q.RationaleUntrusted
	}
	return c.hub.Confirm(ctx, interaction.ConfirmRequest{
		Kind:      interaction.KindHarness,
		Tool:      harness.ProposeTool,
		Summary:   summary,
		EnvBlocks: true,
	})
}

type harnessSessionScopes interface {
	HarnessScope(ctx context.Context, sessionID string) (string, agenthub.AgentType, error)
}

type harnessWorkspaces interface {
	ListWorkspaces() ([]workspace.Workspace, error)
}

// harnessDirectory resolves scope from host data: the session's workspace and
// vendor from Agent Hub, workspace existence from the registry.
type harnessDirectory struct {
	sessions   harnessSessionScopes
	workspaces harnessWorkspaces
}

func (d harnessDirectory) SessionScope(ctx context.Context, sessionID string) (harnessconfig.SessionScope, error) {
	workspaceID, vendor, err := d.sessions.HarnessScope(ctx, sessionID)
	if err != nil {
		return harnessconfig.SessionScope{}, err
	}
	return harnessconfig.SessionScope{WorkspaceID: workspaceID, Vendor: string(vendor)}, nil
}

func (d harnessDirectory) WorkspaceExists(_ context.Context, workspaceID string) (bool, error) {
	all, err := d.workspaces.ListWorkspaces()
	if err != nil {
		return false, err
	}
	for _, w := range all {
		if w.ID == workspaceID {
			return true, nil
		}
	}
	return false, nil
}

// harnessValidator applies the existing customization rules to W's servers and
// skill folders (URL shape, env names, SKILL.md, size limits) without running
// or connecting to anything.
type harnessValidator struct{}

func (harnessValidator) Validate(_ context.Context, w harness.Profile) error {
	c := preferences.AgentCustomization{SkillFolders: append([]string{}, w.SkillFolders...), DisabledSkills: append([]string{}, w.DisabledSkills...), DisabledMCP: append([]string{}, w.DisabledMCP...)}
	for _, s := range w.Servers {
		c.Servers = append(c.Servers, preferences.AgentMCPConfig{Name: strings.TrimSpace(s.Name), Command: s.Command, Args: s.Args, URL: s.URL, EnvKeys: s.EnvKeys, Enabled: s.Enabled})
	}
	if _, err := agenthub.ValidateCustomization(string(agenthub.AgentTypeClaude), c); err != nil {
		var ce *agenthub.CustomizationError
		if errors.As(err, &ce) {
			return &harness.Error{Code: harness.CodeProfileInvalid, Message: ce.Message}
		}
		return harnessconfig.ErrInvalid
	}
	return nil
}

// harnessPolicy is the UI-owned policy. Neither setting exists in the product
// yet, so both are off: narrowing always asks and executable proposals are never
// opted in. The editor (H5) supplies the real source.
type harnessPolicy struct{}

func (harnessPolicy) Policy(context.Context) (harnessconfig.Policy, error) {
	return harnessconfig.Policy{Revision: "default"}, nil
}
