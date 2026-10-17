package agenthub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// codexSubagentsDir holds the role files DevKit writes for Codex, inside the
// vendor's own home. The jail may read it and not write it: an agent that could
// rewrite a role would be rewriting its own subagents' instructions.
const codexSubagentsDir = "devkit-agents"

// codexRoleRetention is how long a role file nobody asked for in this spawn is
// kept. Files are named by content, so a session that is still running keeps the
// file it was started with even after the person edits the profile.
const codexRoleRetention = 24 * time.Hour

// codexRoleFile is Codex's role file (codex_config AgentRoleToml, read from the
// config_file of `agents.<name>`): measured on 0.163.0 — developer_instructions
// is required, model and model_reasoning_effort override the child's. The file
// can also hold sandbox_mode and any other config key, which is why DevKit
// writes exactly these three and nothing else: a role must not move permissions.
type codexRoleFile struct {
	Model                 string `toml:"model,omitempty"`
	ModelReasoningEffort  string `toml:"model_reasoning_effort,omitempty"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// writeCodexSubagents writes one role file per subagent under home and returns
// the `-c agents.<name>.…` arguments that register them for this process only
// (config.toml is rewritten on every spawn and shared by every session, so a
// role there would leak into sessions that did not get it), plus the directory
// the jail must not write. A role the user already defines under the same name is
// not this function's business: planSubagents leaves such a profile out.
func writeCodexSubagents(home string, subagents []SubagentSpec) (args []string, denyWrite []string, err error) {
	if len(subagents) == 0 {
		return nil, nil, nil
	}
	dir := filepath.Join(home, codexSubagentsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("subagent roles: %w", err)
	}
	keep := map[string]bool{}
	for _, sub := range subagents {
		if !tomlBareKey.MatchString(sub.Name) {
			continue
		}
		body, err := toml.Marshal(codexRoleFile{Model: sub.Model, ModelReasoningEffort: sub.Effort, DeveloperInstructions: sub.Prompt})
		if err != nil {
			return nil, nil, fmt.Errorf("subagent roles: %w", err)
		}
		sum := sha256.Sum256(append([]byte(sub.Name+"\x00"), body...))
		file := sub.Name + "-" + hex.EncodeToString(sum[:5]) + ".toml"
		path := filepath.Join(dir, file)
		if _, statErr := os.Stat(path); statErr != nil {
			tmp, err := os.CreateTemp(dir, ".role-*")
			if err != nil {
				return nil, nil, fmt.Errorf("subagent roles: %w", err)
			}
			_, werr := tmp.Write(body)
			cerr := tmp.Close()
			if werr != nil || cerr != nil {
				_ = os.Remove(tmp.Name())
				return nil, nil, fmt.Errorf("subagent roles: write %s", file)
			}
			if err := os.Chmod(tmp.Name(), 0o600); err != nil {
				_ = os.Remove(tmp.Name())
				return nil, nil, fmt.Errorf("subagent roles: %w", err)
			}
			if err := os.Rename(tmp.Name(), path); err != nil {
				_ = os.Remove(tmp.Name())
				return nil, nil, fmt.Errorf("subagent roles: %w", err)
			}
		}
		keep[file] = true
		key := "agents." + sub.Name
		args = append(args, "-c", key+".description="+tomlString(sub.Description), "-c", key+".config_file="+tomlString(path))
	}
	pruneCodexRoles(dir, keep)
	return args, []string{dir}, nil
}

func pruneCodexRoles(dir string, keep map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if keep[entry.Name()] || entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > codexRoleRetention {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
