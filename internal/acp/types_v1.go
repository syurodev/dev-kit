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
	AuthMethods       []AuthMethod    `json:"authMethods,omitempty"`
	Meta              json.RawMessage `json:"_meta,omitempty"`
}

// AuthMethod is one way the agent says a user can sign in (schema: AuthMethod).
// A "terminal" method is not run through authenticate: the client runs the
// agent's own command with Args appended, in a terminal the user can see.
// _meta["terminal-auth"] may carry that full command line instead, and stays
// raw here because what to trust in it is the adapter's decision, not the
// wire type's.
type AuthMethod struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type,omitempty"`
	Args        []string        `json:"args,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
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
	SessionID string            `json:"sessionId"`
	Modes     *SessionModeState `json:"modes,omitempty"`
	Meta      json.RawMessage   `json:"_meta,omitempty"`
}

// SessionModeState is the mode set session/new and session/load answer with
// (schema: SessionModeState). It is the only place the agent lists its modes:
// current_mode_update later names the new current one and nothing else.
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// SessionMode is one mode the agent can run in (schema: SessionMode).
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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
// claude-agent-acp 0.74.0: it echoes the session id and the mode set.
type SessionLoadResult struct {
	SessionID string            `json:"sessionId"`
	Modes     *SessionModeState `json:"modes,omitempty"`
	Meta      json.RawMessage   `json:"_meta,omitempty"`
}

// SessionDeleteRequest is the params object for session/delete. The agent
// tears down whatever it still holds in memory and removes the session's file
// from its own store; the result is empty.
type SessionDeleteRequest struct {
	SessionID string          `json:"sessionId"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// SessionSetModeRequest is the params object for session/set_mode.
type SessionSetModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SessionSetConfigOptionRequest is the params object for session/set_config_option.
//
// `configId`, and it is not a typo for the `id` the agent emits. A
// config_option_update lists options under `id`; the request that changes one
// names it `configId` — claude-agent-acp 0.74 reads `params.configId` and
// answers "Unknown config option: undefined" to anything else, which is what
// DevKit sent for as long as this said `optionId`. Every model and effort
// change failed, silently: the picker moved, the call was refused, and the
// optimistic update put the old value straight back.
type SessionSetConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// PromptRequest is the params object for session/prompt// PromptRequest is the params object for session/prompt// PromptRequest is the params object for session/prompt.
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

// ContentBlock is one unit of a prompt or a message. Field names, and which
// ones each variant requires, are taken from the v1 schema the SDK generates
// (@agentclientprotocol/sdk, dist/v2/schema), not from the prose:
//
//	text           text
//	image          data (base64), mimeType
//	resource_link  name, uri            — a pointer; the reader opens it
//	resource       resource{uri, text|blob, mimeType} — the contents, inline
//
// One struct rather than a union, because the wire is one JSON object with a
// discriminator and Go's zero values already mean "absent" once every field is
// omitempty. A union would buy correctness DevKit does not need and cost every
// call site a type switch.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Data is base64 for type=image; MimeType is required alongside it.
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	// URI and Name carry type=resource_link.
	URI  string `json:"uri,omitempty"`
	Name string `json:"name,omitempty"`
	// Resource carries type=resource: the contents travel with the prompt
	// instead of the reader having to open a path.
	Resource *ResourceContents `json:"resource,omitempty"`
}

// ResourceContents is the body of an embedded resource block. Exactly one of
// Text or Blob is set; Blob is base64.
type ResourceContents struct {
	URI      string `json:"uri"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
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
