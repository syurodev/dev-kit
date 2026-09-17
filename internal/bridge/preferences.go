package bridge

import (
	"context"
	"errors"

	"devkit/internal/agenthub"
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

// PreferencesGetThemeFamily reads which palette the UI is painted with.
//
// Deliberately no applyTheme call on the write path below, unlike SetTheme:
// the native window frame follows the MODE, and a family supplies both modes.
// Switching from GitHub to Dracula does not make a light titlebar dark.
func (s *AppService) PreferencesGetThemeFamily() (preferences.ThemeFamily, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "theme_family", map[string]any{
		"action": "get_theme_family",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.ThemeFamily()
	})
	if err != nil {
		return "", err
	}
	return result.(preferences.ThemeFamily), nil
}

// PreferencesGetAgentNotifications reports whether Agent Hub notifies when an
// agent needs the user or finishes. On by default.
func (s *AppService) PreferencesGetAgentNotifications() (bool, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "agent_notifications", map[string]any{
		"action": "get_agent_notifications",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.AgentNotifications()
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// PreferencesSetAgentNotifications turns the notifications on or off. Turning
// them on is when the OS is asked for permission to show them — a moment the
// user chose, rather than the first time an agent needs them, when the dialog
// would cover the very prompt it is about.
func (s *AppService) PreferencesSetAgentNotifications(enabled bool) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "agent_notifications", map[string]any{
		"action":  "set_agent_notifications",
		"enabled": enabled,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		if err := s.container.Preferences.SetAgentNotifications(enabled); err != nil {
			return nil, err
		}
		if enabled && s.container.AgentNotifications != nil {
			s.container.AgentNotifications.Authorize()
		}
		return nil, nil
	})
	return err
}

// PreferencesGetAgentCheckpoints reads how Agent Hub snapshots a project's
// files around each turn.
func (s *AppService) PreferencesGetAgentCheckpoints() (preferences.AgentCheckpointSettings, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "agent_checkpoints", map[string]any{
		"action": "get_agent_checkpoints",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.AgentCheckpoints()
	})
	if err != nil {
		return preferences.AgentCheckpointSettings{}, err
	}
	return result.(preferences.AgentCheckpointSettings), nil
}

// PreferencesSetAgentCheckpoints stores the settings and applies them to the
// running engine.
//
// Applied rather than left for the next restart, because the numbers are what a
// user reaches for when a project is not being checkpointed — raising the file
// ceiling and having to quit DevKit to find out whether it helped is not a
// setting, it is a guess. The engine keeps the shadow repositories it has
// already warmed, so a change costs nothing.
func (s *AppService) PreferencesSetAgentCheckpoints(settings preferences.AgentCheckpointSettings) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "agent_checkpoints", map[string]any{
		"action":  "set_agent_checkpoints",
		"enabled": settings.Enabled,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		if err := s.container.Preferences.SetAgentCheckpoints(settings); err != nil {
			return nil, err
		}
		s.applyCheckpointSettings(settings)
		return nil, nil
	})
	return err
}

// applyCheckpointSettings makes a stored preference true of the running engine.
//
// Both halves: the numbers go to the engine, and the switch goes to the hub —
// an engine with new limits that the hub is not calling would be a setting that
// looks applied and is not.
func (s *AppService) applyCheckpointSettings(settings preferences.AgentCheckpointSettings) {
	checkpoints := s.container.AgentCheckpoints
	if checkpoints == nil {
		return
	}
	checkpoints.ApplySettings(settings.MaxFileSizeMiB, settings.RetentionDays, settings.ExtraExcludes)
	if s.container.AgentHub == nil {
		return
	}
	if settings.Enabled {
		s.container.AgentHub.SetCheckpoints(checkpoints)
		return
	}
	// nil, not a disabled engine: the shadow repositories stay on disk and stay
	// warm, so turning it back on does not re-pay the first snapshot of every
	// project.
	s.container.AgentHub.SetCheckpoints(nil)
}

// AgentHubCheckpointAvailability is what Settings shows beside the switch:
// whether the machine can do this at all, and which projects it could not.
func (s *AppService) AgentHubCheckpointAvailability() (agenthub.CheckpointAvailability, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "agent_checkpoints", map[string]any{
		"action": "checkpoint_availability",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		settings, err := s.container.Preferences.AgentCheckpoints()
		if err != nil {
			return nil, err
		}
		return s.container.AgentCheckpoints.Availability(settings.Enabled), nil
	})
	if err != nil {
		return agenthub.CheckpointAvailability{}, err
	}
	return result.(agenthub.CheckpointAvailability), nil
}

func (s *AppService) PreferencesSetThemeFamily(family preferences.ThemeFamily) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "theme_family", map[string]any{
		"action": "set_theme_family",
		"family": string(family),
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		if !preferences.ValidThemeFamily(family) {
			return nil, preferences.ErrInvalidThemeFamily
		}
		return nil, s.container.Preferences.SetThemeFamily(family)
	})
	return err
}
