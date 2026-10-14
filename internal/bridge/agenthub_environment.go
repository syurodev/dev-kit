package bridge

import (
	"context"
	"devkit/internal/agenthub"
)

// AgentHubEnvironment reads a process snapshot or the next-session setup.
// Audit metadata contains only the vendor/session identity, never configuration
// values, credential environment variables or the report's filesystem paths.
func (s *AppService) AgentHubEnvironment(ctx context.Context, agent, sessionID string) (agenthub.EnvironmentReport, error) {
	id := agentRuntimeID(agent)
	scope := "environment:" + id
	if sessionID != "" {
		scope = "session:" + hubSessionID(sessionID)
	}
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", scope, map[string]any{
		"action": "environment", "agent": id, "session_id": hubSessionID(sessionID),
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.Environment(callCtx, agenthub.AgentType(agent), sessionID)
	})
	if err != nil {
		return agenthub.EnvironmentReport{}, err
	}
	return result.(agenthub.EnvironmentReport), nil
}
