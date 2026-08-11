package install

import (
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
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
