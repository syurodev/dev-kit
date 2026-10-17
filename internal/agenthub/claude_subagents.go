package agenthub

import "encoding/json"

// claudeSubagentsMeta adds the person's subagents to _meta.claudeCode.options.agents
// (the Agent SDK's programmatic subagents; claude-agent-acp spreads the options
// into the SDK's). Only description, prompt, model and effort are written — never
// tools, disallowedTools, mcpServers, permissionMode or skills, so a subagent
// inherits the session's tools and permissions as they are and every call it
// makes passes the same gate as the session's own. A subagent of the same name in
// the person's .claude/agents is unaffected: planSubagents leaves such a profile out.
func claudeSubagentsMeta(meta json.RawMessage, subagents []SubagentSpec) json.RawMessage {
	if len(subagents) == 0 {
		return meta
	}
	var body map[string]any
	if json.Unmarshal(meta, &body) != nil {
		return meta
	}
	claudeCode, _ := body["claudeCode"].(map[string]any)
	options, _ := claudeCode["options"].(map[string]any)
	if options == nil {
		return meta
	}
	agents, _ := options["agents"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
	}
	for _, sub := range subagents {
		def := map[string]any{"description": sub.Description, "prompt": sub.Prompt}
		// "default" is Claude's picker entry for "whatever the account's default
		// is"; as an agent's model that means the main model, i.e. no field.
		if sub.Model != "" && sub.Model != "default" {
			def["model"] = sub.Model
		}
		if sub.Effort != "" {
			def["effort"] = sub.Effort
		}
		agents[sub.Name] = def
	}
	options["agents"] = agents
	encoded, err := json.Marshal(body)
	if err != nil {
		return meta
	}
	return encoded
}
