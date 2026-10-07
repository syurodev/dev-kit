package agenthub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"devkit/internal/codexapp"
)

// codexOutputLimit is how much of a command's output its tool call keeps: the
// tail, where an error usually is. Codex sends the whole output once, on
// item/completed — Phase 0 saw no outputDelta on the wire.
const codexOutputLimit = 64 << 10

// codexUpdate is one ACP session/update payload — the `update` object
// RuntimeHandlers.OnUpdate takes — and the raw Codex item it came from, kept
// for FR-8. Meta is nil for deltas: there are thousands, and each is a
// fragment of an item whose completion carries the whole thing.
type codexUpdate struct {
	Update json.RawMessage
	Meta   json.RawMessage
}

// codexOpenItem is an item whose tool call is on screen as in_progress. An
// approval for it reuses its title and content, and an interrupted turn
// closes it (Phase 0: Codex never completes an item it interrupts).
type codexOpenItem struct {
	Title string
	Kind  string
	// McpServer is the server of an mcpToolCall item, so an approval for it —
	// which names the server but not the item — can find the card it is about.
	McpServer string
	McpTool   string
	Content   []ToolCallContent
	Locations []ToolCallLocation
	RawInput  json.RawMessage
}

// codexTranslator turns one app-server session's notifications into ACP
// updates, so core, store and UI never see Codex's shapes (FR-7). It holds
// the little state a translation needs, so there is one per runtime.
//
// Safe for concurrent use: mu guards every field below. codexapp.Conn's read
// loop delivers notifications to Translate one at a time (Handler.Notification
// runs on the read loop), but the runtime adapter also calls approvalFor from
// Handler.Request's own goroutine (one per server request) and calls closeOpen
// from wherever a turn ends without turn/completed — both concurrently with
// Translate. mu is never held while calling out to anything (PermissionWaiter,
// the store, …): every locked section only touches this struct's own fields.
type codexTranslator struct {
	mu sync.Mutex

	// threadID scopes Translate to one thread once set: empty
	// (newCodexTranslator's default, and every test that predates per-thread
	// scoping) accepts every thread.
	threadID string

	open          map[string]codexOpenItem
	openOrder     []string
	messageDeltas map[string]bool
	thoughtDeltas map[string]bool
	// reasoningIndex is the last summaryIndex seen per open reasoning item,
	// so a new index can be told from a repeat of the same one.
	reasoningIndex map[string]int
	// closedByClose is the set of ids closeOpen most recently closed (finding
	// 10). Only the latest close's set is kept: a late item/completed for one
	// of those ids must be folded into the failed card rather than
	// reconstructing a fresh in_progress one.
	closedByClose map[string]bool
}

func newCodexTranslator() *codexTranslator {
	return &codexTranslator{
		open:           map[string]codexOpenItem{},
		messageDeltas:  map[string]bool{},
		thoughtDeltas:  map[string]bool{},
		reasoningIndex: map[string]int{},
		closedByClose:  map[string]bool{},
	}
}

// codexNotifications is the dispatch table (spec §5.4). Supporting a new
// notification is one entry here and one method; anything absent is not
// translated, and the runtime logs it.
var codexNotifications = map[string]func(*codexTranslator, json.RawMessage) []codexUpdate{
	codexapp.MethodItemStarted:           (*codexTranslator).itemStarted,
	codexapp.MethodItemCompleted:         (*codexTranslator).itemCompleted,
	codexapp.MethodAgentMessageDelta:     (*codexTranslator).agentMessageDelta,
	codexapp.MethodReasoningSummaryDelta: (*codexTranslator).reasoningDelta,
	codexapp.MethodTurnPlanUpdated:       (*codexTranslator).planUpdated,
	codexapp.MethodTokenUsageUpdated:     (*codexTranslator).tokenUsage,
	codexapp.MethodThreadNameUpdated:     (*codexTranslator).threadName,
	codexapp.MethodTurnCompleted:         (*codexTranslator).turnCompleted,
}

// codexTranslates reports whether method has a translation.
func codexTranslates(method string) bool {
	_, ok := codexNotifications[method]
	return ok
}

// Translate returns the ACP updates for one notification, possibly none.
func (t *codexTranslator) Translate(method string, params json.RawMessage) []codexUpdate {
	handle, ok := codexNotifications[method]
	if !ok {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.threadID != "" && !t.sameThreadLocked(params) {
		return nil
	}
	return handle(t, params)
}

// sameThreadLocked reports whether params either carries no top-level
// threadId, or carries this translator's own: the only two cases
// that may pass through once setThread has scoped the translator. A
// notification with a different threadId belongs to another thread
// multiplexed on the same connection — a sub-agent thread started by
// collabAgentToolCall, in the one case Phase 0 has for it — and must not
// touch this transcript's open items or deltas. Called with mu held.
func (t *codexTranslator) sameThreadLocked(params json.RawMessage) bool {
	var env struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(params, &env) != nil || env.ThreadID == "" {
		return true
	}
	return env.ThreadID == t.threadID
}

// setThread scopes this translator to one thread (see the threadID field
// doc). The adapter calls it once it learns the id from thread/start or
// thread/resume's response, before handing the connection its first
// notification.
func (t *codexTranslator) setThread(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.threadID = id
}

// openMcpCall returns the one open MCP tool call on server, or ok=false when
// there is none or more than one — two calls in flight on the same server
// cannot be told apart by an approval that names only the server.
func (t *codexTranslator) openMcpCall(server string) (string, codexOpenItem, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var id string
	for _, candidate := range t.openOrder {
		if t.open[candidate].McpServer != server {
			continue
		}
		if id != "" {
			return "", codexOpenItem{}, false
		}
		id = candidate
	}
	if id == "" {
		return "", codexOpenItem{}, false
	}
	return id, t.open[id], true
}

// openItem returns what the translator knows of an in-progress item. Called
// from approvalFor's caller, on a different goroutine than Translate's own —
// see the type doc.
func (t *codexTranslator) openItem(id string) (codexOpenItem, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	it, ok := t.open[id]
	return it, ok
}

// codexToolCallWire is ACP's tool_call / tool_call_update, as
// ParseSessionUpdate reads it.
type codexToolCallWire struct {
	SessionUpdate string             `json:"sessionUpdate"`
	ToolCallID    string             `json:"toolCallId"`
	Title         string             `json:"title,omitempty"`
	Kind          string             `json:"kind,omitempty"`
	Status        string             `json:"status,omitempty"`
	Content       []ToolCallContent  `json:"content,omitempty"`
	Locations     []ToolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
}

func marshalUpdate(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func codexTextChunk(kind UpdateKind, text string) json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(kind),
		"content":       ContentBlock{Type: "text", Text: text},
	})
}

func codexTextContent(text string) ToolCallContent {
	return ToolCallContent{Type: "content", Content: &ContentBlock{Type: "text", Text: text}}
}

func decodeCodexItem(params json.RawMessage) (json.RawMessage, codexapp.ItemHeader, bool) {
	var n codexapp.ItemNotification
	if json.Unmarshal(params, &n) != nil || len(n.Item) == 0 {
		return nil, codexapp.ItemHeader{}, false
	}
	var h codexapp.ItemHeader
	if json.Unmarshal(n.Item, &h) != nil || h.ID == "" {
		return nil, codexapp.ItemHeader{}, false
	}
	return n.Item, h, true
}

func (t *codexTranslator) itemStarted(params json.RawMessage) []codexUpdate {
	item, h, ok := decodeCodexItem(params)
	if !ok {
		return nil
	}
	it, ok := codexToolItem(h.Type, item)
	if !ok {
		// Messages and reasoning stream as deltas; anything else is shown
		// when it completes.
		return nil
	}
	if _, seen := t.open[h.ID]; !seen {
		t.openOrder = append(t.openOrder, h.ID)
	}
	t.open[h.ID] = it
	return []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
		SessionUpdate: string(UpdateToolCall),
		ToolCallID:    h.ID,
		Title:         it.Title,
		Kind:          it.Kind,
		Status:        "in_progress",
		Content:       it.Content,
		Locations:     it.Locations,
		RawInput:      it.RawInput,
	})}}
}

// codexToolItem describes the item types shown as tool calls.
func codexToolItem(itemType string, item json.RawMessage) (codexOpenItem, bool) {
	switch itemType {
	case codexapp.ItemCommandExecution:
		var c codexapp.CommandExecutionItem
		if json.Unmarshal(item, &c) != nil {
			return codexOpenItem{}, false
		}
		raw, _ := json.Marshal(map[string]string{"command": c.Command, "cwd": c.Cwd})
		return codexOpenItem{Title: codexCommandTitle(c.Command, c.CommandActions), Kind: "execute", RawInput: raw}, true
	case codexapp.ItemFileChange:
		var f codexapp.FileChangeItem
		if json.Unmarshal(item, &f) != nil {
			return codexOpenItem{}, false
		}
		return codexOpenItem{
			Title:     codexFileChangeTitle(f.Changes),
			Kind:      codexFileChangeKind(f.Changes),
			Content:   codexFileChangeContent(f.Changes),
			Locations: codexFileChangeLocations(f.Changes),
		}, true
	case codexapp.ItemMcpToolCall:
		var m codexapp.McpToolCallItem
		if json.Unmarshal(item, &m) != nil {
			return codexOpenItem{}, false
		}
		return codexOpenItem{Title: m.Server + "." + m.Tool, Kind: "other", McpServer: m.Server, McpTool: m.Tool, RawInput: m.Arguments}, true
	case codexapp.ItemWebSearch:
		var w codexapp.WebSearchItem
		if json.Unmarshal(item, &w) != nil {
			return codexOpenItem{}, false
		}
		title := "Web search"
		if w.Query != "" {
			title += ": " + w.Query
		}
		raw, _ := json.Marshal(map[string]string{"query": w.Query})
		return codexOpenItem{Title: title, Kind: "fetch", RawInput: raw}, true
	}
	return codexOpenItem{}, false
}

func (t *codexTranslator) itemCompleted(params json.RawMessage) []codexUpdate {
	item, h, ok := decodeCodexItem(params)
	if !ok {
		return nil
	}
	meta := append(json.RawMessage(nil), item...)

	switch h.Type {
	case codexapp.ItemAgentMessage:
		streamed := t.messageDeltas[h.ID]
		delete(t.messageDeltas, h.ID)
		var m codexapp.AgentMessageItem
		if streamed || json.Unmarshal(item, &m) != nil || m.Text == "" {
			// FR-8: a streamed message produces no update here,
			// so there is nothing for meta to ride on either — its text
			// already reached the transcript through item/agentMessage/delta.
			return nil
		}
		return []codexUpdate{{Update: codexTextChunk(UpdateAgentMessageChunk, m.Text), Meta: meta}}
	case codexapp.ItemReasoning:
		streamed := t.thoughtDeltas[h.ID]
		delete(t.thoughtDeltas, h.ID)
		delete(t.reasoningIndex, h.ID)
		var r codexapp.ReasoningItem
		if streamed || json.Unmarshal(item, &r) != nil {
			return nil // FR-8: same reasoning as agentMessage above.
		}
		text := strings.Join(r.Summary, "\n\n")
		if text == "" {
			return nil
		}
		return []codexUpdate{{Update: codexTextChunk(UpdateAgentThoughtChunk, text), Meta: meta}}
	case codexapp.ItemUserMessage, codexapp.ItemHookPrompt:
		// FR-8: DevKit writes the user's own message itself, and
		// hooks are disabled, so neither produces an update — and neither's
		// raw item is kept as meta, for the same reason streamed text above
		// carries none.
		return nil
	}

	status, content, rawOutput, known := codexToolResult(h.Type, item)
	if !known {
		// A type this build does not translate is shown, not dropped (FR-8),
		// but rawOutput is capped: an unknown item — imageGeneration,
		// say — can carry a base64 payload, and only meta, not the rendered
		// transcript, is meant to hold the whole thing regardless of size.
		return []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
			SessionUpdate: string(UpdateToolCall),
			ToolCallID:    h.ID,
			Title:         h.Type,
			Kind:          "other",
			Status:        "completed",
			RawOutput:     codexCapRawOutput(meta, h.Type),
		}), Meta: meta}}
	}
	var out []codexUpdate
	_, isOpen := t.open[h.ID]
	if !isOpen && !t.closedByClose[h.ID] {
		// Completed without a start we saw: show the call before closing it.
		// An id closeOpen already closed is not this case — its
		// card already ended failed, and a late completed for it must only
		// update that card, never resurrect a fresh in_progress one.
		out = t.itemStarted(params)
	}
	t.forget(h.ID)
	return append(out, codexUpdate{Update: marshalUpdate(codexToolCallWire{
		SessionUpdate: string(UpdateToolCallUpdate),
		ToolCallID:    h.ID,
		Status:        status,
		Content:       content,
		RawOutput:     rawOutput,
	}), Meta: meta})
}

// codexToolResult is how a completed tool item ends: its ACP status, content
// to show (nil keeps what the start showed) and raw output.
func codexToolResult(itemType string, item json.RawMessage) (status string, content []ToolCallContent, rawOutput json.RawMessage, known bool) {
	switch itemType {
	case codexapp.ItemCommandExecution:
		var c codexapp.CommandExecutionItem
		if json.Unmarshal(item, &c) != nil {
			return "failed", nil, nil, true
		}
		status = "completed"
		if c.Status == codexapp.ItemFailed || c.Status == codexapp.ItemDeclined || (c.ExitCode != nil && *c.ExitCode != 0) {
			status = "failed"
		}
		output := ""
		if c.AggregatedOutput != nil {
			output = codexTail(*c.AggregatedOutput, codexOutputLimit)
		}
		if output == "" && c.Status == codexapp.ItemDeclined {
			output = "Declined."
		}
		if output != "" {
			content = []ToolCallContent{codexTextContent(output)}
		}
		rawOutput, _ = json.Marshal(struct {
			ExitCode   *int   `json:"exitCode"`
			DurationMs *int64 `json:"durationMs"`
		}{c.ExitCode, c.DurationMs})
		return status, content, rawOutput, true
	case codexapp.ItemFileChange:
		var f codexapp.FileChangeItem
		if json.Unmarshal(item, &f) != nil || f.Status == codexapp.ItemFailed || f.Status == codexapp.ItemDeclined {
			return "failed", nil, nil, true
		}
		return "completed", nil, nil, true
	case codexapp.ItemMcpToolCall:
		var m codexapp.McpToolCallItem
		if json.Unmarshal(item, &m) != nil {
			return "failed", nil, nil, true
		}
		if m.Status == codexapp.ItemFailed || m.Error != nil {
			msg := "failed"
			if m.Error != nil {
				msg = m.Error.Message
			}
			rawOutput, _ = json.Marshal(map[string]string{"error": msg})
			return "failed", nil, rawOutput, true
		}
		return "completed", nil, codexCapRawOutput(m.Result, ""), true
	case codexapp.ItemWebSearch:
		return "completed", nil, nil, true
	}
	return "", nil, nil, false
}

func (t *codexTranslator) forget(id string) {
	delete(t.open, id)
	for i, open := range t.openOrder {
		if open == id {
			t.openOrder = append(t.openOrder[:i], t.openOrder[i+1:]...)
			break
		}
	}
}

func (t *codexTranslator) agentMessageDelta(params json.RawMessage) []codexUpdate {
	var d codexapp.DeltaNotification
	if json.Unmarshal(params, &d) != nil || d.Delta == "" {
		return nil
	}
	t.messageDeltas[d.ItemID] = true
	return []codexUpdate{{Update: codexTextChunk(UpdateAgentMessageChunk, d.Delta)}}
}

func (t *codexTranslator) reasoningDelta(params json.RawMessage) []codexUpdate {
	var d codexapp.DeltaNotification
	if json.Unmarshal(params, &d) != nil || d.Delta == "" {
		return nil
	}
	text := d.Delta
	if last, seen := t.reasoningIndex[d.ItemID]; seen && last != d.SummaryIndex {
		// A new summaryIndex is a new paragraph of the summary:
		// Codex sends no separator of its own between them, so without this
		// two adjacent parts would run together as one word.
		text = "\n\n" + text
	}
	t.reasoningIndex[d.ItemID] = d.SummaryIndex
	t.thoughtDeltas[d.ItemID] = true
	return []codexUpdate{{Update: codexTextChunk(UpdateAgentThoughtChunk, text)}}
}

func (t *codexTranslator) planUpdated(params json.RawMessage) []codexUpdate {
	var p codexapp.TurnPlanUpdatedNotification
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	entries := make([]PlanEntry, 0, len(p.Plan))
	for _, step := range p.Plan {
		status := "pending"
		switch step.Status {
		case "inProgress":
			status = "in_progress"
		case "completed":
			status = "completed"
		}
		entries = append(entries, PlanEntry{Content: step.Step, Status: status, Priority: "medium"})
	}
	return []codexUpdate{{Update: marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdatePlan),
		"entries":       entries,
	})}}
}

// tokenUsage feeds the composer's context meter: `used` is the last request's
// tokens against the model's window. Codex reports no cost (subscription).
func (t *codexTranslator) tokenUsage(params json.RawMessage) []codexUpdate {
	var n codexapp.TokenUsageUpdatedNotification
	if json.Unmarshal(params, &n) != nil {
		return nil
	}
	var size int64
	if n.TokenUsage.ModelContextWindow != nil {
		size = *n.TokenUsage.ModelContextWindow
	}
	return []codexUpdate{{Update: marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateUsage),
		"used":          n.TokenUsage.Last.TotalTokens,
		"size":          size,
	})}}
}

func (t *codexTranslator) threadName(params json.RawMessage) []codexUpdate {
	var n codexapp.ThreadNameUpdatedNotification
	if json.Unmarshal(params, &n) != nil || strings.TrimSpace(n.ThreadName) == "" {
		return nil
	}
	return []codexUpdate{{Update: marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateSessionInfo),
		"title":         n.ThreadName,
	})}}
}

// turnCompleted closes every tool call still open, via closeOpenLocked: an
// interrupted turn leaves its running command without an item/completed
// (Phase 0), and a card left in_progress would spin forever.
func (t *codexTranslator) turnCompleted(params json.RawMessage) []codexUpdate {
	var n codexapp.TurnCompletedNotification
	_ = json.Unmarshal(params, &n)
	reason := "Ended without a result."
	switch n.Turn.Status {
	case codexapp.TurnInterrupted:
		reason = "Cancelled."
	case codexapp.TurnFailed:
		reason = "The turn failed."
	}
	out := t.closeOpenLocked(reason)
	t.messageDeltas = map[string]bool{}
	t.thoughtDeltas = map[string]bool{}
	t.reasoningIndex = map[string]int{}
	return out
}

// closeOpen closes every open tool call the way turnCompleted does — a failed
// tool_call_update, in start order, carrying reason as its content — and
// forgets them. It exists for every path that ends a turn without
// ever sending turn/completed: error{willRetry:false} with no completion
// behind it, the app-server process dying, or the prompt's own context being
// cancelled. It only closes tool calls; messageDeltas/thoughtDeltas/
// reasoningIndex key on message and reasoning item ids, which never appear in
// openOrder, so there is nothing of theirs for this to forget.
//
// Safe to call from any goroutine, independently of Translate — see the type
// doc.
func (t *codexTranslator) closeOpen(reason string) []codexUpdate {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closeOpenLocked(reason)
}

// closeOpenLocked is closeOpen's body, split out so turnCompleted can call it
// while Translate already holds mu — mu is not reentrant, so turnCompleted
// must not go through closeOpen itself.
func (t *codexTranslator) closeOpenLocked(reason string) []codexUpdate {
	var out []codexUpdate
	closed := make(map[string]bool, len(t.openOrder))
	for _, id := range t.openOrder {
		closed[id] = true
		out = append(out, codexUpdate{Update: marshalUpdate(codexToolCallWire{
			SessionUpdate: string(UpdateToolCallUpdate),
			ToolCallID:    id,
			Status:        "failed",
			Content:       []ToolCallContent{codexTextContent(reason)},
		})})
	}
	// Only the most recent close's ids are remembered, so a late
	// item/completed for one of them is folded into its already-failed card
	// instead of reopening a fresh in_progress one.
	t.closedByClose = closed
	t.open = map[string]codexOpenItem{}
	t.openOrder = nil
	return out
}

// codexCommandTitle is the command as the model wrote it — Codex wraps it in
// the shell invocation (`/usr/bin/zsh -lc "…"`), which is noise on a card.
func codexCommandTitle(command string, actions []codexapp.CommandAction) string {
	for _, a := range actions {
		if strings.TrimSpace(a.Command) != "" {
			return a.Command
		}
	}
	return command
}

func codexFileChangeKind(changes []codexapp.FileUpdateChange) string {
	allDeletes := len(changes) > 0
	for _, c := range changes {
		if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
			return "move"
		}
		if c.Kind.Type != "delete" {
			allDeletes = false
		}
	}
	if allDeletes {
		return "delete"
	}
	return "edit"
}

func codexFileChangeTitle(changes []codexapp.FileUpdateChange) string {
	if len(changes) != 1 {
		return fmt.Sprintf("Edit %d files", len(changes))
	}
	c := changes[0]
	verb := "Edit"
	switch {
	case c.Kind.MovePath != nil && *c.Kind.MovePath != "":
		verb = "Move"
	case c.Kind.Type == "add":
		verb = "Create"
	case c.Kind.Type == "delete":
		verb = "Delete"
	}
	return verb + " " + filepath.Base(c.Path)
}

func codexFileChangeLocations(changes []codexapp.FileUpdateChange) []ToolCallLocation {
	out := make([]ToolCallLocation, 0, len(changes))
	for _, c := range changes {
		path := c.Path
		if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
			path = *c.Kind.MovePath
		}
		out = append(out, ToolCallLocation{Path: path})
	}
	return out
}

// codexFileChangeContent builds ACP diff content from Codex's own diffs —
// never from the file on disk: the adapter runs outside the jail and the path
// is the agent's, so reading it could show the transcript a file the agent
// could not read (spec §5.2).
func codexFileChangeContent(changes []codexapp.FileUpdateChange) []ToolCallContent {
	out := make([]ToolCallContent, 0, len(changes))
	for _, c := range changes {
		switch c.Kind.Type {
		case "add":
			// The diff is the whole new file (measured: no @@). No oldText
			// means a new file.
			out = append(out, ToolCallContent{Type: "diff", Path: c.Path, NewText: c.Diff})
		case "delete":
			before := c.Diff
			out = append(out, ToolCallContent{Type: "diff", Path: c.Path, OldText: &before})
		default:
			before, after := codexHunkTexts(c.Diff)
			path := c.Path
			if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
				path = *c.Kind.MovePath
			}
			out = append(out, ToolCallContent{Type: "diff", Path: path, OldText: &before, NewText: after})
		}
	}
	return out
}

// codexHunkTexts splits a unified diff's hunks into the before and after text
// of the lines they cover, "…" between hunks. Only the changed region: the
// rest of the file is exactly what DevKit declines to read.
func codexHunkTexts(diff string) (before, after string) {
	var old, neu []string
	hunks := 0
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if hunks > 0 {
				old, neu = append(old, "…"), append(neu, "…")
			}
			hunks++
		case hunks == 0:
			// File headers (--- / +++) before the first hunk.
		case strings.HasPrefix(line, "+"):
			neu = append(neu, line[1:])
		case strings.HasPrefix(line, "-"):
			old = append(old, line[1:])
		case strings.HasPrefix(line, " "):
			old, neu = append(old, line[1:]), append(neu, line[1:])
		case line == "":
			old, neu = append(old, ""), append(neu, "")
		}
		// "\ No newline at end of file" and anything else: skipped.
	}
	return codexJoinLines(old), codexJoinLines(neu)
}

func codexJoinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// codexCapRawOutput is raw, unless it is larger than codexOutputLimit — an
// item like imageGeneration can carry a base64 image, and rawOutput is what
// rides in the rendered transcript, not just in meta (FR-8 keeps the whole
// original item there regardless of size). itemType names the
// item in an unknown-item's stand-in; pass "" for mcpToolCall's result, which
// has no type of its own worth naming.
func codexCapRawOutput(raw json.RawMessage, itemType string) json.RawMessage {
	if len(raw) <= codexOutputLimit {
		return raw
	}
	stub := map[string]any{"truncated": true, "bytes": len(raw)}
	if itemType != "" {
		stub["type"] = itemType
	}
	return marshalUpdate(stub)
}

// codexTail keeps the last limit bytes of s on a rune boundary, marked as cut.
func codexTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[len(s)-limit:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return "…\n" + s
}
