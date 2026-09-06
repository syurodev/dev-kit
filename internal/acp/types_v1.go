package acp

import "encoding/json"

// InitializeRequest is the params object for the ACP initialize method.
type InitializeRequest struct {
	ProtocolVersion    int             `json:"protocolVersion"`
	ClientCapabilities map[string]any  `json:"clientCapabilities,omitempty"`
	ClientInfo         map[string]any  `json:"clientInfo,omitempty"`
	Meta               json.RawMessage `json:"_meta,omitempty"`
}

// InitializeResult is the result object for the ACP initialize method
// (schema: InitializeResponse).
type InitializeResult struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	AgentCapabilities map[string]any  `json:"agentCapabilities,omitempty"`
	AgentInfo         map[string]any  `json:"agentInfo,omitempty"`
	Meta              json.RawMessage `json:"_meta,omitempty"`
}

// SessionNewRequest is the params object for session/new
// (schema: NewSessionRequest).
type SessionNewRequest struct {
	Cwd string `json:"cwd"`
	// McpServers is required by schema v1 even when empty: omitempty made an
	// endpoint-less session/new fail with -32602 Invalid params. Always send a
	// non-nil slice.
	McpServers []any           `json:"mcpServers"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// SessionNewResult is the result object for session/new
// (schema: NewSessionResponse).
type SessionNewResult struct {
	SessionID string          `json:"sessionId"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// SessionLoadRequest is the params object for session/load (schema:
// LoadSessionRequest). Same shape as session/new plus the id being resumed;
// the agent replays the session's history as session/update notifications
// before the call returns.
type SessionLoadRequest struct {
	SessionID  string          `json:"sessionId"`
	Cwd        string          `json:"cwd"`
	McpServers []any           `json:"mcpServers"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// SessionLoadResult is the result object for session/load. Measured against
// claude-agent-acp 0.74.0: it echoes the session id and the mode set. Modes
// stay a raw blob for the same reason _meta does — the hub has no session-mode
// surface yet, and inventing structs for a field nothing reads is how a core
// type drifts from the wire.
type SessionLoadResult struct {
	SessionID string          `json:"sessionId"`
	Modes     json.RawMessage `json:"modes,omitempty"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// PromptRequest is the params object for session/prompt// PromptRequest is the params object for session/prompt.
type PromptRequest struct {
	SessionID string          `json:"sessionId"`
	Prompt    []ContentBlock  `json:"prompt"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// PromptResult is the result object for session/prompt
// (schema: PromptResponse).
type PromptResult struct {
	StopReason string          `json:"stopReason"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// ContentBlock is a displayable content unit (text baseline for v1 hub).
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SessionUpdateParams is the params object for session/update notifications
// (schema: SessionNotification). Update stays a polymorphic blob.
type SessionUpdateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// RequestPermissionParams is the params object for session/request_permission
// (schema: RequestPermissionRequest). Field name is toolCall per schema.
type RequestPermissionParams struct {
	SessionID string          `json:"sessionId"`
	Options   json.RawMessage `json:"options"`
	ToolCall  json.RawMessage `json:"toolCall"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// EnvVariable is one environment variable passed to an MCP server (schema: EnvVariable).
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McpServerStdio is the stdio variant of McpServer for session/new.mcpServers
// (schema: McpServerStdio). The stdio variant carries no "type" discriminator,
// and every field is required — build Env as an empty slice, never nil, or
// it serialises as null and the agent rejects the entry.
type McpServerStdio struct {
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env"`
}
