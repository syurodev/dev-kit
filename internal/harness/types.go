// Package harness is the one definition of a session's effective harness
// configuration: which tool groups the DevKit MCP server serves, how large a
// tool result may be, and which skills and MCP servers the vendor is given.
//
// It is a standard-library-only leaf. The registry, the vendor's customization,
// the vendor's own environment facts and path canonicalization are all injected
// by the host, so nothing here reads a database, a file, the environment or the
// MCP registry, and nothing here grants authority: a profile only filters what
// is already available. Callers (Agent Hub, the MCP server, TEAM) consume an
// Effective value; none of them re-implements the merge.
//
// Three inputs are merged. V is the vendor's customization (preferences), W is
// the workspace profile and S is the session override. Absent and present-empty
// fields are different in storage (nil vs empty non-nil, `omitzero` in JSON);
// the merge itself never treats a zero value as "unset".
package harness

import "path/filepath"

// SchemaVersion is the version of the stored profile and override documents.
const SchemaVersion = 1

const (
	// DefaultResultBudget is the budget of a tool with no override. The MCP
	// server's ResultBudget must equal it (pinned by a test in internal/mcp).
	DefaultResultBudget = 24 * 1024
	MinResultBudget     = 2 * 1024
	MaxResultBudget     = 64 * 1024
)

// ProposeTool is the one tool that is served even when its group is off, so an
// agent can always ask for a group. It is a named exception, not a category.
const ProposeTool = "harness.profile.propose"

// RequestTool is the second named exception: the tool a session uses to ask its
// parent or the person for more tool groups (harness.group.request). It is
// served even when its group is off, for the same reason as ProposeTool: a
// session must be able to ask. Serving it changes no group and grants nothing.
const RequestTool = "harness.group.request"

// CutMode is how an over-budget plain-text result is shortened.
type CutMode string

const (
	// CutDefault is "no preference": the lower-precedence rule, then CutHead.
	CutDefault CutMode = ""
	CutHead    CutMode = "head"
	// CutHeadTail keeps both ends of an over-budget plain-text result around a
	// marker (internal/mcp cutHeadAndTail). Structured results keep their
	// structural shrink whatever the mode.
	CutHeadTail CutMode = "headTail"
)

// Group is the part of a tool name before its first dot (the whole name when
// it has none).
func Group(tool string) string {
	for i := 0; i < len(tool); i++ {
		if tool[i] == '.' {
			return tool[:i]
		}
	}
	return tool
}

// Tool is one registry fact. DefinitionBytes is description plus schemas as
// listed; BudgetExempt mirrors the MCP server's fixed exemptions.
type Tool struct {
	Name            string
	DefinitionBytes int
	BudgetExempt    bool
}

// Registry is the full, deterministic tool inventory the host serves.
type Registry struct{ Tools []Tool }

// Server is an MCP server definition. Env values never appear here, only names.
type Server struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	EnvKeys []string `json:"envKeys,omitempty"`
	Enabled bool     `json:"enabled"`
}

// Vendor (V) is the vendor's customization from preferences.
type Vendor struct {
	Revision       int64
	SkillFolders   []string
	DisabledSkills []string // SKILL.md paths
	DisabledMCP    []string
	Servers        []Server
}

// Environment is what the vendor's own configuration defines, as reported by
// the host (not the servers and folders DevKit configures). It is needed to
// reject colliding names and to see that a deny entry hides something.
type Environment struct {
	Servers []Server
	Skills  []string // SKILL.md paths
}

// Profile (W) is the stored workspace profile. A nil field is absent and
// inherits; a non-nil empty field is present and taken as written.
type Profile struct {
	Schema         int             `json:"schema"`
	Revision       int64           `json:"revision"`
	ToolGroups     map[string]bool `json:"toolGroups,omitzero"`
	ResultBudget   *BudgetProfile  `json:"resultBudget,omitzero"`
	DisabledSkills []string        `json:"disabledSkills,omitzero"`
	DisabledMCP    []string        `json:"disabledMCP,omitzero"`
	Servers        []Server        `json:"servers,omitzero"`
	SkillFolders   []string        `json:"skillFolders,omitzero"`
}

// BudgetProfile holds result budget rules by tool and by group.
type BudgetProfile struct {
	Tools  map[string]BudgetRule `json:"tools,omitzero"`
	Groups map[string]BudgetRule `json:"groups,omitzero"`
}

// BudgetRule sets a size, a cut mode or both. Each field falls back on its own:
// tool rule, then group rule, then the default.
type BudgetRule struct {
	Bytes *int    `json:"bytes,omitzero"`
	Cut   CutMode `json:"cut,omitzero"`
}

// Override (S) is the session override: groups turned on for one session.
type Override struct {
	Schema   int      `json:"schema"`
	Revision int64    `json:"revision"`
	Groups   []string `json:"groups,omitzero"`
}

// Inputs is everything Resolve needs.
type Inputs struct {
	Registry    Registry
	Vendor      Vendor
	Environment Environment
	Workspace   *Profile  // nil: no profile, today's behavior
	Session     *Override // nil: no override
	// Canonical resolves symlinks in trusted host code (filepath.EvalSymlinks
	// in production). Nil keeps paths lexical: absolute and cleaned only.
	Canonical func(path string) (string, error)
}

// Revisions are the input revisions an Effective was resolved from. W and S are
// nil when the input did not exist; a missing input is never revision 0.
type Revisions struct {
	Vendor    int64
	Workspace *int64
	Session   *int64
}

// Budget is a tool's effective result budget.
type Budget struct {
	Bytes  int
	Cut    CutMode
	Exempt bool
}

// GroupState is one tool group in the registry.
type GroupState struct {
	Name            string
	On              bool
	Tools           int
	DefinitionBytes int
}

// Effective is the resolved configuration. Treat it as read-only.
type Effective struct {
	Revisions      Revisions
	Fingerprint    string
	Groups         []GroupState // sorted by name
	DisabledSkills []string     // V ∪ W, sorted
	DisabledMCP    []string     // V ∪ W, sorted
	Servers        []Server     // DevKit-configured, after W replaced V, sorted by name
	SkillFolders   []string     // V ∪ W, canonical, sorted

	groupOn    map[string]bool
	budgets    map[string]Budget
	executable map[string]string // what could run or be loaded, keyed by kind:name
	execDigest string
	owned      map[string]bool // executable keys W defines
	suppressed map[string]bool // executable keys only W's deny lists hide
}

// GroupOn reports whether a registry group is on. An unknown group is off.
func (e Effective) GroupOn(group string) bool { return e.groupOn[group] }

// ServedWhateverItsGroup reports whether a tool is one of the named exceptions
// the server lists and accepts even when its group is off. Such a tool says
// nothing about whether its group is on.
func ServedWhateverItsGroup(tool string) bool { return tool == ProposeTool || tool == RequestTool }

// Served reports whether the MCP server lists and accepts a tool. The named
// exceptions are always served; every other tool follows its group.
func (e Effective) Served(tool string) bool {
	if ServedWhateverItsGroup(tool) {
		return true
	}
	_, known := e.budgets[tool]
	return known && e.groupOn[Group(tool)]
}

// Budget is a registry tool's effective budget.
func (e Effective) Budget(tool string) (Budget, bool) {
	b, ok := e.budgets[tool]
	return b, ok
}

// OffGroups lists the groups that are off, with their measured definition cost:
// the compact answer harness.profile.propose gives an agent.
func (e Effective) OffGroups() []GroupState {
	var off []GroupState
	for _, g := range e.Groups {
		if !g.On {
			off = append(off, g)
		}
	}
	return off
}

// Public error codes. They are published in bridge.keptCodes when the
// transport that exposes them lands (H5); this package only names them.
const (
	CodeGroupUnknown    = "harness.groupUnknown"
	CodeBudgetInvalid   = "harness.budgetInvalid"
	CodeProfileInvalid  = "harness.profileInvalid"
	CodeProfileConflict = "harness.profileConflict"
	CodeProposalStale   = "harness.proposalStale"
	CodeGroupOff        = "harness.groupOff"
	// Raised by the proposal service (internal/harnessconfig).
	CodeProfileDenied    = "harness.profileDenied"
	CodeFactsUnavailable = "harness.factsUnavailable"
	CodeNotAvailable     = "harness.notAvailable"
	CodeNoChange         = "harness.noChange"
	// CodeSnapshotUnavailable: a runtime's configuration could not be resolved.
	// The launch is refused; it never falls back to unfiltered defaults.
	CodeSnapshotUnavailable = "harness.snapshotUnavailable"
	// CodeNotEnforceable: the profile needs the MCP server to enforce it and this
	// endpoint cannot, so the launch is refused instead of run unfiltered.
	CodeNotEnforceable = "harness.notEnforceable"
	// CodeNewSessionRequired: reopening this session would adopt a changed set of
	// executables; they take effect in a new session only.
	CodeNewSessionRequired = "harness.newSessionRequired"
)

// Error carries a public code and host-authored text only: never a command,
// argument, URL, environment value or a third party's error text.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, message string) error { return &Error{Code: code, Message: message} }

// ErrStaleSessionOverride is the refusal of a launch whose session override (S)
// names a tool group this build does not register and which has no other stale
// reference. It reuses the published launch-refusal code and says, in fixed host
// text with no stored name, what is wrong and the ways out. It is a refusal only:
// the resolver stays strict and nothing here ignores the unknown name.
var ErrStaleSessionOverride = &Error{Code: CodeSnapshotUnavailable, Message: "This session was not started: its saved tool-group choices include a group this build does not have. Use Revoke on the unavailable group in the session's Tool groups panel and confirm, then open the session again. Closing the session also discards its saved tool-group choices."}

// lexical is the canonicalizer used when the host injects none.
func lexical(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fail(CodeProfileInvalid, "A skill path must be absolute.")
	}
	return filepath.Clean(path), nil
}
