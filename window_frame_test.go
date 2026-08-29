package main

import (
	"testing"

	"devkit/internal/uistate"
)

func newFrameStore(t *testing.T, stored string) *uistate.Service {
	t.Helper()
	service := uistate.NewService(uistate.NewMemoryRepository())
	if stored != "" {
		if err := service.Set(windowFrameKey, stored); err != nil {
			t.Fatalf("seed store: %v", err)
		}
	}
	return service
}

func TestLoadWindowFrameRoundTrip(t *testing.T) {
	store := newFrameStore(t, `{"x":120,"y":64,"width":1440,"height":900,"maximised":true}`)
	frame, ok := loadWindowFrame(store)
	if !ok {
		t.Fatal("a valid stored frame should load")
	}
	want := windowFrame{X: 120, Y: 64, Width: 1440, Height: 900, Maximised: true}
	if frame != want {
		t.Fatalf("frame = %+v, want %+v", frame, want)
	}
}

func TestLoadWindowFrameRejectsUnusable(t *testing.T) {
	cases := map[string]string{
		"nothing stored yet": "",
		"not json":           "not-json",
		// Restoring below the shell's floor would let the platform silently
		// resize the window, and the next save would persist that wrong size.
		"narrower than the floor": `{"x":0,"y":0,"width":800,"height":900}`,
		"shorter than the floor":  `{"x":0,"y":0,"width":1440,"height":400}`,
		"zeroed frame":            `{}`,
	}
	for name, stored := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := loadWindowFrame(newFrameStore(t, stored)); ok {
				t.Fatalf("%s should fall back to the launch default", name)
			}
		})
	}
}

func TestLoadWindowFrameWithoutStore(t *testing.T) {
	if _, ok := loadWindowFrame(nil); ok {
		t.Fatal("a nil store should fall back to the launch default")
	}
}

func TestWindowFrameSameAsAbsorbsScaleFactorRounding(t *testing.T) {
	restored := windowFrame{X: 250, Y: 90, Width: 1200, Height: 760}
	// What Bounds() reports back for that exact frame on a 2x display.
	readBack := windowFrame{X: 250, Y: 90, Width: 1199, Height: 759}
	if !readBack.sameAs(restored) {
		t.Fatal("a 1 DIP read-back difference must not count as a resize, or the window loses a pixel every restart")
	}
}

func TestWindowFrameSameAsDetectsRealChanges(t *testing.T) {
	base := windowFrame{X: 250, Y: 90, Width: 1200, Height: 760}
	cases := map[string]windowFrame{
		"moved":      {X: 400, Y: 90, Width: 1200, Height: 760},
		"resized":    {X: 250, Y: 90, Width: 1400, Height: 760},
		"maximised":  {X: 250, Y: 90, Width: 1200, Height: 760, Maximised: true},
		"nudged 3px": {X: 253, Y: 90, Width: 1200, Height: 760},
	}
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			if changed.sameAs(base) {
				t.Fatalf("%s should be persisted", name)
			}
		})
	}
}
