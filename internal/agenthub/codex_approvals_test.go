package agenthub

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/codexapp"
)

func capturedRequest(t *testing.T, method string) json.RawMessage {
	t.Helper()
	for _, l := range loadCodexCaptureAll(t) {
		if l.Msg.Method == method && l.Msg.IsRequest() {
			return l.Msg.Params
		}
	}
	t.Fatalf("no %s in the capture", method)
	return nil
}

func loadCodexCaptureAll(t *testing.T) []codexCaptureLine {
	t.Helper()
	raw, err := os.ReadFile("testdata/codex_session_capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var out []codexCaptureLine
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var l codexCaptureLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

type permissionParams struct {
	acp.RequestPermissionParams
	options  []codexPermissionOption
	toolCall codexToolCallWire
}

func decodePermission(t *testing.T, raw json.RawMessage) permissionParams {
	t.Helper()
	var p permissionParams
	if err := json.Unmarshal(raw, &p.RequestPermissionParams); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p.Options, &p.options); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p.ToolCall, &p.toolCall); err != nil {
		t.Fatal(err)
	}
	return p
}

func selected(optionID string) json.RawMessage {
	return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"` + optionID + `"}}`)
}

func decision(t *testing.T, body json.RawMessage) string {
	t.Helper()
	var r codexapp.ApprovalResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r.Decision
}

func TestCodexCommandApprovalFollowsAvailableDecisions(t *testing.T) {
	// The captured request (untrusted) offers accept, an execpolicy
	// amendment and cancel only.
	tr := newCodexTranslator()
	a, ok := tr.approvalFor("sess-1", codexapp.MethodCommandApproval, capturedRequest(t, codexapp.MethodCommandApproval))
	if !ok {
		t.Fatal("command approval not recognised")
	}
	p := decodePermission(t, a.Params)
	if p.SessionID != "sess-1" || p.toolCall.Kind != "execute" || p.toolCall.ToolCallID == "" {
		t.Fatalf("params = %+v", p)
	}
	if strings.HasPrefix(p.toolCall.Title, "/usr/bin/zsh") {
		t.Fatalf("title %q carries the shell wrapper", p.toolCall.Title)
	}
	var ids []string
	for _, o := range p.options {
		ids = append(ids, o.OptionID)
		if o.Kind != o.OptionID {
			t.Fatalf("option %+v: kind must equal id for the card to find it", o)
		}
	}
	if strings.Join(ids, ",") != "allow_once,reject_once" {
		t.Fatalf("options = %v, want allow_once,reject_once (no acceptForSession offered by Codex)", ids)
	}
	if strings.Contains(string(a.Params), "Execpolicy") || strings.Contains(string(a.Params), "execpolicy") {
		t.Fatalf("an execpolicy amendment leaked into the request: %s", a.Params)
	}
	for option, want := range map[string]string{
		codexOptionAllow: codexapp.DecisionAccept,
		codexOptionDeny:  codexapp.DecisionCancel, // no decline available
	} {
		if got := decision(t, a.Response(selected(option))); got != want {
			t.Fatalf("%s → %q, want %q", option, got, want)
		}
	}
	if got := decision(t, a.Response(json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`))); got != codexapp.DecisionCancel {
		t.Fatalf("cancelled → %q", got)
	}
	if got := decision(t, a.Response(json.RawMessage(`not json`))); got != codexapp.DecisionCancel {
		t.Fatalf("unreadable → %q, want fail closed", got)
	}
	if got := decision(t, a.Response(selected("allow_always"))); got != codexapp.DecisionCancel {
		t.Fatalf("an option that was never offered → %q, want fail closed", got)
	}
}

func TestCodexCommandApprovalWithEveryDecision(t *testing.T) {
	tr := newCodexTranslator()
	params, _ := json.Marshal(map[string]any{
		"threadId": "th", "turnId": "tu", "itemId": "c9", "command": "go test ./...", "cwd": "/p",
		"availableDecisions": []any{"accept", "acceptForSession", "decline", "cancel"},
	})
	a, ok := tr.approvalFor("s", codexapp.MethodCommandApproval, params)
	if !ok {
		t.Fatal("not recognised")
	}
	if len(decodePermission(t, a.Params).options) != 3 {
		t.Fatalf("params = %s", a.Params)
	}
	for option, want := range map[string]string{
		codexOptionAllow:        codexapp.DecisionAccept,
		codexOptionAllowSession: codexapp.DecisionAcceptForSession,
		codexOptionDeny:         codexapp.DecisionDecline,
	} {
		if got := decision(t, a.Response(selected(option))); got != want {
			t.Fatalf("%s → %q, want %q", option, got, want)
		}
	}
}

func TestCodexFileChangeApprovalShowsTheDiff(t *testing.T) {
	tr := newCodexTranslator()
	// The file change starts (and reaches the translator) before Codex asks
	// about it, as in the capture.
	for _, m := range loadCodexCapture(t, "main") {
		if m.Method == codexapp.MethodItemStarted && strings.Contains(string(m.Params), `"fileChange"`) {
			tr.Translate(m.Method, m.Params)
		}
	}
	a, ok := tr.approvalFor("s", codexapp.MethodFileChangeApproval, capturedRequest(t, codexapp.MethodFileChangeApproval))
	if !ok {
		t.Fatal("not recognised")
	}
	p := decodePermission(t, a.Params)
	if p.toolCall.Kind != "edit" || len(p.toolCall.Content) != 2 || p.toolCall.Content[0].Type != "diff" {
		t.Fatalf("tool call = %+v", p.toolCall)
	}
	if len(p.options) != 3 || decision(t, a.Response(selected(codexOptionDeny))) != codexapp.DecisionDecline {
		t.Fatalf("file change options = %+v", p.options)
	}
}

func TestCodexPermissionsRequestGrantsNothing(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"threadId": "th", "turnId": "tu", "itemId": "p1", "cwd": "/p",
		"permissions": map[string]any{"network": map[string]any{"enabled": true}},
	})
	resp, notice := codexPermissionsRequest(params)
	if string(resp) != `{"permissions":{},"scope":"turn"}` {
		t.Fatalf("response = %s", resp)
	}
	if len(notice) != 1 {
		t.Fatalf("notice = %+v", notice)
	}
	su := ParseSessionUpdate(notice[0].Update)
	if su.ToolCall == nil || su.ToolCall.Status != "failed" || !strings.Contains(su.ToolCall.Content[0].Content.Text, "Settings → Agents → Sandbox") {
		t.Fatalf("notice = %+v", su)
	}
}

func TestCodexMalformedCommandApprovalFailsClosedWithoutACard(t *testing.T) {
	tr := newCodexTranslator()
	for _, params := range []json.RawMessage{
		json.RawMessage(`not json`),
		json.RawMessage(`{"threadId":"th","turnId":"tu"}`), // no itemId
	} {
		a, ok := tr.approvalFor("s", codexapp.MethodCommandApproval, params)
		if !ok {
			t.Fatalf("%s: a command approval must still be recognised as an approval method, not treated as unsupported", params)
		}
		if !a.Malformed || a.Params != nil {
			t.Fatalf("%s: malformed approval = %+v", params, a)
		}
		if got := decision(t, a.Response(nil)); got != codexapp.DecisionCancel {
			t.Fatalf("%s: decision = %q, want cancel (fail closed)", params, got)
		}
	}
}

func TestCodexMalformedFileChangeApprovalFailsClosedWithoutACard(t *testing.T) {
	tr := newCodexTranslator()
	for _, params := range []json.RawMessage{
		json.RawMessage(`not json`),
		json.RawMessage(`{"threadId":"th"}`), // no itemId
	} {
		a, ok := tr.approvalFor("s", codexapp.MethodFileChangeApproval, params)
		if !ok || !a.Malformed || a.Params != nil {
			t.Fatalf("%s: malformed file-change approval = %+v, ok=%v", params, a, ok)
		}
		if got := decision(t, a.Response(nil)); got != codexapp.DecisionCancel {
			t.Fatalf("%s: decision = %q, want cancel", params, got)
		}
	}
}

func TestCodexOtherRequestsAreRefused(t *testing.T) {
	tr := newCodexTranslator()
	for _, m := range []string{"item/tool/requestUserInput", "mcpServer/elicitation/request", "account/chatgptAuthTokens/refresh", "attestation/generate", "applyPatchApproval", "execCommandApproval"} {
		if _, ok := tr.approvalFor("s", m, json.RawMessage(`{}`)); ok {
			t.Fatalf("%s treated as an approval", m)
		}
		if e := codexUnsupportedRequest(m); e.Code != -32601 || !strings.Contains(e.Message, m) {
			t.Fatalf("%s → %+v", m, e)
		}
	}
}

// mcpApprovalRequest is an mcpServer/elicitation/request shaped the way
// codex-rs 0.147.0 builds a tool-call approval (mcp_tool_call.rs,
// build_mcp_tool_approval_elicitation_meta).
func mcpApprovalRequest(server string, persist any) json.RawMessage {
	meta := map[string]any{
		"codex_approval_kind": "mcp_tool_call",
		"tool_title":          "Run a query",
		"tool_params":         map[string]any{"sql": "select 1"},
	}
	if persist != nil {
		meta["persist"] = persist
	}
	return marshalUpdate(map[string]any{
		"threadId": "th-1", "turnId": "tu-1", "serverName": server, "mode": "form",
		"_meta": meta, "message": "Allow devkit to run db.query?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	})
}

func elicitAnswer(t *testing.T, body json.RawMessage) codexapp.McpElicitationResponse {
	t.Helper()
	var r codexapp.McpElicitationResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// Codex asks before an MCP tool runs under Ask and Auto; the person answers
// on a card, and the answer goes back in MCP's elicitation shape. "Always"
// is never offered: Codex would write it into config.toml as a rule.
func TestCodexMcpToolApprovalBecomesACard(t *testing.T) {
	tr := newCodexTranslator()
	a, ok := tr.mcpToolApprovalFor("sess-1", "7", mcpApprovalRequest("devkit", []string{"session", "always"}))
	if !ok {
		t.Fatal("a tool-call approval must become a card")
	}
	p := decodePermission(t, a.Params)
	var ids []string
	for _, o := range p.options {
		ids = append(ids, o.OptionID)
	}
	if strings.Join(ids, ",") != codexOptionAllow+","+codexOptionAllowSession+","+codexOptionDeny {
		t.Fatalf("options = %v, want allow, allow for session, deny", ids)
	}
	if p.toolCall.ToolCallID != "mcp-approval-7" || p.toolCall.Title != "Run a query" {
		t.Fatalf("toolCall = %+v", p.toolCall)
	}

	for option, want := range map[string]struct{ action, meta string }{
		codexOptionAllow:        {"accept", "null"},
		codexOptionAllowSession: {"accept", `{"persist":"session"}`},
		codexOptionDeny:         {"decline", "null"},
		"always":                {"cancel", "null"},
	} {
		got := elicitAnswer(t, a.Response(selected(option)))
		if got.Action != want.action || string(got.Meta) != want.meta {
			t.Errorf("%s → action %q meta %s, want %q %s", option, got.Action, got.Meta, want.action, want.meta)
		}
	}
	if got := elicitAnswer(t, a.Response(nil)); got.Action != "cancel" {
		t.Fatalf("no answer → %q, want cancel", got.Action)
	}
	if body := string(a.Response(nil)); !strings.Contains(body, `"content":null`) || !strings.Contains(body, `"_meta":null`) {
		t.Fatalf("content and _meta are required keys: %s", body)
	}
}

// Without "session" among the persist choices there is no session button.
func TestCodexMcpToolApprovalOffersOnlyWhatCodexRemembers(t *testing.T) {
	a, ok := newCodexTranslator().mcpToolApprovalFor("sess-1", "r", mcpApprovalRequest("devkit", nil))
	if !ok {
		t.Fatal("expected a card")
	}
	for _, o := range decodePermission(t, a.Params).options {
		if o.OptionID == codexOptionAllowSession {
			t.Fatal("allow for session offered without persist=session")
		}
	}
}

// The approval names the server, not the call; with one call open on that
// server the card is that call's.
func TestCodexMcpToolApprovalAttachesToTheOpenCall(t *testing.T) {
	tr := newCodexTranslator()
	tr.Translate(codexapp.MethodItemStarted, json.RawMessage(`{"threadId":"th-1","turnId":"tu-1","item":{"type":"mcpToolCall","id":"call-9","server":"devkit","tool":"db.query","status":"inProgress","arguments":{"sql":"select 1"}}}`))
	a, _ := tr.mcpToolApprovalFor("sess-1", "1", mcpApprovalRequest("devkit", "session"))
	if id := decodePermission(t, a.Params).toolCall.ToolCallID; id != "call-9" {
		t.Fatalf("toolCallId = %q, want the open call", id)
	}
	tr.Translate(codexapp.MethodItemStarted, json.RawMessage(`{"threadId":"th-1","turnId":"tu-1","item":{"type":"mcpToolCall","id":"call-10","server":"devkit","tool":"notes.write","status":"inProgress","arguments":{}}}`))
	a, _ = tr.mcpToolApprovalFor("sess-1", "2", mcpApprovalRequest("devkit", "session"))
	if id := decodePermission(t, a.Params).toolCall.ToolCallID; id != "mcp-approval-2" {
		t.Fatalf("two open calls: toolCallId = %q, want a card of its own", id)
	}
}

// An MCP server's own question is not Codex asking for approval.
func TestCodexMcpElicitationThatIsNotAnApprovalIsNotACard(t *testing.T) {
	body := marshalUpdate(map[string]any{"threadId": "th-1", "serverName": "devkit", "mode": "form", "_meta": nil, "message": "Your name?"})
	if _, ok := newCodexTranslator().mcpToolApprovalFor("sess-1", "1", body); ok {
		t.Fatal("a plain elicitation must be refused, not shown as an approval")
	}
}
