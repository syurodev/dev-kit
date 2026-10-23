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
)

const claudeCLITimeout = 20 * time.Second

// claudeCLI runs the Claude Code CLI under the session jail with spec's account home
// and returns its stdout. It starts the same node + adapter entry a session does and
// adds --cli, which makes the entry spawn the CLI itself (the bundled native binary
// or $CLAUDE_CODE_EXECUTABLE), so both the "bundled" and "on this machine" choices work.
// stdin, when not empty, is written and closed: the CLI answers and exits. The exit
// status is not an error (`claude auth status` exits 1 when signed out and still answers).
// Nothing it prints is logged: an answer may name an account.
func (a *ClaudeAdapter) claudeCLI(ctx context.Context, spec OpenSpec, args []string, stdin string) ([]byte, error) {
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
	cmd.Args = append(append([]string(nil), cmd.Args...), append([]string{"--cli"}, args...)...)
	callCtx, cancel := context.WithTimeout(ctx, claudeCLITimeout)
	defer cancel()
	proc, err := a.runner().StartPiped(callCtx, pol, cmd)
	if err != nil {
		return nil, mapSandboxErr(err)
	}
	in, out := proc.StdinWriter(), proc.StdoutReader()
	if in == nil || out == nil {
		_ = proc.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}
	defer func() { _ = proc.Kill(); _ = proc.Wait() }()
	go func() { <-callCtx.Done(); _ = out.Close() }() // unblocks the read when the time is up
	if stdin != "" {
		_, _ = io.WriteString(in, stdin)
	}
	_ = in.Close()
	data, _ := io.ReadAll(io.LimitReader(out, 4<<20))
	if callCtx.Err() != nil {
		return nil, fmt.Errorf("%w: the Claude CLI did not answer in time", ErrProcessFailed)
	}
	return data, nil
}

const claudeUsageRequest = `{"type":"control_request","request_id":"u1","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// CheckUsage asks the CLI for the data behind /usage (the SDK's get_usage control
// request: experimental, so a shape it does not recognise is "not checkable", never a guess).
func (a *ClaudeAdapter) CheckUsage(ctx context.Context, spec OpenSpec) ([]LimitWindow, error) {
	out, err := a.claudeCLI(ctx, spec,
		[]string{"-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json"}, claudeUsageRequest)
	if err != nil {
		return nil, err
	}
	return parseClaudeUsage(out)
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
			if lw, ok := claudeUsageLimit("Weekly · "+m.DisplayName, &claudeUsageWindow{Utilization: m.Utilization, ResetsAt: m.ResetsAt}); ok {
				windows = append(windows, lw)
			}
		}
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
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// CheckSignIn runs `claude auth status` (JSON; no model call, no prompt).
func (a *ClaudeAdapter) CheckSignIn(ctx context.Context, spec OpenSpec) (SignInState, error) {
	out, err := a.claudeCLI(ctx, spec, []string{"auth", "status"}, "")
	if err != nil {
		return SignInState{}, err
	}
	var st struct {
		LoggedIn         bool   `json:"loggedIn"`
		Email            string `json:"email"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &st); err != nil {
		return SignInState{}, fmt.Errorf("%w: the status answer could not be read", ErrProcessFailed)
	}
	return SignInState{SignedIn: st.LoggedIn, Email: st.Email, Plan: st.SubscriptionType}, nil
}

var (
	_ UsageChecker  = (*ClaudeAdapter)(nil)
	_ SignInChecker = (*ClaudeAdapter)(nil)
)
