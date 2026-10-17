package bridge

import (
	"context"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/preferences"
)

// AgentModelProfilesState is the person's list as Settings edits it. Statuses
// are left empty here — checking one asks its vendor for the model list, which
// can take seconds — and AgentHubCheckModelProfiles fills them in.
type AgentModelProfilesState struct {
	Revision int64                       `json:"revision"`
	Profiles []agenthub.ModelProfileInfo `json:"profiles"`
}

func modelProfilesState(value preferences.AgentModelProfiles) AgentModelProfilesState {
	out := AgentModelProfilesState{Revision: value.Revision, Profiles: make([]agenthub.ModelProfileInfo, 0, len(value.Profiles))}
	names := preferences.SubagentNames(value.Profiles)
	for i, p := range value.Profiles {
		info := agenthub.ModelProfileInfo{
			ID: p.ID, Name: p.Name, Description: p.Description, Vendor: p.Vendor, Model: p.Model, Effort: p.Effort, Notes: p.Notes,
			Subagent: p.Subagent, Prompt: p.Prompt,
		}
		if p.Subagent {
			info.SubagentName = names[i]
		}
		out.Profiles = append(out.Profiles, info)
	}
	return out
}

// uiOnly refuses anything but the window: the profiles steer what an agent
// spends, so only the person edits them.
func uiOnly(ctx context.Context) error {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return agenthub.ErrPermissionDenied
	}
	return nil
}

// AgentHubGetModelProfiles reads the "which model for which job" list.
func (s *AppService) AgentHubGetModelProfiles(ctx context.Context) (AgentModelProfilesState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_model_profiles", map[string]any{"action": "get_agent_model_profiles"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.AgentModelProfiles()
		if err != nil {
			return nil, err
		}
		return modelProfilesState(value), nil
	})
	if err != nil {
		return AgentModelProfilesState{}, err
	}
	return result.(AgentModelProfilesState), nil
}

// AgentHubSetModelProfiles replaces the list when revision is the stored one.
// Audit metadata carries the count, never a name, description or note.
func (s *AppService) AgentHubSetModelProfiles(ctx context.Context, revision int64, profiles []preferences.AgentModelProfile) (AgentModelProfilesState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_model_profiles", map[string]any{"action": "set_agent_model_profiles", "count": len(profiles)}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.SetAgentModelProfiles(preferences.AgentModelProfiles{Revision: revision, Profiles: profiles})
		if err != nil {
			return nil, err
		}
		return modelProfilesState(value), nil
	})
	if err != nil {
		return AgentModelProfilesState{}, err
	}
	return result.(AgentModelProfilesState), nil
}

// AgentHubCheckModelProfiles says, for each saved profile, whether its model
// is still on its vendor's list — or why that cannot be said (signed out, not
// installed). It reads the same cached lists an agent's tools read.
func (s *AppService) AgentHubCheckModelProfiles(ctx context.Context) ([]agenthub.ModelProfileInfo, error) {
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "profiles:list", map[string]any{"action": "check_model_profiles"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.AgentModelProfiles()
		if err != nil {
			return nil, err
		}
		return svc.DescribeModelProfiles(callCtx, value.Profiles), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.ModelProfileInfo), nil
}
