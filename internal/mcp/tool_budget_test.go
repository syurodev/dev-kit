package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// What every tool definition costs an agent: its description and input schema
// are sent in tools/list, and Codex does not preload them — it dumps the
// definitions it wants into context (13 times in one measured session, 2–20 KB
// each). So the size of a definition is paid in the conversation, not once.
//
// The limits sit just above today's sizes. When one fails, shorten the new
// text (say what the tool answers and the one thing that is easy to get wrong;
// the keys of the answer name themselves) before raising it — and say in the
// commit why it had to grow. Never shorten what the person is promised: the
// gate and approval sentences of the pr.*, api.* and agenthub.* tools stay.
const (
	maxDescriptionBytes = 700   // one tool's description
	maxDefinitionBytes  = 2200  // one tool: description + input schema
	maxTotalDescBytes   = 25600 // every description together (25 KB)
	maxTotalDefBytes    = 70000 // every description + input + output schema together
)

func TestToolDefinitionsStayWithinBudget(t *testing.T) {
	var totalDesc, totalDef int
	for _, tool := range listAllTools(t) {
		in, _ := json.Marshal(tool.InputSchema)
		out, _ := json.Marshal(tool.OutputSchema)
		desc := len(tool.Description)
		totalDesc += desc
		totalDef += desc + len(in) + len(out)
		if desc > maxDescriptionBytes {
			t.Errorf("%s: description is %d bytes, limit %d — shorten it", tool.Name, desc, maxDescriptionBytes)
		}
		if def := desc + len(in); def > maxDefinitionBytes {
			t.Errorf("%s: description + input schema is %d bytes, limit %d", tool.Name, def, maxDefinitionBytes)
		}
	}
	t.Logf("%d bytes of descriptions, %d bytes of definitions", totalDesc, totalDef)
	if totalDesc > maxTotalDescBytes {
		t.Errorf("descriptions total %d bytes, limit %d", totalDesc, maxTotalDescBytes)
	}
	if totalDef > maxTotalDefBytes {
		t.Errorf("definitions total %d bytes, limit %d", totalDef, maxTotalDefBytes)
	}
}

// A shorter description may not talk the person out of their say: every tool
// that asks first through a gate still tells the agent that someone will be
// asked, so it does not plan around an approval it will not get.
func TestGateOwnedToolsStillSayTheyAsk(t *testing.T) {
	srv := NewServer(fullDeps())
	gated := map[string]bool{}
	srv.gateOwned.Range(func(k, _ any) bool { gated[k.(string)] = true; return true })
	if len(gated) == 0 {
		t.Fatal("no gate-owned tools found")
	}
	for _, tool := range listAllTools(t) {
		if !gated[tool.Name] {
			continue
		}
		delete(gated, tool.Name)
		d := strings.ToLower(tool.Description)
		says := false
		for _, word := range []string{"user", "person", "gated", "confirmation"} {
			says = says || strings.Contains(d, word)
		}
		if !says {
			t.Errorf("%s asks first but its description does not say so: %q", tool.Name, tool.Description)
		}
	}
	for name := range gated {
		t.Errorf("gate-owned tool %s is missing from the registered MCP surface", name)
	}
	for _, name := range []string{"api.sendAdhoc", "api.saveRequest", "agenthub.createAgentSession", "tasks.board.create"} {
		for _, tool := range listAllTools(t) {
			if tool.Name == name && !strings.Contains(strings.ToLower(tool.Description), "ask") {
				t.Errorf("%s no longer says it asks: %q", name, tool.Description)
			}
		}
	}
}
