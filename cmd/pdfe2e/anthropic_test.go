package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// anthropicHandler speaks enough of the Messages API for Claude Code: streamed
// /v1/messages, and harmless answers for the side requests it makes.
func anthropicHandler(m *modelServer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":100}`))
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m.record(string(raw))
		var req struct {
			Model    string            `json:"model"`
			System   json.RawMessage   `json:"system"`
			Messages []json.RawMessage `json:"messages"`
			Tools    []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &req)
		var tools []string
		for _, t := range req.Tools {
			tools = append(tools, t.Name)
		}
		main := pick(tools, "pdf_list") != ""
		if !main {
			// Titles, summaries, quota probes: anything small. Answer and move on.
			anthropicText(w, req.Stream, "ok")
			return
		}

		var prompts []string
		calls := 0
		for _, rawMsg := range req.Messages {
			var msg struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(rawMsg, &msg)
			var blocks []map[string]any
			if json.Unmarshal(msg.Content, &blocks) != nil {
				var s string
				_ = json.Unmarshal(msg.Content, &s)
				if msg.Role == "user" && s != "" {
					prompts = append(prompts, s)
				}
				continue
			}
			for _, b := range blocks {
				switch b["type"] {
				case "text":
					if msg.Role == "user" {
						prompts = append(prompts, b["text"].(string))
					}
				case "tool_use":
					calls++
					m.rememberCall(b["id"].(string), b["name"].(string))
				case "tool_result":
					m.addResult(anthropicResult(b))
				}
			}
		}
		m.note(anthropicSystem(req.System), prompts, tools)

		step, ok := m.next(calls)
		if !ok {
			anthropicText(w, req.Stream, m.finalText())
			return
		}
		name := pick(tools, step.Tool)
		if name == "" {
			anthropicText(w, req.Stream, "tool "+step.Tool+" is not offered; offered: "+strings.Join(tools, ", "))
			return
		}
		anthropicToolUse(w, name, step.Args, m.newID("toolu_e2e"))
	})
	return mux
}

func anthropicSystem(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

func anthropicResult(b map[string]any) seenResult {
	res := seenResult{CallID: b["tool_use_id"].(string), Raw: mustJSON(b)}
	res.IsError, _ = b["is_error"].(bool)
	switch c := b["content"].(type) {
	case string:
		res.Blocks = append(res.Blocks, resultBlock{Kind: "text", Text: c})
	case []any:
		for _, item := range c {
			blk, _ := item.(map[string]any)
			switch blk["type"] {
			case "text":
				res.Blocks = append(res.Blocks, resultBlock{Kind: "text", Text: blk["text"].(string)})
			case "image":
				src, _ := blk["source"].(map[string]any)
				data, _ := src["data"].(string)
				mime, _ := src["media_type"].(string)
				res.Blocks = append(res.Blocks, resultBlock{Kind: "image", MIME: mime, Data: decodeB64(data)})
			}
		}
	}
	return res
}

func anthropicStart(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	sse(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_e2e", "type": "message", "role": "assistant", "model": "claude-e2e", "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 50, "output_tokens": 1},
	}})
}

func anthropicText(w http.ResponseWriter, stream bool, text string) {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mustJSON(map[string]any{
			"id": "msg_e2e", "type": "message", "role": "assistant", "model": "claude-e2e",
			"content":     []any{map[string]any{"type": "text", "text": text}},
			"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 5, "output_tokens": 1},
		})))
		return
	}
	anthropicStart(w)
	sse(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	sse(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	sse(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sse(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 5}})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}

func anthropicToolUse(w http.ResponseWriter, name string, args map[string]any, id string) {
	anthropicStart(w)
	sse(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}})
	sse(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": mustJSON(args)}})
	sse(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sse(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 20}})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}
