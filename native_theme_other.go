//go:build (!darwin && !windows) || ios || server

package main

import (
	"devkit/internal/preferences"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func applyNativeTheme(window application.Window, theme preferences.Theme) error {
	if !preferences.ValidTheme(theme) {
		return preferences.ErrInvalidTheme
	}
	if window == nil {
		return nil
	}
	dark := theme == preferences.ThemeDark
	if theme == preferences.ThemeSystem {
		if app := application.Get(); app != nil {
			dark = app.Env.IsDarkMode()
		}
	}
	if dark {
		window.SetBackgroundColour(application.NewRGB(16, 18, 20))
	} else {
		window.SetBackgroundColour(application.NewRGB(245, 246, 247))
	}
	return nil
}
