package bridge

import (
	"context"
)

// ActiveProfileDTO is the sticky agent-active profile for SSH or database MCP tools.
type ActiveProfileDTO struct {
	ProfileID string `json:"profileId,omitempty"`
}

func (s *AppService) SSHActiveProfile() (ActiveProfileDTO, error) {
	result, err := s.call(context.Background(), "builtin.ssh", "ssh:connect", "profile:active", map[string]any{
		"action": "active_profile",
	}, func(context.Context) (any, error) {
		return ActiveProfileDTO{ProfileID: s.container.SSH.AgentActiveProfileID()}, nil
	})
	if err != nil {
		return ActiveProfileDTO{}, err
	}
	return result.(ActiveProfileDTO), nil
}

func (s *AppService) SSHSetActiveProfile(id string) error {
	metadata := map[string]any{
		"action": "set_active_profile",
	}
	if id != "" {
		metadata["profile_id"] = recordID(id)
	}
	_, err := s.call(context.Background(), "builtin.ssh", "ssh:connect", "profile:active", metadata, func(context.Context) (any, error) {
		return nil, s.container.SSH.SetAgentActiveProfile(id)
	})
	return err
}
