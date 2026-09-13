//go:build !linux || server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Only WebKitGTK needs its renderer talked down; see native_webview_linux.go.
func configureWebviewEnv() {}

// WKWebView and WebView2 paint backdrop-filter on every compositing path, so the
// frontend's glass never needs flattening here. See native_webview_linux.go.
func webviewStartURL() string { return "/" }

// macOS insets the traffic lights into the app's own bar (MacTitleBarHiddenInset
// in main.go) and Windows keeps its native caption, so neither asks the frontend
// to draw window controls. See native_webview_linux.go for why Linux does.
const framelessWindow = false

// Every other platform lets Wails set the window title itself; only a frameless
// Linux window loses it. See applyWindowTitle in native_webview_linux.go.
func applyWindowTitle(application.Window, string) {}
