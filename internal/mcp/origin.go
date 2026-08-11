package mcp

// OriginMCP is the audit/capability metadata origin for MCP tool calls.
const OriginMCP = "mcp"

// meta builds Caller metadata with origin=mcp and the given action.
func meta(action string, extra map[string]any) map[string]any {
	m := map[string]any{"origin": OriginMCP, "action": action}
	for k, v := range extra {
		m[k] = v
	}
	return m
}
