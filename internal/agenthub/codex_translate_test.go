package agenthub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
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

// TestCodexTranslatorConcurrentTranslateAndApprovalFor is the race
// test: the runtime adapter calls approvalFor from Handler.Request's own
// goroutine (one per server request, conn.go) while notifications keep
// arriving on the read loop's Translate calls, and closeOpen can be called
// from anywhere a turn ends abnormally. None of the three may race on the
// translator's own state. Run with -race; there is nothing to assert beyond
// "it didn't".
func TestCodexTranslatorConcurrentTranslateAndApprovalFor(t *testing.T) {
	tr := newCodexTranslator()
	const n = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("c%d", i)
			start, _ := json.Marshal(item(map[string]any{
				"type": "commandExecution", "id": id, "command": "echo hi", "cwd": "/p", "status": "inProgress",
			}))
			tr.Translate(codexapp.MethodItemStarted, start)
			done, _ := json.Marshal(item(map[string]any{
				"type": "commandExecution", "id": id, "command": "echo hi", "cwd": "/p", "status": "completed", "exitCode": 0,
			}))
			tr.Translate(codexapp.MethodItemCompleted, done)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("c%d", i)
			params, _ := json.Marshal(map[string]any{
				"threadId": "th", "turnId": "tu", "itemId": id, "command": "echo hi", "cwd": "/p",
				"availableDecisions": []any{"accept", "decline", "cancel"},
			})
			tr.approvalFor("sess", codexapp.MethodCommandApproval, params)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			tr.closeOpen("racing")
		}
	}()
	wg.Wait()
}

func TestCodexThreadFilterDropsForeignNotifications(t *testing.T) {
	tr := newCodexTranslator()
	tr.setThread("th-main")

	translateOne(t, tr, codexapp.MethodItemStarted, map[string]any{
		"threadId": "th-main", "turnId": "tu",
		"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "echo hi", "cwd": "/p", "status": "inProgress"},
	})
	if _, ok := tr.openItem("c1"); !ok {
		t.Fatal("setup: item did not open")
	}

	// A sub-agent thread's turn/completed (e.g. from collabAgentToolCall,
	// spec §5.4) must not close an item opened on the home thread.
	if got := translateOne(t, tr, codexapp.MethodTurnCompleted, map[string]any{
		"threadId": "th-sub", "turn": map[string]any{"id": "tu2", "status": "interrupted"},
	}); len(got) != 0 {
		t.Fatalf("foreign-thread turn/completed produced updates: %+v", got)
	}
	if _, ok := tr.openItem("c1"); !ok {
		t.Fatal("foreign-thread turn/completed closed an item on another thread")
	}

	// A sub-agent thread's own message deltas must not leak into this
	// transcript either.
	if got := translateOne(t, tr, codexapp.MethodAgentMessageDelta, map[string]any{
		"threadId": "th-sub", "itemId": "m1", "delta": "leaked",
	}); len(got) != 0 {
		t.Fatalf("foreign-thread delta was emitted: %+v", got)
	}

	// The home thread's own turn/completed still closes it.
	got := translateOne(t, tr, codexapp.MethodTurnCompleted, map[string]any{
		"threadId": "th-main", "turn": map[string]any{"id": "tu", "status": "interrupted"},
	})
	if len(got) != 1 || got[0].ToolCall == nil || got[0].ToolCall.Status != "failed" {
		t.Fatalf("home-thread turn/completed = %+v", got)
	}
}

func TestCodexCloseOpenClosesEveryOpenCall(t *testing.T) {
	tr := newCodexTranslator()
	translateOne(t, tr, codexapp.MethodItemStarted, item(map[string]any{"type": "commandExecution", "id": "c1", "command": "sleep 5", "cwd": "/p", "status": "inProgress"}))
	translateOne(t, tr, codexapp.MethodItemStarted, item(map[string]any{"type": "commandExecution", "id": "c2", "command": "sleep 6", "cwd": "/p", "status": "inProgress"}))

	var got []SessionUpdate
	for _, u := range tr.closeOpen("Process exited.") {
		got = append(got, ParseSessionUpdate(u.Update))
	}
	if len(got) != 2 || got[0].ToolCall.ID != "c1" || got[1].ToolCall.ID != "c2" {
		t.Fatalf("close order/ids = %+v", got)
	}
	for _, u := range got {
		if u.ToolCall.Status != "failed" || len(u.ToolCall.Content) != 1 || u.ToolCall.Content[0].Content.Text != "Process exited." {
			t.Fatalf("closed call = %+v", u.ToolCall)
		}
	}
	if _, ok := tr.openItem("c1"); ok {
		t.Fatal("closeOpen must forget the items it closed")
	}
	if again := tr.closeOpen("again"); len(again) != 0 {
		t.Fatalf("closing with nothing open produced updates: %+v", again)
	}
}

func TestCodexLateCompletedAfterCloseOpenOnlyUpdates(t *testing.T) {
	tr := newCodexTranslator()
	translateOne(t, tr, codexapp.MethodItemStarted, item(map[string]any{"type": "commandExecution", "id": "c1", "command": "sleep 5", "cwd": "/p", "status": "inProgress"}))
	tr.closeOpen("Process exited.")

	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{
		"type": "commandExecution", "id": "c1", "command": "sleep 5", "cwd": "/p", "status": "completed", "exitCode": 0,
	}))
	if len(got) != 1 {
		t.Fatalf("a late item/completed after closeOpen re-opened a card: %+v", got)
	}
	if got[0].ToolCall == nil || got[0].ToolCall.Status != "completed" {
		t.Fatalf("late completed = %+v", got[0])
	}
}

func TestCodexClosedByCloseIsOnlyTheMostRecentClose(t *testing.T) {
	tr := newCodexTranslator()
	translateOne(t, tr, codexapp.MethodItemStarted, item(map[string]any{"type": "commandExecution", "id": "c1", "command": "a", "cwd": "/p", "status": "inProgress"}))
	tr.closeOpen("first close")
	translateOne(t, tr, codexapp.MethodItemStarted, item(map[string]any{"type": "commandExecution", "id": "c2", "command": "b", "cwd": "/p", "status": "inProgress"}))
	tr.closeOpen("second close")

	// c1 was closed by the FIRST closeOpen, which the second one superseded:
	// a late completed for it goes through the normal "never saw a start"
	// path again, proving the remembered set doesn't grow forever.
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{
		"type": "commandExecution", "id": "c1", "command": "a", "cwd": "/p", "status": "completed", "exitCode": 0,
	}))
	if len(got) != 2 {
		t.Fatalf("a superseded close's id should no longer suppress reconstruction: got %d updates: %+v", len(got), got)
	}
}

func TestCodexReasoningSummaryIndexSeparatesParts(t *testing.T) {
	tr := newCodexTranslator()
	got := translateOne(t, tr, codexapp.MethodReasoningSummaryDelta, map[string]any{"itemId": "r1", "delta": "first", "summaryIndex": 0})
	if len(got) != 1 || got[0].Content.Text != "first" {
		t.Fatalf("first delta = %+v", got)
	}
	got = translateOne(t, tr, codexapp.MethodReasoningSummaryDelta, map[string]any{"itemId": "r1", "delta": " part", "summaryIndex": 0})
	if len(got) != 1 || got[0].Content.Text != " part" {
		t.Fatalf("same-index delta = %+v, want no separator", got)
	}
	got = translateOne(t, tr, codexapp.MethodReasoningSummaryDelta, map[string]any{"itemId": "r1", "delta": "second", "summaryIndex": 1})
	if len(got) != 1 || got[0].Content.Text != "\n\nsecond" {
		t.Fatalf("index-change delta = %+v, want a blank line before it", got)
	}
}

func TestCodexUnknownItemRawOutputIsCappedButMetaKeepsTheWhole(t *testing.T) {
	tr := newCodexTranslator()
	big := strings.Repeat("A", codexOutputLimit+100)
	raw, err := json.Marshal(item(map[string]any{"type": "imageGeneration", "id": "g1", "image": big}))
	if err != nil {
		t.Fatal(err)
	}
	updates := tr.Translate(codexapp.MethodItemCompleted, raw)
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(updates))
	}
	su := ParseSessionUpdate(updates[0].Update)
	if su.ToolCall == nil {
		t.Fatalf("no tool call: %+v", su)
	}
	var stub struct {
		Truncated bool   `json:"truncated"`
		Bytes     int    `json:"bytes"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(su.ToolCall.RawOutput, &stub); err != nil || !stub.Truncated || stub.Type != "imageGeneration" || stub.Bytes < codexOutputLimit {
		t.Fatalf("rawOutput = %s", su.ToolCall.RawOutput)
	}
	if len(su.ToolCall.RawOutput) > 200 {
		t.Fatalf("rawOutput was not actually capped: %d bytes", len(su.ToolCall.RawOutput))
	}
	if len(updates[0].Meta) < codexOutputLimit {
		t.Fatalf("meta was capped too, but FR-8 keeps the whole item there: %d bytes", len(updates[0].Meta))
	}
}

func TestCodexFailedMcpCallKeepsTheReasonFromItsResult(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
		want string
	}{
		{"reason in the result", map[string]any{
			"type": "mcpToolCall", "id": "m1", "server": "devkit", "tool": "git.fetch", "status": "failed",
			"result": json.RawMessage(`{"content":[{"type":"text","text":"permission_denied: Permission denied."}],"isError":true}`),
		}, "permission_denied: Permission denied."},
		{"error field wins", map[string]any{
			"type": "mcpToolCall", "id": "m2", "server": "devkit", "tool": "git.fetch", "status": "failed",
			"error":  map[string]any{"message": "transport closed"},
			"result": json.RawMessage(`{"content":[{"type":"text","text":"ignored"}]}`),
		}, "transport closed"},
		{"nothing to say", map[string]any{
			"type": "mcpToolCall", "id": "m3", "server": "devkit", "tool": "git.fetch", "status": "failed",
		}, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := translateOne(t, newCodexTranslator(), codexapp.MethodItemCompleted, item(c.item))
			last := got[len(got)-1].ToolCall
			var out struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(last.RawOutput, &out); err != nil || out.Error != c.want {
				t.Fatalf("rawOutput = %s, want error %q", last.RawOutput, c.want)
			}
		})
	}
}

func TestCodexMcpResultRawOutputIsCapped(t *testing.T) {
	tr := newCodexTranslator()
	big := strings.Repeat("x", codexOutputLimit+50)
	result, err := json.Marshal(map[string]string{"data": big})
	if err != nil {
		t.Fatal(err)
	}
	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{
		"type": "mcpToolCall", "id": "m1", "server": "s", "tool": "t", "status": "completed", "result": json.RawMessage(result),
	}))
	last := got[len(got)-1].ToolCall
	var stub struct {
		Truncated bool `json:"truncated"`
		Bytes     int  `json:"bytes"`
	}
	if err := json.Unmarshal(last.RawOutput, &stub); err != nil || !stub.Truncated || stub.Bytes == 0 {
		t.Fatalf("rawOutput = %s", last.RawOutput)
	}
	if len(last.RawOutput) > 100 {
		t.Fatalf("rawOutput was not actually capped: %d bytes", len(last.RawOutput))
	}
}

// TestCodexTailSplitsMultiByteCharacterCleanly pins codexTail's rune-boundary
// walk-back (used for the 64 KB command-output tail) against a cut point that
// falls in the middle of a multi-byte UTF-8 character, rather than only ever
// exercising it on ASCII.
func TestCodexTailSplitsMultiByteCharacterCleanly(t *testing.T) {
	prefix := strings.Repeat("a", 8)
	s := prefix + "字" + "TAILS" // "字" is 3 bytes (E5 AD 97); limit 7 cuts into its middle byte.
	got := codexTail(s, 7)
	if got != "…\nTAILS" {
		t.Fatalf("tail with cut mid-rune = %q, want %q", got, "…\nTAILS")
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
