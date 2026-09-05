package acp

import (
	"encoding/json"
	"os"
	"testing"
)

func TestUnmarshalInitializeResultFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize_result.json")
	if err != nil {
		t.Fatal(err)
	}
	var res InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.ProtocolVersion != 1 {
		t.Fatalf("protocolVersion=%v", res.ProtocolVersion)
	}
	if res.AgentCapabilities == nil {
		t.Fatal("expected agentCapabilities")
	}
	if res.AgentInfo == nil {
		t.Fatal("expected agentInfo")
	}
	if name, _ := res.AgentInfo["name"].(string); name != "devkit-mock" {
		t.Fatalf("agentInfo.name=%v", res.AgentInfo["name"])
	}
}

func TestUnmarshalSessionUpdateMessageChunkFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/session_update_message_chunk.json")
	if err != nil {
		t.Fatal(err)
	}
	var params SessionUpdateParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if params.SessionID != "sess_abc123" {
		t.Fatalf("sessionId=%q", params.SessionID)
	}
	if len(params.Update) == 0 {
		t.Fatal("expected update blob")
	}

	var update struct {
		SessionUpdate string       `json:"sessionUpdate"`
		Content       ContentBlock `json:"content"`
	}
	if err := json.Unmarshal(params.Update, &update); err != nil {
		t.Fatal(err)
	}
	if update.SessionUpdate != "agent_message_chunk" {
		t.Fatalf("sessionUpdate=%q", update.SessionUpdate)
	}
	if update.Content.Type != "text" {
		t.Fatalf("content.type=%q", update.Content.Type)
	}
	if update.Content.Text != "Hello from ACP" {
		t.Fatalf("content.text=%q", update.Content.Text)
	}
}

func TestMcpServerStdioWire(t *testing.T) {
	raw, err := json.Marshal(McpServerStdio{Name: "devkit", Command: "/opt/devkit-mcp", Args: []string{"--socket", "/tmp/a.sock"}, Env: []EnvVariable{}})
	if err != nil {
		t.Fatal(err)
	}
	// Schema v1 McpServerStdio: required name, command, args, env; no "type" field.
	want := `{"name":"devkit","command":"/opt/devkit-mcp","args":["--socket","/tmp/a.sock"],"env":[]}`
	if string(raw) != want {
		t.Fatalf("wire = %s\nwant %s", raw, want)
	}
}

func TestSessionNewAlwaysSendsMcpServers(t *testing.T) {
	// Schema v1 requires cwd and mcpServers; omitting the empty list made the
	// agent reject session/new with -32602.
	raw, err := json.Marshal(SessionNewRequest{Cwd: "/tmp/proj", McpServers: []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(raw), `{"cwd":"/tmp/proj","mcpServers":[]}`; got != want {
		t.Fatalf("wire = %s\nwant %s", got, want)
	}
}
