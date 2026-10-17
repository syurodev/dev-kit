package main

// End-to-end check that a model profile switched on as a subagent really is a
// subagent of a NEW Claude session and of a NEW Codex session, with the person's
// model, effort and instructions — by running the real runtimes, not reading the
// code. Same harness as the PDF checks: DevKit's Service and sandbox, the real
// claude-agent-acp + Claude Code and the real `codex app-server`; only the model
// API is a local mock that records every request.
//
//	DEVKIT_SUBAGENT_E2E_CLAUDE=<…/claude-acp/0.92.0>   (holds node_modules/@agentclientprotocol/claude-agent-acp)
//	DEVKIT_SUBAGENT_E2E_NODE=<node binary>
//	DEVKIT_SUBAGENT_E2E_CODEX=<the native codex binary>
//	HOME=<a short fake home>     (never the real one: the runtimes read ~/.claude and ~/.codex)
//	go test ./cmd/pdfe2e/ -run Subagent -v -timeout 10m

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/preferences"
	"devkit/internal/sandbox"
)

const (
	rootMarker  = "ROOT-PROMPT-MARKER"
	childMarker = "CHILD-TASK-MARKER"
	roleMarker  = "ROLE-INSTRUCTIONS-MARKER"
)

// seenRequest is one call the runtime made to the model.
type seenRequest struct {
	Kind  string // root | child | side
	Model string
	Raw   string
	Tools []string
}

type recorder struct {
	mu   sync.Mutex
	reqs []seenRequest
}

func (r *recorder) add(s seenRequest) {
	r.mu.Lock()
	r.reqs = append(r.reqs, s)
	r.mu.Unlock()
}

func (r *recorder) all() []seenRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seenRequest(nil), r.reqs...)
}

func (r *recorder) of(kind string) []seenRequest {
	var out []seenRequest
	for _, q := range r.all() {
		if q.Kind == kind {
			out = append(out, q)
		}
	}
	return out
}

func subagentProfiles(vendor, model, effort string) preferences.AgentModelProfiles {
	return preferences.AgentModelProfiles{Profiles: []preferences.AgentModelProfile{
		{ID: "review", Name: "Review", Vendor: vendor, Model: model, Effort: effort, Subagent: true,
			Description: "Use for reading diffs and reviewing code.",
			Prompt:      roleMarker + ": be terse and list findings only."},
		{ID: "quiet", Name: "Quiet", Vendor: vendor, Model: model, Subagent: false,
			Description: "A profile that is not a subagent.", Prompt: "QUIET-MARKER"},
	}}
}

// ---- Claude ----------------------------------------------------------------

func subagentClaudeAdapter(t *testing.T, apiURL string) *agenthub.ClaudeAdapter {
	dir := requireDir(t, "DEVKIT_SUBAGENT_E2E_CLAUDE")
	node := requireDir(t, "DEVKIT_SUBAGENT_E2E_NODE")
	entry := filepath.Join(dir, "node_modules", "@agentclientprotocol", "claude-agent-acp", "dist", "index.js")
	return agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{
		Runner: envRunner{inner: sandbox.DefaultRunner()},
		Runtime: func() (agentruntime.Resolved, error) {
			return agentruntime.Resolved{
				Command:       node,
				Args:          []string{entry},
				ReadOnlyPaths: []string{filepath.Dir(filepath.Dir(node)), dir},
				Env: map[string]string{
					"ANTHROPIC_BASE_URL":                       apiURL,
					"ANTHROPIC_API_KEY":                        "sk-ant-e2e",
					"DISABLE_TELEMETRY":                        "1",
					"DISABLE_AUTOUPDATER":                      "1",
					"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
				},
			}, nil
		},
	})
}

// claudeSubagentMock plays a model that, asked ROOT, calls the subagent tool for
// "review" and then answers; a CHILD (its system prompt holds the role marker)
// answers at once, or first runs childShell through the Bash tool.
func claudeSubagentMock(rec *recorder, childShell string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":100}`))
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string          `json:"model"`
			System   json.RawMessage `json:"system"`
			Messages json.RawMessage `json:"messages"`
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
		body := string(raw)
		system := anthropicSystem(req.System)
		kind := "side"
		switch {
		case strings.Contains(system, roleMarker):
			kind = "child"
		case strings.Contains(body, rootMarker) && len(tools) > 3:
			kind = "root"
		}
		rec.add(seenRequest{Kind: kind, Model: req.Model, Raw: body, Tools: tools})
		hasResult := strings.Contains(string(req.Messages), `"tool_result"`)
		switch kind {
		case "root":
			if hasResult {
				anthropicText(w, req.Stream, "root done")
				return
			}
			tool := pick(tools, "Agent")
			if tool == "" {
				tool = pick(tools, "Task")
			}
			if tool == "" {
				anthropicText(w, req.Stream, "no subagent tool offered; offered: "+strings.Join(tools, ", "))
				return
			}
			anthropicToolUse(w, tool, map[string]any{"description": "review it", "prompt": childMarker + " please review", "subagent_type": "review"}, "toolu_root_1")
		case "child":
			if childShell != "" && !hasResult {
				anthropicToolUse(w, "Bash", map[string]any{"command": childShell, "description": "try a git commit"}, "toolu_child_1")
				return
			}
			anthropicText(w, req.Stream, "child done")
		default:
			anthropicText(w, req.Stream, "ok")
		}
	})
	return mux
}

func TestClaudeSessionGetsASwitchedOnProfileAsASubagent(t *testing.T) {
	rec := &recorder{}
	api := newModelServer(t, func(*modelServer) http.Handler { return claudeSubagentMock(rec, "git commit -m e2e") })
	st := newStack(t, subagentClaudeAdapter(t, api.URL()))
	st.svc.ModelProfiles = func() (preferences.AgentModelProfiles, error) {
		return subagentProfiles("claude", "haiku", "low"), nil
	}

	sess := st.newSession(agenthub.AgentTypeClaude)
	res := st.invoke(sess, rootMarker+" review the diff with your review subagent")
	t.Logf("turn status %q", res.Status)

	for i, q := range rec.all() {
		t.Logf("request %d: %-5s model=%s tools=%d", i, q.Kind, q.Model, len(q.Tools))
	}
	root := rec.of("root")
	if len(root) == 0 {
		t.Fatal("no request from the main agent was seen")
	}
	// The main agent was told about the subagent, with the person's description.
	if !contains(root[0].Raw, "review", "Use for reading diffs and reviewing code.") {
		t.Errorf("the subagent and its description are not in the main agent's request")
	}
	if strings.Contains(root[0].Raw, "QUIET-MARKER") || strings.Contains(root[0].Raw, "A profile that is not a subagent") {
		t.Errorf("a profile that is switched off reached the agent")
	}
	child := rec.of("child")
	if len(child) == 0 {
		t.Fatalf("the subagent never ran; main agent said: %.300s", root[len(root)-1].Raw)
	}
	if !strings.Contains(child[0].Model, "haiku") {
		t.Errorf("the subagent ran on model %q, want the profile's haiku", child[0].Model)
	}
	if !contains(child[0].Raw, roleMarker, childMarker) {
		t.Errorf("the subagent's request lacks the person's instructions or the task")
	}
	if !strings.Contains(child[0].Raw, `"effort":"low"`) {
		t.Errorf("effort low is not in the subagent's request: %.600s", child[0].Raw)
	}
	// The root kept its own model: the profile did not leak into it.
	if strings.Contains(root[0].Model, "haiku") && !strings.Contains(os.Getenv("DEVKIT_SUBAGENT_E2E_ROOT_IS_HAIKU"), "1") {
		t.Errorf("the main agent ran on %q", root[0].Model)
	}
	// The subagent's Bash call went through the same PreToolUse hook as the
	// session's: DevKit refuses `git commit` in a shell and names the tool. The
	// refusal is what the subagent was sent back.
	if len(child) < 2 {
		t.Fatalf("the subagent made %d requests; it should have sent the shell command and then read the answer", len(child))
	}
	if answer := toolResultOf(t, child[len(child)-1].Raw, "toolu_child_1"); !contains(answer, "DevKit", "mcp__devkit__git_commit") {
		t.Errorf("the subagent's shell `git commit` was not stopped by DevKit's hook; its tool result was: %q", answer)
	}
}

// toolResultOf is the text of the tool_result for a tool call id in a request.
func toolResultOf(t *testing.T, raw, id string) string {
	t.Helper()
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	for _, m := range req.Messages {
		var blocks []map[string]any
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b["type"] == "tool_result" && b["tool_use_id"] == id {
				return anthropicResult(b).text()
			}
		}
	}
	return ""
}

func TestClaudeSessionWithTheSwitchOffHasNoSubagent(t *testing.T) {
	rec := &recorder{}
	api := newModelServer(t, func(*modelServer) http.Handler { return claudeSubagentMock(rec, "") })
	st := newStack(t, subagentClaudeAdapter(t, api.URL()))
	st.svc.ModelProfiles = func() (preferences.AgentModelProfiles, error) {
		p := subagentProfiles("claude", "haiku", "low")
		p.Profiles[0].Subagent = false
		return p, nil
	}
	sess := st.newSession(agenthub.AgentTypeClaude)
	st.invoke(sess, rootMarker+" hello")
	root := rec.of("root")
	if len(root) == 0 {
		t.Fatal("no main request")
	}
	if strings.Contains(root[0].Raw, "Use for reading diffs and reviewing code.") || len(rec.of("child")) != 0 {
		t.Errorf("a switched-off profile became a subagent")
	}
}

// ---- Codex -----------------------------------------------------------------

func subagentCodexAdapter(t *testing.T, apiURL string) *agenthub.CodexAdapter {
	bin := requireDir(t, "DEVKIT_SUBAGENT_E2E_CODEX")
	return agenthub.NewCodexAdapter(agenthub.CodexOptions{
		Command: bin,
		Home:    shortDir(t, "codexhome"),
		APIKey:  func(context.Context) (string, error) { return "sk-e2e", nil },
		Runner:  envRunner{inner: sandbox.DefaultRunner(), args: []string{"-c", `openai_base_url="` + apiURL + `/v1"`}},
		Version: "e2e",
	})
}

// codexSubagentMock: ROOT calls spawn_agent with agent_type review, waits for the
// child's request to come in, then answers. CHILD is recognised by the role's
// developer instructions.
func codexSubagentMock(rec *recorder) http.Handler {
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
		body := string(raw)
		var req struct {
			Model string            `json:"model"`
			Input []json.RawMessage `json:"input"`
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		namespace := ""
		var names []string
		var walk func(tools []json.RawMessage, ns string)
		walk = func(tools []json.RawMessage, ns string) {
			for _, tr := range tools {
				var t struct {
					Name  string            `json:"name"`
					Tools []json.RawMessage `json:"tools"`
				}
				_ = json.Unmarshal(tr, &t)
				names = append(names, t.Name)
				if t.Name == "spawn_agent" {
					namespace = ns
				}
				walk(t.Tools, t.Name)
			}
		}
		walk(req.Tools, "")
		// Codex 0.163's app-server delivers its tools as an input item.
		for _, rawItem := range req.Input {
			var item struct {
				Type  string            `json:"type"`
				Tools []json.RawMessage `json:"tools"`
			}
			if json.Unmarshal(rawItem, &item) == nil && item.Type == "additional_tools" {
				walk(item.Tools, "")
			}
		}
		kind := "side"
		switch {
		case strings.Contains(body, roleMarker):
			kind = "child"
		case strings.Contains(body, rootMarker):
			kind = "root"
		}
		rec.add(seenRequest{Kind: kind, Model: req.Model, Raw: body, Tools: names})
		if dump := os.Getenv("DEVKIT_SUBAGENT_E2E_DUMP"); dump != "" {
			_ = os.WriteFile(dump+"."+kind, raw, 0o600)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		hasOutput := strings.Contains(body, `"function_call_output"`)
		switch {
		case kind == "root" && !hasOutput && contains(strings.Join(names, ","), "spawn_agent"):
			call := map[string]any{"type": "function_call", "id": "fc_root", "call_id": "call_root", "name": "spawn_agent", "arguments": mustJSON(map[string]any{"agent_type": "review", "task_name": "review_it", "fork_turns": "none", "message": childMarker + " please review"}), "status": "completed"}
			if namespace != "" {
				call["namespace"] = namespace
			}
			openaiCreated(w)
			sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": call})
			openaiCompleted(w, call)
		case kind == "root" && hasOutput:
			// Give the child time to make its request before the turn ends.
			time.Sleep(6 * time.Second)
			openaiText(w, "root done")
		default:
			openaiText(w, "child done")
		}
	})
}

func TestCodexSessionGetsASwitchedOnProfileAsASubagent(t *testing.T) {
	rec := &recorder{}
	api := newModelServer(t, func(*modelServer) http.Handler { return codexSubagentMock(rec) })
	st := newStack(t, subagentCodexAdapter(t, api.URL()))
	st.svc.ModelProfiles = func() (preferences.AgentModelProfiles, error) {
		return subagentProfiles("codex", "gpt-e2e-review", "low"), nil
	}

	sess := st.newSession(agenthub.AgentTypeCodex)
	res := st.invoke(sess, rootMarker+" review the diff with your review subagent")
	t.Logf("turn status %q", res.Status)
	for i, q := range rec.all() {
		t.Logf("request %d: %-5s model=%s tools=%v", i, q.Kind, q.Model, q.Tools)
	}

	root := rec.of("root")
	if len(root) == 0 {
		t.Fatal("no request from the main agent was seen")
	}
	if !contains(root[0].Raw, "Use for reading diffs and reviewing code.") {
		t.Errorf("the role's description is not in spawn_agent's tool description")
	}
	if strings.Contains(root[0].Raw, "A profile that is not a subagent") {
		t.Errorf("a profile that is switched off reached the agent")
	}
	child := rec.of("child")
	if len(child) == 0 {
		t.Fatal("the subagent never ran")
	}
	if child[0].Model != "gpt-e2e-review" {
		t.Errorf("the subagent ran on %q, want gpt-e2e-review", child[0].Model)
	}
	if !contains(child[0].Raw, roleMarker, `"effort":"low"`) {
		t.Errorf("the subagent's request lacks the person's instructions or the effort: %.600s", child[0].Raw)
	}
	if root[0].Model == "gpt-e2e-review" {
		t.Errorf("the main agent ran on the profile's model")
	}
}
