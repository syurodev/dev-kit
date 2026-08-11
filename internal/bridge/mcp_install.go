package bridge

import (
	"context"
	"fmt"

	"devkit/internal/mcp/install"
)

func (s *AppService) McpListAgents() ([]string, error) {
	result, err := s.call(context.Background(), "builtin.mcpinstall", "mcp:install", "agent:list", map[string]any{
		"action": "list_agents",
	}, func(context.Context) (any, error) {
		agents := install.ListAgents()
		out := make([]string, len(agents))
		for i, a := range agents {
			out[i] = string(a)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (s *AppService) McpInstall(agent, scope, projectPath string) (string, error) {
	agentScope := "agent:" + agent
	result, err := s.call(context.Background(), "builtin.mcpinstall", "mcp:install", agentScope, map[string]any{
		"action":       "install",
		"agent":        agent,
		"scope":        scope,
		"project_path": projectPath,
	}, func(context.Context) (any, error) {
		return mcpInstall(agent, scope, projectPath)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) McpUninstall(agent, scope, projectPath string) (string, error) {
	agentScope := "agent:" + agent
	result, err := s.call(context.Background(), "builtin.mcpinstall", "mcp:install", agentScope, map[string]any{
		"action":       "uninstall",
		"agent":        agent,
		"scope":        scope,
		"project_path": projectPath,
	}, func(context.Context) (any, error) {
		return mcpUninstall(agent, scope, projectPath)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func mcpInstall(agent, scope, projectPath string) (string, error) {
	cmd, err := install.ResolveCommand()
	if err != nil {
		return "", err
	}
	req := install.InstallRequest{
		Agent:       install.Agent(agent),
		Scope:       install.Scope(scope),
		ProjectPath: projectPath,
		Command:     cmd,
	}
	path, err := install.ConfigPath(req)
	if err != nil {
		return "", err
	}
	if err := install.Install(req); err != nil {
		return "", err
	}
	return path, nil
}

func mcpUninstall(agent, scope, projectPath string) (string, error) {
	req := install.InstallRequest{
		Agent:       install.Agent(agent),
		Scope:       install.Scope(scope),
		ProjectPath: projectPath,
	}
	path, err := install.ConfigPath(req)
	if err != nil {
		return "", err
	}
	if err := install.Uninstall(req); err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return path, nil
}
