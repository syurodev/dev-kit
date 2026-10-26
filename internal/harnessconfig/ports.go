// Package harnessconfig turns a request to change the workspace harness profile
// (W) or a session override (S) into an exact, host-authored effective diff that
// a person can approve, and applies it only if every input is unchanged.
//
// It never resolves, merges or classifies on its own: internal/harness does, for
// the before and the after. It holds no SQL, no prompt UI and no preferences;
// those are ports implemented by the composition root. Nothing here is a public
// transport yet (H5), and nothing here changes a running session (H3/H4).
package harnessconfig

import (
	"context"

	"devkit/internal/harness"
)

// Store is the persistence port (implemented by storage.HarnessStore).
type Store interface {
	GetProfile(ctx context.Context, workspaceID string) (*harness.Profile, harness.Stamp, error)
	GetOverride(ctx context.Context, sessionID, workspaceID string) (*harness.Override, harness.Stamp, error)
	Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error)
}

// SessionScope is what the host knows about a live session.
type SessionScope struct {
	WorkspaceID string
	Vendor      string
}

// Directory resolves identity from host data, never from a request. A session
// that is gone, closed or without a workspace is ErrScopeGone.
type Directory interface {
	SessionScope(ctx context.Context, sessionID string) (SessionScope, error)
	WorkspaceExists(ctx context.Context, workspaceID string) (bool, error)
}

// VendorState is V and the vendor-defined facts for one vendor. FactsComplete is
// false when the host cannot report the command, arguments, URL and env names of
// the vendor's own servers and skills: that is unknown, not "none".
type VendorState struct {
	Vendor        harness.Vendor
	Environment   harness.Environment
	FactsComplete bool
}

// Vendors reads V from preferences and the vendor-defined facts.
type Vendors interface {
	// Names lists the vendors a workspace profile applies to.
	Names() []string
	State(ctx context.Context, vendor string) (VendorState, error)
}

// Policy holds the UI-owned settings. Revision changes whenever either changes,
// so an approval is bound to the policy it was given under. Agents cannot patch it.
type Policy struct {
	Revision        string
	AutoNarrowing   bool
	AllowExecutable bool
}

type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

// Validator applies the existing customization rules (URLs, env names, skill
// folders, size limits) to a candidate W without running anything.
type Validator interface {
	Validate(ctx context.Context, w harness.Profile) error
}
