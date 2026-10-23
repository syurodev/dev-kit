package agenthub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"devkit/internal/sandbox"
)

// cliRunner stands in for the Claude CLI: it records its arguments, its environment
// and everything written to stdin, then prints reply and exits.
type cliRunner struct {
	reply string
	args  *[]string
	env   *map[string]string
	stdin string // path the script writes stdin to
	// script replaces the default "read stdin, print reply, exit"; REPLY, CAP and PIDFILE are set.
	script  string
	pidFile string
}

func (r cliRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	if r.args != nil {
		*r.args = append([]string(nil), c.Args...)
	}
	if r.env != nil {
		*r.env = p.Env
	}
	script := r.script
	if script == "" {
		script = `cat >"$CAP"; printf '%s\n' "$REPLY"`
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "CAP="+r.stdin, "REPLY="+r.reply, "PIDFILE="+r.pidFile)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return sandbox.NewProcPiped(cmd, in, out, &bytes.Buffer{}), nil
}

func claudeWithCLI(t *testing.T, reply string) (*ClaudeAdapter, *[]string, *map[string]string, string) {
	t.Helper()
	return claudeWithCLIScript(t, reply, "", "")
}

func claudeWithCLIScript(t *testing.T, reply, script, pidFile string) (*ClaudeAdapter, *[]string, *map[string]string, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := new([]string)
	env := new(map[string]string)
	stdin := filepath.Join(t.TempDir(), "stdin")
	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: cliRunner{reply: reply, args: args, env: env, stdin: stdin, script: script, pidFile: pidFile}})
	a.cliGrace = 100 * time.Millisecond
	return a, args, env, stdin
}

const measuredUsageReply = `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"subscription_type":"team","rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":69,"resets_at":"2026-10-22T07:20:00.067385+00:00"},"seven_day":{"utilization":16,"resets_at":"2026-10-27T06:00:00.067406+00:00"},"seven_day_opus":null,"model_scoped":[{"display_name":"Fable","utilization":0,"resets_at":"2026-10-27T06:00:00+00:00"}]}}}}`

func TestClaudeCheckUsageSendsGetUsageThroughTheCLIAndReadsPercentages(t *testing.T) {
	a, args, _, stdinPath := claudeWithCLI(t, measuredUsageReply)
	windows, err := a.CheckUsage(t.Context(), OpenSpec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--cli", "-p", "--verbose", "--input-format", "stream-json", "--output-format"} {
		if !slices.Contains(*args, want) {
			t.Errorf("args %v lack %q (the CLI refuses stream-json output without --verbose)", *args, want)
		}
	}
	sent, _ := os.ReadFile(stdinPath)
	if !strings.Contains(string(sent), `"subtype":"get_usage"`) || !strings.Contains(string(sent), `"skip_behaviors":true`) {
		t.Errorf("stdin = %q, want the get_usage control request", sent)
	}
	if len(windows) != 3 || windows[0].Label != "5-hour" || windows[0].UsedPercent != 69 || windows[1].Label != "Weekly" || windows[1].UsedPercent != 16 || windows[2].Label != "Weekly · Fable" {
		t.Fatalf("windows = %+v", windows)
	}
	if want := time.Date(2026, 10, 22, 7, 20, 0, 0, time.UTC).Unix(); windows[0].ResetsAt != want {
		t.Errorf("resetsAt = %d, want %d (the ISO time, to the second)", windows[0].ResetsAt, want)
	}
}

func TestClaudeCheckUsageReadsUtilizationAsPercentNotAsAFraction(t *testing.T) {
	reply := `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":1,"resets_at":"2026-10-22T07:20:00+00:00"}}}}}`
	a, _, _, _ := claudeWithCLI(t, reply)
	windows, err := a.CheckUsage(t.Context(), OpenSpec{})
	if err != nil || len(windows) != 1 || windows[0].UsedPercent != 1 {
		t.Fatalf("1 means 1 %% here (get_usage is 0-100, unlike rate_limit_event): %+v, %v", windows, err)
	}
}

func TestClaudeCheckUsageSaysNotCheckableWhenThereIsNothingToRead(t *testing.T) {
	for name, reply := range map[string]string{
		"signed out":    `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"subscription_type":null,"rate_limits_available":false,"rate_limits":null}}}`,
		"error answer":  `{"type":"control_response","response":{"subtype":"error","request_id":"u1","error":"unknown request"}}`,
		"no answer":     `{"type":"system","subtype":"init"}`,
		"empty windows": `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"rate_limits_available":true,"rate_limits":{}}}}`,
	} {
		a, _, _, _ := claudeWithCLI(t, reply)
		if _, err := a.CheckUsage(t.Context(), OpenSpec{}); !errors.Is(err, ErrUsageNotCheckable) {
			t.Errorf("%s: error = %v, want ErrUsageNotCheckable", name, err)
		}
	}
}

func TestClaudeCheckUsageSkipsLinesThatAreNotItsAnswer(t *testing.T) {
	a, _, _, _ := claudeWithCLI(t, `{"type":"system","subtype":"init"}`+"\n"+measuredUsageReply)
	if windows, err := a.CheckUsage(t.Context(), OpenSpec{}); err != nil || len(windows) != 3 {
		t.Fatalf("windows = %+v, %v", windows, err)
	}
}

func TestClaudeCLIRunsInTheAccountsConfigDirectory(t *testing.T) {
	a, _, env, _ := claudeWithCLI(t, measuredUsageReply)
	home := filepath.Join(t.TempDir(), "work")
	if _, err := a.CheckUsage(t.Context(), OpenSpec{AccountHome: home}); err != nil {
		t.Fatal(err)
	}
	if (*env)["CLAUDE_CONFIG_DIR"] != home {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", (*env)["CLAUDE_CONFIG_DIR"], home)
	}
}

func TestClaudeCheckSignInReadsAuthStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		reply string
		want  SignInState
	}{
		"signed in":  {`{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","subscriptionType":"team"}`, SignInState{SignedIn: true, Email: "a@b.c", Plan: "team"}},
		"signed out": {`{"loggedIn":false,"authMethod":"none"}`, SignInState{}},
	} {
		a, args, env, _ := claudeWithCLI(t, tc.reply)
		home := filepath.Join(t.TempDir(), "acct")
		got, err := a.CheckSignIn(t.Context(), OpenSpec{AccountHome: home})
		if (*env)["CLAUDE_CONFIG_DIR"] != home {
			t.Errorf("%s: CLAUDE_CONFIG_DIR = %q, want %q", name, (*env)["CLAUDE_CONFIG_DIR"], home)
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", name, got, err, tc.want)
		}
		if !slices.Contains(*args, "auth") || !slices.Contains(*args, "status") || !slices.Contains(*args, "--cli") {
			t.Errorf("%s: args = %v", name, *args)
		}
	}
	a, _, _, _ := claudeWithCLI(t, `not json at all`)
	if _, err := a.CheckSignIn(t.Context(), OpenSpec{}); !errors.Is(err, ErrProcessFailed) {
		t.Errorf("an unreadable status answer is ErrProcessFailed (unknown), not \"signed out\": %v", err)
	}
}

func TestClaudeCheckSignInSkipsABannerBeforeTheJSON(t *testing.T) {
	a, _, _, _ := claudeWithCLI(t, "Update available: 9.9.9\n"+`{"type":"x"}`+"\n"+`{"loggedIn":true,"email":"a@b.c","subscriptionType":"pro"}`)
	got, err := a.CheckSignIn(t.Context(), OpenSpec{})
	if err != nil || got != (SignInState{SignedIn: true, Email: "a@b.c", Plan: "pro"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestClaudeCLIPersistsNoSessionForTheUsageProbe(t *testing.T) {
	a, args, _, _ := claudeWithCLI(t, measuredUsageReply)
	if _, err := a.CheckUsage(t.Context(), OpenSpec{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*args, "--no-session-persistence") {
		t.Errorf("args %v: -p would write a session file into the shared projects store", *args)
	}
}

// pidGone reports whether the process whose pid the script wrote has been reaped.
func pidGone(t *testing.T, file string) bool {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the fake never wrote its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return syscall.Kill(pid, 0) == syscall.ESRCH
}

func TestClaudeCLIReturnsTheAnswerWithoutWaitingForASlowExit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := `echo $$ >"$PIDFILE"; cat >"$CAP"; printf '%s\n' "$REPLY"; exec sleep 30`
	a, _, _, _ := claudeWithCLIScript(t, measuredUsageReply, script, pidFile)
	start := time.Now()
	windows, err := a.CheckUsage(t.Context(), OpenSpec{})
	if err != nil || len(windows) != 3 {
		t.Fatalf("a good answer from a slow-to-exit CLI is still the answer: %+v, %v", windows, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v, the CLI's 30 s sleep was waited out", d)
	}
	if !pidGone(t, pidFile) {
		t.Error("the CLI process was left running")
	}
	a2, _, _, _ := claudeWithCLIScript(t, `{"loggedIn":true,"email":"a@b.c"}`, `printf '%s\n' "$REPLY"; exec sleep 30`, pidFile)
	start = time.Now()
	if got, err := a2.CheckSignIn(t.Context(), OpenSpec{}); err != nil || !got.SignedIn {
		t.Fatalf("sign-in: %+v, %v", got, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("sign-in took %v", d)
	}
}

func TestClaudeCLITimesOutWithoutAnAnswerAndKillsTheProcess(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	a, _, _, _ := claudeWithCLIScript(t, "", `echo $$ >"$PIDFILE"; cat >/dev/null; exec sleep 30`, pidFile)
	a.cliTimeout = 300 * time.Millisecond
	start := time.Now()
	if _, err := a.CheckUsage(t.Context(), OpenSpec{}); !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("error = %v, want ErrProcessFailed", err)
	}
	if _, err := a.CheckSignIn(t.Context(), OpenSpec{}); !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("sign-in error = %v, want ErrProcessFailed", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
	if !pidGone(t, pidFile) {
		t.Error("the CLI process was left running after the deadline")
	}
}

func TestClaudeCLICancellationIsTheCallersContextError(t *testing.T) {
	a, _, _, _ := claudeWithCLIScript(t, "", `cat >/dev/null; exec sleep 30`, "")
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)
	if _, err := a.CheckUsage(ctx, OpenSpec{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if _, err := a.CheckSignIn(ctx, OpenSpec{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("sign-in error = %v, want context.Canceled", err)
	}
}

func TestClaudeCLIBoundsWhatItReads(t *testing.T) {
	for name, script := range map[string]string{
		"endless lines": `cat >/dev/null; exec yes xxxxxxxxxxxxxxxxxxxxxxxx`,
		"one huge line": `cat >/dev/null; head -c 9000000 /dev/zero | tr '\0' x; exec sleep 30`,
	} {
		pidFile := filepath.Join(t.TempDir(), "pid")
		a, _, _, _ := claudeWithCLIScript(t, "", "echo $$ >\"$PIDFILE\"; "+script, pidFile)
		a.cliTimeout = 10 * time.Second
		start := time.Now()
		if _, err := a.CheckUsage(t.Context(), OpenSpec{}); !errors.Is(err, ErrUsageNotCheckable) {
			t.Errorf("%s: error = %v, want ErrUsageNotCheckable", name, err)
		}
		if d := time.Since(start); d > 8*time.Second {
			t.Errorf("%s: took %v", name, d)
		}
		if !pidGone(t, pidFile) {
			t.Errorf("%s: process left running", name)
		}
	}
}

func TestClaudeCLIProbeDropsHostCredentialsForAnAccountHome(t *testing.T) {
	secrets := []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
		"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "AWS_BEARER_TOKEN_BEDROCK", "CLAUDE_FOO_SECRET"}
	for _, k := range secrets {
		t.Setenv(k, "host-"+k)
	}
	share := false
	a, _, env, _ := claudeWithCLI(t, measuredUsageReply)
	if _, err := a.CheckUsage(t.Context(), OpenSpec{AccountHome: filepath.Join(t.TempDir(), "acct"), PrivateHome: &share}); err != nil {
		t.Fatal(err)
	}
	for _, k := range secrets {
		if v, ok := (*env)[k]; ok {
			t.Errorf("%s = %q reached an account probe: it would describe the host's login", k, v)
		}
	}
	if (*env)["PATH"] == "" {
		t.Error("PATH was dropped with the credentials")
	}
	// The built-in account's probe is unchanged: it runs as sessions do.
	a2, _, env2, _ := claudeWithCLI(t, measuredUsageReply)
	if _, err := a2.CheckUsage(t.Context(), OpenSpec{}); err != nil {
		t.Fatal(err)
	}
	if (*env2)["ANTHROPIC_API_KEY"] != "host-ANTHROPIC_API_KEY" {
		t.Errorf("built-in probe lost the allow-listed key: %v", (*env2)["ANTHROPIC_API_KEY"])
	}
}

func TestParseClaudeUsageLabelsAreTrimmedClippedAndUnique(t *testing.T) {
	long := strings.Repeat("é", 60)
	reply := `{"type":"control_response","response":{"subtype":"success","request_id":"u1","response":{"rate_limits_available":true,"rate_limits":{` +
		`"seven_day_opus":{"utilization":10,"resets_at":"2026-10-27T06:00:00+00:00"},` +
		`"model_scoped":[` +
		`{"display_name":"Opus","utilization":99},` +
		`{"display_name":"   ","utilization":5},` +
		`{"display_name":"","utilization":5},` +
		`{"display_name":"  Haiku \n x ","utilization":7},` +
		`{"display_name":"` + long + `","utilization":8}]}}}}`
	windows, err := parseClaudeUsage([]byte(reply))
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, w := range windows {
		labels = append(labels, w.Label)
	}
	want := []string{"Weekly · Opus", "Weekly · Haiku x", "Weekly · " + strings.Repeat("é", 40)}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	if windows[0].UsedPercent != 10 {
		t.Errorf("the first Opus window wins: %+v", windows[0])
	}
}
