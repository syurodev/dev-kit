package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devkit/internal/sandbox"
)

const (
	claudeCLITimeout = 20 * time.Second
	// claudeCLIExitGrace is how long a CLI that has answered (or been told to stop by a
	// closed stdin) is given to exit on its own before it is killed, as codexExitGrace.
	claudeCLIExitGrace = 2 * time.Second
	// claudeCLIMaxOutput bounds what is read from the CLI, a line included.
	claudeCLIMaxOutput = 4 << 20
)

func (a *ClaudeAdapter) cliTimeoutOrDefault() time.Duration {
	if a.cliTimeout > 0 {
		return a.cliTimeout
	}
	return claudeCLITimeout
}

func (a *ClaudeAdapter) cliGraceOrDefault() time.Duration {
	if a.cliGrace > 0 {
		return a.cliGrace
	}
	return claudeCLIExitGrace
}

// claudeProbeEnv removes what would make a probe of an added account describe the
// host's login instead: API keys and tokens, a base URL that redirects the traffic,
// and the switches to another provider (Bedrock, Vertex, Foundry). Whatever the launch
// or the spec set on purpose (l.env, spec.ExtraEnv) stays. Only the probe path calls
// it: a session's environment is unchanged.
func claudeProbeEnv(env map[string]string, l launch, spec OpenSpec) {
	for key, value := range env {
		if !claudeCredentialEnvName(key) {
			continue
		}
		if v, ok := l.env[key]; ok && v == value {
			continue
		}
		if v, ok := spec.ExtraEnv[key]; ok && v == value {
			continue
		}
		delete(env, key)
	}
}

func claudeCredentialEnvName(key string) bool {
	switch {
	case strings.HasPrefix(key, "ANTHROPIC_"), strings.HasPrefix(key, "CLAUDE_CODE_USE_"),
		key == "CLAUDE_API_KEY", key == "CLAUDE_CODE_OAUTH_TOKEN", key == "CLAUDE_CODE_OAUTH_REFRESH_TOKEN",
		key == "AWS_BEARER_TOKEN_BEDROCK":
		return true
	case strings.HasPrefix(key, "CLAUDE_"):
		for _, word := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD", "CREDENTIAL"} {
			if strings.Contains(key, word) {
				return true
			}
		}
	}
	return false
}

// claudeCLI runs the Claude Code CLI under the session jail with spec's account home
// and returns the first stdout line awaited accepts. It starts the same node + adapter
// entry a session does and adds --cli, which makes the entry spawn the CLI itself (the
// bundled native binary or $CLAUDE_CODE_EXECUTABLE), so both the "bundled" and "on this
// machine" choices work. stdin, when not empty, is written and closed. The exit status
// is not an error (`claude auth status` exits 1 when signed out and still answers).
//
// Stdout is read line by line as it arrives, bounded by claudeCLIMaxOutput and the
// deadline: the answer is returned the moment its line arrives, whether or not the CLI
// is slow to exit. A nil line with a nil error means the output ended without it. The
// deadline is ErrProcessFailed, the caller's own cancellation is ctx.Err(). The process
// is always closed, given a grace period, killed and waited for before the scratch
// directory goes and the lease is released (the --cli entry starts the native claude as a
// grandchild, which a kill of the direct child alone may orphan under Seatbelt).
// For an added account (spec.AccountHome) the host's credential variables are removed;
// the caller builds spec like accountProbeSpec does (deny paths, private/shared home).
// Nothing it prints is logged: an answer may name an account.
func (a *ClaudeAdapter) claudeCLI(ctx context.Context, spec OpenSpec, args []string, stdin string, awaited func(line []byte) bool) ([]byte, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	defer l.releaseLease()
	work, err := os.MkdirTemp("", "devkit-claude-cli-")
	if err != nil {
		return nil, fmt.Errorf("%w: scratch directory: %v", ErrProcessFailed, err)
	}
	defer os.RemoveAll(work)
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	pol, cmd := sandboxFor(work, l, spec)
	if spec.AccountHome != "" {
		claudeProbeEnv(pol.Env, l, spec)
	}
	cmd.Args = append(append([]string(nil), cmd.Args...), append([]string{"--cli"}, args...)...)
	callCtx, cancel := context.WithTimeout(ctx, a.cliTimeoutOrDefault())
	defer cancel()
	proc, err := a.runner().StartPiped(callCtx, pol, cmd)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, mapSandboxErr(err)
	}
	in, out := proc.StdinWriter(), proc.StdoutReader()
	if in == nil || out == nil {
		_ = proc.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}
	// Runs first of the defers: the process is gone before the scratch directory is
	// removed and the lease released.
	defer reapClaudeCLI(proc, in, out, a.cliGraceOrDefault())
	go func() { <-callCtx.Done(); _ = out.Close() }() // unblocks the read when the time is up
	if stdin != "" {
		_, _ = io.WriteString(in, stdin)
	}
	_ = in.Close()
	sc := bufio.NewScanner(io.LimitReader(out, claudeCLIMaxOutput))
	sc.Buffer(make([]byte, 0, 64<<10), claudeCLIMaxOutput)
	for sc.Scan() {
		if line := sc.Bytes(); awaited(line) {
			return append([]byte(nil), line...), nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if callCtx.Err() != nil {
		return nil, fmt.Errorf("%w: the Claude CLI did not answer in time", ErrProcessFailed)
	}
	return nil, nil
}

// reapClaudeCLI ends the process however the read ended, as codexProcess.reap does:
// the pipes close first (the CLI exits on EOF and takes what it started with it, which a
// SIGKILL of the direct child would not under Seatbelt), a process that has not gone in
// grace is killed, and it is waited for either way.
func reapClaudeCLI(proc *sandbox.Proc, in io.Closer, out io.Closer, grace time.Duration) {
	_ = in.Close()
	_ = out.Close()
	exited := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(grace):
		_ = proc.Kill()
		<-exited
	}
}

const claudeUsageRequest = `{"type":"control_request","request_id":"u1","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// CheckUsage asks the CLI for the data behind /usage (the SDK's get_usage control
// request: experimental, so a shape it does not recognise is "not checkable", never a guess).
func (a *ClaudeAdapter) CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error) {
	// --no-session-persistence (listed by `claude --help`, "only works with --print") keeps
	// -p from writing a session file into the shared projects/ store for a probe.
	out, err := a.claudeCLI(ctx, spec,
		[]string{"-p", "--verbose", "--no-session-persistence", "--input-format", "stream-json", "--output-format", "stream-json"},
		claudeUsageRequest, isClaudeUsageAnswer)
	if err != nil {
		return nil, err
	}
	return parseClaudeUsage(out)
}

// isClaudeUsageAnswer: the control_response to the get_usage request (id u1).
func isClaudeUsageAnswer(line []byte) bool {
	var l struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	return json.Unmarshal(line, &l) == nil && l.Type == "control_response" && l.Response.RequestID == "u1"
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// parseClaudeUsage reads the control_response line of a get_usage answer. get_usage's
// utilization is 0-100 already (SDKControlGetUsageResponse), unlike rate_limit_event's
// 0-1 fractions: unifiedPercent must not be used here.
func parseClaudeUsage(out []byte) ([]LimitWindow, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var line struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
				Response  struct {
					Available  bool `json:"rate_limits_available"`
					RateLimits *struct {
						FiveHour       *claudeUsageWindow `json:"five_hour"`
						SevenDay       *claudeUsageWindow `json:"seven_day"`
						SevenDayOpus   *claudeUsageWindow `json:"seven_day_opus"`
						SevenDaySonnet *claudeUsageWindow `json:"seven_day_sonnet"`
						ModelScoped    []struct {
							DisplayName string   `json:"display_name"`
							Utilization *float64 `json:"utilization"`
							ResetsAt    *string  `json:"resets_at"`
						} `json:"model_scoped"`
					} `json:"rate_limits"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "control_response" || line.Response.RequestID != "u1" {
			continue
		}
		if line.Response.Subtype != "success" {
			return nil, fmt.Errorf("%w: %s", ErrUsageNotCheckable, clipLine(line.Response.Error))
		}
		rl := line.Response.Response.RateLimits
		if !line.Response.Response.Available || rl == nil {
			return nil, fmt.Errorf("%w: the account has no plan limits to read", ErrUsageNotCheckable)
		}
		var windows []LimitWindow
		for _, w := range []struct {
			key    string
			window *claudeUsageWindow
		}{{"five_hour", rl.FiveHour}, {"seven_day", rl.SevenDay}, {"seven_day_opus", rl.SevenDayOpus}, {"seven_day_sonnet", rl.SevenDaySonnet}} {
			if lw, ok := claudeUsageLimit(claudeLimitLabel(w.key), w.window); ok {
				windows = append(windows, lw)
			}
		}
		for _, m := range rl.ModelScoped {
			name := clipTrim(strings.Join(strings.Fields(m.DisplayName), " "), 40)
			if name == "" {
				continue
			}
			if lw, ok := claudeUsageLimit("Weekly · "+name, &claudeUsageWindow{Utilization: m.Utilization, ResetsAt: m.ResetsAt}); ok {
				windows = append(windows, lw)
			}
		}
		windows = dedupeWindowsByLabel(windows)
		if len(windows) == 0 {
			return nil, fmt.Errorf("%w: the answer lists no window", ErrUsageNotCheckable)
		}
		return windows, nil
	}
	return nil, fmt.Errorf("%w: the CLI sent no answer", ErrUsageNotCheckable)
}

func claudeUsageLimit(label string, w *claudeUsageWindow) (LimitWindow, bool) {
	if w == nil || w.Utilization == nil {
		return LimitWindow{}, false
	}
	pct := roundPercent(math.Min(100, math.Max(0, *w.Utilization)))
	lw := LimitWindow{Label: label, UsedPercent: pct, Status: percentStatus(pct)}
	if w.ResetsAt != nil {
		if at, err := time.Parse(time.RFC3339, *w.ResetsAt); err == nil {
			lw.ResetsAt = at.Unix()
		}
	}
	return lw, true
}

func clipLine(s string) string {
	return clipTrim(strings.Join(strings.Fields(s), " "), 200)
}

func clipTrim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}

// dedupeWindowsByLabel keeps the first window of each label.
func dedupeWindowsByLabel(in []LimitWindow) []LimitWindow {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, w := range in {
		if seen[w.Label] {
			continue
		}
		seen[w.Label] = true
		out = append(out, w)
	}
	return out
}

// isClaudeAuthStatus: a line that decodes to a JSON object with a loggedIn key (a banner
// or an update notice before it is not the answer).
func isClaudeAuthStatus(line []byte) bool {
	var o map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(line), &o) != nil {
		return false
	}
	_, ok := o["loggedIn"]
	return ok
}

// claudeAuthStatusLines finds the status object in the CLI's output line by line.
// Claude Code 2.1.310 prints it indented over several lines, older output was one
// line, so a line is tried alone first and then as part of the object that a line
// starting with "{" in column 0 opened. Lines are joined without their newline, which
// JSON allows between tokens and never inside a string.
type claudeAuthStatusLines struct {
	pending []byte
	answer  []byte
}

// claudeAuthStatusMaxPending bounds the lines kept for an object that never closes.
const claudeAuthStatusMaxPending = 64 << 10

func (s *claudeAuthStatusLines) accept(raw []byte) bool {
	line := bytes.TrimSpace(raw)
	if isClaudeAuthStatus(line) {
		s.answer = append([]byte(nil), line...)
		return true
	}
	opens := bytes.HasPrefix(raw, []byte("{"))
	if opens || len(s.pending)+len(line) > claudeAuthStatusMaxPending {
		s.pending = s.pending[:0]
	}
	if !opens && len(s.pending) == 0 {
		return false
	}
	s.pending = append(s.pending, line...)
	if isClaudeAuthStatus(s.pending) {
		s.answer = s.pending
		return true
	}
	return false
}

// CheckSignIn runs `claude auth status` (JSON; no model call, no prompt). It takes no
// --no-session-persistence: that flag only works with --print, and this command writes
// no session.
func (a *ClaudeAdapter) CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error) {
	var lines claudeAuthStatusLines
	if _, err := a.claudeCLI(ctx, spec, []string{"auth", "status"}, "", lines.accept); err != nil {
		return SignInState{}, err
	}
	line := lines.answer
	var st struct {
		LoggedIn         bool   `json:"loggedIn"`
		Email            string `json:"email"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if line == nil || json.Unmarshal(bytes.TrimSpace(line), &st) != nil {
		return SignInState{}, fmt.Errorf("%w: the status answer could not be read", ErrProcessFailed)
	}
	return SignInState{SignedIn: st.LoggedIn, Email: st.Email, Plan: st.SubscriptionType}, nil
}

var (
	_ UsageChecker  = (*ClaudeAdapter)(nil)
	_ SignInChecker = (*ClaudeAdapter)(nil)
)
