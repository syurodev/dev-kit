//go:build darwin && !ios && !server

package main

/*
#cgo CFLAGS: -mmacosx-version-min=10.13 -x objective-c
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>
#import <Cocoa/Cocoa.h>

static void devkitSetWindowAppearance(void *handle, const char *appearanceName) {
	NSWindow *window = (NSWindow *)handle;
	if (appearanceName == NULL) {
		[window setAppearance:nil];
		return;
	}
	NSString *name = [NSString stringWithUTF8String:appearanceName];
	[window setAppearance:[NSAppearance appearanceNamed:name]];
}
*/
import "C"

import (
	"errors"
	"unsafe"

	"devkit/internal/preferences"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func applyNativeTheme(window application.Window, theme preferences.Theme) error {
	if window == nil || window.NativeWindow() == nil {
		return errors.New("native window is not available")
	}

	var appearance *C.char
	switch theme {
	case preferences.ThemeDark:
		appearance = C.CString(string(application.NSAppearanceNameDarkAqua))
	case preferences.ThemeLight:
		appearance = C.CString(string(application.NSAppearanceNameAqua))
	case preferences.ThemeSystem:
		// nil clears the window override so AppKit follows the system again.
	default:
		return preferences.ErrInvalidTheme
	}
	if appearance != nil {
		defer C.free(unsafe.Pointer(appearance))
	}

	handle := window.NativeWindow()
	application.InvokeSync(func() {
		C.devkitSetWindowAppearance(handle, appearance)
	})
	return nil
}
