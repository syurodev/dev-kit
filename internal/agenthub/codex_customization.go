package agenthub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/pelletier/go-toml/v2"
)

// inheritCodexCustomization imports portable customization, never the user's
// auth or session databases. Called only with ShareHome enabled. The managed
// home remains the authority for DevKit's sign-in and identity-bound MCP.
func inheritCodexCustomization(home, userHome string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(userHome, "config.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read user config: %w", err)
	}
	if err == nil {
		var config map[string]any
		if toml.Unmarshal(data, &config) != nil {
			// Parser errors can quote a line containing a credential.
			return nil, errors.New("user config.toml is not valid TOML")
		}
		portable := make(map[string]any)
		for _, key := range []string{"mcp_servers", "skills", "plugins", "features", "projects", "web_search", "agents", "personality"} {
			if value, ok := config[key]; ok {
				portable[key] = value
			}
		}
		if servers, ok := portable["mcp_servers"].(map[string]any); ok {
			delete(servers, "devkit") // only the session endpoint may own this name
		}
		encoded, err := toml.Marshal(portable)
		if err != nil {
			return nil, errors.New("cannot encode user customization")
		}
		if err := os.WriteFile(filepath.Join(home, "config.toml"), append([]byte(codexConfigHeader), encoded...), 0o600); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md"} {
		data, err := os.ReadFile(filepath.Join(userHome, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(home, name), data, 0o600); err != nil {
			return nil, err
		}
	}

	var roots []string
	for _, name := range []string{"skills", "plugins"} {
		source := filepath.Join(userHome, name)
		info, err := os.Stat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("user %s must be a directory", name)
		}
		roots = append(roots, source)
		if name == "plugins" {
			if err := linkCodexCustomization(source, filepath.Join(home, name)); err != nil {
				return nil, err
			}
			continue
		}
		// Keep the managed skills directory writable for bundled .system skills
		// and session-installed skills. User skills are read-only links into the
		// shared home, so helpers and relative references retain their layout.
		dest := filepath.Join(home, name)
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Name() == ".system" {
				continue // the pinned runtime installs its own system skills
			}
			if err := linkCodexCustomization(filepath.Join(source, entry.Name()), filepath.Join(dest, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	return roots, nil
}

// Existing managed installs win. Never overwrite a real file/directory or a
// link someone installed themselves, and never write into the user's source.
func linkCodexCustomization(source, dest string) error {
	if _, err := os.Lstat(dest); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Symlink(source, dest); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func codexPrivatePaths(userHome string, roots []string) []string {
	// Explicit paths remain hidden even if created after this session starts.
	out := []string{filepath.Join(userHome, "auth.json"), filepath.Join(userHome, "sessions"), filepath.Join(userHome, "archived_sessions")}
	entries, err := os.ReadDir(userHome)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return []string{userHome}
		}
		return out
	}
	for _, entry := range entries {
		path := filepath.Join(userHome, entry.Name())
		if !slices.Contains(roots, path) {
			out = append(out, path)
		}
	}
	return out
}
