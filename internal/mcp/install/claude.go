package install

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func installClaude(req InstallRequest) error {
	path, err := ConfigPath(req)
	if err != nil {
		return err
	}
	doc, err := readClaudeDoc(path)
	if err != nil {
		return err
	}
	servers := mcpServersMap(doc)
	servers[ServerName] = serverEntry(req.Command)
	doc["mcpServers"] = servers
	return writeClaudeDoc(path, doc)
}

func uninstallClaude(req InstallRequest) error {
	path, err := ConfigPath(req)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	doc, err := readClaudeDoc(path)
	if err != nil {
		return err
	}
	servers := mcpServersMap(doc)
	delete(servers, ServerName)
	doc["mcpServers"] = servers
	return writeClaudeDoc(path, doc)
}

func readClaudeDoc(path string) (map[string]any, error) {
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
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func writeClaudeDoc(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func mcpServersMap(doc map[string]any) map[string]any {
	raw, ok := doc["mcpServers"]
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
