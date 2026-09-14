package agenthub

import (
	"encoding/json"

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
	// turned up in a real claude-agent-acp 0.74 capture and is the source of
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
	ID        string             `json:"id"`
	Title     string             `json:"title,omitempty"`
	Kind      string             `json:"kind,omitempty"`
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
}

// ConfigSelectOption is one selectable value of a select-typed ConfigOption.
//
// The wire allows either a flat list of values or a list of named groups of
// them; both arrive here flat, with Group naming the group a value came from.
// Claude groups its models, and a flat list with the grouping still readable is
// less to get wrong than two shapes for the same menu.
type ConfigSelectOption struct {
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
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
	// MessageID identifies the assistant message a chunk belongs to. Claude
	// sends it on every chunk; it is a stricter coalescing key than "same kind"
	// because two consecutive messages would otherwise fold into one bubble.
	MessageID string `json:"messageId,omitempty"`
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

	switch UpdateKind(wire.SessionUpdate) {
	case UpdateUserMessageChunk, UpdateAgentMessageChunk, UpdateAgentThoughtChunk:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.MessageID = wire.MessageID
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
		// The goal-carrying shape of this kind has no title. Leaving it as
		// UpdateUnknown would put it back in the log every session; naming it
		// with an empty Title says "understood, nothing to apply".
		out.Kind = UpdateSessionInfo
		out.Title = wire.Title
	case UpdateConfigOptions:
		out.Kind = UpdateConfigOptions
		out.ConfigOptions = parseConfigOptions(wire.ConfigOptions)
	case UpdateAsyncTaskSpawned, UpdateAsyncTaskProgress, UpdateAsyncTaskState:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.AsyncTask = parseAsyncTask(wire)
	case UpdateUsage:
		out.Kind = UpdateUsage
		usage := Usage{Used: wire.Used, Size: wire.Size}
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
	}
	_ = json.Unmarshal(wire.Content, &call.Content)
	_ = json.Unmarshal(wire.Locations, &call.Locations)
	return call
}
