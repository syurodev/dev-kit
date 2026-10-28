package bridge

import (
	"context"

	"devkit/internal/preferences"
)

// AgentIdleReleaseState is the person's setting for giving back the memory of idle
// agent sessions, as the window sees it.
type AgentIdleReleaseState struct {
	// Enabled turns the release on. It is on until the person turns it off.
	Enabled bool `json:"enabled"`
	// ChildMinutes is how long a sub-session (one with a parent) may sit idle
	// before its process is released; TopMinutes is the same for the others. A
	// released session stays in the rail and reopens on the next prompt.
	ChildMinutes int `json:"childMinutes"`
	TopMinutes   int `json:"topMinutes"`
}

func idleReleaseState(p preferences.AgentIdleRelease) AgentIdleReleaseState {
	return AgentIdleReleaseState{Enabled: p.Enabled, ChildMinutes: p.ChildMinutes, TopMinutes: p.TopMinutes}
}

// AgentIdleReleaseGet reads the setting. Window only: the check is made here in Go
// and no MCP tool reaches this method.
func (s *AppService) AgentIdleReleaseGet(ctx context.Context) (AgentIdleReleaseState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_idle_release", map[string]any{"action": "get_agent_idle_release"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		p, err := s.container.Preferences.AgentIdleRelease()
		if err != nil {
			return nil, err
		}
		return idleReleaseState(p), nil
	})
	if err != nil {
		return AgentIdleReleaseState{}, err
	}
	return result.(AgentIdleReleaseState), nil
}

// AgentIdleReleaseSet stores the setting and returns what is stored. A time outside
// 2 minutes to 24 hours is refused and nothing changes. Window only.
func (s *AppService) AgentIdleReleaseSet(ctx context.Context, enabled bool, childMinutes, topMinutes int) (AgentIdleReleaseState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_idle_release", map[string]any{
		"action": "set_agent_idle_release", "enabled": enabled, "child_minutes": childMinutes, "top_minutes": topMinutes,
	}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		p, err := s.container.Preferences.SetAgentIdleRelease(preferences.AgentIdleRelease{Enabled: enabled, ChildMinutes: childMinutes, TopMinutes: topMinutes})
		if err != nil {
			return nil, err
		}
		return idleReleaseState(p), nil
	})
	if err != nil {
		return AgentIdleReleaseState{}, err
	}
	return result.(AgentIdleReleaseState), nil
}
