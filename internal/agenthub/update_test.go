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

// config_option_update is how the agent publishes the session's settings —
// model, effort, fast mode. Payload shape taken from claude-agent-acp 0.74's
// own ConfigOptionUpdate/SessionConfigOption declarations: `options` is either
// a flat list of values or a list of groups, and `currentValue` is a value id
// on a select but a bool on a toggle.
func TestParseSessionUpdateConfigOptions(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"config_option_update","configOptions":[
		{"id":"model","name":"Model","category":"model","type":"select","currentValue":"opus",
		 "options":[{"group":"anthropic","name":"Anthropic","options":[
			{"value":"opus","name":"Opus 5"},{"value":"haiku","name":"Haiku 4.5"}]}]},
		{"id":"fast_mode","name":"Fast mode","type":"boolean","currentValue":false}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Kind != agenthub.UpdateConfigOptions {
		t.Fatalf("kind = %q, want config_option_update", got.Kind)
	}
	if len(got.ConfigOptions) != 2 {
		t.Fatalf("options = %d, want 2", len(got.ConfigOptions))
	}
	model := got.ConfigOptions[0]
	if model.ID != "model" || model.Type != "select" || string(model.CurrentValue) != `"opus"` {
		t.Fatalf("model option = %+v", model)
	}
	// A group's members arrive flat, each keeping the group it came from.
	if len(model.Options) != 2 {
		t.Fatalf("model values = %+v, want the group flattened", model.Options)
	}
	if model.Options[1].Value != "haiku" || model.Options[1].Group != "Anthropic" {
		t.Fatalf("grouped value = %+v", model.Options[1])
	}
	// A boolean's currentValue is a bool, not a value id: leaving it raw is what
	// lets one field carry both without the parse failing on the other shape.
	if string(got.ConfigOptions[1].CurrentValue) != "false" {
		t.Fatalf("fast mode currentValue = %s", got.ConfigOptions[1].CurrentValue)
	}
}

// A flat option list is the other half of the same wire shape.
func TestParseSessionUpdateConfigOptionsUngrouped(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"config_option_update","configOptions":[
		{"id":"effort","type":"select","currentValue":"high",
		 "options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}`)

	got := agenthub.ParseSessionUpdate(raw)

	values := got.ConfigOptions[0].Options
	if len(values) != 2 || values[0].Value != "low" || values[0].Group != "" {
		t.Fatalf("values = %+v", values)
	}
}

// The three async_task_* kinds describe one background task between them: the
// spawn names it, progress reports what it is doing, the state update ends it.
// No single message is complete, which is why they all land on one struct.
func TestParseSessionUpdateAsyncTask(t *testing.T) {
	spawned := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"async_task_spawned",
		"asyncTaskId":"t1","name":"Build","taskType":"bash","description":"go build ./...",
		"showInTranscript":true,"canStop":true,"toolCallId":"call_9"}`))
	if spawned.Kind != agenthub.UpdateAsyncTaskSpawned {
		t.Fatalf("kind = %q, want async_task_spawned", spawned.Kind)
	}
	if spawned.AsyncTask == nil {
		t.Fatal("asyncTask is nil")
	}
	if spawned.AsyncTask.ID != "t1" || spawned.AsyncTask.Name != "Build" {
		t.Fatalf("task = %+v", spawned.AsyncTask)
	}
	if !spawned.AsyncTask.ShowInTranscript || !spawned.AsyncTask.CanStop {
		t.Fatalf("flags = %+v; the vendor said to show it and that it can be stopped", spawned.AsyncTask)
	}
	if spawned.AsyncTask.ToolCallID != "call_9" {
		t.Fatalf("toolCallId = %q; the task is unlinkable from its tool call without it", spawned.AsyncTask.ToolCallID)
	}

	progress := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"async_task_progress",
		"asyncTaskId":"t1","lastToolName":"Bash",
		"usage":{"totalTokens":1200,"toolUses":3,"durationMs":8400}}`))
	if progress.Kind != agenthub.UpdateAsyncTaskProgress {
		t.Fatalf("kind = %q, want async_task_progress", progress.Kind)
	}
	if progress.AsyncTask.Usage == nil || progress.AsyncTask.Usage.TotalTokens != 1200 {
		t.Fatalf("usage = %+v", progress.AsyncTask.Usage)
	}
	if progress.AsyncTask.Usage.ToolUses != 3 || progress.AsyncTask.Usage.DurationMs != 8400 {
		t.Fatalf("usage = %+v", progress.AsyncTask.Usage)
	}
	// Progress says nothing about the name; a reader keeps it by id from spawn.
	if progress.AsyncTask.Name != "" {
		t.Fatalf("name = %q, want empty on a progress update", progress.AsyncTask.Name)
	}

	done := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"async_task_state_update",
		"asyncTaskId":"t1","state":"completed","summary":"ok","outputFilePath":"/tmp/t1.log"}`))
	if done.Kind != agenthub.UpdateAsyncTaskState {
		t.Fatalf("kind = %q, want async_task_state_update", done.Kind)
	}
	if done.AsyncTask.State != "completed" || done.AsyncTask.Summary != "ok" {
		t.Fatalf("state = %+v", done.AsyncTask)
	}
	if done.AsyncTask.OutputFilePath != "/tmp/t1.log" {
		t.Fatalf("outputFilePath = %q", done.AsyncTask.OutputFilePath)
	}
	// usage_update carries its own numbers at the top level; an async task's
	// ride in a `usage` object. Neither may claim the other's.
	if done.Usage != nil {
		t.Fatalf("Usage = %+v; an async task's spend is not the session's", done.Usage)
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
	// claude-agent-acp 0.74 emits sixteen sessionUpdate kinds; this build once
	// modelled six. The other ten degraded to "unknown" with the vendor's own
	// name thrown away, so an unmodelled kind and a malformed notification
	// looked identical and neither was reported.
	// All sixteen are modelled now, so the example has to be a kind no version
	// has shipped — every real one stopped being an example, which is what the
	// report is for.
	u := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"seventeenth_kind","taskId":"t1"}`))
	if u.Kind != agenthub.UpdateUnknown {
		t.Fatalf("kind = %q, want unknown", u.Kind)
	}
	if u.WireKind != "seventeenth_kind" {
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

func TestParseSubagentUpdates(t *testing.T) {
	// Captured from claude-agent-acp 0.74.0 with the air extension declared.
	// Without that declaration these two never arrive at all — only the
	// subagent's text does, as ordinary agent_message_chunk.
	spawned := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"subagent_spawned",` +
		`"subagentSessionId":"a3df49ca2b5690428","name":"List directory files",` +
		`"task":"List the files and directories in /private/tmp/proj.","capabilities":{}}`))
	if spawned.Kind != agenthub.UpdateSubagentSpawned {
		t.Fatalf("kind = %q, want subagent_spawned", spawned.Kind)
	}
	if spawned.Subagent == nil || spawned.Subagent.ID != "a3df49ca2b5690428" {
		t.Fatalf("subagent = %+v, want the vendor's id", spawned.Subagent)
	}
	if spawned.Subagent.Name != "List directory files" || spawned.Subagent.Task == "" {
		t.Fatalf("subagent = %+v, want name and task", spawned.Subagent)
	}

	// The state update carries the id and nothing else about the subagent, so
	// a reader has to keep the pair together rather than expect either message
	// to be complete on its own.
	state := agenthub.ParseSessionUpdate([]byte(`{"sessionUpdate":"subagent_state_update",` +
		`"subagentSessionId":"a3df49ca2b5690428","state":"completed"}`))
	if state.Kind != agenthub.UpdateSubagentState {
		t.Fatalf("kind = %q, want subagent_state_update", state.Kind)
	}
	if state.Subagent == nil || state.Subagent.State != "completed" {
		t.Fatalf("subagent = %+v, want the new state", state.Subagent)
	}
	if state.Subagent.Name != "" {
		t.Fatalf("name = %q, want empty; only spawn carries it", state.Subagent.Name)
	}
}

func TestClientCapabilitiesCarryTheAirExtension(t *testing.T) {
	// Two gates, not one: `subagents` forwards a subagent's text, the air
	// extension is what makes the vendor say a subagent exists at all.
	body, err := json.Marshal(agenthub.DevkitClientCapabilitiesForTest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var caps struct {
		Subagents map[string]any `json:"subagents"`
		Meta      struct {
			JetBrains struct {
				Air struct {
					Version      int      `json:"version"`
					Capabilities []string `json:"capabilities"`
				} `json:"air"`
			} `json:"jetbrains"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(body, &caps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if caps.Subagents == nil {
		t.Fatal("subagents not declared")
	}
	air := caps.Meta.JetBrains.Air
	if air.Version != 1 {
		t.Fatalf("air version = %d, want 1", air.Version)
	}
	var found bool
	for _, c := range air.Capabilities {
		if c == "nativeSubagentSessions" {
			found = true
		}
	}
	if !found {
		t.Fatalf("air capabilities = %v, want nativeSubagentSessions", air.Capabilities)
	}
}

// A chunk can carry an embedded resource, the same shape DevKit sends when a
// prompt has an attachment. The struct this parses into used to be a private
// copy of acp.ContentBlock without the Resource field, so the body arrived and
// was dropped on the floor. This is the test that fails if the copy comes back.
func TestParseSessionUpdateKeepsAnEmbeddedResource(t *testing.T) {
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"resource","resource":{"uri":"file:///w/main.go","text":"package main","mimeType":"text/x-go"}}}`)

	got := agenthub.ParseSessionUpdate(raw)

	if got.Content == nil {
		t.Fatal("Content is nil for a resource block")
	}
	if got.Content.Resource == nil {
		t.Fatal("Resource is nil; the block's body was dropped")
	}
	if got.Content.Resource.Text != "package main" {
		t.Fatalf("Resource.Text = %q", got.Content.Resource.Text)
	}
}
