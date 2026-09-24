package codexapp

import "encoding/json"

// Item and turn notifications, and the approval requests about items: what
// agenthub's translator reads. Only the fields a translation uses are
// modelled; the whole item still travels on as raw JSON (FR-8), so a field
// not listed here is kept, just not interpreted. Shapes are from
// `codex app-server generate-ts` for 0.147.0 and checked against the Phase 0
// capture in items_test.go.

const (
	MethodItemStarted           = "item/started"
	MethodItemCompleted         = "item/completed"
	MethodAgentMessageDelta     = "item/agentMessage/delta"
	MethodReasoningSummaryDelta = "item/reasoning/summaryTextDelta"
	MethodTurnPlanUpdated       = "turn/plan/updated"
	MethodTokenUsageUpdated     = "thread/tokenUsage/updated"
	MethodThreadNameUpdated     = "thread/name/updated"
	MethodTurnCompleted         = "turn/completed"
	MethodError                 = "error"

	// Server requests about an item.
	MethodCommandApproval     = "item/commandExecution/requestApproval"
	MethodFileChangeApproval  = "item/fileChange/requestApproval"
	MethodPermissionsApproval = "item/permissions/requestApproval"
	// MethodMcpElicitation is an MCP server asking the person something —
	// and, with _meta.codex_approval_kind "mcp_tool_call", Codex itself
	// asking whether an MCP tool may run.
	MethodMcpElicitation = "mcpServer/elicitation/request"
)

// MCP tool-call approval metadata (codex-rs protocol/src/mcp_approval_meta.rs).
const (
	McpApprovalKindKey      = "codex_approval_kind"
	McpApprovalKindToolCall = "mcp_tool_call"
	McpPersistKey           = "persist"
	McpPersistSession       = "session"
	McpToolTitleKey         = "tool_title"
	McpToolParamsKey        = "tool_params"
)

// Elicitation actions.
const (
	ElicitAccept  = "accept"
	ElicitDecline = "decline"
	ElicitCancel  = "cancel"
)

// Item types.
const (
	ItemAgentMessage     = "agentMessage"
	ItemReasoning        = "reasoning"
	ItemUserMessage      = "userMessage"
	ItemCommandExecution = "commandExecution"
	ItemFileChange       = "fileChange"
	ItemMcpToolCall      = "mcpToolCall"
	ItemWebSearch        = "webSearch"
	ItemHookPrompt       = "hookPrompt"
)

// Statuses shared by command, file-change and MCP items.
const (
	ItemInProgress = "inProgress"
	ItemCompleted  = "completed"
	ItemFailed     = "failed"
	ItemDeclined   = "declined"
)

// Approval decisions. acceptWithExecpolicyAmendment is deliberately absent:
// it writes a permanent rule, and an approval must never do that.
const (
	DecisionAccept           = "accept"
	DecisionAcceptForSession = "acceptForSession"
	DecisionDecline          = "decline"
	DecisionCancel           = "cancel"
)

// ItemNotification is item/started and item/completed.
type ItemNotification struct {
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
	Item     json.RawMessage `json:"item"`
}

// ItemHeader is what every item carries.
type ItemHeader struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type AgentMessageItem struct {
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Phase *string `json:"phase"`
}

type ReasoningItem struct {
	ID      string   `json:"id"`
	Summary []string `json:"summary"`
}

// CommandAction is Codex's parse of what a command does. Its Command is the
// command as the model wrote it; CommandExecutionItem.Command is that wrapped
// in the shell invocation (`/usr/bin/zsh -lc "…"`).
type CommandAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type CommandExecutionItem struct {
	ID               string          `json:"id"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	Status           string          `json:"status"`
	CommandActions   []CommandAction `json:"commandActions"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	DurationMs       *int64          `json:"durationMs"`
}

type PatchChangeKind struct {
	Type     string  `json:"type"` // add | delete | update
	MovePath *string `json:"move_path"`
}

// FileUpdateChange is one file in a fileChange item. Diff is a unified diff's
// hunks for an update, and the whole content for an add (measured: no @@).
type FileUpdateChange struct {
	Path string          `json:"path"`
	Kind PatchChangeKind `json:"kind"`
	Diff string          `json:"diff"`
}

type FileChangeItem struct {
	ID      string             `json:"id"`
	Changes []FileUpdateChange `json:"changes"`
	Status  string             `json:"status"`
}

type McpToolCallError struct {
	Message string `json:"message"`
}

type McpToolCallItem struct {
	ID        string            `json:"id"`
	Server    string            `json:"server"`
	Tool      string            `json:"tool"`
	Status    string            `json:"status"`
	Arguments json.RawMessage   `json:"arguments"`
	Result    json.RawMessage   `json:"result"`
	Error     *McpToolCallError `json:"error"`
}

type WebSearchItem struct {
	ID    string `json:"id"`
	Query string `json:"query"`
}

// DeltaNotification is item/agentMessage/delta and
// item/reasoning/summaryTextDelta. SummaryIndex is reasoning-only: which
// summary paragraph (0-based) this delta belongs to. agentMessage/delta does
// not carry the field, so it decodes as zero there — harmless, since the
// translator only reads it for reasoning deltas.
type DeltaNotification struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	ItemID       string `json:"itemId"`
	Delta        string `json:"delta"`
	SummaryIndex int    `json:"summaryIndex"`
}

type TurnPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"` // pending | inProgress | completed
}

type TurnPlanUpdatedNotification struct {
	Explanation *string        `json:"explanation"`
	Plan        []TurnPlanStep `json:"plan"`
}

type TokenUsageBreakdown struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

type ThreadTokenUsage struct {
	Total              TokenUsageBreakdown `json:"total"`
	Last               TokenUsageBreakdown `json:"last"`
	ModelContextWindow *int64              `json:"modelContextWindow"`
}

type TokenUsageUpdatedNotification struct {
	ThreadID   string           `json:"threadId"`
	TurnID     string           `json:"turnId"`
	TokenUsage ThreadTokenUsage `json:"tokenUsage"`
}

type ThreadNameUpdatedNotification struct {
	ThreadID   string `json:"threadId"`
	ThreadName string `json:"threadName,omitempty"`
}

type TurnCompletedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type ErrorNotification struct {
	Error     TurnError `json:"error"`
	WillRetry bool      `json:"willRetry"`
	ThreadID  string    `json:"threadId"`
	TurnID    string    `json:"turnId"`
}

// CommandApprovalParams is item/commandExecution/requestApproval.
// AvailableDecisions lists what this request accepts, as strings or objects
// (measured under untrusted: accept, an execpolicy amendment, cancel — no
// decline, no acceptForSession). nil means the field was absent.
type CommandApprovalParams struct {
	ThreadID           string            `json:"threadId"`
	TurnID             string            `json:"turnId"`
	ItemID             string            `json:"itemId"`
	Reason             *string           `json:"reason"`
	Command            *string           `json:"command"`
	Cwd                *string           `json:"cwd"`
	CommandActions     []CommandAction   `json:"commandActions"`
	AvailableDecisions []json.RawMessage `json:"availableDecisions"`
}

type FileChangeApprovalParams struct {
	ThreadID  string  `json:"threadId"`
	TurnID    string  `json:"turnId"`
	ItemID    string  `json:"itemId"`
	Reason    *string `json:"reason"`
	GrantRoot *string `json:"grantRoot"`
}

type PermissionsApprovalParams struct {
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	ItemID      string          `json:"itemId"`
	Reason      *string         `json:"reason"`
	Permissions json.RawMessage `json:"permissions"`
}

// ApprovalResponse answers a command or file-change approval.
type ApprovalResponse struct {
	Decision string `json:"decision"`
}

// PermissionsApprovalResponse answers item/permissions/requestApproval.
// DevKit always grants nothing: under externalSandbox the jail, not Codex,
// decides access.
type PermissionsApprovalResponse struct {
	Permissions struct{} `json:"permissions"`
	Scope       string   `json:"scope"`
}

// McpElicitationParams is mcpServer/elicitation/request. Meta is the
// request's _meta, which is where a tool-call approval says what it is.
type McpElicitationParams struct {
	ThreadID   string          `json:"threadId"`
	TurnID     *string         `json:"turnId"`
	ServerName string          `json:"serverName"`
	Mode       string          `json:"mode"`
	Meta       json.RawMessage `json:"_meta"`
	Message    string          `json:"message"`
}

// McpElicitationResponse answers it. Content and Meta are sent as null when
// empty: both keys are required on the wire.
type McpElicitationResponse struct {
	Action  string          `json:"action"`
	Content json.RawMessage `json:"content"`
	Meta    json.RawMessage `json:"_meta"`
}
