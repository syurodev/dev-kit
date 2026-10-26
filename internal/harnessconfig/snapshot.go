package harnessconfig

import (
	"context"
	"errors"

	"devkit/internal/harness"
)

// ErrSnapshotUnavailable is every way a runtime's configuration can fail to
// resolve (unreadable profile, vendor read error, no registry, invalid input).
// The caller refuses the launch; it must never fall back to unfiltered tools.
var ErrSnapshotUnavailable = &harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "The harness configuration for this session could not be resolved."}

// SnapshotRequest is built by Agent Hub from its own session row, never from a
// tool argument or a caller's claim. SessionID is the session being opened: a
// fresh child asks with its own new id, so it cannot see its parent's override.
type SnapshotRequest struct {
	SessionID   string
	WorkspaceID string
	Vendor      string
}

// ProviderConfig wires the same ports the proposal service uses.
type ProviderConfig struct {
	Store     Store
	Vendors   Vendors
	Registry  func() harness.Registry
	Canonical func(string) (string, error)
}

// Provider resolves the effective configuration a runtime is about to open
// with. It uses harness.Resolve and nothing else to merge, reads W, S and V
// once, and returns a detached value.
type Provider struct{ cfg ProviderConfig }

func NewProvider(cfg ProviderConfig) *Provider { return &Provider{cfg: cfg} }

// Snapshot resolves the configuration for one session. A positively resolved
// configuration with no profile and no override is Default; anything that
// cannot be resolved is an error, never a default.
func (p *Provider) Snapshot(ctx context.Context, req SnapshotRequest) (harness.Snapshot, error) {
	if p == nil || p.cfg.Store == nil || p.cfg.Vendors == nil || p.cfg.Registry == nil ||
		req.SessionID == "" || req.WorkspaceID == "" || req.Vendor == "" {
		return harness.Snapshot{}, ErrSnapshotUnavailable
	}
	w, wStamp, err := p.cfg.Store.GetProfile(ctx, req.WorkspaceID)
	if err != nil {
		return harness.Snapshot{}, ErrSnapshotUnavailable
	}
	o, oStamp, err := p.cfg.Store.GetOverride(ctx, req.SessionID, req.WorkspaceID)
	if err != nil {
		// An override written for another workspace does not follow the session
		// and is not applied. Anything else (a corrupt row) refuses the launch.
		if !isScope(err) {
			return harness.Snapshot{}, ErrSnapshotUnavailable
		}
		o, oStamp = nil, harness.Stamp{}
	}
	vs, err := p.cfg.Vendors.State(ctx, req.Vendor)
	if err != nil {
		return harness.Snapshot{}, ErrSnapshotUnavailable
	}
	env := vs.Environment
	if !vs.FactsComplete {
		env = harness.Environment{} // reported as incomplete in the reference; never as verified
	}
	in := harness.Inputs{
		Registry:    p.cfg.Registry(),
		Vendor:      vs.Vendor,
		Environment: env,
		Workspace:   withRevision(w, wStamp.Revision),
		Session:     overrideWithRevision(o, oStamp.Revision),
		Canonical:   p.cfg.Canonical,
	}
	if w == nil {
		in.Workspace = nil
	}
	if o == nil {
		in.Session = nil
	}
	eff, err := harness.Resolve(in)
	if err != nil {
		return harness.Snapshot{}, ErrSnapshotUnavailable
	}
	snap := harness.Snapshot{
		Ref: harness.SnapshotRef{
			Schema: harness.SnapshotSchema, WorkspaceID: req.WorkspaceID, SessionID: req.SessionID, Vendor: req.Vendor,
			Revisions: eff.Revisions, Fingerprint: eff.Fingerprint, ExecutableDigest: eff.ExecutableDigest(),
			WorkspaceDigest: eff.WorkspaceDigest(), ExecutableEntries: eff.ExecutableEntries(),
			FactsComplete: vs.FactsComplete, Default: w == nil && o == nil,
		},
		Effective: eff,
		Vendor:    vs.Vendor,
	}
	return snap.Clone(), nil
}

func isScope(err error) bool { return errors.Is(err, harness.ErrStoreScope) }
