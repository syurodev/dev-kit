package agenthub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"devkit/internal/acp"
	"devkit/internal/preferences"
	"gopkg.in/yaml.v3"
)

// CustomizationError carries only host-authored guidance, never parser input,
// command arguments, environment values or paths from a third-party error.
type CustomizationError struct{ Message string }

func (e *CustomizationError) Error() string     { return e.Message }
func (e *CustomizationError) Unwrap() error     { return ErrInvalidConfigOption }
func invalidCustomization(message string) error { return &CustomizationError{Message: message} }

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func skillMetadata(folder string) (name string, err error) {
	data, err := readEnvironmentFile(filepath.Join(folder, "SKILL.md"), 64*1024)
	if err != nil {
		return "", err
	}
	body, start := strings.CutPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n")
	header, _, end := strings.Cut(body, "\n---")
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if !start || !end || yaml.Unmarshal([]byte(header), &meta) != nil || !skillNamePattern.MatchString(meta.Name) || strings.TrimSpace(meta.Description) == "" {
		return "", fmt.Errorf("SKILL.md needs a valid name and description")
	}
	return meta.Name, nil
}

// ValidateCustomization accepts local skill directories and stdio/HTTP MCP
// definitions. It never executes a command, connects, or edits a personal file.
func ValidateCustomization(vendor string, value preferences.AgentCustomization) (preferences.AgentCustomization, error) {
	invalid := func(message string) (preferences.AgentCustomization, error) {
		return value, invalidCustomization(message)
	}
	if vendor != string(AgentTypeClaude) && vendor != string(AgentTypeCodex) {
		return value, ErrVendorUnknown
	}
	data, _ := json.Marshal(value)
	if len(data) > 64*1024 || len(value.SkillFolders) > 64 || len(value.Servers) > 64 || len(value.DisabledSkills) > 300 || len(value.DisabledMCP) > 100 {
		return invalid("Agent configuration is too large.")
	}
	names := map[string]bool{}
	for i, folder := range value.SkillFolders {
		if !filepath.IsAbs(folder) {
			return invalid("Choose an absolute skill directory.")
		}
		folder, err := filepath.EvalSymlinks(folder)
		if err != nil {
			return invalid("Cannot read a selected skill directory.")
		}
		name, err := skillMetadata(folder)
		if err != nil {
			return invalid("Each skill directory needs SKILL.md with a name and description.")
		}
		if names[name] {
			return invalid("Imported skills must have different names.")
		}
		names[name] = true
		value.SkillFolders[i] = folder
	}
	for i, path := range value.DisabledSkills {
		if !filepath.IsAbs(path) || filepath.Base(path) != "SKILL.md" {
			return invalid("Invalid disabled skill path.")
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			value.DisabledSkills[i] = real
		}
	}

	names = map[string]bool{}
	for _, name := range value.DisabledMCP {
		if !tomlBareKey.MatchString(name) || name == "devkit" {
			return invalid("The DevKit server cannot be disabled; other server names must use letters, numbers, underscores or hyphens.")
		}
	}
	for i := range value.Servers {
		server := &value.Servers[i]
		server.Name = strings.TrimSpace(server.Name)
		server.Command = strings.TrimSpace(server.Command)
		server.URL = strings.TrimSpace(server.URL)
		if !tomlBareKey.MatchString(server.Name) || server.Name == "devkit" || names[server.Name] {
			return invalid("Choose a unique MCP name using letters, numbers, underscores or hyphens; devkit is reserved.")
		}
		names[server.Name] = true
		if (server.Command == "") == (server.URL == "") {
			return invalid("An MCP server needs either a command or an HTTP URL.")
		}
		if server.URL != "" {
			u, err := url.Parse(server.URL)
			if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || !slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()))) {
				return invalid("Use HTTPS, or HTTP on localhost, without credentials in the URL.")
			}
			if len(server.Args) > 0 || len(server.EnvKeys) > 0 {
				return invalid("HTTP servers do not accept command arguments or environment keys.")
			}
		}
		for _, key := range server.EnvKeys {
			if !envKeyPattern.MatchString(key) {
				return invalid("Environment entries must be variable names, not values.")
			}
		}
	}
	return value, nil
}

func disabledSkill(value preferences.AgentCustomization, path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	for _, disabled := range value.DisabledSkills {
		candidate, err := filepath.EvalSymlinks(disabled)
		if err != nil {
			candidate = disabled
		}
		if real == candidate {
			return true
		}
	}
	return false
}

// Claude imports local skills as a session-private plugin. Relative helpers
// stay beside SKILL.md; closing the runtime removes only our temporary links.
func claudeCustomization(value preferences.AgentCustomization) (plugin string, cleanup func(), err error) {
	cleanup = func() {}
	if len(value.SkillFolders) == 0 {
		return "", cleanup, nil
	}
	plugin, err = os.MkdirTemp("", "devkit-skills-")
	if err != nil {
		return "", cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(plugin) }
	if err = os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o700); err == nil {
		err = os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"), []byte(`{"name":"devkit-local-skills","version":"1.0.0"}`), 0o600)
	}
	if err == nil {
		err = os.MkdirAll(filepath.Join(plugin, "skills"), 0o700)
	}
	for _, folder := range value.SkillFolders {
		if err != nil {
			break
		}
		if disabledSkill(value, filepath.Join(folder, "SKILL.md")) {
			continue
		}
		var name string
		name, err = skillMetadata(folder)
		if err == nil {
			err = os.Symlink(folder, filepath.Join(plugin, "skills", name))
		}
	}
	if err != nil {
		cleanup()
		return "", func() {}, invalidCustomization("Cannot load imported skills. Check their source folders, then reload.")
	}
	return plugin, cleanup, nil
}

func claudeCustomizationMeta(meta json.RawMessage, value preferences.AgentCustomization, plugin string) json.RawMessage {
	var body map[string]any
	_ = json.Unmarshal(meta, &body)
	options := body["claudeCode"].(map[string]any)["options"].(map[string]any)
	if plugin != "" {
		options["plugins"] = []any{map[string]any{"type": "local", "path": plugin}}
	}
	denied := []string{}
	for _, path := range value.DisabledSkills {
		name, err := skillMetadata(filepath.Dir(path))
		if err == nil {
			denied = append(denied, "Skill("+name+")", "Skill(devkit-local-skills:"+name+")")
		}
	}
	for _, name := range value.DisabledMCP {
		denied = append(denied, "mcp__"+name+"__*")
	}
	for _, server := range value.Servers {
		if !server.Enabled {
			denied = append(denied, "mcp__"+server.Name+"__*")
		}
	}
	if len(denied) > 0 {
		options["disallowedTools"] = denied
	}
	encoded, _ := json.Marshal(body)
	return encoded
}

func customizationMCP(value preferences.AgentCustomization, env map[string]string) []any {
	servers := []any{}
	for _, server := range value.Servers {
		if !server.Enabled || slices.Contains(value.DisabledMCP, server.Name) {
			continue
		}
		if server.URL != "" {
			servers = append(servers, map[string]any{"name": server.Name, "type": "http", "url": server.URL, "headers": []any{}})
			continue
		}
		variables := []acp.EnvVariable{}
		for _, key := range server.EnvKeys {
			if v, exists := env[key]; exists {
				variables = append(variables, acp.EnvVariable{Name: key, Value: v})
			}
		}
		args := server.Args
		if args == nil {
			args = []string{}
		}
		servers = append(servers, acp.McpServerStdio{Name: server.Name, Command: server.Command, Args: args, Env: variables})
	}
	return servers
}

func importedSkillAlias(folder string) string {
	hash := sha256.Sum256([]byte(folder))
	return fmt.Sprintf("devkit-import-%x", hash[:12])
}

func customizationEnvironment(ctx context.Context, report *EnvironmentReport, value preferences.AgentCustomization) {
	for _, folder := range value.SkillFolders {
		name, err := skillMetadata(folder)
		entry := EnvironmentEntry{Name: name, Kind: "skills", Status: "configured", Source: filepath.Join(folder, "SKILL.md"), Detail: "Imported in DevKit. Reload the session after updating the source."}
		if err != nil {
			entry.Name, entry.Status, entry.Detail = filepath.Base(folder), "unavailable", "Imported skill could not be read."
		}
		addEnvironmentEntry(report, entry)
	}
	for _, server := range value.Servers {
		addEnvironmentEntry(report, EnvironmentEntry{Name: server.Name, Kind: "mcp", Status: "configured", Source: "DevKit", Detail: "Configured in DevKit; connectivity and authentication have not been tested."})
	}
	for i := range report.Entries {
		entry := &report.Entries[i]
		if (entry.Kind == "skills" && disabledSkill(value, entry.Source)) || (entry.Kind == "mcp" && slices.Contains(value.DisabledMCP, entry.Name)) {
			entry.Status, entry.Detail = "disabled", "Disabled in DevKit. Changes apply after reloading the session."
			if report.Agent == string(AgentTypeClaude) {
				entry.Detail = "Tool invocation is disabled in DevKit. Inherited MCP servers may still connect."
			}
		}
		for _, server := range value.Servers {
			if entry.Kind == "mcp" && entry.Name == server.Name && !server.Enabled {
				entry.Status, entry.Detail = "disabled", "Disabled in DevKit."
			}
		}
	}
}
