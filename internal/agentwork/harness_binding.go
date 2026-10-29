package agentwork

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// This file holds the value types and the pure rules for binding a managed
// child to the harness configuration it was meant to run with (intended) and
// the one it actually runs with (actual). agentwork is a stdlib-only leaf, so
// it never imports internal/harness: the app adapter maps a harness snapshot to
// HarnessFacts. Nothing here knows a model, effort, account, command, path,
// URL or environment value, and nothing here is a public surface.

// HarnessBindingSchemaVersion versions the binding record itself. It is
// separate from ManagedPackageSchemaVersion and from the harness snapshot
// schema, so none of them has to move when another does.
const HarnessBindingSchemaVersion = 1

// HarnessSourceSchemaSupported is the one harness snapshot schema this build
// reads. The adapter maps harness.SnapshotSchema onto it and a test keeps the
// two equal; any other value fails closed.
const HarnessSourceSchemaSupported = 1

const (
	HarnessPhaseIntended = "intended"
	HarnessPhaseActual   = "actual"
)

// Refusal codes. They are fixed strings consumed by the later dispatcher and
// are not public error codes.
const (
	HarnessNotConfigured     = "harness_not_configured"
	HarnessUnavailable       = "harness_unavailable"
	HarnessSchemaUnsupported = "harness_schema_unsupported"
	HarnessOverridePresent   = "harness_override_present"
	HarnessRequiredGroupOff  = "harness_required_group_off"
	HarnessConfigChanged     = "harness_config_changed"
	HarnessEndpointNotLive   = "harness_endpoint_not_live"
	HarnessChildScope        = "harness_child_scope"
	HarnessChildMode         = "harness_child_mode"
	HarnessChildUnbound      = "harness_child_unbound"
	HarnessBindingStale      = "harness_binding_stale"
	HarnessBindingMissing    = "harness_binding_missing"
)

var (
	// ErrHarnessBindingExists is returned when a binding for the same attempt
	// and phase is already stored. The stored row is never replaced.
	ErrHarnessBindingExists = errors.New("harness binding already recorded for this attempt and phase")
	// ErrHarnessAttemptState means the attempt is not in a state this binding
	// phase applies to. It is not a configuration refusal.
	ErrHarnessAttemptState = errors.New("harness binding does not apply to this attempt state")
	// ErrHarnessPolicyClosed means the group is disabled, paused or ended.
	ErrHarnessPolicyClosed = errors.New("harness binding refused: managed group is closed")
)

// IntendedChild names the child about to be created. It names vendor and role
// only; model, effort and account stay under the existing model-choice and
// account policy and are deliberately absent.
type IntendedChild struct{ Vendor, Role string }

// HarnessFacts is the secret-free, host-mapped identity of one resolved
// configuration. A nil workspace or session revision means the input is
// absent; it is never zero.
type HarnessFacts struct {
	SourceSchema                       int
	Vendor, WorkspaceID                string
	VendorRevision                     int64
	WorkspaceRevision, SessionRevision *int64
	Fingerprint, ExecutableDigest      string
	FactsComplete, Default             bool
}

// HarnessResolution adds the groups that are on (intended) or served now
// (actual).
type HarnessResolution struct {
	Facts  HarnessFacts
	Groups []string
}

// HarnessBinding is one immutable row.
type HarnessBinding struct {
	ID            string
	SchemaVersion int
	Phase         string
	WorkspaceID   string
	BoardID       string
	ParentTaskID  string
	GroupID       string
	WorkItemID    string
	TaskID        string
	AttemptID     string
	PackageID     string
	Role          string
	SessionID     string // actual phase only
	Required      []string
	Facts         HarnessFacts
	CreatedAt     time.Time
}

// HarnessHost is implemented by the app adapter, the only place that knows
// both this package and the harness.
type HarnessHost interface {
	RequiredGroups(ctx context.Context, role string) ([]string, error)
	ResolveIntended(ctx context.Context, scope PackageScope, child IntendedChild) (HarnessResolution, error)
	// ResolveActual checks identity, scope, grants and mode of the attempt's
	// child itself and answers with what the child was given and serves now.
	ResolveActual(ctx context.Context, attempt Attempt) (HarnessResolution, error)
}

// ManagedHarnessStore is the ledger side of the binding.
type ManagedHarnessStore interface {
	GetManagedAttempt(context.Context, string) (Attempt, error)
	GetCurrentManagedAttempt(context.Context, string) (Attempt, error)
	GetWorkItem(context.Context, string) (WorkItem, error)
	GetWorkGroup(context.Context, string) (GroupState, error)
	GetContextPackage(context.Context, string) (StoredContextPackage, error)
	InsertHarnessBinding(ctx context.Context, b HarnessBinding, expectedAttemptRevision int64) error
	GetHarnessBinding(ctx context.Context, attemptID, phase string) (HarnessBinding, error)
}

// HarnessRefusal is the only refusal shape: a bounded fixed code and, for a
// missing group, registry group names (never a value).
type HarnessRefusal struct {
	Code   string
	Groups []string
}

func (e *HarnessRefusal) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Groups) == 0 {
		return "managed harness: " + e.Code
	}
	return "managed harness: " + e.Code + " (" + strings.Join(e.Groups, ",") + ")"
}

func refusal(code string, groups ...string) *HarnessRefusal {
	return &HarnessRefusal{Code: code, Groups: groups}
}

// ConfigurationNeeded reports whether the person can fix the refusal by
// configuration. It never says a retry or a rebuild will help.
func ConfigurationNeeded(err error) bool {
	var r *HarnessRefusal
	if !errors.As(err, &r) || r == nil {
		return false
	}
	switch r.Code {
	case HarnessNotConfigured, HarnessOverridePresent, HarnessRequiredGroupOff, HarnessConfigChanged:
		return true
	}
	return false
}

// compareFacts reports whether two configurations are the same. The session
// revision is not compared (a child with its own override is refused by
// checkResolution), and an absent revision differs from zero. Model, account,
// effort and the requirements digest are inputs to nothing here.
func compareFacts(a, b HarnessFacts) bool {
	return a.Vendor == b.Vendor && a.WorkspaceID == b.WorkspaceID && a.VendorRevision == b.VendorRevision &&
		sameRevision(a.WorkspaceRevision, b.WorkspaceRevision) &&
		a.Fingerprint == b.Fingerprint && a.ExecutableDigest == b.ExecutableDigest &&
		a.FactsComplete == b.FactsComplete && a.Default == b.Default
}

func sameRevision(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// normalizeGroups returns a sorted copy without empties or repeats.
func normalizeGroups(groups []string) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	uniq := out[:0]
	for i, g := range out {
		if i == 0 || g != out[i-1] {
			uniq = append(uniq, g)
		}
	}
	return uniq
}

func missingGroups(required, have []string) []string {
	set := make(map[string]bool, len(have))
	for _, g := range have {
		set[g] = true
	}
	var missing []string
	for _, g := range normalizeGroups(required) {
		if !set[g] {
			missing = append(missing, g)
		}
	}
	return missing
}

// checkResolution applies the fixed check order, and a refusal never falls
// through to a weaker check: unknown schema, an override (S) on the child,
// a required group that is not on, and finally a configuration that differs
// from the bound intended facts (nil intended skips that last comparison).
// Checks that need the host (identity, scope, mode, availability, endpoint)
// happen before it is called and arrive as refusals of their own.
func checkResolution(phase string, res HarnessResolution, required []string, intended *HarnessFacts) error {
	if res.Facts.SourceSchema != HarnessSourceSchemaSupported || (intended != nil && intended.SourceSchema != HarnessSourceSchemaSupported) {
		return refusal(HarnessSchemaUnsupported)
	}
	if phase != HarnessPhaseIntended && phase != HarnessPhaseActual {
		return refusal(HarnessSchemaUnsupported)
	}
	if res.Facts.SessionRevision != nil {
		return refusal(HarnessOverridePresent)
	}
	if missing := missingGroups(required, res.Groups); len(missing) > 0 {
		return refusal(HarnessRequiredGroupOff, missing...)
	}
	if intended != nil && !compareFacts(*intended, res.Facts) {
		return refusal(HarnessConfigChanged)
	}
	return nil
}
