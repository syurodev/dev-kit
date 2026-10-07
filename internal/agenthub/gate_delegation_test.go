package agenthub

import (
	"devkit/internal/acp"
	"encoding/json"
	"testing"
)

func TestCodexDelegatesOnlyRegisteredGatedCalls(t *testing.T) {
	r := &codexRuntime{tr: newCodexTranslator(), gateOwnedMCP: map[string][]string{"devkit": {"db.query"}}}
	for _, test := range []struct {
		server, tool, kind string
		want               bool
	}{
		{"devkit", "db.query", "mcp_tool_call", true},
		{"external", "db.query", "mcp_tool_call", false},
		{"devkit", "notes.delete", "mcp_tool_call", false},
		{"devkit", "db.query", "unrelated", false},
		{"devkit", "", "mcp_tool_call", false},
	} {
		params, _ := json.Marshal(map[string]any{"serverName": test.server, "_meta": map[string]string{"codex_approval_kind": test.kind, "tool_name": test.tool}})
		if got := r.delegatesMCPGate(params); got != test.want {
			t.Fatalf("%s/%s/%s = %v", test.server, test.tool, test.kind, got)
		}
	}
	r.gateOwnedMCP = nil
	if r.delegatesMCPGate(json.RawMessage(`{"serverName":"devkit","_meta":{"codex_approval_kind":"mcp_tool_call","tool_name":"db.query"}}`)) {
		t.Fatal("delegated without host declaration")
	}
}

func TestClaudeUsesScopedGateDelegationInSDKOptions(t *testing.T) {
	spec := OpenSpec{McpServers: []acp.McpServerStdio{{Name: "devkit"}}, GateOwnedMCP: map[string][]string{"devkit": {"db.query", "ssh.runCommand"}, "not-connected": {"danger"}}}
	var meta struct {
		ClaudeCode struct {
			Options struct {
				AllowedTools []string `json:"allowedTools"`
			} `json:"options"`
		} `json:"claudeCode"`
	}
	if err := json.Unmarshal(sessionNewMeta("", "", delegatedClaudeTools(spec)...), &meta); err != nil {
		t.Fatal(err)
	}
	names := meta.ClaudeCode.Options.AllowedTools
	if len(names) != 2 || names[0] != "mcp__devkit__db_query" || names[1] != "mcp__devkit__ssh_runCommand" {
		t.Fatalf("SDK delegation = %v", names)
	}
}
