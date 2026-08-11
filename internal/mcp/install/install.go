package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Agent string
type Scope string

const (
	AgentClaudeCode Agent = "claude-code"
	AgentCodex      Agent = "codex"
	ScopeGlobal     Scope = "global"
	ScopeProject    Scope = "project"
	ServerName            = "devkit"
)

type InstallRequest struct {
	Agent       Agent
	Scope       Scope
	ProjectPath string // required when ScopeProject
	Command     string // absolute path to proxy binary
}

var (
	ErrUnknownAgent        = errors.New("unknown MCP agent")
	ErrUnknownScope        = errors.New("unknown MCP install scope")
	ErrProjectPathRequired = errors.New("project path is required for project scope")
	ErrProjectPathMissing  = errors.New("project path does not exist")
	ErrCommandRequired     = errors.New("command path is required")
	ErrCommandNotAbsolute  = errors.New("command path must be absolute")
	ErrProxyNotFound       = errors.New("devkit-mcp not found; build or install the proxy first")
	ErrEphemeralCommand    = errors.New("devkit-mcp path is ephemeral (go run / temp); build bin/devkit-mcp (e.g. task mcp:install) or pass --command /absolute/path")
)

func ListAgents() []Agent {
	return []Agent{AgentClaudeCode, AgentCodex}
}

// ResolveCommand finds an absolute path to the devkit-mcp proxy binary.
// Prefer PATH (LookPath), then bin/devkit-mcp under cwd, then next to a non-temp executable.
func ResolveCommand() (string, error) {
	if p, err := exec.LookPath("devkit-mcp"); err == nil {
		if abs, err := absCommandPath(p); err != nil {
			return "", err
		} else if abs != "" {
			return abs, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if abs, err := resolveBuiltBinary(filepath.Join(cwd, "bin", "devkit-mcp")); err != nil {
			return "", err
		} else if abs != "" {
			return abs, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", ErrProxyNotFound
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe, _ = os.Executable()
	}
	if isEphemeralPath(exe) {
		return "", ErrProxyNotFound
	}
	if abs, err := resolveBuiltBinary(filepath.Join(filepath.Dir(exe), "bin", "devkit-mcp")); err != nil {
		return "", err
	} else if abs != "" {
		return abs, nil
	}
	candidate := filepath.Join(filepath.Dir(exe), "devkit-mcp")
	if abs, err := resolveBuiltBinary(candidate); err != nil {
		return "", err
	} else if abs != "" {
		return abs, nil
	}
	return "", ErrProxyNotFound
}

func absCommandPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := rejectEphemeralCommandPath(abs); err != nil {
		return "", nil
	}
	return abs, nil
}

func resolveBuiltBinary(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", nil
	}
	return absCommandPath(path)
}

func isEphemeralPath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// Only reject go-run / go-test temp binaries — not every path under TempDir
	// (tests and some installs legitimately live there).
	return strings.Contains(abs, "go-build")
}

func rejectEphemeralCommandPath(abs string) error {
	if isEphemeralPath(abs) {
		return ErrEphemeralCommand
	}
	return nil
}

// ConfigPath returns the agent config file path for the request (without writing).
func ConfigPath(req InstallRequest) (string, error) {
	if err := validateAgentScope(req); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch req.Agent {
	case AgentClaudeCode:
		if req.Scope == ScopeGlobal {
			return filepath.Join(home, ".claude.json"), nil
		}
		return filepath.Join(req.ProjectPath, ".mcp.json"), nil
	case AgentCodex:
		if req.Scope == ScopeGlobal {
			return filepath.Join(home, ".codex", "config.toml"), nil
		}
		return filepath.Join(req.ProjectPath, ".codex", "config.toml"), nil
	default:
		return "", ErrUnknownAgent
	}
}

func Install(req InstallRequest) error {
	if err := validateInstall(req); err != nil {
		return err
	}
	switch req.Agent {
	case AgentClaudeCode:
		return installClaude(req)
	case AgentCodex:
		return installCodex(req)
	default:
		return ErrUnknownAgent
	}
}

func Uninstall(req InstallRequest) error {
	if err := validateAgentScope(req); err != nil {
		return err
	}
	if req.Scope == ScopeProject {
		if err := requireProjectDir(req.ProjectPath); err != nil {
			return err
		}
	}
	switch req.Agent {
	case AgentClaudeCode:
		return uninstallClaude(req)
	case AgentCodex:
		return uninstallCodex(req)
	default:
		return ErrUnknownAgent
	}
}

func validateInstall(req InstallRequest) error {
	if err := validateAgentScope(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Command) == "" {
		return ErrCommandRequired
	}
	if !filepath.IsAbs(req.Command) {
		return ErrCommandNotAbsolute
	}
	if err := rejectEphemeralCommandPath(req.Command); err != nil {
		return err
	}
	if req.Scope == ScopeProject {
		if err := requireProjectDir(req.ProjectPath); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentScope(req InstallRequest) error {
	switch req.Agent {
	case AgentClaudeCode, AgentCodex:
	default:
		return fmt.Errorf("%w: %s", ErrUnknownAgent, req.Agent)
	}
	switch req.Scope {
	case ScopeGlobal, ScopeProject:
	default:
		return fmt.Errorf("%w: %s", ErrUnknownScope, req.Scope)
	}
	if req.Scope == ScopeProject && strings.TrimSpace(req.ProjectPath) == "" {
		return ErrProjectPathRequired
	}
	return nil
}

func requireProjectDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrProjectPathRequired
	}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrProjectPathMissing
		}
		return err
	}
	if !st.IsDir() {
		return ErrProjectPathMissing
	}
	return nil
}

func serverEntry(command string) map[string]any {
	return map[string]any{
		"command": command,
		"args":    []any{},
	}
}
