package bridge

import (
	"context"

	"devkit/internal/preferences"
)

// AgentSubagentLimitsState is the cap on subagents as Settings edits it.
// CodexMaxSubagents is nil when unset (DevKit passes Codex nothing).
type AgentSubagentLimitsState struct {
	Revision          int64 `json:"revision"`
	CodexMaxSubagents *int  `json:"codexMaxSubagents"`
}

func subagentLimitsState(value preferences.AgentSubagentLimits) AgentSubagentLimitsState {
	return AgentSubagentLimitsState{Revision: value.Revision, CodexMaxSubagents: value.Codex.MaxSubagents}
}

// AgentHubGetSubagentLimits reads the cap on subagents. UI only: it steers what
// an agent can spend, so the person alone reads and edits it.
func (s *AppService) AgentHubGetSubagentLimits(ctx context.Context) (AgentSubagentLimitsState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_subagent_limits", map[string]any{"action": "get_agent_subagent_limits"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.AgentSubagentLimits()
		if err != nil {
			return nil, err
		}
		return subagentLimitsState(value), nil
	})
	if err != nil {
		return AgentSubagentLimitsState{}, err
	}
	return result.(AgentSubagentLimitsState), nil
}

// AgentHubSetSubagentLimits replaces the cap when revision is the stored one.
// codexMaxSubagents nil clears it. A session that is already running keeps the
// limit it started with until it is reloaded.
func (s *AppService) AgentHubSetSubagentLimits(ctx context.Context, revision int64, codexMaxSubagents *int) (AgentSubagentLimitsState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_subagent_limits", map[string]any{"action": "set_agent_subagent_limits", "set": codexMaxSubagents != nil}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.SetAgentSubagentLimits(preferences.AgentSubagentLimits{
			Revision: revision,
			Codex:    preferences.CodexSubagentLimits{MaxSubagents: codexMaxSubagents},
		})
		if err != nil {
			return nil, err
		}
		return subagentLimitsState(value), nil
	})
	if err != nil {
		return AgentSubagentLimitsState{}, err
	}
	return result.(AgentSubagentLimitsState), nil
}
