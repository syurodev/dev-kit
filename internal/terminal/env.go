// internal/terminal/env.go
package terminal

import (
	"os"
	"runtime"
	"strings"
)

// termType is what the frontend's xterm.js emulates, and what the SSH
// terminal already asks the remote side for (internal/ssh/terminal.go).
const termType = "xterm-256color"

// parentTerminalVars describe the terminal DevKit itself was launched from, if
// any. Inherited, they tell the shell it is inside tmux, kitty or iTerm — and
// prompts and editors then emit sequences for THAT terminal into xterm.js.
var parentTerminalVars = []string{
	"TERM_PROGRAM_VERSION",
	"TERMINFO",
	"TMUX",
	"TMUX_PANE",
	"STY",
	"VTE_VERSION",
	"KITTY_WINDOW_ID",
	"KITTY_PID",
	"WEZTERM_PANE",
	"ITERM_SESSION_ID",
	"ALACRITTY_WINDOW_ID",
	"WT_SESSION",
}

// terminalEnv is base (nil means the app's own environment) as the program in
// one of our PTYs should see it: told which terminal it is talking to, and
// that the terminal speaks UTF-8.
//
// Neither can be left to inheritance. A desktop app started from a launcher
// has no TERM at all, and one started from another terminal has THAT
// terminal's. zsh without a terminfo entry it can find loses cursor movement
// and clear-to-end-of-line, so every redraw leaves the old text behind
// ("fetch originin") and prompt glyphs come out as "?➜". A GUI session is
// also allowed to have no locale, and a shell in the C locale counts a
// multibyte glyph as several columns while xterm.js draws it as one.
func terminalEnv(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	drop := map[string]bool{"TERM": true, "COLORTERM": true, "TERM_PROGRAM": true}
	for _, k := range parentTerminalVars {
		drop[k] = true
	}
	env := make([]string, 0, len(base)+4)
	vars := make(map[string]string, len(base))
	for _, kv := range base {
		key, value, _ := strings.Cut(kv, "=")
		if drop[key] {
			continue
		}
		vars[key] = value
		env = append(env, kv)
	}
	env = append(env,
		"TERM="+termType,
		"COLORTERM=truecolor",
		"TERM_PROGRAM=DevKit",
	)
	if !localeSettled(vars) {
		// LC_CTYPE only: the encoding is the terminal's business, the
		// language of messages and the date format are not.
		env = append(env, "LC_CTYPE="+utf8Locale())
	}
	return env
}

// localeSettled reports whether the character type needs no help: LC_ALL is
// set (whatever it says — it overrides LC_CTYPE, so it is the user's call), or
// LC_CTYPE, else LANG, already names a UTF-8 locale.
func localeSettled(vars map[string]string) bool {
	if v := vars["LC_ALL"]; v != "" {
		return true
	}
	v := vars["LC_CTYPE"]
	if v == "" {
		v = vars["LANG"]
	}
	v = strings.ToLower(v)
	return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
}

// utf8Locale is a UTF-8 locale that exists without the user generating one:
// glibc and musl ship C.UTF-8; macOS has no C.UTF-8 but always has en_US.
func utf8Locale() string {
	if runtime.GOOS == "darwin" {
		return "en_US.UTF-8"
	}
	return "C.UTF-8"
}
