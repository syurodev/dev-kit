package agenthub

import (
	"encoding/json"
	"strings"
	"testing"
)

// The window reads a session's granted folders from the row (a folder granted
// with workspace.requestWrite mid-turn reaches the UI that way), so the bound
// JSON carries them; MCP callers get the row through forAgent, which blanks them.
func TestAgentSessionJSONCarriesTheGrantedFolders(t *testing.T) {
	raw, err := json.Marshal(AgentSession{ID: "s1", ExtraReadWritePaths: []string{"/work/api"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ExtraReadWritePaths":["/work/api"]`) {
		t.Fatalf("the granted folders are missing from the session JSON: %s", raw)
	}
	raw, err = json.Marshal(AgentSession{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ExtraReadWritePaths") {
		t.Fatalf("a session with no grants should not carry the field: %s", raw)
	}
}
