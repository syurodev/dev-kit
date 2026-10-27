package harnessconfig

import (
	"context"
	"errors"

	"devkit/internal/harness"
)

// The public controls are the one application contract MCP tools and the window
// share (H5a). Everything here is a bounded, detached value with fixed host
// vocabulary: no command, argument, URL, skill path, environment value or raw
// error cause ever travels in it. The implementation (internal/app) derives scope
// from the authenticated identity and the persisted session, never from a field
// of a request.

// GroupSetting is how a workspace profile (W) treats one tool group.
type GroupSetting string

const (
	// SettingInherit: W has no entry for the group, so the default (on) applies.
	SettingInherit GroupSetting = "inherit"
	SettingOn      GroupSetting = "on"
	SettingOff     GroupSetting = "off"
)

// Availability is a three-valued fact. Unknown is not "off": it means DevKit has
// nothing positive to say (no acknowledged endpoint, an unreadable configuration).
type Availability string

const (
	AvailableOn      Availability = "on"
	AvailableOff     Availability = "off"
	AvailableUnknown Availability = "unknown"
)

// GroupRow is one MCP tool group as the registry holds it and as the saved
// configuration and the running session treat it. Definition bytes are the size
// of the tool definitions a client receives, not tokens, cache savings or quota.
type GroupRow struct {
	Name            string       `json:"name"`
	Tools           int          `json:"tools"`
	DefinitionBytes int          `json:"definitionBytes"`
	Workspace       GroupSetting `json:"workspace"`
	// Session is true when the session override (S) turns the group on.
	Session bool `json:"session"`
	// Saved is what the saved V, W and S resolve to for a launch: what a reload or
	// a new session would serve.
	Saved Availability `json:"saved"`
	// Running is what the session's endpoint serves right now; unknown unless the
	// endpoint is open and has acknowledged exactly what it serves.
	Running Availability `json:"running"`
}

// SessionState is the point-in-time account of one session's harness. It is an
// observation, not a promise.
type SessionState struct {
	// State is the agenthub vocabulary: unconfigured, unavailable, not_applied,
	// current, reload_required or new_session_required.
	State string `json:"state"`
	// Live: the current endpoint acknowledged exactly what it serves and nothing is
	// pending. A stored change, an opening endpoint or a notification is never Live.
	Live bool `json:"live"`
	// Active: an enforcing endpoint exists. Paused: its admission is closed.
	// Pending: a stored change awaits reconciliation.
	Active  bool `json:"active"`
	Paused  bool `json:"paused"`
	Pending bool `json:"pending"`
	// ReloadRequired: the saved workspace or vendor configuration moved after this
	// session opened, or the endpoint serves tools the vendor was not told about.
	ReloadRequired bool `json:"reloadRequired"`
	// NewSessionRequired: executables moved; a reload cannot adopt them.
	NewSessionRequired bool `json:"newSessionRequired"`
	// Overrides are the groups the session override turns on.
	Overrides []string `json:"overrides"`
}

// ProfileView is the group projection of one workspace, and of one session of it
// when SessionID is set. Rows are sorted by name.
type ProfileView struct {
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId,omitempty"`
	// ProfileRevision is W's stamp. ProfilePresent is false for no profile (never
	// written, or deleted): an absent W is not an explicit one.
	ProfileRevision  int64         `json:"profileRevision"`
	ProfilePresent   bool          `json:"profilePresent"`
	OverrideRevision int64         `json:"overrideRevision,omitempty"`
	OverridePresent  bool          `json:"overridePresent,omitempty"`
	Groups           []GroupRow    `json:"groups"`
	Session          *SessionState `json:"session,omitempty"`
	// Budgets is set only when the read asked for them.
	Budgets *BudgetPage `json:"budgets,omitempty"`
}

// GroupRequest is an agent's ask to turn tool groups on for its own session.
// Groups empty is discovery: nothing is prepared and nobody is asked.
type GroupRequest struct {
	Groups []string `json:"groups,omitempty"`
	// Budgets is the other, mutually exclusive form: result-budget edits of the
	// agent's OWN workspace profile (never a session: S holds groups only). It
	// cannot be combined with Groups.
	Budgets []BudgetEdit `json:"budgets,omitempty"`
	// Rationale is the agent's own words; it is shown labelled as untrusted and
	// decides nothing.
	Rationale string `json:"rationale,omitempty"`
}

// GroupEdit is one W change from the editor: Inherit removes the entry.
type GroupEdit struct {
	Group   string       `json:"group"`
	Setting GroupSetting `json:"setting"`
}

// BudgetKind says whether a budget edit names one tool or a whole tool group.
type BudgetKind string

const (
	BudgetTool  BudgetKind = "tool"
	BudgetGroup BudgetKind = "group"
)

// BudgetEdit changes the result budget of one tool or group in the WORKSPACE
// profile, and nothing else. Size and cut mode are separate fields with three
// intents each: omitted leaves the field alone, a value sets it, and a reset
// removes it so the next level (group, then the 24 KiB / head default) applies. A
// rule left with neither field is removed. Sibling fields and every other rule are
// never touched. Sizes are bytes, 2,048 to 65,536 inclusive; the modes are the
// strings "head" and "headTail".
type BudgetEdit struct {
	Kind       BudgetKind `json:"kind"`
	Name       string     `json:"name"`
	Bytes      *int       `json:"bytes,omitempty"`
	ResetBytes bool       `json:"resetBytes,omitempty"`
	Cut        string     `json:"cut,omitempty"`
	ResetCut   bool       `json:"resetCut,omitempty"`
}

// BudgetSource says which level an effective budget field comes from.
type BudgetSource string

const (
	BudgetFromTool    BudgetSource = "tool"
	BudgetFromGroup   BudgetSource = "group"
	BudgetFromDefault BudgetSource = "default"
)

// BudgetFilter pages the per-tool budget read. The cursor is bound to the filter
// that produced it: presenting it with another group is refused, never reread as
// the start of a different list.
type BudgetFilter struct {
	Group  string `json:"group,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ToolBudgetRow is one registered tool's result limit: what the workspace profile
// says explicitly for the tool and for its group, what is in effect, and where each
// field of the effective value comes from. An exempt tool is never cut and has no
// limit to show. Sizes are bytes of host text, not tokens.
type ToolBudgetRow struct {
	Tool            string `json:"tool"`
	Group           string `json:"group"`
	DefinitionBytes int    `json:"definitionBytes"`
	Exempt          bool   `json:"exempt"`
	// ToolBytes and ToolCut are the explicit rule on the tool itself (nil / empty:
	// none). GroupBytes and GroupCut are the group's.
	ToolBytes      *int         `json:"toolBytes,omitempty"`
	ToolCut        string       `json:"toolCut,omitempty"`
	GroupBytes     *int         `json:"groupBytes,omitempty"`
	GroupCut       string       `json:"groupCut,omitempty"`
	EffectiveBytes int          `json:"effectiveBytes,omitempty"`
	EffectiveCut   string       `json:"effectiveCut,omitempty"`
	BytesFrom      BudgetSource `json:"bytesFrom,omitempty"`
	CutFrom        BudgetSource `json:"cutFrom,omitempty"`
}

// GroupBudgetRow is one group's explicit rule and how many of its tools a rule can
// reach (the others are exempt from cutting).
type GroupBudgetRow struct {
	Group    string `json:"group"`
	Tools    int    `json:"tools"`
	Cuttable int    `json:"cuttable"`
	Bytes    *int   `json:"bytes,omitempty"`
	Cut      string `json:"cut,omitempty"`
}

// BudgetPage is a bounded page of the workspace's result limits. Groups appear on
// the first page only.
type BudgetPage struct {
	WorkspaceID     string           `json:"workspaceId"`
	ProfileRevision int64            `json:"profileRevision"`
	ProfilePresent  bool             `json:"profilePresent"`
	DefaultBytes    int              `json:"defaultBytes"`
	MinBytes        int              `json:"minBytes"`
	MaxBytes        int              `json:"maxBytes"`
	Group           string           `json:"group,omitempty"`
	Groups          []GroupBudgetRow `json:"groups,omitempty"`
	Tools           []ToolBudgetRow  `json:"tools"`
	NextCursor      string           `json:"nextCursor,omitempty"`
}

// ReviewChange is one host-authored effect of a prepared change. Scope is
// "stored" for the saved configuration and "running" for the session.
type ReviewChange struct {
	Vendor string `json:"vendor,omitempty"`
	Field  string `json:"field"`
	Key    string `json:"key,omitempty"`
	Effect string `json:"effect"`
	Scope  string `json:"scope"`
}

// Review is a prepared proposal as the person sees it before approving. ID is a
// lookup key held by this process for this caller, not an approval.
type Review struct {
	ID                string         `json:"id"`
	Target            string         `json:"target"`
	WorkspaceID       string         `json:"workspaceId"`
	SessionID         string         `json:"sessionId,omitempty"`
	Tier              string         `json:"tier"`
	Summary           string         `json:"summary"`
	Changes           []ReviewChange `json:"changes"`
	NeedsConfirmation bool           `json:"needsConfirmation"`
	// RationaleUntrusted is the proposer's text; display it labelled as untrusted.
	RationaleUntrusted string `json:"rationaleUntrusted,omitempty"`
}

// OutcomeStatus is the one word a transport and the window agree on.
type OutcomeStatus string

const (
	// OutcomeApplied: committed. With Live the current endpoint acknowledged it;
	// without, the change is saved for the next permitted launch.
	OutcomeApplied OutcomeStatus = "applied"
	// OutcomeStaged: committed and installed on an endpoint whose vendor runtime is
	// still opening. Not live.
	OutcomeStaged OutcomeStatus = "staged"
	// OutcomePaused: committed, but the session's tool access is closed until it is
	// reconciled. Do not propose or apply again.
	OutcomePaused OutcomeStatus = "paused"
	// OutcomeUnknown: the store could not say whether the write is durable. Access
	// stays closed; reconcile. This is neither a rollback nor a success.
	OutcomeUnknown OutcomeStatus = "unknown"
)

// Outcome is what a change or a reconciliation did, in a form that survives a
// failure response. It carries no tool, path or value.
type Outcome struct {
	Status OutcomeStatus `json:"status"`
	Target string        `json:"target,omitempty"`
	Commit CommitState   `json:"commit"`
	// Publication is installed, staged, pending or empty.
	Publication PublicationState `json:"publication,omitempty"`
	// Code is the published harness code when the outcome is not clean.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`

	ProfileRevision  int64 `json:"profileRevision,omitempty"`
	ProfilePresent   bool  `json:"profilePresent,omitempty"`
	OverrideRevision int64 `json:"overrideRevision,omitempty"`
	OverridePresent  bool  `json:"overridePresent,omitempty"`

	// Serving is the identity the endpoint acknowledged: the S stamp it serves and
	// the fingerprint of the effective configuration.
	ServingOverrideRevision int64  `json:"servingOverrideRevision,omitempty"`
	ServingOverridePresent  bool   `json:"servingOverridePresent,omitempty"`
	ServingFingerprint      string `json:"servingFingerprint,omitempty"`

	Live bool `json:"live"`
	// ReloadRequired: other sessions keep the profile they opened with until they
	// are reloaded or opened again.
	ReloadRequired bool `json:"reloadRequired,omitempty"`
	// DiscoveryReloadRequired: the server accepts calls to the newly enabled groups
	// but the vendor's tool list may not show them until it refreshes or reloads.
	DiscoveryReloadRequired bool `json:"discoveryReloadRequired,omitempty"`
	// Recovery is short, host-authored guidance for the person or the agent.
	Recovery string `json:"recovery,omitempty"`
	// Guidance is the replacement note for the model; set only for groups the
	// endpoint actually serves.
	Guidance string `json:"guidance,omitempty"`
}

// CleanRationale is the form in which an agent's words may be shown: one bounded
// line of printable text, control and formatting characters removed. It is the same
// cleaning a prepared proposal applies, exported so the scope question shows the
// agent's text no differently from the confirmation.
func CleanRationale(s string) string { return cleanRationale(s) }

// Fixed recovery text per status. It names no tool, path or value.
const (
	recoveryPaused  = "The change is saved, but this session's tool access is paused. Reconcile the session; do not propose the change again."
	recoveryUnknown = "DevKit could not confirm whether the change was saved. This session's tool access stays paused. Reconcile the session to read the result back; do not propose the change again."
	recoveryStaged  = "The change is saved and prepared, but the session's runtime has not confirmed it yet."
	recoveryStored  = "The change is saved. It takes effect the next time the session is allowed to start."
)

// OutcomeOf reports what a change did. An error that carries no committed result
// (a refusal before the write) is returned as is and yields no outcome: nothing
// was saved. A *PublicationError yields the outcome of its committed result.
func OutcomeOf(target Target, res Result, err error) (Outcome, error) {
	var pe *PublicationError
	switch {
	case err == nil:
	case errors.As(err, &pe):
		res = pe.Result
	default:
		return Outcome{}, err
	}
	out := Outcome{
		Target: string(target), Commit: res.Commit, Publication: res.Publication.State,
		ProfileRevision: res.Profile.Revision, ProfilePresent: res.Profile.Present,
		OverrideRevision: res.Override.Revision, OverridePresent: res.Override.Present,
		ServingOverrideRevision: res.Publication.Serving.Session.Revision,
		ServingOverridePresent:  res.Publication.Serving.Session.Present,
		ServingFingerprint:      res.Publication.Serving.Fingerprint,
		Live:                    res.Live, ReloadRequired: res.ReloadRequired,
	}
	switch {
	case res.Commit == CommitUnknown:
		out.Status, out.Live = OutcomeUnknown, false
		out.Code, out.Message, out.Recovery = harness.CodeSnapshotUnavailable, ErrPublicationPending.Message, recoveryUnknown
	case res.Publication.State == PublicationPending:
		out.Status, out.Live = OutcomePaused, false
		out.Code, out.Message, out.Recovery = harness.CodeSnapshotUnavailable, ErrPublicationPending.Message, recoveryPaused
	case res.Publication.State == PublicationStaged:
		out.Status, out.Live = OutcomeStaged, false
		out.Recovery = recoveryStaged
	default:
		out.Status = OutcomeApplied
		if res.Publication.State == PublicationNone {
			out.Recovery = recoveryStored
		}
	}
	return out, nil
}

// OutcomeOfReconcile reports what a reconciliation did. It changes nothing
// durable, so Commit is empty; a failure leaves the session paused.
func OutcomeOfReconcile(pub Publication, err error) (Outcome, error) {
	var pe *PublicationError
	if err != nil && !errors.As(err, &pe) {
		return Outcome{}, err
	}
	out := Outcome{
		Publication:             pub.State,
		ServingOverrideRevision: pub.Serving.Session.Revision, ServingOverridePresent: pub.Serving.Session.Present,
		ServingFingerprint: pub.Serving.Fingerprint, Live: pub.State == PublicationInstalled,
	}
	switch {
	case err != nil || pub.State == PublicationPending:
		out.Status, out.Live, out.Publication = OutcomePaused, false, PublicationPending
		out.Code, out.Message, out.Recovery = harness.CodeSnapshotUnavailable, ErrPublicationPending.Message, recoveryPaused
	case pub.State == PublicationStaged:
		out.Status, out.Live = OutcomeStaged, false
		out.Recovery = recoveryStaged
	case pub.State == PublicationNone:
		out.Status = OutcomeApplied
		out.Recovery = "Nothing is running for this session, so there was nothing to reconcile."
	default:
		out.Status = OutcomeApplied
	}
	return out, nil
}

// RequestResult is the answer to an agent's group request: the discovery
// projection, and the outcome when a change was made.
type RequestResult struct {
	// Discovery is true when no groups were requested and nothing was prepared.
	Discovery bool        `json:"discovery"`
	Profile   ProfileView `json:"profile"`
	Outcome   *Outcome    `json:"outcome,omitempty"`
}

// Controls is the application facade MCP tools and the window both use. Every
// method resolves scope from the authenticated identity in ctx and from the
// persisted session and workspace, and refuses a caller the operation is not
// for. Nothing here applies a change without the shared confirmation flow.
type Controls interface {
	// Profile is the agent's own session: groups, saved and running state.
	Profile(ctx context.Context) (ProfileView, error)
	// Request asks, for the agent's own session, to turn groups on. With no groups
	// it is discovery and changes nothing. Otherwise the person chooses this
	// session or the workspace, reviews the exact effect and approves; the Outcome
	// says what was saved and what became of the running endpoint.
	Request(ctx context.Context, req GroupRequest) (RequestResult, error)

	// WorkspaceProfile and SessionProfile are the window's reads.
	WorkspaceProfile(ctx context.Context, workspaceID string) (ProfileView, error)
	SessionProfile(ctx context.Context, workspaceID, sessionID string) (ProfileView, error)
	// PrepareWorkspaceGroups prepares an edit of W's group entries only.
	PrepareWorkspaceGroups(ctx context.Context, workspaceID string, edits []GroupEdit) (Review, error)
	// PrepareRevoke prepares removing groups from a session's override.
	PrepareRevoke(ctx context.Context, workspaceID, sessionID string, groups []string) (Review, error)
	// ProfileWithBudgets is Profile plus a bounded page of the agent's own
	// workspace's result limits.
	ProfileWithBudgets(ctx context.Context, f BudgetFilter) (ProfileView, error)
	// WorkspaceBudgets is the window's bounded read of a workspace's result limits.
	WorkspaceBudgets(ctx context.Context, workspaceID string, f BudgetFilter) (BudgetPage, error)
	// PrepareBudgetEdits prepares partial result-budget edits of a workspace's
	// profile. Writes nothing; Apply asks and commits.
	PrepareBudgetEdits(ctx context.Context, workspaceID string, edits []BudgetEdit) (Review, error)
	// Apply asks the person through the shared hub and commits the prepared change.
	Apply(ctx context.Context, id string) (Outcome, error)
	Cancel(ctx context.Context, id string) error
	// Reconcile brings a session's endpoint to its latest durable override.
	Reconcile(ctx context.Context, workspaceID, sessionID string) (Outcome, error)
}
