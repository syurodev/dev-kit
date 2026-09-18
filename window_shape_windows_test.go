//go:build windows && !server

package main

import "testing"

func TestWindowsCornerRadiusOnlyOnWindows11(t *testing.T) {
	if r, ok := windowsCornerRadius(22631); !ok || r != 8 {
		t.Fatalf("Windows 11: %v %v, want 8 true", r, ok)
	}
	if _, ok := windowsCornerRadius(19045); ok {
		t.Fatal("Windows 10 reported a radius; its windows are square")
	}
}
