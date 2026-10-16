package agenthub

import (
	"encoding/json"
	"strings"

	"devkit/internal/acp"
)

// UpdateKind is the `sessionUpdate` discriminator of an ACP session/update
// notification. The transport keeps the notification a raw blob (see
// acp.SessionUpdateParams); this is where it becomes something the hub and the
// UI can branch on without either of them learning ACP's wire shape.
type UpdateKind string

const (
	UpdateUserMessageChunk  UpdateKind = "user_message_chunk"
	UpdateAgentMessageChunk UpdateKind = "agent_message_chunk"
	UpdateAgentThoughtChunk UpdateKind = "agent_thought_chunk"
	UpdateToolCall          UpdateKind = "tool_call"
	UpdateToolCallUpdate    UpdateKind = "tool_call_update"
	UpdatePlan              UpdateKind = "plan"
	UpdateAvailableCommands UpdateKind = "available_commands_update"
	UpdateCurrentMode       UpdateKind = "current_mode_update"
	// UpdateUsage was not in the spec text this parser was written from; it
	// turned up in real claude-agent-acp captures (0.74, 0.78) and is the source of
	// the context counter and running cost the composer shows.
	UpdateUsage UpdateKind = "usage_update"
	// UpdateSessionInfo carries what the vendor knows about the session as a
	// whole. Two shapes share the kind: a generated title, and (behind the
	// goal extension) an objective in _meta. Only the title is modelled — an
	// objective is something a human typed with /goal, not a name to show.
	UpdateSessionInfo UpdateKind = "session_info_update"
	// UpdateSubagentSpawned and UpdateSubagentState report an agent the vendor
	// started inside its own process. They are not DevKit sessions: nothing
	// here created them, Invoke cannot reach them, and CloseSession has no
	// hold on them. They are events in the parent's transcript, and the
	// vendor's subagentSessionId is the only handle to them.
	UpdateSubagentSpawned UpdateKind = "subagent_spawned"
	UpdateSubagentState   UpdateKind = "subagent_state_update"
	// UpdateConfigOptions carries the whole set of per-session settings the
	// agent exposes — model, effort, agent, fast mode — with their current
	// values. Every notification is the full set, not a delta, so the latest one
	// replaces the last (see isSessionState).
	UpdateConfigOptions UpdateKind = "config_option_update"
	// UpdateAsyncTask* report work the vendor runs in the background, past the
	// end of the turn that started it. DevKit asks for them: the air extension
	// declared in devkitClientCapabilities lists asyncTasks, so the agent sends
	// them whether or not anything renders them.
	UpdateAsyncTaskSpawned  UpdateKind = "async_task_spawned"
	UpdateAsyncTaskProgress UpdateKind = "async_task_progress"
	UpdateAsyncTaskState    UpdateKind = "async_task_state_update"
	// UpdateConnectors is DevKit's own: the MCP servers a vendor started for the
	// session and their states, on `_devkit.connectors`. No vendor sends it; an
	// adapter that learns of them (Codex's startup status) writes it. It is
	// session state — one current list, replaced whole — not a thing said.
	UpdateConnectors UpdateKind = "devkit_connectors_update"
	// UpdateUnknown is a kind this build does not model. The raw blob is kept
	// so a vendor that ships a new kind degrades to "not rendered" rather than
	// "silently dropped" — the Meta Inspector surface reads Raw.
	UpdateUnknown UpdateKind = "unknown"
)

// ContentBlock is one ACP displayable unit. It is the wire type, not a copy of
// it: the transcript stores what the vendor sent, so a second struct here only
// bought a chance to drift. It already had — acp.ContentBlock grew `resource`
// when prompts learned to carry file contents, and the copy did not, so an
// embedded resource coming back from the agent lost its body on the way to the
// UI. Alias, so there is one shape and one place to widen it.
type ContentBlock = acp.ContentBlock

// SessionMode is one mode the agent advertised. The wire type, for the same
// reason ContentBlock is.
type SessionMode = acp.SessionMode

// ToolCallLocation is a file the tool call touched. ACP says clients should
// render these to implement follow-along.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ToolCallContent is one of ACP's three tool-call content shapes.
type ToolCallContent struct {
	// Type is "content", "diff" or "terminal".
	Type string `json:"type"`
	// Content is set when Type == "content".
	Content *ContentBlock `json:"content,omitempty"`
	// Path/OldText/NewText are set when Type == "diff". OldText is nil for a
	// newly created file, which is not the same as an empty file.
	Path    string  `json:"path,omitempty"`
	OldText *string `json:"oldText,omitempty"`
	NewText string  `json:"newText,omitempty"`
	// TerminalID is set when Type == "terminal" and refers to a terminal the
	// agent created through ACP's terminal/* methods.
	TerminalID string `json:"terminalId,omitempty"`
	// Truncated says the page read shortened this item: text cut to a
	// preview, or a diff too large to preview reduced to its path.
	Truncated bool `json:"truncated,omitempty"`
}

// ToolCall is the normalized form of a tool_call / tool_call_update payload.
type ToolCall struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Kind  string `json:"kind,omitempty"`
	// Name is the tool itself ("Read", "Agent", "compact"). Kind cannot tell a
	// subagent from a compaction: claude-agent-acp 0.78 sends both as `think`.
	// The top-level `name` arrived in 0.77; compaction carries only
	// _meta.claudeCode.toolName, which is read when `name` is absent.
	Name      string             `json:"name,omitempty"`
	Status    string             `json:"status,omitempty"`
	Content   []ToolCallContent  `json:"content,omitempty"`
	Locations []ToolCallLocation `json:"locations,omitempty"`
	RawInput  json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput json.RawMessage    `json:"rawOutput,omitempty"`
	// Truncated says this copy had oversized raw fields cut to a preview. Set
	// only on the page read — the stored row and the detail read stay whole.
	Truncated bool `json:"truncated,omitempty"`
}

// PlanEntry is one step of a `plan` update.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// AvailableCommand is one entry of an available_commands_update.
type AvailableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// AvailableCommandInput is present when a command expects text after its name.
type AvailableCommandInput struct {
	Hint string `json:"hint,omitempty"`
}

// Usage is the context/cost snapshot carried by a usage_update. Shape taken
// from a live capture, not from the spec: `used`/`size` are context tokens and
// `cost` is a currency amount, not a token count.
type Usage struct {
	Used     int64   `json:"used"`
	Size     int64   `json:"size"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency,omitempty"`
	// RateLimit is the subscription's own ceiling, which the context window
	// says nothing about: a turn can fit and still be refused.
	RateLimit *RateLimit `json:"rateLimit,omitempty"`
	// Tokens and Limits are what only some vendors report; see TokenBreakdown
	// and LimitWindow. Absent means "this vendor does not say", not zero.
	Tokens *TokenBreakdown `json:"tokens,omitempty"`
	Limits []LimitWindow   `json:"limits,omitempty"`
}

// ConfigSelectOption is one selectable value of a select-typed ConfigOption.
//
// The wire allows either a flat list of values or a list of named groups of
// them; both arrive here flat, with Group naming the group a value came from.
// Claude groups its models, and a flat list with the grouping still readable is
// less to get wrong than two shapes for the same menu.
type ConfigSelectOption struct {
	Value            string               `json:"value"`
	Name             string               `json:"name,omitempty"`
	Description      string               `json:"description,omitempty"`
	Group            string               `json:"group,omitempty"`
	DefaultEffort    string               `json:"defaultEffort,omitempty"`
	SupportedEfforts []ConfigSelectOption `json:"supportedEfforts,omitempty"`
}

// ConfigOption is one per-session setting from a config_option_update.
//
// CurrentValue stays raw because its type follows Type: a value id on a
// "select", a bool on a "boolean". Raw is what the UI wants anyway — it reaches
// the frontend as a JSON string or a JSON boolean, which is exactly the union
// TypeScript needs.
type ConfigOption struct {
	ID           string               `json:"id"`
	Name         string               `json:"name,omitempty"`
	Description  string               `json:"description,omitempty"`
	Category     string               `json:"category,omitempty"`
	Type         string               `json:"type,omitempty"`
	CurrentValue json.RawMessage      `json:"currentValue,omitempty"`
	DefaultValue string               `json:"defaultValue,omitempty"`
	Options      []ConfigSelectOption `json:"options,omitempty"`
}

// AsyncTaskUsage is what a background task has spent so far.
type AsyncTaskUsage struct {
	TotalTokens int64 `json:"totalTokens"`
	ToolUses    int64 `json:"toolUses"`
	DurationMs  int64 `json:"durationMs"`
}

// AsyncTask is a background task the vendor is running, as reported by the
// three async_task_* kinds.
//
// Like Subagent, no single message is complete: Name, TaskType, ShowInTranscript
// and CanStop arrive only on spawn, State only on the state update, Usage and
// LastToolName only on progress. A reader keeps them together by ID.
type AsyncTask struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	TaskType    string `json:"taskType,omitempty"`
	Description string `json:"description,omitempty"`
	Summary     string `json:"summary,omitempty"`
	State       string `json:"state,omitempty"`
	// LastToolName is what the task was doing when it last reported in.
	LastToolName string `json:"lastToolName,omitempty"`
	// OutputFilePath is the task's durable log. It can arrive after the spawn.
	OutputFilePath string `json:"outputFilePath,omitempty"`
	// ToolCallID ties the task back to the tool call that started it, when the
	// vendor knows the correlation — which it may only learn later.
	ToolCallID string `json:"toolCallId,omitempty"`
	// ShowInTranscript is the vendor's own answer to whether this task is worth
	// showing. Spawn-only, so false also means "this message did not say".
	ShowInTranscript bool            `json:"showInTranscript,omitempty"`
	CanStop          bool            `json:"canStop,omitempty"`
	Usage            *AsyncTaskUsage `json:"usage,omitempty"`
}

// SessionUpdate is the hub-owned view of one session/update notification.
// Raw is always populated, including for UpdateUnknown.
// SessionFailure is a session-scoped problem the vendor reports through the
// air extension DevKit asks for (devkitClientCapabilities lists
// sessionFailure): a sign-in that expired, a usage limit reached, a provider
// outage. It rides on a session_info_update's _meta rather than as a kind of
// its own, and it is the only structured word the agent gives for these —
// without it the user sees a turn that stopped and no reason.
//
// Actions are the vendor's own hints for the way out: "login", "retry",
// "new_session". Severity is "error" or "warning"; a warning is a notice, not
// a session that stopped.
type SessionFailure struct {
	ID       string   `json:"id"`
	Revision int      `json:"revision"`
	Category string   `json:"category"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Details  string   `json:"details,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Actions  []string `json:"actions,omitempty"`
}

// SessionGoal is the objective a session is working towards, from the goal
// extension: set with /goal or through _session/goal, cleared the same way,
// and published on a session_info_update's _meta whenever it changes.
type SessionGoal struct {
	Objective  string `json:"objective"`
	Status     string `json:"status,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
	LastReason string `json:"lastReason,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// RateLimit is what a subscription has left, as Claude attaches it to a
// usage_update. Status is "allowed", "allowed_warning" or "rejected";
// ResetsAt is a Unix second, Utilization a percentage.
type RateLimit struct {
	Status      string  `json:"status"`
	ResetsAt    int64   `json:"resetsAt,omitempty"`
	LimitType   string  `json:"limitType,omitempty"`
	Utilization float64 `json:"utilization,omitempty"`
}

// TokenCounts is one tally of tokens, as a vendor splits them.
type TokenCounts struct {
	Total     int64 `json:"total"`
	Input     int64 `json:"input"`
	Cached    int64 `json:"cached"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
}

// TokenBreakdown is what a vendor says the session's tokens went to: the whole
// session so far, and the last request alone. Only a vendor that reports the
// split sends it (Codex does; Claude's usage_update carries a total only), and
// the summary draws a section for it only then — never an empty one.
type TokenBreakdown struct {
	Session     TokenCounts `json:"session"`
	LastRequest TokenCounts `json:"lastRequest"`
}

// LimitWindow is one ceiling a plan puts on the account — a window that
// resets, not the context window. Label is the vendor's own window ("5-hour",
// "Weekly"); ResetsAt is a Unix second; Status is "ok", "warning" or "reached".
type LimitWindow struct {
	Label       string  `json:"label"`
	UsedPercent float64 `json:"usedPercent"`
	ResetsAt    int64   `json:"resetsAt,omitempty"`
	Status      string  `json:"status,omitempty"`
}

// Connector is an MCP server the vendor started for the session, and where it
// got to. State is "starting", "ready", "failed" or "cancelled"; NeedsLogin
// says the server is waiting for the person to sign in to it.
type Connector struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	NeedsLogin bool   `json:"needsLogin,omitempty"`
}

type SessionUpdate struct {
	Kind     UpdateKind         `json:"kind"`
	Content  *ContentBlock      `json:"content,omitempty"`
	ToolCall *ToolCall          `json:"toolCall,omitempty"`
	Plan     []PlanEntry        `json:"plan,omitempty"`
	Commands []AvailableCommand `json:"commands,omitempty"`
	Mode     string             `json:"mode,omitempty"`
	// AvailableModes is the agent's whole mode list, on a current_mode_update.
	// The agent's own update never carries it — only session/new and
	// session/load list modes, and the service turns that answer into this
	// update (see modeStateUpdate). The recorder keeps the list across the
	// bare updates that follow.
	AvailableModes []SessionMode `json:"availableModes,omitempty"`
	// Title is the vendor's own name for the session, generated after the
	// first exchange. Empty on the goal-carrying shape of the same kind.
	Title string `json:"title,omitempty"`
	// Subagent carries the vendor-side agent a subagent_* update is about.
	Subagent *Subagent `json:"subagent,omitempty"`
	// AsyncTask carries the background task an async_task_* update is about.
	AsyncTask *AsyncTask `json:"asyncTask,omitempty"`
	// ConfigOptions is the full set of settings from a config_option_update.
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
	Usage         *Usage         `json:"usage,omitempty"`
	// Failure and Goal both ride on session_info_update's _meta; see their
	// types. GoalCleared says the goal was removed, which Goal == nil cannot:
	// most session_info_updates carry no goal at all.
	Failure     *SessionFailure `json:"failure,omitempty"`
	Goal        *SessionGoal    `json:"goal,omitempty"`
	GoalCleared bool            `json:"goalCleared,omitempty"`
	// Connectors is the whole list of MCP servers and their states, on a
	// devkit_connectors_update. Each update replaces the list.
	Connectors []Connector `json:"connectors,omitempty"`
	// MessageID identifies the assistant message a chunk belongs to. Claude
	// sends it on every chunk; it is a stricter coalescing key than "same kind"
	// because two consecutive messages would otherwise fold into one bubble.
	MessageID string `json:"messageId,omitempty"`
	// Origin is the vendor session an update came from when it is not the
	// session itself — a subagent, which claude-agent-acp streams under the
	// subagent's own sessionId and its own message ids (measured on 0.78).
	// Empty for the session's own updates. Rewind forks at a message of the
	// session's own thread; a subagent's id is not in that thread.
	Origin string `json:"origin,omitempty"`
	// Attachments names what a prompt carried besides its text, on DevKit's own
	// echo of it. A prompt of only images or files is still a turn, and the echo
	// is what starts one.
	Attachments []PromptAttachment `json:"attachments,omitempty"`
	// WireKind is the sessionUpdate string exactly as the vendor sent it, kept
	// even when Kind degrades to UpdateUnknown. Without it an unmodelled kind
	// is indistinguishable from a malformed one, and the whole point of
	// reporting unknowns is to learn what the vendor started sending.
	//
	// Not persisted: for every kind this build models it is the same string as
	// Kind, and for the ones it does not, Raw already carries it. A field on
	// every transcript row that no reader reads is a bigger database for
	// nothing.
	WireKind string          `json:"-"`
	Raw      json.RawMessage `json:"raw,omitempty"`
}

// PromptAttachment is one file, image or ref a prompt carried: its name only.
// The content was sent to the agent and is not kept — images are transient by
// design (InvokeInput.Images), a file is still on disk under its path, and a
// ref never had content DevKit held in the first place (InvokeInput.Refs).
type PromptAttachment struct {
	Name string `json:"name"`
	// Kind is "file", "image", "document" or "ref".
	Kind string `json:"kind"`
	// URI is the devkit:// EntityRef URI, set only when Kind is "ref".
	URI string `json:"uri,omitempty"`
	// DocumentID and Pages are set when Kind is "document": a PDF kept in the
	// session's document store (documents.go), named by the handle the agent
	// reads it with.
	DocumentID string `json:"documentId,omitempty"`
	Pages      int    `json:"pages,omitempty"`
}

// DevKitWireKey is where DevKit annotates an update in transit: the adapter
// marks a subagent's update with its origin, and the service's own prompt echo
// carries the prompt's attachments. Neither is something the vendor sends, and
// a key of DevKit's own keeps them from being mistaken for a vendor field.
const DevKitWireKey = "_devkit"

type wireDevKit struct {
	Origin      string             `json:"origin"`
	Attachments []PromptAttachment `json:"attachments"`
	// What an adapter adds to a usage_update or session_info_update that the
	// vendor's own wire shape has no place for.
	Tokens     *TokenBreakdown `json:"tokens"`
	Limits     []LimitWindow   `json:"limits"`
	Connectors []Connector     `json:"connectors"`
}

// Subagent is an agent the vendor ran inside its own process, as reported by
// subagent_spawned and subagent_state_update.
//
// Name and Task arrive on spawn and are absent on the state update, which
// carries only the id and the new state — so a reader has to keep the pair
// together by ID rather than expect either message to be complete.
type Subagent struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Task  string `json:"task,omitempty"`
	State string `json:"state,omitempty"`
}

// IsChunk reports whether the update is a streaming text fragment. Chunks are
// the only kind that arrive many-per-second, which is why the transcript
// recorder coalesces them instead of writing a row each.
func (u SessionUpdate) IsChunk() bool {
	switch u.Kind {
	case UpdateUserMessageChunk, UpdateAgentMessageChunk, UpdateAgentThoughtChunk:
		return true
	default:
		return false
	}
}

// Text returns the text carried by a chunk update, empty for everything else.
func (u SessionUpdate) Text() string {
	if u.Content == nil {
		return ""
	}
	return u.Content.Text
}

// wireUpdate mirrors the ACP payload. Every field is optional on purpose: a
// malformed or partial notification must degrade to UpdateUnknown rather than
// fail a turn, because the hub cannot ask the vendor to resend.
// Content is a RawMessage, not a *ContentBlock: the key is polymorphic across
// kinds — an object on a chunk, an array of ToolCallContent on a tool call.
// Typing it as the object shape would make every tool_call notification fail to
// unmarshal and degrade to UpdateUnknown.
type wireUpdate struct {
	SessionUpdate     string          `json:"sessionUpdate"`
	Meta              json.RawMessage `json:"_meta"`
	DevKit            *wireDevKit     `json:"_devkit"`
	Content           json.RawMessage `json:"content"`
	ToolCallID        string          `json:"toolCallId"`
	Title             string          `json:"title"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	Locations         json.RawMessage `json:"locations"`
	RawInput          json.RawMessage `json:"rawInput"`
	RawOutput         json.RawMessage `json:"rawOutput"`
	Entries           json.RawMessage `json:"entries"`
	Commands          json.RawMessage `json:"availableCommands"`
	CurrentMode       string          `json:"currentModeId"`
	AvailableModes    json.RawMessage `json:"availableModes"`
	SubagentSessionID string          `json:"subagentSessionId"`
	Name              string          `json:"name"`
	Task              string          `json:"task"`
	State             string          `json:"state"`
	MessageID         string          `json:"messageId"`
	Used              int64           `json:"used"`
	Size              int64           `json:"size"`
	Cost              json.RawMessage `json:"cost"`
	ConfigOptions     json.RawMessage `json:"configOptions"`
	AsyncTaskID       string          `json:"asyncTaskId"`
	TaskType          string          `json:"taskType"`
	Description       string          `json:"description"`
	Summary           string          `json:"summary"`
	LastToolName      string          `json:"lastToolName"`
	OutputFilePath    string          `json:"outputFilePath"`
	ShowInTranscript  bool            `json:"showInTranscript"`
	CanStop           bool            `json:"canStop"`
	// TaskUsage is an async task's spend. It shares no key with usage_update,
	// which carries its numbers at the top level instead.
	TaskUsage json.RawMessage `json:"usage"`
}

// wireConfigOption mirrors one entry of a config_option_update. `options` is
// polymorphic — values or groups of values — so both shapes are read here and
// the group's own values are flattened out.
type wireConfigOption struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	Type         string          `json:"type"`
	CurrentValue json.RawMessage `json:"currentValue"`
	DefaultValue string          `json:"defaultValue"`
	Options      []struct {
		ConfigSelectOption
		Options []ConfigSelectOption `json:"options"`
	} `json:"options"`
}

// ParseSessionUpdate normalizes one session/update blob. It never returns an
// error: an unparseable notification becomes UpdateUnknown with Raw intact,
// because dropping a turn over a field the vendor added is worse than showing
// one row the UI cannot decorate.
func ParseSessionUpdate(raw json.RawMessage) SessionUpdate {
	out := SessionUpdate{Kind: UpdateUnknown, Raw: append(json.RawMessage(nil), raw...)}
	if len(raw) == 0 {
		return out
	}

	var wire wireUpdate
	if err := json.Unmarshal(raw, &wire); err != nil {
		return out
	}
	out.WireKind = wire.SessionUpdate
	if wire.DevKit != nil {
		out.Origin = wire.DevKit.Origin
	}

	switch UpdateKind(wire.SessionUpdate) {
	case UpdateUserMessageChunk, UpdateAgentMessageChunk, UpdateAgentThoughtChunk:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.MessageID = wire.MessageID
		if wire.DevKit != nil && out.Kind == UpdateUserMessageChunk {
			out.Attachments = wire.DevKit.Attachments
		}
		var block ContentBlock
		if err := json.Unmarshal(wire.Content, &block); err == nil {
			out.Content = &block
		}
		// The wire blob duplicates content.text byte for byte, and chunks are
		// the per-token kind: every row and every event carried the message
		// twice.
		out.Raw = nil
	case UpdateToolCall, UpdateToolCallUpdate:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.ToolCall = parseToolCall(wire)
		// Same duplication, bigger: the wire carries rawInput and rawOutput
		// and so does the normalized ToolCall — the row stored both copies.
		out.Raw = nil
	case UpdatePlan:
		out.Kind = UpdatePlan
		_ = json.Unmarshal(wire.Entries, &out.Plan)
	case UpdateAvailableCommands:
		out.Kind = UpdateAvailableCommands
		_ = json.Unmarshal(wire.Commands, &out.Commands)
	case UpdateCurrentMode:
		out.Kind = UpdateCurrentMode
		out.Mode = wire.CurrentMode
		_ = json.Unmarshal(wire.AvailableModes, &out.AvailableModes)
	case UpdateSubagentSpawned, UpdateSubagentState:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.Subagent = &Subagent{
			ID:    wire.SubagentSessionID,
			Name:  wire.Name,
			Task:  wire.Task,
			State: wire.State,
		}
	case UpdateSessionInfo:
		// Three shapes share this kind: a generated title, a goal, and a
		// session failure. Leaving the last two as UpdateUnknown would put
		// them back in the log every session; naming the kind with nothing
		// applied says "understood, nothing to show".
		out.Kind = UpdateSessionInfo
		out.Title = wire.Title
		out.Failure = parseSessionFailure(wire.Meta)
		out.Goal, out.GoalCleared = parseSessionGoal(wire.Meta)
	case UpdateConnectors:
		out.Kind = UpdateConnectors
		if wire.DevKit != nil {
			out.Connectors = wire.DevKit.Connectors
		}
	case UpdateConfigOptions:
		out.Kind = UpdateConfigOptions
		out.ConfigOptions = parseConfigOptions(wire.ConfigOptions)
	case UpdateAsyncTaskSpawned, UpdateAsyncTaskProgress, UpdateAsyncTaskState:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.AsyncTask = parseAsyncTask(wire)
	case UpdateUsage:
		out.Kind = UpdateUsage
		usage := Usage{Used: wire.Used, Size: wire.Size, RateLimit: parseRateLimit(wire.Meta)}
		usage.Limits = limitsOfRateLimit(usage.RateLimit)
		if wire.DevKit != nil {
			usage.Tokens = wire.DevKit.Tokens
			if len(wire.DevKit.Limits) > 0 {
				usage.Limits = wire.DevKit.Limits
			}
		}
		var cost struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		}
		if err := json.Unmarshal(wire.Cost, &cost); err == nil {
			usage.Amount = cost.Amount
			usage.Currency = cost.Currency
		}
		out.Usage = &usage
	}
	return out
}

// parseConfigOptions flattens the option list of a config_option_update. An
// entry is either a value or a named group of values; a group contributes its
// members, each tagged with the group's name.
func parseConfigOptions(raw json.RawMessage) []ConfigOption {
	var wire []wireConfigOption
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil
	}
	out := make([]ConfigOption, 0, len(wire))
	for _, w := range wire {
		option := ConfigOption{
			ID:           w.ID,
			Name:         w.Name,
			Description:  w.Description,
			Category:     w.Category,
			Type:         w.Type,
			CurrentValue: w.CurrentValue,
			DefaultValue: w.DefaultValue,
		}
		for _, entry := range w.Options {
			if len(entry.Options) == 0 {
				option.Options = append(option.Options, entry.ConfigSelectOption)
				continue
			}
			for _, member := range entry.Options {
				member.Group = entry.Name
				option.Options = append(option.Options, member)
			}
		}
		out = append(out, option)
	}
	return out
}

// parseAsyncTask shapes the background-task fields. Which of them are set
// depends on the kind, so every one of them may be empty.
func parseAsyncTask(wire wireUpdate) *AsyncTask {
	task := &AsyncTask{
		ID:               wire.AsyncTaskID,
		Name:             wire.Name,
		TaskType:         wire.TaskType,
		Description:      wire.Description,
		Summary:          wire.Summary,
		State:            wire.State,
		LastToolName:     wire.LastToolName,
		OutputFilePath:   wire.OutputFilePath,
		ToolCallID:       wire.ToolCallID,
		ShowInTranscript: wire.ShowInTranscript,
		CanStop:          wire.CanStop,
	}
	var usage AsyncTaskUsage
	if err := json.Unmarshal(wire.TaskUsage, &usage); err == nil {
		task.Usage = &usage
	}
	return task
}

// parseToolCall shapes the tool-call fields. A tool_call_update legitimately
// carries only the fields that changed, so every one of these may be empty —
// merging a partial onto the previous state is the recorder's job, not this
// function's.
func parseToolCall(wire wireUpdate) *ToolCall {
	call := &ToolCall{
		ID:        wire.ToolCallID,
		Title:     wire.Title,
		Kind:      wire.Kind,
		Status:    wire.Status,
		RawInput:  wire.RawInput,
		RawOutput: wire.RawOutput,
		Name:      wire.Name,
	}
	if call.Name == "" && len(wire.Meta) > 0 {
		var meta struct {
			ClaudeCode struct {
				ToolName string `json:"toolName"`
			} `json:"claudeCode"`
		}
		if json.Unmarshal(wire.Meta, &meta) == nil {
			call.Name = meta.ClaudeCode.ToolName
		}
	}
	_ = json.Unmarshal(wire.Content, &call.Content)
	_ = json.Unmarshal(wire.Locations, &call.Locations)
	return call
}

// wireSessionInfoMeta is session_info_update's _meta: the air extension's
// failure envelope, and the goal extension's snapshot.
//
// Goal is a pointer to a pointer in effect — `"goal": null` is how a cleared
// goal arrives, and it has to be told apart from a _meta with no goal key at
// all, which is every other session_info_update.
type wireSessionInfoMeta struct {
	JetBrains struct {
		Air struct {
			SessionFailure *SessionFailure `json:"sessionFailure"`
		} `json:"air"`
	} `json:"jetbrains"`
	Goal json.RawMessage `json:"goal"`
}

// parseSessionFailure reads the air extension's failure out of a
// session_info_update's _meta. nil when there is none, which is the usual case.
func parseSessionFailure(meta json.RawMessage) *SessionFailure {
	if len(meta) == 0 {
		return nil
	}
	var parsed wireSessionInfoMeta
	if err := json.Unmarshal(meta, &parsed); err != nil {
		return nil
	}
	failure := parsed.JetBrains.Air.SessionFailure
	if failure == nil || failure.Title == "" {
		return nil
	}
	return failure
}

// parseSessionGoal reads the goal extension's snapshot. The second return is
// whether the goal was explicitly cleared ("goal": null).
func parseSessionGoal(meta json.RawMessage) (*SessionGoal, bool) {
	if len(meta) == 0 {
		return nil, false
	}
	var parsed wireSessionInfoMeta
	if err := json.Unmarshal(meta, &parsed); err != nil {
		return nil, false
	}
	if len(parsed.Goal) == 0 {
		return nil, false
	}
	if string(parsed.Goal) == "null" {
		return nil, true
	}
	var goal SessionGoal
	if err := json.Unmarshal(parsed.Goal, &goal); err != nil || goal.Objective == "" {
		return nil, false
	}
	return &goal, false
}

// parseRateLimit reads Claude's own rate-limit block off a usage_update's
// _meta. Its key is namespaced by the vendor, not by ACP.
func parseRateLimit(meta json.RawMessage) *RateLimit {
	if len(meta) == 0 {
		return nil
	}
	var parsed struct {
		RateLimit *struct {
			Status        string  `json:"status"`
			ResetsAt      int64   `json:"resetsAt"`
			RateLimitType string  `json:"rateLimitType"`
			Utilization   float64 `json:"utilization"`
		} `json:"_claude/rateLimit"`
	}
	if err := json.Unmarshal(meta, &parsed); err != nil || parsed.RateLimit == nil || parsed.RateLimit.Status == "" {
		return nil
	}
	return &RateLimit{
		Status:      parsed.RateLimit.Status,
		ResetsAt:    parsed.RateLimit.ResetsAt,
		LimitType:   parsed.RateLimit.RateLimitType,
		Utilization: parsed.RateLimit.Utilization,
	}
}

// limitsOfRateLimit is Claude's rate-limit block as a LimitWindow, so the
// summary reads one shape whichever vendor reported the ceiling. Claude sends
// the window the event is about, not all of them, so this is one entry.
func limitsOfRateLimit(r *RateLimit) []LimitWindow {
	if r == nil {
		return nil
	}
	status := "ok"
	switch r.Status {
	case "allowed_warning":
		status = "warning"
	case "rejected":
		status = "reached"
	}
	return []LimitWindow{{Label: claudeLimitLabel(r.LimitType), UsedPercent: r.Utilization, ResetsAt: r.ResetsAt, Status: status}}
}

// claudeLimitLabel names Claude's rate-limit types the way its own /usage does.
func claudeLimitLabel(limitType string) string {
	switch limitType {
	case "five_hour":
		return "5-hour"
	case "seven_day":
		return "Weekly"
	case "seven_day_opus":
		return "Weekly · Opus"
	case "seven_day_sonnet":
		return "Weekly · Sonnet"
	case "overage":
		return "Extra usage"
	case "":
		return "Plan"
	}
	return strings.ReplaceAll(limitType, "_", " ")
}
