//go:build server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Server mode serves the same services over HTTP with no window at all, so
// there is nothing for a menu to attach to — and application.DefaultApplicationMenu
// does not exist in a server build, so naming it here would not compile.
func setsApplicationMenu() bool { return false }

func buildApplicationMenu(*application.App) *application.Menu { return nil }
