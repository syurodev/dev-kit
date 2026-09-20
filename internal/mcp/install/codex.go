package install

import (
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"

	"devkit/internal/codexhook"
)

func installCodex(req InstallRequest) error {
	path, err := ConfigPath(req)
	if err != nil {
		return err
	}
	doc, err := readCodexDoc(path)
	if err != nil {
		return err
	}
	servers := mcpServersTOML(doc)
	servers[ServerName] = serverEntry(req.Command)
	doc["mcp_servers"] = servers
	setDevKitHooks(doc)
	return writeCodexDoc(path, doc)
}

func uninstallCodex(req InstallRequest) error {
	path, err := ConfigPath(req)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	doc, err := readCodexDoc(path)
	if err != nil {
		return err
	}
	servers := mcpServersTOML(doc)
	delete(servers, ServerName)
	if len(servers) == 0 {
		delete(doc, "mcp_servers")
	} else {
		doc["mcp_servers"] = servers
	}
	removeDevKitHooks(doc)
	return writeCodexDoc(path, doc)
}

func readCodexDoc(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func writeCodexDoc(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func mcpServersTOML(doc map[string]any) map[string]any {
	raw, ok := doc["mcp_servers"]
	if !ok || raw == nil {
		return map[string]any{}
	}
	switch v := raw.(type) {
	case map[string]any:
		return v
	default:
		return map[string]any{}
	}
}

// setDevKitHooks declares one MCP-tool handler per event codexhook.Registered
// names, pointed at DevKit's own MCP server and tool.
//
// codex-rs's HooksToml flattens its event fields (#[serde(flatten)]), so on
// disk PreToolUse et al. sit as keys directly under [hooks] — sibling to
// "state", not nested under a "hooks.events" table of their own. This writes
// them at that same level; nesting them one level deeper would be a config
// Codex silently never reads.
//
// It is called on every install, not just the first, which is what makes
// re-running Install after codexhook.Registered changes pick up the change:
// removeDevKitOwnedGroup strips whatever DevKit wrote last time before this
// adds the current set back. Any matcher group belonging to something ELSE —
// the user's own hooks, a different MCP server — is left exactly as it was;
// only a group that is unmistakably DevKit's own is ever touched.
func setDevKitHooks(doc map[string]any) {
	hooks := hooksTable(doc)
	for _, event := range codexhook.Registered {
		key := string(event)
		groups := removeDevKitOwnedGroup(asMatcherGroups(hooks[key]))
		groups = append(groups, devKitMatcherGroup(event))
		hooks[key] = groups
	}
	doc["hooks"] = hooks
}

// removeDevKitHooks drops every matcher group DevKit owns, across every key
// under [hooks] that holds an array of them — not only the events
// codexhook.Registered names today, so an uninstall after a future DevKit
// build shrank that list still cleans up what an older DevKit wrote. "state"
// (a table, not an array) and anything else that is not a matcher-group list
// are left untouched by construction: asMatcherGroups returns nothing for
// them. An event left with nothing else is dropped entirely, and "hooks"
// itself is dropped once nothing is left in it — the same shrink-to-nothing
// rule mcp_servers already follows.
func removeDevKitHooks(doc map[string]any) {
	hooks := hooksTable(doc)
	for key, raw := range hooks {
		groups := asMatcherGroups(raw)
		if groups == nil {
			continue // "state", or anything else that is not a group list
		}
		remaining := removeDevKitOwnedGroup(groups)
		if len(remaining) == 0 {
			delete(hooks, key)
			continue
		}
		hooks[key] = remaining
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
}

func hooksTable(doc map[string]any) map[string]any {
	raw, ok := doc["hooks"]
	if !ok || raw == nil {
		return map[string]any{}
	}
	if table, ok := raw.(map[string]any); ok {
		return table
	}
	return map[string]any{}
}

// asMatcherGroups reads a [hooks] value as an array of matcher-group tables,
// or returns nil for anything else — "state" (a table) included. nil rather
// than an empty slice distinguishes "not a group list" from "a group list
// with nothing DevKit owns in it", which removeDevKitHooks relies on to leave
// non-event keys alone.
func asMatcherGroups(raw any) []map[string]any {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	groups := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if group, ok := item.(map[string]any); ok {
			groups = append(groups, group)
		}
	}
	return groups
}

// removeDevKitOwnedGroup drops any matcher group whose handlers are exactly
// DevKit's own single mcp_tool call — its signature, not just its server
// name, so a group the user built by hand that happens to also call the
// devkit server is left alone.
func removeDevKitOwnedGroup(groups []map[string]any) []map[string]any {
	kept := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		if !isDevKitGroup(group) {
			kept = append(kept, group)
		}
	}
	return kept
}

func isDevKitGroup(group map[string]any) bool {
	handlers, ok := group["hooks"].([]any)
	if !ok || len(handlers) != 1 {
		return false
	}
	handler, ok := handlers[0].(map[string]any)
	if !ok {
		return false
	}
	return handler["type"] == "mcp_tool" &&
		handler["server"] == ServerName &&
		handler["tool"] == codexhook.MCPToolName
}

func devKitMatcherGroup(event codexhook.Event) map[string]any {
	group := map[string]any{
		"hooks": []any{map[string]any{
			"type":   "mcp_tool",
			"server": ServerName,
			"tool":   codexhook.MCPToolName,
			"input":  argumentTemplate(event),
		}},
	}
	if codexhook.NeedsMatcher(event) {
		// "*" is the sentinel codex-rs's own matcher tests use for "every
		// tool name" — not valid regex on its own, so it has to be this
		// exact string rather than any other unrestricted-looking pattern.
		group["matcher"] = "*"
	}
	return group
}

func argumentTemplate(event codexhook.Event) map[string]any {
	args := codexhook.Arguments(event)
	input := make(map[string]any, len(args))
	for key, value := range args {
		input[key] = value
	}
	return input
}
