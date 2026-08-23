//go:build windows && !server

package main

import (
	"fmt"
	"unsafe"

	"devkit/internal/preferences"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var dwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

func applyNativeTheme(window application.Window, theme preferences.Theme) error {
	if window == nil || window.NativeWindow() == nil {
		return fmt.Errorf("native window is not available")
	}
	dark := int32(0)
	switch theme {
	case preferences.ThemeDark:
		dark = 1
	case preferences.ThemeLight:
	case preferences.ThemeSystem:
		if app := application.Get(); app != nil && app.Env.IsDarkMode() {
			dark = 1
		}
	default:
		return preferences.ErrInvalidTheme
	}

	// DWMWA_USE_IMMERSIVE_DARK_MODE updates the native non-client frame.
	const immersiveDarkMode = 20
	result, _, callErr := dwmSetWindowAttribute.Call(
		uintptr(window.NativeWindow()),
		immersiveDarkMode,
		uintptr(unsafe.Pointer(&dark)),
		unsafe.Sizeof(dark),
	)
	if result != 0 {
		return fmt.Errorf("set native window theme: %w", callErr)
	}
	// With the Mica backdrop active the window colour must stay transparent —
	// repainting it opaque here would cover the backdrop. Solid-window builds
	// (pre-22621) repaint with the frontend's --background-app token values.
	if !nativeBackdropTranslucent() {
		if dark != 0 {
			window.SetBackgroundColour(application.NewRGB(2, 2, 2))
		} else {
			window.SetBackgroundColour(application.NewRGB(233, 234, 238))
		}
	}
	return nil
}
