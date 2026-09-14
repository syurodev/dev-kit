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
	ID              string
	AgentType       AgentType
	ProjectPath     string
	Status          SessionStatus
	ParentSessionID *string
	Depth           int
	Title           *string
	ACPSessionID    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TurnRecord is one completed (or failed) prompt turn on a session.
type TurnRecord struct {
	ID         string
	SessionID  string
	StopReason string
	UsageJSON  json.RawMessage
	RawMeta    json.RawMessage
	CreatedAt  time.Time
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
