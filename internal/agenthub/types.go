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
	// AgentTypeOpenCode is opencode (https://opencode.ai), run as `opencode acp`.
	AgentTypeOpenCode AgentType = "opencode"
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
	Preview string `json:"Preview,omitempty"`
	// Stop is why the last turn did not complete, when it did not (see
	// SessionStop). Set when a turn ends in a failure, cleared when the next turn
	// starts; nil on a session whose last turn completed.
	Stop *SessionStop `json:"Stop,omitempty"`
	// AccountID is the vendor account this session's process runs as ("" is the
	// built-in account: the machine's own Claude login, DevKit's own Codex home).
	// Set when a turn moves the session to another account (account_switch.go).
	AccountID    string `json:"AccountID,omitempty"`
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
	// Activity is what the session's live process is doing that no DevKit turn
	// started (bgwork.go). Derived from the process, never stored: Service.ListSessions
	// and GetStatus fill it, the store does not, and MCP results blank it.
	Activity *SessionActivity `json:"activity,omitempty"`
}

// SessionActivity is what a session's live process is doing that no DevKit turn started.
type SessionActivity struct {
	AgentTurn bool `json:"agentTurn,omitempty"` // the vendor runs a turn of its own (taskType "turn")
	Tasks     int  `json:"tasks,omitempty"`     // running background tasks other than that turn (taskType "shell", others)
}

// Zero reports that there is nothing to show.
func (a SessionActivity) Zero() bool { return !a.AgentTurn && a.Tasks == 0 }

// TurnRecord is one completed (or failed) prompt turn on a session.
type TurnRecord struct {
	Attribution *TurnAttribution
	ID          string
	SessionID   string
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
	CostUSD               float64
	InputTokens           int
	OutputTokens          int
	TotalTokens           *int            `json:",omitempty"`
	CachedInputTokens     *int            `json:",omitempty"`
	CachedWriteTokens     *int            `json:",omitempty"`
	ReasoningOutputTokens *int            `json:",omitempty"`
	RawMeta               json.RawMessage // always set to input meta copy
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
