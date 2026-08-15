package bridge

import (
	"errors"
	"testing"

	"devkit/internal/app"
	"devkit/internal/preferences"
)

func TestPreferencesThemeRoundTripAndAppliesNativeTheme(t *testing.T) {
	container := app.NewContainer()
	var applied preferences.Theme
	service := NewAppService(container, func(theme preferences.Theme) error {
		applied = theme
		return nil
	})

	if err := service.PreferencesSetTheme(preferences.ThemeLight); err != nil {
		t.Fatalf("PreferencesSetTheme: %v", err)
	}
	if applied != preferences.ThemeLight {
		t.Fatalf("applied = %q, want light", applied)
	}
	got, err := service.PreferencesGetTheme()
	if err != nil {
		t.Fatalf("PreferencesGetTheme: %v", err)
	}
	if got != preferences.ThemeLight {
		t.Fatalf("theme = %q, want light", got)
	}
}

func TestPreferencesSetThemeRejectsInvalidValue(t *testing.T) {
	service := NewAppService(app.NewContainer())
	err := service.PreferencesSetTheme("sepia")
	if err == nil {
		t.Fatal("PreferencesSetTheme error = nil, want validation error")
	}
}

func TestPreferencesSetThemeReturnsNativeApplyFailure(t *testing.T) {
	wantErr := errors.New("native failure")
	container := app.NewContainer()
	service := NewAppService(container, func(preferences.Theme) error { return wantErr })
	if err := service.PreferencesSetTheme(preferences.ThemeLight); err == nil {
		t.Fatal("error = nil, want native failure")
	}
	got, err := container.Preferences.Theme()
	if err != nil {
		t.Fatal(err)
	}
	if got != preferences.ThemeDark {
		t.Fatalf("persisted theme = %q, want unchanged dark", got)
	}
}
