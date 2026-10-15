package bridge

import (
	"context"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
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

func (s *AppService) AgentHubResumeSession(ctx context.Context, sessionID string, reload bool) error {
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+hubSessionID(sessionID), map[string]any{"action": "resume_session", "session_id": hubSessionID(sessionID), "reload": reload}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.ResumeSession(callCtx, sessionID, reload)
	})
	return err
}

func (s *AppService) AgentHubGetCustomization(ctx context.Context, vendor string) (preferences.AgentCustomization, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_customization:"+agentRuntimeID(vendor), map[string]any{"action": "get_agent_customization", "agent": agentRuntimeID(vendor)}, func(callCtx context.Context) (any, error) {
		if caller, ok := identity.FromContext(callCtx); ok && caller.Kind != identity.KindUI {
			return nil, agenthub.ErrPermissionDenied
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.AgentCustomization(vendor)
	})
	if err != nil {
		return preferences.AgentCustomization{}, err
	}
	return result.(preferences.AgentCustomization), nil
}

func (s *AppService) AgentHubSetCustomization(ctx context.Context, vendor string, value preferences.AgentCustomization) (preferences.AgentCustomization, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_customization:"+agentRuntimeID(vendor), map[string]any{"action": "set_agent_customization", "agent": agentRuntimeID(vendor)}, func(callCtx context.Context) (any, error) {
		if caller, ok := identity.FromContext(callCtx); ok && caller.Kind != identity.KindUI {
			return nil, agenthub.ErrPermissionDenied
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		validated, err := agenthub.ValidateCustomization(vendor, value)
		if err != nil {
			return nil, err
		}
		return s.container.Preferences.SetAgentCustomization(vendor, validated)
	})
	if err != nil {
		return preferences.AgentCustomization{}, err
	}
	return result.(preferences.AgentCustomization), nil
}
