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

// session/new and session/load both answer with the session's mode set
// (schema: SessionModeState); kept raw, nothing downstream could offer it.
func TestSessionResultsCarryTheModeSet(t *testing.T) {
	modes := `"modes":{"currentModeId":"default","availableModes":[` +
		`{"id":"default","name":"Default","description":"Ask before edits"},` +
		`{"id":"bypassPermissions","name":"Bypass Permissions"}]}`
	var created SessionNewResult
	if err := json.Unmarshal([]byte(`{"sessionId":"s1",`+modes+`}`), &created); err != nil {
		t.Fatal(err)
	}
	var loaded SessionLoadResult
	if err := json.Unmarshal([]byte(`{"sessionId":"s1",`+modes+`}`), &loaded); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]*SessionModeState{"session/new": created.Modes, "session/load": loaded.Modes} {
		if got == nil || got.CurrentModeID != "default" || len(got.AvailableModes) != 2 {
			t.Fatalf("%s modes = %+v", name, got)
		}
		if m := got.AvailableModes[0]; m.ID != "default" || m.Name != "Default" || m.Description != "Ask before edits" {
			t.Fatalf("%s first mode = %+v", name, m)
		}
	}
}
