//go:build !darwin || ios || server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func nativeTitlebarInsetHeight(application.Window) float64 {
	return 0
}
