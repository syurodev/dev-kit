package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// openaiHandler speaks enough of the Responses API for `codex app-server`:
// streamed /responses, and empty-but-valid answers for what it asks around it.
func openaiHandler(m *modelServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Header.Get("Upgrade") == "websocket" {
			http.Error(w, "websocket transport is not offered", http.StatusUpgradeRequired)
			return
		}
		if !(r.Method == http.MethodPost && strings.HasSuffix(path, "/responses")) {
			// Model lists, rate-limit probes, anything else: a valid empty object.
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(path, "/models") {
				_, _ = w.Write([]byte(`{"models":[],"data":[],"object":"list"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		m.record(string(raw))
		if os.Getenv("DEVKIT_PDF_E2E_DUMP") != "" {
			_ = os.WriteFile(os.Getenv("DEVKIT_PDF_E2E_DUMP"), raw, 0o600)
		}
		var req struct {
			Instructions string            `json:"instructions"`
			Input        []json.RawMessage `json:"input"`
			Tools        []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		var tools []string
		for _, t := range req.Tools {
			if t.Name != "" {
				tools = append(tools, t.Name)
			}
		}
		// Codex 0.147 delivers its tools as an input item, nested in namespaces.
		for _, rawItem := range req.Input {
			var item struct {
				Type  string          `json:"type"`
				Tools json.RawMessage `json:"tools"`
			}
			if json.Unmarshal(rawItem, &item) == nil && item.Type == "additional_tools" {
				tools = append(tools, nestedToolNames(item.Tools)...)
			}
		}
		calls := 0
		var prompts []string
		for _, rawItem := range req.Input {
			var item map[string]any
			_ = json.Unmarshal(rawItem, &item)
			switch item["type"] {
			case "message":
				if item["role"] == "user" || item["role"] == "developer" {
					blocks, _ := item["content"].([]any)
					for _, b := range blocks {
						if blk, ok := b.(map[string]any); ok {
							if txt, ok := blk["text"].(string); ok {
								prompts = append(prompts, txt)
							}
						}
					}
				}
			case "function_call", "custom_tool_call":
				calls++
				m.rememberCall(item["call_id"].(string), item["name"].(string))
			case "function_call_output", "custom_tool_call_output":
				m.addResult(openaiResult(item))
			}
		}
		system := req.Instructions
		m.note(system, prompts, tools)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		step, ok := m.next(calls)
		if !ok {
			openaiText(w, m.finalText())
			return
		}
		if step.Code != "" {
			if pick(tools, "exec") == "" {
				openaiText(w, "exec is not offered; offered: "+strings.Join(tools, ", "))
				return
			}
			openaiCustomCall(w, "exec", step.Code, m.newID("call_e2e"))
			return
		}
		name := pick(tools, step.Tool)
		if name == "" {
			openaiText(w, "tool "+step.Tool+" is not offered; offered: "+strings.Join(tools, ", "))
			return
		}
		openaiCall(w, name, step.Args, m.newID("call_e2e"))
	})
}

// openaiResult reads a function_call_output: its output is a string, or a list
// of input_text / input_image content items.
func openaiResult(item map[string]any) seenResult {
	res := seenResult{CallID: item["call_id"].(string), Raw: mustJSON(item)}
	switch out := item["output"].(type) {
	case string:
		// A string may itself be the JSON of an MCP result.
		res.Blocks = append(res.Blocks, resultBlock{Kind: "text", Text: out})
	case []any:
		for _, it := range out {
			blk, _ := it.(map[string]any)
			switch blk["type"] {
			case "input_text", "output_text", "text":
				txt, _ := blk["text"].(string)
				res.Blocks = append(res.Blocks, resultBlock{Kind: "text", Text: txt})
			case "input_image", "image":
				url, _ := blk["image_url"].(string)
				mime, data := splitDataURL(url)
				res.Blocks = append(res.Blocks, resultBlock{Kind: "image", MIME: mime, Data: data})
			}
		}
	case map[string]any:
		// {"content": [...], "success": true}
		if content, ok := out["content"].([]any); ok {
			return openaiResult(map[string]any{"call_id": res.CallID, "output": content})
		}
	}
	return res
}

func splitDataURL(url string) (string, []byte) {
	rest, ok := strings.CutPrefix(url, "data:")
	if !ok {
		return "", nil
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok {
		return "", nil
	}
	return strings.TrimSuffix(meta, ";base64"), decodeB64(data)
}

func openaiUsage() map[string]any {
	return map[string]any{
		"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
		"input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0},
	}
}

func openaiCreated(w http.ResponseWriter) {
	sse(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_e2e", "object": "response", "status": "in_progress", "model": "gpt-e2e", "output": []any{}}})
}

func openaiCompleted(w http.ResponseWriter, item map[string]any) {
	sse(w, "response.completed", map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "resp_e2e", "object": "response", "status": "completed", "model": "gpt-e2e", "output": []any{item}, "usage": openaiUsage(),
	}})
}

func openaiCall(w http.ResponseWriter, name string, args map[string]any, id string) {
	openaiCreated(w)
	added := map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": name, "arguments": "", "status": "in_progress"}
	sse(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": added})
	done := map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": name, "arguments": mustJSON(args), "status": "completed"}
	sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	openaiCompleted(w, done)
}

func openaiText(w http.ResponseWriter, text string) {
	openaiCreated(w)
	sse(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": "msg_e2e", "role": "assistant", "content": []any{}, "status": "in_progress"}})
	sse(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_e2e", "output_index": 0, "content_index": 0, "delta": text})
	done := map[string]any{"type": "message", "id": "msg_e2e", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text}}}
	sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	openaiCompleted(w, done)
}

// nestedToolNames lists tool names in a (possibly namespaced) tools array.
func nestedToolNames(raw json.RawMessage) []string {
	var tools []struct {
		Name  string          `json:"name"`
		Tools json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(raw, &tools)
	var out []string
	for _, t := range tools {
		if t.Name != "" {
			out = append(out, t.Name)
		}
		if len(t.Tools) > 0 {
			out = append(out, nestedToolNames(t.Tools)...)
		}
	}
	return out
}

func openaiCustomCall(w http.ResponseWriter, name, input, id string) {
	openaiCreated(w)
	added := map[string]any{"type": "custom_tool_call", "id": "ctc_" + id, "call_id": id, "name": name, "input": "", "status": "in_progress"}
	sse(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": added})
	done := map[string]any{"type": "custom_tool_call", "id": "ctc_" + id, "call_id": id, "name": name, "input": input, "status": "completed"}
	sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	openaiCompleted(w, done)
}
