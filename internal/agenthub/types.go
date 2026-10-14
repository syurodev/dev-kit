package agenthub

import (
	"encoding/json"
	"time"
)

// AgentType identifies a registered vendor adapter.
type AgentType string

const (
	AgentTypeMock   AgentType = "mock"
	AgentTypeClaude AgentType = "claude"
	AgentTypeCodex  AgentType = "codex"
)

// SessionStatus is the hub-visible lifecycle state of an AgentSession.
type SessionStatus string

const (
	SessionStatusIdle               SessionStatus = "idle"
	SessionStatusRunning            SessionStatus = "running"
	SessionStatusAwaitingPermission SessionStatus = "awaiting_permission"
	SessionStatusError              SessionStatus = "error"
	SessionStatusClosed             SessionStatus = "closed"
)

// AgentSession is the hub-owned session record (DevKit UUID, not ACP id).
type AgentSession struct {
	ID          string
	AgentType   AgentType
	ProjectPath string
	// WorkspaceID is the session's chosen context, independent of its cwd.
	// Empty means a standalone folder or an older, ambiguous session.
	WorkspaceID     string
	Status          SessionStatus
	ParentSessionID *string
	Depth           int
	Title           *string
	// Preview is where the session got to, one line: the first sentence of
	// the agent's last message, or why the turn failed. Written when a turn
	// ends. Agent output, stored like the transcript; MCP results blank it
	// (internal/mcp/tools_agenthub.go), the UI shows it under the title.
	Preview      string `json:"Preview,omitempty"`
	ACPSessionID string
	// ExtraReadWritePaths is the jail widening CreateSession was given (a
	// workspace's other services — see CreateInput). Stored with the session
	// because the jail is rebuilt from scratch on every resume and rewind,
	// and a grant that lived only in the first OpenSpec would silently narrow
	// back to Cwd the first time the process was respawned. Not sent to the
	// UI or to agents: nothing there reads it, and it is a list of paths.
	ExtraReadWritePaths []string `json:"-"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TurnRecord is one completed (or failed) prompt turn on a session.
type TurnRecord struct {
	ID        string
	SessionID string
	// PromptMessageID is the message id of the prompt that started the turn —
	// the echo's id. Empty on rows written before migration 22.
	PromptMessageID string
	StopReason      string
	UsageJSON       json.RawMessage
	RawMeta         json.RawMessage
	CreatedAt       time.Time
}

// NormalizedUsage is adapter-normalized cost/token info from vendor _meta.
type NormalizedUsage struct {
	CostUSD      float64
	InputTokens  int
	OutputTokens int
	RawMeta      json.RawMessage // always set to input meta copy
}

// TranscriptPage is one bounded window of a session's transcript, oldest
// entry first so a page can feed the same loader a full listing does.
//
// HasMore says older entries exist above the window — the transcript used to
// travel as one unbounded array, and a session with months of history paid
// for all of it on every open.
type TranscriptPage struct {
	Entries []TranscriptEntry `json:"entries"`
	HasMore bool              `json:"hasMore"`
	// State is the session-level rows (mode, config options, commands, usage)
	// that sit above the window. They are upserted in place and keep the seq
	// their kind first appeared at, so a long session's newest page did not
	// carry them and reopening it lost the mode and model pickers and the
	// command menu. Newest page only, and only kinds Entries does not hold.
	State []TranscriptEntry `json:"state,omitempty"`
}

// ContextFile is one bounded file suggestion for the prompt composer.
type ContextFile struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"`
}
