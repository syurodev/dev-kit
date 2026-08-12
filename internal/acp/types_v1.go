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
	Cwd        string          `json:"cwd"`
	McpServers []any           `json:"mcpServers,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// SessionNewResult is the result object for session/new
// (schema: NewSessionResponse).
type SessionNewResult struct {
	SessionID string          `json:"sessionId"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// PromptRequest is the params object for session/prompt.
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
