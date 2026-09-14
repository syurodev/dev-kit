package agenthub_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// For the two kinds that stream per token, Raw was a byte-for-byte duplicate
// of fields the normalized form already carries: every chunk row held its text
// twice, every tool row held rawInput/rawOutput twice, and the event carried
// the wire twice more. The parser drops it at the source so no writer can
// reintroduce the copy.
func TestParseSessionUpdateDropsRedundantRawForStreamedKinds(t *testing.T) {
	chunk := agenthub.ParseSessionUpdate(json.RawMessage(
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"},"messageId":"m1"}`))
	if chunk.Raw != nil {
		t.Fatalf("chunk kept Raw (%d bytes); content.text already carries it", len(chunk.Raw))
	}

	tool := agenthub.ParseSessionUpdate(json.RawMessage(
		`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Edit","rawInput":{"path":"a.go"},"rawOutput":{"diff":"..."}}`))
	if tool.Raw != nil {
		t.Fatalf("tool call kept Raw (%d bytes); rawInput/rawOutput already carry it", len(tool.Raw))
	}

	// Unknown kinds keep Raw: it is the only copy of what the vendor sent.
	unknown := agenthub.ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"something_new","x":1}`))
	if len(unknown.Raw) == 0 {
		t.Fatal("unknown kind lost Raw — that row is the Meta Inspector's whole content")
	}
}

// A tool call with a huge raw output must not make the whole page heavy: the
// page carries a bounded preview and says it was cut; the detail read still
// returns everything the recorder stored.
func TestTranscriptPagePreviewsOversizedToolRaw(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := agenthub.NewSQLStore(db)
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	svc := agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	huge := strings.Repeat("x", 64*1024)
	payload, err := json.Marshal(agenthub.SessionUpdate{
		Kind: agenthub.UpdateToolCall,
		ToolCall: &agenthub.ToolCall{
			ID:        "big",
			Title:     "Run tests",
			RawOutput: json.RawMessage(`"` + huge + `"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscript(ctx, agenthub.TranscriptEntry{
		ID:         "e1",
		SessionID:  sess.ID,
		Seq:        1,
		Kind:       agenthub.UpdateToolCall,
		ToolCallID: "big",
		Payload:    payload,
	}); err != nil {
		t.Fatal(err)
	}

	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("page = %d entries, want 1", len(page.Entries))
	}
	var previewed agenthub.SessionUpdate
	if err := json.Unmarshal(page.Entries[0].Payload, &previewed); err != nil {
		t.Fatal(err)
	}
	if previewed.ToolCall == nil || !previewed.ToolCall.Truncated {
		t.Fatalf("previewed call = %+v, want truncated=true", previewed.ToolCall)
	}
	if got := len(string(previewed.ToolCall.RawOutput)); got > agenthub.ToolRawPreviewLimit+64 {
		t.Fatalf("previewed rawOutput = %d bytes, want at most the preview limit (plus quotes)", got)
	}

	detail, err := svc.ToolCallDetail(ctx, sess.ID, "big")
	if err != nil {
		t.Fatal(err)
	}
	var full agenthub.SessionUpdate
	if err := json.Unmarshal(detail.Payload, &full); err != nil {
		t.Fatal(err)
	}
	if full.ToolCall == nil || full.ToolCall.Truncated {
		t.Fatalf("detail call = %+v, want the untruncated row", full.ToolCall)
	}
	if got := len(string(full.ToolCall.RawOutput)); got < 64*1024 {
		t.Fatalf("detail rawOutput = %d bytes, want everything the recorder stored", got)
	}
}
