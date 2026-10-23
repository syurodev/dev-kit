package agenthub

import (
	"strings"
	"testing"
)

// Agents may read nothing about vendor accounts and change nothing: no MCP tool
// mentions them (the credentials module's accounts.* tools are a different thing),
// and the prompt does not teach one.
func TestNoToolTouchesAgentAccounts(t *testing.T) {
	for name := range mcpToolNames(t) {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "agentaccount") || strings.Contains(lower, "accountpolicy") || strings.Contains(lower, "accountusage") {
			t.Errorf("tool %s touches the vendor accounts; only the window may", name)
		}
	}
	prompt := strings.ToLower(devkitPrompt(claudeNaming, ""))
	for _, phrase := range []string{"account policy", "switch account", "vendor account"} {
		if strings.Contains(prompt, phrase) {
			t.Errorf("the prompt tells agents about vendor accounts (%q)", phrase)
		}
	}
}
