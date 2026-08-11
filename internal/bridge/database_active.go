package bridge

import (
	"context"
)

func (s *AppService) DBActiveProfile() (ActiveProfileDTO, error) {
	result, err := s.call(context.Background(), "builtin.database", "db:query", "profile:active", map[string]any{
		"action": "active_profile",
	}, func(context.Context) (any, error) {
		return ActiveProfileDTO{ProfileID: s.container.Database.AgentActiveProfileID()}, nil
	})
	if err != nil {
		return ActiveProfileDTO{}, err
	}
	return result.(ActiveProfileDTO), nil
}

func (s *AppService) DBSetActiveProfile(id string) error {
	metadata := map[string]any{
		"action": "set_active_profile",
	}
	if id != "" {
		metadata["profile_id"] = recordID(id)
	}
	_, err := s.call(context.Background(), "builtin.database", "db:query", "profile:active", metadata, func(context.Context) (any, error) {
		return nil, s.container.Database.SetAgentActiveProfile(id)
	})
	return err
}
