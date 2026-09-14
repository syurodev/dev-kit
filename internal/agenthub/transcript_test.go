package agenthub_test

import (
	"context"
	"encoding/json"
	"testing"

	"devkit/internal/agenthub"

	"github.com/google/uuid"
)

func recordAll(t *testing.T, store agenthub.Store, sessionID string, raws ...string) {
	t.Helper()
	rec := agenthub.NewRecorder(store)
	for _, raw := range raws {
		if err := rec.Record(context.Background(), sessionID, agenthub.ParseSessionUpdate(json.RawMessage(raw))); err != nil {
			t.Fatalf("record %s: %v", raw, err)
		}
	}
}

// Chunks arrive per token. One row each would make this table grow faster than
// anything else in DevKit, so consecutive chunks of the same kind fold into one.
func TestRecorderCoalescesConsecutiveChunks(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Found "}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"the "}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"bug."}}`,
	)

	entries, err := store.ListTranscript(context.Background(), session)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 coalesced row", len(entries))
	}
	if entries[0].Text != "Found the bug." {
		t.Fatalf("text = %q", entries[0].Text)
	}
}

// A thought and a message are different kinds, so they must not merge into one
// bubble even when they arrive back to back.
func TestRecorderSplitsChunksOfDifferentKinds(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"answer"}}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Kind != agenthub.UpdateAgentThoughtChunk || entries[1].Kind != agenthub.UpdateAgentMessageChunk {
		t.Fatalf("kinds = %q, %q", entries[0].Kind, entries[1].Kind)
	}
	if entries[0].Seq >= entries[1].Seq {
		t.Fatalf("seq not increasing: %d, %d", entries[0].Seq, entries[1].Seq)
	}
}

// ACP sends tool_call once and tool_call_update repeatedly for the same id. A
// transcript that appended each would show one call five times.
func TestRecorderMergesToolCallUpdatesOntoOneRow(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Edit RefundService","kind":"edit","status":"pending"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed","content":[{"type":"diff","path":"/a.java","newText":"x"}]}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 merged tool call", len(entries))
	}

	var got agenthub.SessionUpdate
	if err := json.Unmarshal(entries[0].Payload, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if got.ToolCall == nil {
		t.Fatal("tool call missing from payload")
	}
	if got.ToolCall.Status != "completed" {
		t.Fatalf("status = %q, want completed", got.ToolCall.Status)
	}
	// A status-only update carries no title; an empty field means unchanged,
	// never cleared.
	if got.ToolCall.Title != "Edit RefundService" {
		t.Fatalf("title = %q, want it preserved across partial updates", got.ToolCall.Title)
	}
	if got.ToolCall.Kind != "edit" {
		t.Fatalf("kind = %q, want it preserved", got.ToolCall.Kind)
	}
	if len(got.ToolCall.Content) != 1 || got.ToolCall.Content[0].Type != "diff" {
		t.Fatalf("content = %+v", got.ToolCall.Content)
	}
}

func TestRecorderKeepsSeparateToolCallsApart(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","status":"completed"}`,
		`{"sessionUpdate":"tool_call","toolCallId":"c2","status":"pending"}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

// An unmodelled kind still earns a row: the Meta Inspector reads Raw, and a
// dropped notification cannot be recovered.
func TestRecorderPersistsUnknownKinds(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session, `{"sessionUpdate":"something_new","payload":{"a":1}}`)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 1 || entries[0].Kind != agenthub.UpdateUnknown {
		t.Fatalf("entries = %+v", entries)
	}
	var got agenthub.SessionUpdate
	if err := json.Unmarshal(entries[0].Payload, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(got.Raw) == 0 {
		t.Fatal("raw blob was dropped")
	}
}

func TestRecorderChunkAfterToolCallStartsNewRow(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"before"}}`,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","status":"completed"}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"after"}}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[2].Text != "after" {
		t.Fatalf("last text = %q; a chunk must not fold into the row before the tool call", entries[2].Text)
	}
}

func TestListTranscriptIsScopedToOneSession(t *testing.T) {
	store := newTestStore(t)
	a := seedSession(t, store)
	b := seedSession(t, store)

	recordAll(t, store, a, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"a"}}`)
	recordAll(t, store, b, `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"b"}}`)

	entries, _ := store.ListTranscript(context.Background(), a)
	if len(entries) != 1 || entries[0].Text != "a" {
		t.Fatalf("entries = %+v", entries)
	}
}

// seedSession inserts a session row so the transcript foreign key holds.
func seedSession(t *testing.T, store agenthub.Store) string {
	t.Helper()
	id := uuid.NewString()
	if err := store.InsertSession(context.Background(), sampleSession(id)); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return id
}

// Two assistant messages in one turn must not fold into one bubble just because
// both are agent_message_chunk. Claude tags each with its own messageId.
func TestRecorderSplitsChunksByMessageID(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"first"},"messageId":"m1"}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" part"},"messageId":"m1"}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"second"},"messageId":"m2"}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (one per messageId)", len(entries))
	}
	if entries[0].Text != "first part" {
		t.Fatalf("entries[0].Text = %q", entries[0].Text)
	}
	if entries[1].Text != "second" {
		t.Fatalf("entries[1].Text = %q", entries[1].Text)
	}
}

// Usage arrives on every turn and the command list can be resent mid-session.
// Appending each would grow the transcript without adding anything a person
// reads, so session-level kinds keep exactly one row and it is updated in place.
func TestRecorderUpsertsSessionStateKinds(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	recordAll(t, store, session,
		`{"sessionUpdate":"usage_update","used":100,"size":1000000,"cost":{"amount":0.01,"currency":"USD"}}`,
		`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact"}]}`,
		`{"sessionUpdate":"usage_update","used":24503,"size":1000000,"cost":{"amount":0.21,"currency":"USD"}}`,
		`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact"},{"name":"init"}]}`,
		`{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","type":"select","currentValue":"opus"}]}`,
		`{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","type":"select","currentValue":"haiku"}]}`,
	)

	entries, _ := store.ListTranscript(context.Background(), session)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want one row per session-level kind", len(entries))
	}

	byKind := map[agenthub.UpdateKind]agenthub.TranscriptEntry{}
	for _, e := range entries {
		byKind[e.Kind] = e
	}

	var usage agenthub.SessionUpdate
	if err := json.Unmarshal(byKind[agenthub.UpdateUsage].Payload, &usage); err != nil {
		t.Fatalf("usage payload: %v", err)
	}
	if usage.Usage == nil || usage.Usage.Used != 24503 {
		t.Fatalf("usage = %+v, want the latest value", usage.Usage)
	}

	var commands agenthub.SessionUpdate
	if err := json.Unmarshal(byKind[agenthub.UpdateAvailableCommands].Payload, &commands); err != nil {
		t.Fatalf("commands payload: %v", err)
	}
	if len(commands.Commands) != 2 {
		t.Fatalf("commands = %+v, want the latest list", commands.Commands)
	}

	// The agent republishes the whole option set on every model switch, effort
	// change and plan-mode exit, so this one matters most of the three.
	var config agenthub.SessionUpdate
	if err := json.Unmarshal(byKind[agenthub.UpdateConfigOptions].Payload, &config); err != nil {
		t.Fatalf("config payload: %v", err)
	}
	if len(config.ConfigOptions) != 1 || string(config.ConfigOptions[0].CurrentValue) != `"haiku"` {
		t.Fatalf("config = %+v, want the latest value", config.ConfigOptions)
	}
}
