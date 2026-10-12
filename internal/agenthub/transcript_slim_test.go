package agenthub

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolUpdate(t *testing.T, name, rawInput string, image string) SessionUpdate {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "c1",
		"title":         name,
		"name":          name,
		"status":        "completed",
		"rawInput":      json.RawMessage(rawInput),
		"rawOutput":     `[{"type":"text","text":"caption"},{"type":"image","source":{"data":"` + image + `"}}]`,
		"content": []any{
			map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "Document doc1 · image 1 = page 2."}},
			map[string]any{"type": "content", "content": map[string]any{"type": "image", "mimeType": "image/png", "data": image}},
		},
	})
	return ParseSessionUpdate(raw)
}

func TestPageImagesAreNotKeptInTheStoredTranscript(t *testing.T) {
	picture := strings.Repeat("QUJD", 40_000) // ~160 KB of base64
	for name, in := range map[string]SessionUpdate{
		"claude": toolUpdate(t, "mcp__devkit__pdf_viewPages", `{"documentId":"doc1","pages":2}`, picture),
		"codex":  toolUpdate(t, "exec", `{"input":"const r = await tools.mcp__devkit__pdf_viewPages({documentId:'doc1',pages:2}); image(r.content[1])"}`, picture),
	} {
		stored, ok := slimPageImages(in)
		if !ok {
			t.Fatalf("%s: nothing was removed", name)
		}
		for _, item := range stored.ToolCall.Content {
			if item.Content.Type == "image" && (item.Content.Data != "" || !item.Truncated) {
				t.Errorf("%s: the stored image kept its data or did not say it was cut: %+v", name, item)
			}
		}
		if !strings.Contains(stored.ToolCall.Content[0].Content.Text, "image 1 = page 2") {
			t.Errorf("%s: the caption was lost", name)
		}
		if string(stored.ToolCall.RawOutput) != `"`+pageImageMarker+`"` {
			t.Errorf("%s: raw output = %.120s", name, stored.ToolCall.RawOutput)
		}
		// The live copy is untouched.
		if in.ToolCall.Content[1].Content.Data != picture || len(in.ToolCall.RawOutput) < 100_000 {
			t.Errorf("%s: slimming changed the update it was given", name)
		}
		if raw, _ := json.Marshal(stored); len(raw) > 4096 {
			t.Errorf("%s: the stored update is still %d bytes", name, len(raw))
		}
	}
}

func TestOtherToolsKeepTheirImages(t *testing.T) {
	picture := strings.Repeat("QUJD", 40_000)
	in := toolUpdate(t, "mcp__other__screenshot", `{"url":"x"}`, picture)
	stored, ok := slimPageImages(in)
	if ok || stored.ToolCall.Content[1].Content.Data != picture {
		t.Fatal("an image from some other tool was dropped")
	}
	if _, ok := slimPageImages(SessionUpdate{}); ok {
		t.Fatal("an update with no tool call was changed")
	}
}
