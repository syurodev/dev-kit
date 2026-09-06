package agenthub_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"devkit/internal/agenthub"
)

func TestParseSessionUpdateChunk(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello from ACP"}}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateAgentMessageChunk {
		t.Fatalf("kind = %q, want agent_message_chunk", got.Kind)
	}
	if !got.IsChunk() {
		t.Fatal("IsChunk() = false for a message chunk")
	}
	if got.Text() != "Hello from ACP" {
		t.Fatalf("Text() = %q", got.Text())
	}
	if len(got.Raw) == 0 {
		t.Fatal("Raw is empty; the original blob must survive parsing")
	}
}

func TestParseSessionUpdateThoughtChunk(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"weighing options"}}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateAgentThoughtChunk {
		t.Fatalf("kind = %q", got.Kind)
	}
	if !got.IsChunk() {
		t.Fatal("a thought chunk must count as a chunk so it coalesces")
	}
}

// A tool call carries `content` as an ARRAY while a chunk carries it as an
// OBJECT. Typing that key as the object shape made every tool call fall through
// to UpdateUnknown, which is the bug this test pins.
func TestParseSessionUpdateToolCallWithDiff(t *testing.T) {
	raw := json.RawMessage(`{
		"sessionUpdate":"tool_call",
		"toolCallId":"call_1",
		"title":"Edit RefundService.java",
		"kind":"edit",
		"status":"in_progress",
		"content":[{"type":"diff","path":"/acme/billing/RefundService.java","oldText":"old","newText":"new"}],
		"locations":[{"path":"/acme/billing/RefundService.java","line":124}]
	}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateToolCall {
		t.Fatalf("kind = %q, want tool_call", got.Kind)
	}
	if got.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if got.ToolCall.ID != "call_1" || got.ToolCall.Kind != "edit" || got.ToolCall.Status != "in_progress" {
		t.Fatalf("tool call = %+v", *got.ToolCall)
	}
	if len(got.ToolCall.Content) != 1 || got.ToolCall.Content[0].Type != "diff" {
		t.Fatalf("content = %+v", got.ToolCall.Content)
	}
	if got.ToolCall.Content[0].OldText == nil || *got.ToolCall.Content[0].OldText != "old" {
		t.Fatalf("oldText = %v", got.ToolCall.Content[0].OldText)
	}
	if len(got.ToolCall.Locations) != 1 || got.ToolCall.Locations[0].Line == nil || *got.ToolCall.Locations[0].Line != 124 {
		t.Fatalf("locations = %+v", got.ToolCall.Locations)
	}
}

// oldText is absent for a newly created file, which is not the same as an
// empty file — the pointer has to stay nil so the UI can tell them apart.
func TestParseSessionUpdateToolCallNewFileHasNilOldText(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"c","content":[{"type":"diff","path":"/new.go","newText":"package main"}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.ToolCall == nil || len(got.ToolCall.Content) != 1 {
		t.Fatalf("tool call = %+v", got.ToolCall)
	}
	if got.ToolCall.Content[0].OldText != nil {
		t.Fatalf("oldText = %q, want nil for a created file", *got.ToolCall.Content[0].OldText)
	}
}

func TestParseSessionUpdateToolCallTerminal(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"c","kind":"execute","content":[{"type":"terminal","terminalId":"term_7"}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.ToolCall == nil || len(got.ToolCall.Content) != 1 {
		t.Fatalf("tool call = %+v", got.ToolCall)
	}
	if got.ToolCall.Content[0].TerminalID != "term_7" {
		t.Fatalf("terminalId = %q", got.ToolCall.Content[0].TerminalID)
	}
}

func TestParseSessionUpdatePlan(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"plan","entries":[{"content":"Locate the rounding","status":"completed"},{"content":"Run the tests","status":"in_progress"}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdatePlan || len(got.Plan) != 2 {
		t.Fatalf("kind=%q plan=%+v", got.Kind, got.Plan)
	}
	if got.Plan[1].Status != "in_progress" {
		t.Fatalf("plan[1] = %+v", got.Plan[1])
	}
}

func TestParseSessionUpdateAvailableCommands(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact","description":"summarise the turn"}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateAvailableCommands || len(got.Commands) != 1 {
		t.Fatalf("kind=%q commands=%+v", got.Kind, got.Commands)
	}
	if got.Commands[0].Name != "compact" {
		t.Fatalf("command = %+v", got.Commands[0])
	}
}

// A kind this build does not model must keep its blob rather than vanish: the
// Meta Inspector reads Raw, and a dropped notification is unrecoverable.
func TestParseSessionUpdateUnknownKeepsRaw(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"something_new","payload":{"a":1}}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateUnknown {
		t.Fatalf("kind = %q, want unknown", got.Kind)
	}
	if string(got.Raw) != string(raw) {
		t.Fatalf("Raw = %s", got.Raw)
	}
}

func TestParseSessionUpdateMalformedDoesNotPanic(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, {}, json.RawMessage(`not json`), json.RawMessage(`[]`)} {
		got := agenthub.ParseSessionUpdate(raw)
		if got.Kind != agenthub.UpdateUnknown {
			t.Fatalf("kind = %q for %q", got.Kind, raw)
		}
	}
}

// usage_update is not in the spec text this parser was written from. It turned
// up in a live claude-agent-acp 0.74 capture, and it is where the composer's
// context counter and running cost come from.
func TestParseSessionUpdateUsage(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"usage_update","used":24503,"size":1000000,"cost":{"amount":0.21,"currency":"USD"}}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateUsage {
		t.Fatalf("kind = %q, want usage_update", got.Kind)
	}
	if got.Usage == nil {
		t.Fatal("usage is nil")
	}
	if got.Usage.Used != 24503 || got.Usage.Size != 1000000 {
		t.Fatalf("context = %d/%d", got.Usage.Used, got.Usage.Size)
	}
	if got.Usage.Amount != 0.21 || got.Usage.Currency != "USD" {
		t.Fatalf("cost = %v %s", got.Usage.Amount, got.Usage.Currency)
	}
}

func TestParseSessionUpdateChunkCarriesMessageID(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"},"messageId":"7d557a03"}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.MessageID != "7d557a03" {
		t.Fatalf("messageId = %q", got.MessageID)
	}
}

func TestParseKeepsTheVendorsOwnKindForUnknowns(t *testing.T) {
	// claude-agent-acp emits fifteen sessionUpdate kinds; this build models
	// six. The other nine degraded to "unknown" with the vendor's own name
	// thrown away, so an unmodelled kind and a malformed notification looked
	// identical and neither was reported.
	u := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"subagent_spawned","agentId":"a1"}`))
	if u.Kind != agenthub.UpdateUnknown {
		t.Fatalf("kind = %q, want unknown", u.Kind)
	}
	if u.WireKind != "subagent_spawned" {
		t.Fatalf("wireKind = %q, want the vendor's own string", u.WireKind)
	}
	if len(u.Raw) == 0 {
		t.Fatal("raw dropped; the payload is the only record of what arrived")
	}

	// A payload that is not a session/update at all has no kind to report.
	bad := agenthub.ParseSessionUpdate([]byte(`not json`))
	if bad.Kind != agenthub.UpdateUnknown || bad.WireKind != "" {
		t.Fatalf("malformed parse = %+v, want unknown with no wire kind", bad)
	}

	// Not persisted: the stored payload would carry the same string twice.
	body, err := json.Marshal(agenthub.ParseSessionUpdate(
		[]byte(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"x"}}`)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(body, []byte("wireKind")) {
		t.Fatalf("wireKind persisted into the transcript payload: %s", body)
	}
}

func TestParseSessionInfoCarriesTheTitleButNotTheGoal(t *testing.T) {
	// One kind, two payloads. Measured off a real session:
	//   {"sessionUpdate":"session_info_update","title":"Dev-kit-backend",
	//    "updatedAt":"2026-09-06T15:06:15.260Z"}
	// and, behind the goal extension, the same kind with _meta.goal and no
	// title. Reading only the goal shape is how this kind went unmodelled.
	titled := agenthub.ParseSessionUpdate(
		[]byte(`{"sessionUpdate":"session_info_update","title":"Dev-kit-backend","updatedAt":"2026-09-06T15:06:15.260Z"}`))
	if titled.Kind != agenthub.UpdateSessionInfo {
		t.Fatalf("kind = %q, want session_info_update", titled.Kind)
	}
	if titled.Title != "Dev-kit-backend" {
		t.Fatalf("title = %q, want the vendor's generated name", titled.Title)
	}

	// The goal shape is understood and carries nothing to apply. It must not
	// fall back to unknown, or it would be logged as new every session.
	goal := agenthub.ParseSessionUpdate(
		[]byte(`{"sessionUpdate":"session_info_update","_meta":{"goal":{"objective":"ship it"}}}`))
	if goal.Kind != agenthub.UpdateSessionInfo {
		t.Fatalf("kind = %q, want session_info_update", goal.Kind)
	}
	if goal.Title != "" {
		t.Fatalf("title = %q, want empty on the goal shape", goal.Title)
	}
}
