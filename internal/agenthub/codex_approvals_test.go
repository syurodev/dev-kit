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
