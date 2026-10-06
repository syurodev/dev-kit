// internal/terminal/env_test.go
package terminal

import (
	"slices"
	"strings"
	"testing"
)

func envValue(env []string, key string) (string, bool) {
	var value string
	found := false
	// Last wins, as exec.Cmd resolves duplicates.
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			value, found = v, true
		}
	}
	return value, found
}

func TestTerminalEnvNamesXtermWhateverWasInherited(t *testing.T) {
	for _, base := range [][]string{
		{"PATH=/usr/bin"}, // launched from a desktop launcher: no TERM
		{"PATH=/usr/bin", "TERM=xterm-kitty", "KITTY_PID=1"},     // launched from another terminal
		{"PATH=/usr/bin", "TERM=tmux-256color", "TMUX=/tmp/t,1"}, // from inside tmux
	} {
		env := terminalEnv(base)
		if v, _ := envValue(env, "TERM"); v != "xterm-256color" {
			t.Errorf("%v: TERM = %q, want xterm-256color", base, v)
		}
		if v, _ := envValue(env, "COLORTERM"); v != "truecolor" {
			t.Errorf("%v: COLORTERM = %q", base, v)
		}
		for _, k := range []string{"TMUX", "KITTY_PID"} {
			if _, ok := envValue(env, k); ok {
				t.Errorf("%v: %s leaked from the parent terminal", base, k)
			}
		}
		if v, _ := envValue(env, "PATH"); v != "/usr/bin" {
			t.Errorf("%v: PATH = %q, want it kept", base, v)
		}
	}
}

func TestTerminalEnvGivesAUTF8CtypeOnlyWhenMissing(t *testing.T) {
	cases := []struct {
		base []string
		want string // "" = LC_CTYPE must not be added
	}{
		{[]string{}, utf8Locale()},
		{[]string{"LANG=C"}, utf8Locale()},
		{[]string{"LANG=vi_VN.UTF-8"}, ""},
		{[]string{"LANG=en_US.utf8"}, ""},
		{[]string{"LANG=C", "LC_CTYPE=en_US.UTF-8"}, ""},
		{[]string{"LC_ALL=C"}, ""}, // explicit: the user's call
	}
	for _, c := range cases {
		env := terminalEnv(c.base)
		got, ok := envValue(env, "LC_CTYPE")
		inherited, _ := envValue(c.base, "LC_CTYPE")
		if c.want == "" {
			if got != inherited {
				t.Errorf("%v: LC_CTYPE = %q, want it left as %q", c.base, got, inherited)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("%v: LC_CTYPE = %q, want %q", c.base, got, c.want)
		}
		baseLang, _ := envValue(c.base, "LANG")
		if lang, _ := envValue(env, "LANG"); lang != baseLang {
			t.Errorf("%v: LANG changed to %q", c.base, lang)
		}
	}
}

func TestTerminalEnvDoesNotModifyBase(t *testing.T) {
	base := []string{"TERM=dumb", "LANG=C"}
	orig := slices.Clone(base)
	terminalEnv(base)
	if !slices.Equal(base, orig) {
		t.Fatalf("base modified: %v", base)
	}
}
