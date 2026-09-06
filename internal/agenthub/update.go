package agenthub

import "encoding/json"

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
	// UpdateUnknown is a kind this build does not model. The raw blob is kept
	// so a vendor that ships a new kind degrades to "not rendered" rather than
	// "silently dropped" — the Meta Inspector surface reads Raw.
	UpdateUnknown UpdateKind = "unknown"
)

// ContentBlock is one ACP displayable unit. Only `text` carries meaning for
// every vendor; the rest are kept as-is so the UI can decide what it can show.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	// Data is base64 for image/audio. It is deliberately not decoded here: the
	// hub never needs the bytes, and decoding would double a screenshot in RAM
	// on its way to the UI.
	Data string `json:"data,omitempty"`
	URI  string `json:"uri,omitempty"`
	Name string `json:"name,omitempty"`
}

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
}

// PlanEntry is one step of a `plan` update.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// AvailableCommand is one entry of an available_commands_update.
type AvailableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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

// SessionUpdate is the hub-owned view of one session/update notification.
// Raw is always populated, including for UpdateUnknown.
type SessionUpdate struct {
	Kind     UpdateKind         `json:"kind"`
	Content  *ContentBlock      `json:"content,omitempty"`
	ToolCall *ToolCall          `json:"toolCall,omitempty"`
	Plan     []PlanEntry        `json:"plan,omitempty"`
	Commands []AvailableCommand `json:"commands,omitempty"`
	Mode     string             `json:"mode,omitempty"`
	// Title is the vendor's own name for the session, generated after the
	// first exchange. Empty on the goal-carrying shape of the same kind.
	Title string `json:"title,omitempty"`
	// Subagent carries the vendor-side agent a subagent_* update is about.
	Subagent *Subagent `json:"subagent,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`
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
	SubagentSessionID string          `json:"subagentSessionId"`
	Name              string          `json:"name"`
	Task              string          `json:"task"`
	State             string          `json:"state"`
	MessageID         string          `json:"messageId"`
	Used              int64           `json:"used"`
	Size              int64           `json:"size"`
	Cost              json.RawMessage `json:"cost"`
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
	case UpdateToolCall, UpdateToolCallUpdate:
		out.Kind = UpdateKind(wire.SessionUpdate)
		out.ToolCall = parseToolCall(wire)
	case UpdatePlan:
		out.Kind = UpdatePlan
		_ = json.Unmarshal(wire.Entries, &out.Plan)
	case UpdateAvailableCommands:
		out.Kind = UpdateAvailableCommands
		_ = json.Unmarshal(wire.Commands, &out.Commands)
	case UpdateCurrentMode:
		out.Kind = UpdateCurrentMode
		out.Mode = wire.CurrentMode
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
