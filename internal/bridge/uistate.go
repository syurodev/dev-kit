package bridge

import (
	"context"
	"errors"
)

var errUIStateUnavailable = errors.New("ui state is not available")

func (s *AppService) UIStateGet(key string) (string, error) {
	result, err := s.call(context.Background(), "builtin.uistate", "ui:read", "key:"+key, map[string]any{
		"action": "get",
		"key":    key,
	}, func(context.Context) (any, error) {
		if s.container.UIState == nil {
			return nil, errUIStateUnavailable
		}
		value, _, err := s.container.UIState.Get(key)
		return value, err
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) UIStateSet(key, value string) error {
	_, err := s.call(context.Background(), "builtin.uistate", "ui:write", "key:"+key, map[string]any{
		"action": "set",
		"key":    key,
	}, func(context.Context) (any, error) {
		if s.container.UIState == nil {
			return nil, errUIStateUnavailable
		}
		return nil, s.container.UIState.Set(key, value)
	})
	return err
}
