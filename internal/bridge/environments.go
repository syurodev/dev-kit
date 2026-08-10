package bridge

import (
	"context"

	"devkit/internal/environments"
)

func (s *AppService) EnvListTags() ([]environments.Tag, error) {
	result, err := s.call(context.Background(), "builtin.environments", "env:read", "tag:list", map[string]any{
		"action": "list_tags",
	}, func(context.Context) (any, error) {
		return s.container.Environments.ListTags()
	})
	if err != nil {
		return nil, err
	}
	return result.([]environments.Tag), nil
}

func (s *AppService) EnvCreateTag(name string, requiresConfirmation bool) (environments.Tag, error) {
	result, err := s.call(context.Background(), "builtin.environments", "env:write", "tag:new", map[string]any{
		"action": "create_tag",
	}, func(context.Context) (any, error) {
		return s.container.Environments.CreateTag(name, requiresConfirmation)
	})
	if err != nil {
		return environments.Tag{}, err
	}
	return result.(environments.Tag), nil
}

func (s *AppService) EnvUpdateTag(id string, requiresConfirmation bool) (environments.Tag, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.environments", "env:write", "tag:"+opaqueID, map[string]any{
		"action": "update_tag",
		"tag_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Environments.UpdateTag(id, requiresConfirmation)
	})
	if err != nil {
		return environments.Tag{}, err
	}
	return result.(environments.Tag), nil
}

func (s *AppService) EnvDeleteTag(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.environments", "env:write", "tag:"+opaqueID, map[string]any{
		"action": "delete_tag",
		"tag_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Environments.DeleteTag(id)
	})
	return err
}
