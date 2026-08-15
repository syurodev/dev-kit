package bridge

import (
	"context"
	"errors"

	"devkit/internal/preferences"
)

var errPreferencesUnavailable = errors.New("preferences are not available")

func (s *AppService) PreferencesGetTheme() (preferences.Theme, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "theme", map[string]any{
		"action": "get_theme",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.Theme()
	})
	if err != nil {
		return "", err
	}
	return result.(preferences.Theme), nil
}

func (s *AppService) PreferencesSetTheme(theme preferences.Theme) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "theme", map[string]any{
		"action": "set_theme",
		"theme":  string(theme),
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		if !preferences.ValidTheme(theme) {
			return nil, preferences.ErrInvalidTheme
		}
		previousTheme, err := s.container.Preferences.Theme()
		if err != nil {
			return nil, err
		}
		if s.applyTheme != nil {
			if err := s.applyTheme(theme); err != nil {
				return nil, err
			}
		}
		if err := s.container.Preferences.SetTheme(theme); err != nil {
			// Keep the native frame aligned with the persisted preference if the
			// database write fails after a successful native update.
			if s.applyTheme != nil {
				_ = s.applyTheme(previousTheme)
			}
			return nil, err
		}
		return nil, nil
	})
	return err
}
