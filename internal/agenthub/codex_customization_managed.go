package agenthub

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"devkit/internal/preferences"
	"github.com/pelletier/go-toml/v2"
)

// applyCodexCustomization merges DevKit-owned choices after portable personal
// settings. Source directories stay linked, and only our own aliases are removed.
func applyCodexCustomization(home string, value preferences.AgentCustomization) ([]string, []string, error) {
	skills := filepath.Join(home, "skills")
	if err := os.MkdirAll(skills, 0o700); err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(skills)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "devkit-import-") && entry.Type()&os.ModeSymlink != 0 {
			if err := os.Remove(filepath.Join(skills, entry.Name())); err != nil {
				return nil, nil, err
			}
		}
	}
	roots := []string{}
	for _, folder := range value.SkillFolders {
		if _, err := skillMetadata(folder); err != nil {
			return nil, nil, invalidCustomization("Cannot read an imported skill. Check its source folder, then reload.")
		}
		if err := os.Symlink(folder, filepath.Join(skills, importedSkillAlias(folder))); err != nil {
			return nil, nil, err
		}
		roots = append(roots, folder)
	}
	var config map[string]any
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return nil, nil, err
	}
	if err = toml.Unmarshal(data, &config); err != nil {
		return nil, nil, err
	}
	if config == nil {
		config = map[string]any{}
	}
	skillConfig, _ := config["skills"].(map[string]any)
	if skillConfig == nil {
		skillConfig = map[string]any{}
	}
	configs, _ := skillConfig["config"].([]any)

	disabledPaths := map[string]bool{}
	addDisabled := func(path string) {
		disabledPaths[path] = true
		disabledPaths[filepath.Dir(path)] = true
		if real, err := filepath.EvalSymlinks(path); err == nil {
			disabledPaths[real] = true
			disabledPaths[filepath.Dir(real)] = true
		}
	}
	for _, path := range value.DisabledSkills {
		addDisabled(path)
	}
	for _, folder := range value.SkillFolders {
		if disabledSkill(value, filepath.Join(folder, "SKILL.md")) {
			addDisabled(filepath.Join(skills, importedSkillAlias(folder), "SKILL.md"))
		}
	}
	paths := make([]string, 0, len(disabledPaths))
	for path := range disabledPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		configs = append(configs, map[string]any{"path": path, "enabled": false})
	}

	if len(configs) > 0 {
		skillConfig["config"] = configs
		config["skills"] = skillConfig
	}
	servers, _ := config["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	argv := []string{}
	if len(configs) > 0 {
		parts := []string{}
		for _, item := range configs {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			path, ok := entry["path"].(string)
			if !ok {
				continue
			}
			enabled, ok := entry["enabled"].(bool)
			if !ok {
				enabled = true
			}
			parts = append(parts, "{path="+tomlString(path)+",enabled="+strconv.FormatBool(enabled)+"}")
		}
		argv = append(argv, "-c", "skills.config=["+strings.Join(parts, ",")+"]")
	}
	for _, server := range value.Servers {
		fields := map[string]any{"enabled": server.Enabled}
		if server.URL != "" {
			fields["url"] = server.URL
		} else {
			fields["command"] = server.Command
			fields["args"] = server.Args
			fields["env_vars"] = server.EnvKeys
		}
		servers[server.Name] = fields
		parts := []string{"enabled=" + strconv.FormatBool(server.Enabled)}
		if server.URL != "" {
			parts = append(parts, "url="+tomlString(server.URL))
		} else {
			parts = append(parts, "command="+tomlString(server.Command), "args="+tomlStrings(server.Args), "env_vars="+tomlStrings(server.EnvKeys))
		}
		argv = append(argv, "-c", "mcp_servers."+server.Name+"={"+strings.Join(parts, ",")+"}")
	}
	for _, name := range value.DisabledMCP {
		argv = append(argv, "-c", "mcp_servers."+name+".enabled=false")
	}
	for _, server := range value.Servers {
		if !server.Enabled {
			argv = append(argv, "-c", "mcp_servers."+server.Name+".enabled=false")
		}
	}
	if len(servers) > 0 {
		config["mcp_servers"] = servers
	}
	encoded, err := toml.Marshal(config)
	if err != nil {
		return nil, nil, err
	}
	err = os.WriteFile(filepath.Join(home, "config.toml"), append([]byte(codexConfigHeader), encoded...), 0o600)
	return roots, argv, err
}
