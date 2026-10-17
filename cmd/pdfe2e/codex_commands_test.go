package main

// Drives DevKit's Codex session commands (/compact, /review) through the real
// `codex app-server` against a scripted mock model, the same harness as
// subagent_limits_test.go. The measurements in AGENTS.md ("Codex session
// commands") come from the raw-protocol probe described there; this test holds
// the path DevKit actually takes: Service.Invoke → codexRuntime.Prompt → RPC →
// translator → the updates the window receives.
//
//	DEVKIT_SUBAGENT_E2E_CODEX=<the native codex binary>
//	HOME=<a short fake home>
//	go test ./cmd/pdfe2e/ -run CodexCommands -v -timeout 10m

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"devkit/internal/agenthub"
)

type commandsModel struct {
	mu       sync.Mutex
	requests []modelRequest
}

type modelRequest struct {
	Compaction bool     // the request carried compaction_trigger
	Items      []string // type/role of each input item
	Texts      []string // user message texts
	HasSummary bool     // the history held a compaction item
}

func commandsMock(m *commandsModel) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			http.Error(w, "websocket transport is not offered", http.StatusUpgradeRequired)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/models") {
				_, _ = w.Write([]byte(`{"models":[],"data":[],"object":"list"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input []json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(raw, &req)
		seen := modelRequest{}
		for _, rawItem := range req.Input {
			var item struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			_ = json.Unmarshal(rawItem, &item)
			seen.Items = append(seen.Items, item.Type+"/"+item.Role)
			switch item.Type {
			case "compaction_trigger":
				seen.Compaction = true
			case "compaction":
				seen.HasSummary = true
			case "message":
				if item.Role == "user" {
					for _, c := range item.Content {
						seen.Texts = append(seen.Texts, c.Text)
					}
				}
			}
		}
		m.mu.Lock()
		m.requests = append(m.requests, seen)
		m.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case seen.Compaction:
			openaiCreated(w)
			item := map[string]any{"type": "compaction", "encrypted_content": "ENC"}
			sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			openaiCompleted(w, item)
		case containsAny(seen.Texts, "Review the current code changes"):
			findings, _ := json.Marshal(map[string]any{
				"findings":                 []any{map[string]any{"title": "[P1] Missing check", "body": "Line two can be nil.", "confidence_score": 0.8, "priority": 1, "code_location": map[string]any{"absolute_file_path": "/x/a.txt", "line_range": map[string]any{"start": 2, "end": 3}}}},
				"overall_correctness":      "patch is incorrect",
				"overall_explanation":      "One blocking issue.",
				"overall_confidence_score": 0.7,
			})
			openaiText(w, string(findings))
		default:
			openaiText(w, "ok")
		}
	})
}

func containsAny(texts []string, part string) bool {
	for _, t := range texts {
		if strings.Contains(t, part) {
			return true
		}
	}
	return false
}

func TestCodexCommands(t *testing.T) {
	model := &commandsModel{}
	api := newModelServer(t, func(*modelServer) http.Handler { return commandsMock(model) })
	st := newStack(t, limitsAdapter(t, api.URL(), nil))
	sess := st.newSession(agenthub.AgentTypeCodex)

	if res := st.invoke(sess, "hello"); res.Status != "completed" {
		t.Fatalf("hello: %+v", res)
	}
	if res := st.invoke(sess, "/compact"); res.Status != "completed" {
		t.Fatalf("/compact: %+v", res)
	}
	if res := st.invoke(sess, "/review"); res.Status != "completed" {
		t.Fatalf("/review: %+v", res)
	}
	if res := st.invoke(sess, "and after"); res.Status != "completed" {
		t.Fatalf("after: %+v", res)
	}

	model.mu.Lock()
	defer model.mu.Unlock()
	compactions, withSummary := 0, 0
	for i, req := range model.requests {
		t.Logf("request %d: compaction=%v summary=%v items=%v", i, req.Compaction, req.HasSummary, req.Items)
		if req.Compaction {
			compactions++
		}
		if req.HasSummary {
			withSummary++
		}
		for _, text := range req.Texts {
			if text == "/compact" || text == "/review" {
				t.Errorf("request %d sent %q to the model as text", i, text)
			}
		}
	}
	if compactions != 1 {
		t.Errorf("compaction requests = %d, want 1", compactions)
	}
	if withSummary == 0 {
		t.Error("no later request carried the compaction: the thread did not keep it")
	}

	st.emitMu.Lock()
	defer st.emitMu.Unlock()
	var commands, compactCard, reviewCard, findings bool
	for _, raw := range st.emitted {
		u := agenthub.ParseSessionUpdate(raw)
		switch u.Kind {
		case agenthub.UpdateAvailableCommands:
			names := map[string]bool{}
			for _, c := range u.Commands {
				names[c.Name] = true
			}
			commands = commands || (names["compact"] && names["review"])
		case agenthub.UpdateToolCall:
			compactCard = compactCard || (u.ToolCall != nil && u.ToolCall.Name == "compact")
			reviewCard = reviewCard || (u.ToolCall != nil && u.ToolCall.Name == "review")
		}
		findings = findings || strings.Contains(string(raw), "One blocking issue.")
	}
	t.Logf("announced=%v compactCard=%v reviewCard=%v findings=%v", commands, compactCard, reviewCard, findings)
	if !commands || !compactCard || !reviewCard || !findings {
		t.Errorf("announced=%v compactCard=%v reviewCard=%v findings=%v", commands, compactCard, reviewCard, findings)
	}
}
