//go:build windows && !server

package main

import "github.com/wailsapp/wails/v3/pkg/w32"

// nativeBackdropTranslucent reports whether this Windows build can show a
// compositor backdrop (Mica) behind a translucent window. Wails gates real
// backdrop types on Windows 11 22H2 (build 22621); on anything older its
// translucent path falls back to the legacy BlurBehind accent, which performs
// poorly — so pre-22621 stays a solid window instead.
func nativeBackdropTranslucent() bool {
	return w32.SupportsBackdropTypes()
}
