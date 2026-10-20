package main

import "testing"

func TestWithPerfFlag(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	for _, tc := range []struct {
		name, url string
		env       map[string]string
		want      string
	}{
		{"unset leaves the url alone", "/", nil, "/"},
		{"only 1 turns it on", "/", map[string]string{"DEVKIT_PERF": "0"}, "/"},
		{"on", "/", map[string]string{"DEVKIT_PERF": "1"}, "/?perf"},
		{"joins an existing query", "/?software-compositing", map[string]string{"DEVKIT_PERF": "1"}, "/?software-compositing&perf"},
	} {
		if got := withPerfFlag(tc.url, env(tc.env)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
