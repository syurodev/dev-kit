//go:build !darwin || ios || server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func enableNativeHighRefreshRate(application.Window) bool {
	return false
}
