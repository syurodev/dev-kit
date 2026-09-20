package bridge

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/app"
	"devkit/internal/notify"
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

// The family goes through the same capability gateway the theme does, under a
// scope of its own — and a scope that is not granted is denied, which the
// frontend surfaces as the palette snapping back to the previous one (setFamily
// applies optimistically and reverts when the save rejects).
//
// Exercised here rather than against preferences.Service, because the service
// never sees the gateway: the first version of this shipped with the service
// tested, the grant missing, and switching palettes silently undoing itself.
func TestPreferencesThemeFamilyRoundTripThroughTheGateway(t *testing.T) {
	service := NewAppService(app.NewContainer())

	if err := service.PreferencesSetThemeFamily(preferences.ThemeFamilyDracula); err != nil {
		t.Fatalf("PreferencesSetThemeFamily: %v", err)
	}
	got, err := service.PreferencesGetThemeFamily()
	if err != nil {
		t.Fatalf("PreferencesGetThemeFamily: %v", err)
	}
	if got != preferences.ThemeFamilyDracula {
		t.Fatalf("family = %q, want %q", got, preferences.ThemeFamilyDracula)
	}
}

// Setting the palette must not touch the window frame: a titlebar is light or
// dark, and a family supplies both modes.
func TestPreferencesSetThemeFamilyLeavesTheNativeFrameAlone(t *testing.T) {
	applied := preferences.Theme("")
	service := NewAppService(app.NewContainer(), func(theme preferences.Theme) error {
		applied = theme
		return nil
	})
	if err := service.PreferencesSetThemeFamily(preferences.ThemeFamilyAyu); err != nil {
		t.Fatalf("PreferencesSetThemeFamily: %v", err)
	}
	if applied != "" {
		t.Fatalf("applyTheme called with %q; the family must not move the native frame", applied)
	}
}

type countingDesktop struct{ requests atomic.Int32 }

func (*countingDesktop) Start() error { return nil }
func (d *countingDesktop) RequestAuthorization() (bool, error) {
	d.requests.Add(1)
	return true, nil
}
func (*countingDesktop) Send(notify.Notification, bool) error { return nil }

func TestPreferencesAgentNotificationsDefaultOnAndTurningOnAsksTheOS(t *testing.T) {
	container := app.NewContainer()
	desktop := &countingDesktop{}
	container.AgentNotifications = &notify.Notifier{Desktop: desktop, Enabled: func() bool {
		on, err := container.Preferences.AgentNotifications()
		return err == nil && on
	}}
	service := NewAppService(container)

	if on, err := service.PreferencesGetAgentNotifications(); err != nil || !on {
		t.Fatalf("default = %v, err = %v; want on", on, err)
	}
	if err := service.PreferencesSetAgentNotifications(false); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	if on, err := service.PreferencesGetAgentNotifications(); err != nil || on {
		t.Fatalf("after turning off = %v, err = %v", on, err)
	}
	if got := desktop.requests.Load(); got != 0 {
		t.Fatalf("asked the OS %d times while turning it off", got)
	}

	if err := service.PreferencesSetAgentNotifications(true); err != nil {
		t.Fatalf("turn on: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for desktop.requests.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("turning notifications on did not ask the OS for permission")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPreferencesSetThemeFamilyRejectsUnknown(t *testing.T) {
	service := NewAppService(app.NewContainer())
	if err := service.PreferencesSetThemeFamily("nord"); err == nil {
		t.Fatal("PreferencesSetThemeFamily error = nil, want validation error")
	}
}
