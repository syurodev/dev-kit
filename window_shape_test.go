package main

import "testing"

func TestMacFallbackCornerRadiusFollowsTheOSVersion(t *testing.T) {
	for _, tc := range []struct {
		major int
		want  float64
	}{{15, 10}, {26, 16}, {27, 16}, {0, 10}} {
		if got := macFallbackCornerRadius(tc.major); got != tc.want {
			t.Errorf("macOS %d: radius %v, want %v", tc.major, got, tc.want)
		}
	}
}
