package agenthub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

func previewFixture(t *testing.T) (*agenthub.Service, *agenthub.SQLStore, string) {
	t.Helper()
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
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{
		AgentType: agenthub.AgentTypeMock,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, sess.ID
}

func appendToolRow(t *testing.T, store *agenthub.SQLStore, sessionID, toolCallID string, payload []byte) {
	t.Helper()
	if err := store.AppendTranscript(context.Background(), agenthub.TranscriptEntry{
		ID:         sessionID + "-" + toolCallID,
		SessionID:  sessionID,
		Kind:       agenthub.UpdateToolCall,
		ToolCallID: toolCallID,
		Payload:    payload,
	}); err != nil {
		t.Fatal(err)
	}
}

func pagePayloads(t *testing.T, svc *agenthub.Service, sessionID string) map[string]json.RawMessage {
	t.Helper()
	page, err := svc.TranscriptPage(context.Background(), sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	for _, entry := range page.Entries {
		out[entry.ToolCallID] = entry.Payload
	}
	return out
}

// A tool call with a huge raw output must not make the whole page heavy: the
// page carries a bounded preview and says it was cut; the detail read still
// returns everything the recorder stored.
func TestTranscriptPagePreviewsOversizedToolRaw(t *testing.T) {
	svc, store, sessionID := previewFixture(t)
	ctx := context.Background()

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
	appendToolRow(t, store, sessionID, "big", payload)

	var previewed agenthub.SessionUpdate
	if err := json.Unmarshal(pagePayloads(t, svc, sessionID)["big"], &previewed); err != nil {
		t.Fatal(err)
	}
	if previewed.ToolCall == nil || !previewed.ToolCall.Truncated {
		t.Fatalf("previewed call = %+v, want truncated=true", previewed.ToolCall)
	}
	if got := len(previewed.ToolCall.RawOutput); got > agenthub.ToolRawPreviewLimit {
		t.Fatalf("previewed rawOutput = %d bytes, want at most the preview limit", got)
	}

	detail, err := svc.ToolCallDetail(ctx, sessionID, "big")
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

// The preview used to be strconv.Quote of the first 8 KiB. A cut inside a
// multi-byte character — any Vietnamese, CJK or emoji output — made Quote write
// \xNN, which is not JSON; the page marshal failed, and the row went out whole
// and unmarked, the opposite of what the preview is for.
func TestTranscriptPagePreviewCutIsValidJSONForAnyText(t *testing.T) {
	svc, store, sessionID := previewFixture(t)

	units := map[string]string{
		"vietnamese":  "Tiếng Việt có dấu: người, trưởng, quyển ",
		"cjk":         "漢字かな交じり文、中文输出",
		"emoji":       "🙂👍🏽🇻🇳",
		"box":         "┌─┬─┐│ │└─┴─┘",
		"html":        "<a href='x'>&amp;</a>",
		"unprintable": "\x7f\u0085\u2028\U000E0001",
	}
	raws := map[string]string{}
	for name, unit := range units {
		// Every padding lands the 8 KiB cut on a different byte of the
		// character, so at least one of them splits it.
		for pad := 0; pad < utf8.UTFMax; pad++ {
			id := fmt.Sprintf("%s-%d", name, pad)
			text := strings.Repeat("a", pad) + strings.Repeat(unit, 2*agenthub.ToolRawPreviewLimit/len(unit)+1)
			// Built by hand so every character reaches the row literally;
			// json.Marshal would escape some of them to ASCII first.
			raw := `"` + text + `"`
			raws[id] = raw
			appendToolRow(t, store, sessionID, id, []byte(`{"kind":"tool_call","toolCall":{"id":"`+id+`","rawOutput":`+raw+`}}`))
		}
	}

	payloads := pagePayloads(t, svc, sessionID)
	for id, raw := range raws {
		var got struct {
			ToolCall struct {
				RawOutput json.RawMessage `json:"rawOutput"`
				Truncated bool            `json:"truncated"`
			} `json:"toolCall"`
		}
		if err := json.Unmarshal(payloads[id], &got); err != nil {
			t.Fatalf("%s: page payload is not JSON: %v", id, err)
		}
		if !got.ToolCall.Truncated {
			t.Fatalf("%s: preview not marked truncated (payload %d bytes)", id, len(payloads[id]))
		}
		if n := len(got.ToolCall.RawOutput); n > agenthub.ToolRawPreviewLimit {
			t.Fatalf("%s: preview is %d bytes, over the %d limit", id, n, agenthub.ToolRawPreviewLimit)
		}
		var preview string
		if err := json.Unmarshal(got.ToolCall.RawOutput, &preview); err != nil {
			t.Fatalf("%s: preview is not a JSON string: %v", id, err)
		}
		if !utf8.ValidString(preview) || !strings.HasPrefix(raw, preview) || preview == "" {
			t.Fatalf("%s: preview %q is not a clean prefix of the stored output", id, preview[:min(len(preview), 40)])
		}
	}
}

// Rows written before the parser dropped Raw still carry the whole wire beside
// the fields the preview cuts, and tool content — a long log, a big diff — was
// never bounded at all. A diff is not cut inside: it keeps its path, loses its
// texts, and is marked truncated; the detail read has it whole.
func TestTranscriptPageBoundsToolContentAndDropsRaw(t *testing.T) {
	svc, store, sessionID := previewFixture(t)
	ctx := context.Background()

	huge := strings.Repeat("line of output\n", 2000)
	old := strings.Repeat("old line\n", 2000)
	legacyRaw, err := json.Marshal(map[string]any{"sessionUpdate": "tool_call", "rawOutput": huge})
	if err != nil {
		t.Fatal(err)
	}
	appendToolRow(t, store, sessionID, "legacy",
		[]byte(`{"kind":"tool_call","toolCall":{"id":"legacy","title":"Read","rawOutput":"42 lines"},"raw":`+string(legacyRaw)+`}`))

	edit, err := json.Marshal(agenthub.SessionUpdate{Kind: agenthub.UpdateToolCall, ToolCall: &agenthub.ToolCall{
		ID: "edit", Title: "Edit", Content: []agenthub.ToolCallContent{{Type: "diff", Path: "main.go", OldText: &old, NewText: huge}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	appendToolRow(t, store, sessionID, "edit", edit)

	logRow, err := json.Marshal(agenthub.SessionUpdate{Kind: agenthub.UpdateToolCall, ToolCall: &agenthub.ToolCall{
		ID: "log", Title: "Run", Content: []agenthub.ToolCallContent{{Type: "content", Content: &agenthub.ContentBlock{Type: "text", Text: huge}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	appendToolRow(t, store, sessionID, "log", logRow)

	payloads := pagePayloads(t, svc, sessionID)
	decode := func(id string) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(payloads[id], &m); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return m
	}

	if legacy := decode("legacy"); legacy["raw"] != nil || len(payloads["legacy"]) > agenthub.ToolRawPreviewLimit {
		t.Fatalf("legacy row still carries raw (%d bytes on the page)", len(payloads["legacy"]))
	}

	editCall := decode("edit")["toolCall"].(map[string]any)
	diff := editCall["content"].([]any)[0].(map[string]any)
	if editCall["truncated"] != true || diff["truncated"] != true || diff["path"] != "main.go" {
		t.Fatalf("oversized diff on the page = %v (call truncated=%v), want path kept and truncated", diff, editCall["truncated"])
	}
	if _, has := diff["newText"]; has {
		t.Fatal("oversized diff kept newText: a diff is summarized, never cut inside")
	}
	if _, has := diff["oldText"]; has {
		t.Fatal("oversized diff kept oldText: a diff is summarized, never cut inside")
	}

	logCall := decode("log")["toolCall"].(map[string]any)
	item := logCall["content"].([]any)[0].(map[string]any)
	text, _ := item["content"].(map[string]any)["text"].(string)
	if logCall["truncated"] != true || item["truncated"] != true || len(text) > agenthub.ToolRawPreviewLimit || !strings.HasPrefix(huge, text) || text == "" {
		t.Fatalf("log content on the page: %d bytes, truncated=%v", len(text), item["truncated"])
	}

	detail, err := svc.ToolCallDetail(ctx, sessionID, "edit")
	if err != nil {
		t.Fatal(err)
	}
	var full agenthub.SessionUpdate
	if err := json.Unmarshal(detail.Payload, &full); err != nil {
		t.Fatal(err)
	}
	got := full.ToolCall.Content[0]
	if got.NewText != huge || got.OldText == nil || *got.OldText != old || got.Truncated {
		t.Fatal("detail read did not return the whole diff")
	}
}
