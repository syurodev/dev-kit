package agenthub

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
}

func (r cliRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	if r.args != nil {
		*r.args = append([]string(nil), c.Args...)
	}
	if r.env != nil {
		*r.env = p.Env
	}
	cmd := exec.Command("sh", "-c", `cat >"$CAP"; printf '%s\n' "$REPLY"`)
	cmd.Env = append(os.Environ(), "CAP="+r.stdin, "REPLY="+r.reply)
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
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := new([]string)
	env := new(map[string]string)
	stdin := filepath.Join(t.TempDir(), "stdin")
	return NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: cliRunner{reply: reply, args: args, env: env, stdin: stdin}}), args, env, stdin
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
		a, args, _, _ := claudeWithCLI(t, tc.reply)
		got, err := a.CheckSignIn(t.Context(), OpenSpec{})
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", name, got, err, tc.want)
		}
		if !slices.Contains(*args, "auth") || !slices.Contains(*args, "status") || !slices.Contains(*args, "--cli") {
			t.Errorf("%s: args = %v", name, *args)
		}
	}
	a, _, _, _ := claudeWithCLI(t, `not json at all`)
	if _, err := a.CheckSignIn(t.Context(), OpenSpec{}); err == nil {
		t.Error("an unreadable status answer is an error, not \"signed out\"")
	}
}
