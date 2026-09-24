package agenthub

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"devkit/internal/codexapp"
)

type codexCaptureLine struct {
	Dir     string           `json:"dir"`
	Session string           `json:"session"`
	Msg     codexapp.Message `json:"msg"`
}

func loadCodexCapture(t *testing.T, session string) []codexapp.Message {
	t.Helper()
	f, err := os.Open("testdata/codex_session_capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []codexapp.Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		var l codexCaptureLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		if l.Session == session && l.Dir == "in" && l.Msg.IsNotification() {
			out = append(out, l.Msg)
		}
	}
	return out
}

// replay runs a captured session through a fresh translator and parses every
// update the way the store and UI will.
func replay(t *testing.T, session string) ([]SessionUpdate, []codexUpdate) {
	t.Helper()
	tr := newCodexTranslator()
	var parsed []SessionUpdate
	var raw []codexUpdate
	for _, m := range loadCodexCapture(t, session) {
		for _, u := range tr.Translate(m.Method, m.Params) {
			su := ParseSessionUpdate(u.Update)
			if su.Kind == UpdateUnknown {
				t.Fatalf("translator emitted an update ParseSessionUpdate does not know: %s", u.Update)
			}
			parsed = append(parsed, su)
			raw = append(raw, u)
		}
	}
	return parsed, raw
}

func TestCodexReplayMainSession(t *testing.T) {
	updates, raw := replay(t, "main")

	// Every word the agent said reaches the transcript exactly once: the
	// streamed chunks equal the completed messages, in order.
	var streamed strings.Builder
	for _, u := range updates {
		if u.Kind == UpdateAgentMessageChunk {
			streamed.WriteString(u.Content.Text)
		}
	}
	var completed strings.Builder
	for _, m := range loadCodexCapture(t, "main") {
		if m.Method != codexapp.MethodItemCompleted {
			continue
		}
		var n codexapp.ItemNotification
		_ = json.Unmarshal(m.Params, &n)
		var msg codexapp.AgentMessageItem
		var h codexapp.ItemHeader
		_ = json.Unmarshal(n.Item, &h)
		if h.Type == codexapp.ItemAgentMessage && json.Unmarshal(n.Item, &msg) == nil {
			completed.WriteString(msg.Text)
		}
	}
	if streamed.String() == "" || streamed.String() != completed.String() {
		t.Fatalf("streamed text %q\nwant the completed messages %q", streamed.String(), completed.String())
	}

	calls := map[string]*ToolCall{}
	var order []string
	for _, u := range updates {
		if u.ToolCall == nil {
			continue
		}
		c := u.ToolCall
		if prev, ok := calls[c.ID]; ok {
			if c.Status != "" {
				prev.Status = c.Status
			}
			if len(c.Content) > 0 {
				prev.Content = c.Content
			}
			continue
		}
		copied := *c
		calls[c.ID] = &copied
		order = append(order, c.ID)
	}
	var commands, edits int
	for _, id := range order {
		c := calls[id]
		if c.Status != "completed" {
			t.Fatalf("tool call %s ended %q, want completed", id, c.Status)
		}
		switch c.Kind {
		case "execute":
			commands++
			if strings.HasPrefix(c.Title, "/usr/bin/zsh") {
				t.Fatalf("command title %q still carries the shell wrapper", c.Title)
			}
		case "edit":
			edits++
			if c.Title != "Edit 2 files" || len(c.Content) != 2 {
				t.Fatalf("file change = %+v", c)
			}
			notes, plan := c.Content[0], c.Content[1]
			if notes.OldText == nil || *notes.OldText != "line two\n" || notes.NewText != "line two\nline three\n" {
				t.Fatalf("notes.txt diff = old %v new %q", notes.OldText, notes.NewText)
			}
			if plan.OldText != nil || !strings.HasPrefix(plan.NewText, "- [ ] ") {
				t.Fatalf("plan.md (new file) diff = old %v new %q", plan.OldText, plan.NewText)
			}
		}
	}
	if commands != 2 || edits != 1 {
		t.Fatalf("tool calls: %d commands, %d edits; want 2 and 1", commands, edits)
	}

	var lastUsage *Usage
	for _, u := range updates {
		if u.Usage != nil {
			lastUsage = u.Usage
		}
	}
	if lastUsage == nil || lastUsage.Used != 12957 || lastUsage.Size != 258400 {
		t.Fatalf("last usage = %+v, want used 12957 of 258400", lastUsage)
	}

	// FR-8: every completed item's raw Codex JSON rides along as meta.
	var withMeta int
	for _, u := range raw {
		if len(u.Meta) > 0 {
			withMeta++
		}
	}
	if withMeta == 0 {
		t.Fatal("no update carried the raw Codex item")
	}
}

func TestCodexReplayInterruptClosesRunningCommand(t *testing.T) {
	updates, _ := replay(t, "interrupt")
	var id, final string
	for _, u := range updates {
		if u.ToolCall == nil || u.ToolCall.Kind != "execute" && id == "" {
			continue
		}
		if id == "" {
			id = u.ToolCall.ID
		}
		if u.ToolCall.ID == id && u.ToolCall.Status != "" {
			final = u.ToolCall.Status
		}
	}
	if id == "" || final != "failed" {
		t.Fatalf("interrupted command %q ended %q, want failed (Codex never completes it)", id, final)
	}
}

func translateOne(t *testing.T, tr *codexTranslator, method string, params any) []SessionUpdate {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var out []SessionUpdate
	for _, u := range tr.Translate(method, raw) {
		out = append(out, ParseSessionUpdate(u.Update))
	}
	return out
}

func item(v any) map[string]any {
	return map[string]any{"threadId": "th", "turnId": "tu", "item": v}
}

func TestCodexCompletedMessageWithoutDeltasIsSentWhole(t *testing.T) {
	tr := newCodexTranslator()
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "agentMessage", "id": "m1", "text": "hello"}))
	if len(got) != 1 || got[0].Kind != UpdateAgentMessageChunk || got[0].Content.Text != "hello" {
		t.Fatalf("got %+v", got)
	}
	translateOne(t, tr, codexapp.MethodAgentMessageDelta, map[string]any{"itemId": "m2", "delta": "hi"})
	if again := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "agentMessage", "id": "m2", "text": "hi"})); len(again) != 0 {
		t.Fatalf("a streamed message was sent twice: %+v", again)
	}
}

func TestCodexReasoningSummary(t *testing.T) {
	tr := newCodexTranslator()
	got := translateOne(t, tr, codexapp.MethodReasoningSummaryDelta, map[string]any{"itemId": "r1", "delta": "thinking"})
	if len(got) != 1 || got[0].Kind != UpdateAgentThoughtChunk || got[0].Content.Text != "thinking" {
		t.Fatalf("got %+v", got)
	}
	got = translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "reasoning", "id": "r2", "summary": []string{"a", "b"}}))
	if len(got) != 1 || got[0].Content.Text != "a\n\nb" {
		t.Fatalf("unstreamed summary = %+v", got)
	}
}

func TestCodexDeclinedCommandFails(t *testing.T) {
	tr := newCodexTranslator()
	cmd := map[string]any{"type": "commandExecution", "id": "c1", "command": "rm -rf x", "cwd": "/p", "status": "inProgress"}
	translateOne(t, tr, codexapp.MethodItemStarted, item(cmd))
	cmd["status"] = "declined"
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(cmd))
	last := got[len(got)-1].ToolCall
	if last.Status != "failed" || len(last.Content) != 1 || last.Content[0].Content.Text != "Declined." {
		t.Fatalf("declined command = %+v", last)
	}
}

func TestCodexCommandOutputKeepsTheTail(t *testing.T) {
	tr := newCodexTranslator()
	out := strings.Repeat("x", codexOutputLimit) + "THE END"
	exit := 1
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{
		"type": "commandExecution", "id": "c1", "command": "make", "cwd": "/p", "status": "completed",
		"aggregatedOutput": out, "exitCode": exit,
	}))
	if len(got) != 2 || got[0].ToolCall.Status != "in_progress" {
		t.Fatalf("a command completed without a start must be shown first: %+v", got)
	}
	last := got[1].ToolCall
	text := last.Content[0].Content.Text
	if last.Status != "failed" || !strings.HasSuffix(text, "THE END") || len(text) > codexOutputLimit+8 {
		t.Fatalf("status %q, output %d bytes ending %q", last.Status, len(text), text[len(text)-10:])
	}
}

func TestCodexUnknownItemIsShownNotDropped(t *testing.T) {
	tr := newCodexTranslator()
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "imageGeneration", "id": "g1"}))
	if len(got) != 1 || got[0].ToolCall == nil || got[0].ToolCall.Title != "imageGeneration" || got[0].ToolCall.Kind != "other" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodexPlanNameAndIgnored(t *testing.T) {
	tr := newCodexTranslator()
	got := translateOne(t, tr, codexapp.MethodTurnPlanUpdated, map[string]any{"plan": []map[string]string{
		{"step": "read", "status": "completed"}, {"step": "edit", "status": "inProgress"}, {"step": "test", "status": "pending"},
	}})
	if len(got) != 1 || len(got[0].Plan) != 3 || got[0].Plan[1].Status != "in_progress" || got[0].Plan[2].Status != "pending" {
		t.Fatalf("plan = %+v", got)
	}
	got = translateOne(t, tr, codexapp.MethodThreadNameUpdated, map[string]any{"threadId": "th", "threadName": "Fix login"})
	if len(got) != 1 || got[0].Title != "Fix login" {
		t.Fatalf("name = %+v", got)
	}
	for _, m := range []string{"hook/started", "thread/status/changed", "remoteControl/status/changed", "turn/diff/updated"} {
		if codexTranslates(m) {
			t.Fatalf("%s must not be translated", m)
		}
	}
}

func TestCodexHunkTexts(t *testing.T) {
	before, after := codexHunkTexts("--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n a\n-b\n+B\n@@ -9 +9 @@\n-x\n+y\n\\ No newline at end of file\n")
	if before != "a\nb\n…\nx\n" || after != "a\nB\n…\ny\n" {
		t.Fatalf("before %q after %q", before, after)
	}
}

func TestCodexFileChangeKinds(t *testing.T) {
	move := "/p/new.go"
	cases := []struct {
		changes []codexapp.FileUpdateChange
		kind    string
		title   string
	}{
		{[]codexapp.FileUpdateChange{{Path: "/p/a.go", Kind: codexapp.PatchChangeKind{Type: "delete"}}}, "delete", "Delete a.go"},
		{[]codexapp.FileUpdateChange{{Path: "/p/old.go", Kind: codexapp.PatchChangeKind{Type: "update", MovePath: &move}}}, "move", "Move old.go"},
		{[]codexapp.FileUpdateChange{{Path: "/p/n.go", Kind: codexapp.PatchChangeKind{Type: "add"}}}, "edit", "Create n.go"},
	}
	for _, c := range cases {
		if k, title := codexFileChangeKind(c.changes), codexFileChangeTitle(c.changes); k != c.kind || title != c.title {
			t.Fatalf("%+v → %q %q, want %q %q", c.changes, k, title, c.kind, c.title)
		}
	}
}
