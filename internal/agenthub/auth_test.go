package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/internal/acp"
	"devkit/internal/sandbox"
)

// The shape claude-agent-acp 0.74.0 answers initialize with when
// _meta["terminal-auth"] is declared (measured against the managed runtime;
// 0.78.0 offers the same two methods, TestClaudeACPAuthMethodsLive).
func measuredAuthMethods(node, entry string) []acp.AuthMethod {
	meta := func(label string, flags ...string) json.RawMessage {
		body, _ := json.Marshal(map[string]any{"terminal-auth": map[string]any{
			"command": node,
			"args":    append([]string{entry, "--cli", "auth", "login"}, flags...),
			"label":   label,
		}})
		return body
	}
	return []acp.AuthMethod{
		{ID: "claude-ai-login", Name: "Claude Subscription", Description: "Use Claude subscription ", Type: "terminal",
			Args: []string{"--cli", "auth", "login", "--claudeai"}, Meta: meta("Claude Login", "--claudeai")},
		{ID: "console-login", Name: "Anthropic Console", Description: "Use Anthropic Console (API usage billing)", Type: "terminal",
			Args: []string{"--cli", "auth", "login", "--console"}, Meta: meta("Anthropic Console Login", "--console")},
		{ID: "gateway", Name: "Custom model gateway", Type: "gateway"},
	}
}

func TestInitializeResultParsesAuthMethods(t *testing.T) {
	wire := `{"protocolVersion":1,"authMethods":[{"id":"console-login","name":"Anthropic Console","description":"Use Anthropic Console (API usage billing)","type":"terminal","args":["--cli","auth","login","--console"],"_meta":{"terminal-auth":{"command":"/n/node","args":["/a/index.js","--cli","auth","login","--console"],"label":"Anthropic Console Login"}}}]}`
	var res acp.InitializeResult
	if err := json.Unmarshal([]byte(wire), &res); err != nil {
		t.Fatal(err)
	}
	got := resolveAuthMethods(res.AuthMethods, launch{path: "/n/node", args: []string{"/a/index.js"}})
	want := []AuthMethod{{
		ID: "console-login", Name: "Anthropic Console", Description: "Use Anthropic Console (API usage billing)",
		Label: "Anthropic Console Login", Command: "/n/node",
		Args: []string{"/a/index.js", "--cli", "auth", "login", "--console"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestResolveAuthMethodsAcceptsTheLaunchCommand(t *testing.T) {
	l := launch{path: "/opt/node/bin/node", args: []string{"/rt/0.74.0/dist/index.js"}}
	got := resolveAuthMethods(measuredAuthMethods(l.path, l.args[0]), l)

	if len(got) != 2 {
		t.Fatalf("want the two terminal methods (gateway is not a command), got %+v", got)
	}
	if got[0].ID != "claude-ai-login" || got[0].Label != "Claude Login" || got[1].Label != "Anthropic Console Login" {
		t.Fatalf("ids/labels: %+v", got)
	}
	if got[0].Description != "Use Claude subscription" {
		t.Fatalf("description not trimmed: %q", got[0].Description)
	}
	for _, m := range got {
		if m.Command != l.path {
			t.Fatalf("%s: command %q, want the launch command", m.ID, m.Command)
		}
	}
	if want := []string{l.args[0], "--cli", "auth", "login", "--claudeai"}; !reflect.DeepEqual(got[0].Args, want) {
		t.Fatalf("args %v, want %v", got[0].Args, want)
	}
}

func TestResolveAuthMethodsRefusesAForeignCommand(t *testing.T) {
	l := launch{path: "/opt/node/bin/node", args: []string{"/rt/0.74.0/dist/index.js"}}
	cases := map[string][]acp.AuthMethod{
		// Another program entirely.
		"other binary": measuredAuthMethods("/tmp/evil", l.args[0]),
		// The right node, but running a script DevKit did not install.
		"other script": measuredAuthMethods(l.path, "/tmp/evil.js"),
		// Not absolute: whatever PATH finds first.
		"relative": measuredAuthMethods("node", l.args[0]),
	}
	for name, raw := range cases {
		got := resolveAuthMethods(raw, l)
		if len(got) != 2 {
			t.Fatalf("%s: a refused command drops the method: %+v", name, got)
		}
		for _, m := range got {
			if m.Command != l.path {
				t.Fatalf("%s: ran %q", name, m.Command)
			}
			// Fallback: the launch command with the method's own args appended.
			if m.Args[0] != l.args[0] || strings.Contains(strings.Join(m.Args, " "), "evil") {
				t.Fatalf("%s: args %v", name, m.Args)
			}
		}
		if want := []string{l.args[0], "--cli", "auth", "login", "--console"}; !reflect.DeepEqual(got[1].Args, want) {
			t.Fatalf("%s: fallback args %v, want %v", name, got[1].Args, want)
		}
	}
}

func TestResolveAuthMethodsWithoutMetaUsesArgs(t *testing.T) {
	l := launch{path: "/bin/claude-agent-acp"}
	got := resolveAuthMethods([]acp.AuthMethod{
		{ID: "claude-login", Name: "Log in with Claude", Type: "terminal", Args: []string{"--cli"}},
		{ID: "agent-auth", Name: "Agent", Type: ""},
	}, l)
	if len(got) != 1 || got[0].Label != "Log in with Claude" || !reflect.DeepEqual(got[0].Args, []string{"--cli"}) {
		t.Fatalf("got %+v", got)
	}
}

// scriptedAgentRunner starts a shell that answers each JSON-RPC request with
// the next canned line, then holds stdin open like a live agent. Request ids
// are 1, 2, … per connection, so the lines carry them in order.
type scriptedAgentRunner struct {
	replies []string
	starts  *int
	log     string
}

func (r scriptedAgentRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	if r.starts != nil {
		*r.starts++
	}
	var script strings.Builder
	for _, reply := range r.replies {
		if reply == "__refuse_once__" {
			script.WriteString("read line; ")
			if r.log != "" {
				fmt.Fprintf(&script, "printf '%%s\\n' \"$line\" >> '%s'; ", r.log)
			}
			script.WriteString("id=$(printf '%s' \"$line\" | sed -n 's/.*\\\"id\\\":\\([0-9][0-9]*\\).*/\\1/p'); printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":%s,\\\"error\\\":{\\\"code\\\":-32603,\\\"message\\\":\\\"The provider is unavailable: 503\\\"}}\\n' \"$id\"; ")
			script.WriteString("read line; ")
			if r.log != "" {
				fmt.Fprintf(&script, "printf '%%s\\n' \"$line\" >> '%s'; ", r.log)
			}
			script.WriteString("id=$(printf '%s' \"$line\" | sed -n 's/.*\\\"id\\\":\\([0-9][0-9]*\\).*/\\1/p'); printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"method\\\":\\\"session/update\\\",\\\"params\\\":{\\\"sessionId\\\":\\\"ses_conformance\\\",\\\"update\\\":{\\\"sessionUpdate\\\":\\\"agent_message_chunk\\\",\\\"content\\\":{\\\"type\\\":\\\"text\\\",\\\"text\\\":\\\"recovered\\\"}}}}\\n'; printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":%s,\\\"result\\\":{\\\"stopReason\\\":\\\"end_turn\\\"}}\\n' \"$id\"; ")
			continue
		}
		if reply == "__hold_until_cancel__" {
			script.WriteString("read line; ")
			if r.log != "" {
				fmt.Fprintf(&script, "printf '%%s\\n' \"$line\" >> '%s'; ", r.log)
			}
			script.WriteString("prompt_id=$(printf '%s' \"$line\" | sed -n 's/.*\\\"id\\\":\\([0-9][0-9]*\\).*/\\1/p'); ")
			script.WriteString("printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":\"ses_conformance\",\"update\":{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"working\"}}}}'; ")
			script.WriteString("IFS= read -r cancel_line; ")
			if r.log != "" {
				fmt.Fprintf(&script, "printf '%%s\\n' \"$cancel_line\" >> '%s'; ", r.log)
			}
			script.WriteString("case \"$cancel_line\" in *'\"method\":\"session/cancel\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"stopReason\":\"cancelled\"}}\\n' \"$prompt_id\" ;; *) exit 2 ;; esac; exit 0; ")
			continue
		}
		script.WriteString("read line; ")
		if r.log != "" {
			fmt.Fprintf(&script, "printf '%%s\\n' \"$line\" >> '%s'; ", r.log)
		}
		fmt.Fprintf(&script, "printf '%%s\\n' '%s'; ", reply)
	}
	script.WriteString("cat >/dev/null")
	cmd := exec.Command("sh", "-c", script.String())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{}), nil
}

func fakeAgentBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// A signed-out agent answers session/new with ACP's authRequired and stays
// alive. That used to read as "Agent process failed"; it is a sign-in.
func TestOpenMapsAuthRequiredOnSessionNew(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: scriptedAgentRunner{replies: []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"Authentication required"}}`,
	}}})
	_, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("want ErrAgentAuthRequired, got %v", err)
	}
	if errors.Is(err, ErrProcessFailed) {
		t.Fatalf("an auth error must not also read as a crash: %v", err)
	}
}

func TestLoadMapsAuthRequiredOnSessionLoad(t *testing.T) {
	a := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: scriptedAgentRunner{replies: []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"Authentication required"}}`,
	}}})
	_, err := a.Load(context.Background(), OpenSpec{Cwd: t.TempDir()}, "vendor-session")
	if !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("want ErrAgentAuthRequired, got %v", err)
	}
}

func TestAuthMethodsProbesOnceAndResolvesSignIn(t *testing.T) {
	bin := fakeAgentBinary(t)
	starts := 0
	// The override command has no args, so terminal-auth must name bin itself.
	reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"authMethods":[`+
		`{"id":"console-login","name":"Anthropic Console","type":"terminal","args":["--cli","auth","login","--console"],"_meta":{"terminal-auth":{"command":%q,"args":["--cli","auth","login","--console"],"label":"Anthropic Console Login"}}}]}}`, bin)
	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedAgentRunner{replies: []string{reply}, starts: &starts}})

	methods, err := a.AuthMethods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0].Command != bin || methods[0].Label != "Anthropic Console Login" {
		t.Fatalf("methods = %+v", methods)
	}

	cmd, err := a.SignInCommand(context.Background(), "console-login")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != bin || !reflect.DeepEqual(cmd.Args, []string{"--cli", "auth", "login", "--console"}) || len(cmd.Env) == 0 {
		t.Fatalf("sign-in command = %+v", cmd)
	}
	if starts != 1 {
		t.Fatalf("agent started %d times; listing then starting a sign-in should probe once", starts)
	}

	if _, err := a.SignInCommand(context.Background(), "made-up"); !errors.Is(err, ErrAuthMethodUnknown) {
		t.Fatalf("unknown method: %v", err)
	}
}

func TestIsAuthRequired(t *testing.T) {
	for _, err := range []error{
		&acp.RPCError{Code: -32000, Message: "Authentication required"},
		fmt.Errorf("session/new: %w", &acp.RPCError{Code: -32000, Message: "Authentication required"}),
		&acp.RPCError{Code: -32603, Message: "Internal error", Data: json.RawMessage(`{"errorKind":"authentication_failed"}`)},
	} {
		if !isAuthRequired(err) {
			t.Fatalf("%v: not recognised as auth required", err)
		}
	}
	for _, err := range []error{
		fmt.Errorf("plain"),
		&acp.RPCError{Code: -32603, Message: "Internal error"},
		&acp.RPCError{Code: -32002, Message: "Resource not found"},
	} {
		if isAuthRequired(err) {
			t.Fatalf("%v: wrongly recognised as auth required", err)
		}
	}
}

func TestSignInEnvKeepsTheUsersEnvAndDropsDevKitsOwn(t *testing.T) {
	t.Setenv("DEVKIT_CLAUDE_ACP_COMMAND", "/secret/override")
	t.Setenv("HOME_TEST_MARKER", "kept")
	t.Setenv("PATH", "/usr/bin")
	// Checked per entry, and never printed: the test's own environment carries
	// tokens, and MANPATH= would match a substring search for PATH=.
	var paths []string
	marker := false
	for _, kv := range signInEnv(map[string]string{"PATH": "/opt/node/bin:/usr/bin"}) {
		key, value, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(key, "DEVKIT_"):
			t.Fatalf("DevKit's own %s leaked into the login", key)
		case key == "PATH":
			paths = append(paths, value)
		case kv == "HOME_TEST_MARKER=kept":
			marker = true
		}
	}
	if !marker {
		t.Fatal("the user's environment was not inherited")
	}
	if len(paths) != 1 || paths[0] != "/opt/node/bin:/usr/bin" {
		t.Fatalf("PATH must be the launch PATH, once: %q", paths)
	}
}

// A DevKit started from a terminal inside Claude Code carries its markers; a
// login that inherited them would think it runs nested in that session. The
// user's own Claude Code configuration still passes through.
func TestSignInEnvDropsNestedClaudeMarkers(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	seen := map[string]bool{}
	for _, kv := range signInEnv(map[string]string{"PATH": "/opt/node/bin:/usr/bin"}) {
		key, _, _ := strings.Cut(kv, "=")
		seen[key] = true
	}
	if seen["CLAUDECODE"] || seen["CLAUDE_CODE_ENTRYPOINT"] {
		t.Fatal("a nested-session marker reached the login")
	}
	if !seen["CLAUDE_CODE_USE_BEDROCK"] {
		t.Fatal("the user's own Claude Code configuration was dropped")
	}
}

func TestSignInCommandForPutsTheAccountHomeInTheLoginEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/the/hosts/own")
	bin := fakeAgentBinary(t)
	starts := 0
	reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"authMethods":[`+
		`{"id":"console-login","name":"Anthropic Console","type":"terminal","args":["--cli","auth","login","--console"],"_meta":{"terminal-auth":{"command":%q,"args":["--cli","auth","login","--console"],"label":"Anthropic Console Login"}}}]}}`, bin)
	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedAgentRunner{replies: []string{reply}, starts: &starts}})

	home := filepath.Join(t.TempDir(), "work")
	cmd, err := a.SignInCommandFor(context.Background(), "console-login", home)
	if err != nil {
		t.Fatal(err)
	}
	releaseLater(t, cmd)
	var got []string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			got = append(got, kv)
		}
	}
	if len(got) != 1 || got[0] != "CLAUDE_CONFIG_DIR="+home {
		t.Fatalf("CLAUDE_CONFIG_DIR entries = %v, want exactly the account's home (the host's own value must lose)", got)
	}
	if n := len(cmd.Args); n < 4 || cmd.Args[n-4] != "--cli" || cmd.Args[n-3] != "auth" || cmd.Args[n-2] != "login" {
		t.Errorf("args = %v, want the adapter's --cli auth login ...", cmd.Args)
	}

	plain, err := a.SignInCommand(context.Background(), "console-login")
	if err != nil {
		t.Fatal(err)
	}
	releaseLater(t, plain)
	for _, kv := range plain.Env {
		if kv == "CLAUDE_CONFIG_DIR="+home {
			t.Error("the built-in sign-in must not pick up an account's home")
		}
	}
	if _, err := a.SignInCommandFor(context.Background(), "made-up", home); !errors.Is(err, ErrAuthMethodUnknown) {
		t.Errorf("unknown method: %v, want ErrAuthMethodUnknown", err)
	}
	// A relative home would put the login wherever the terminal happens to be.
	if _, err := a.SignInCommandFor(context.Background(), "console-login", "relative/home"); !errors.Is(err, ErrAccountHome) {
		t.Errorf("relative home: %v, want ErrAccountHome", err)
	}
}

// The built-in sign-in keeps the environment it always had, the host's own
// CLAUDE_CONFIG_DIR included.
func TestSignInCommandWithoutAHomeLeavesTheHostsConfigDirAlone(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/the/hosts/own")
	bin := fakeAgentBinary(t)
	starts := 0
	reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"authMethods":[`+
		`{"id":"console-login","name":"Anthropic Console","type":"terminal","args":["--cli","auth","login","--console"],"_meta":{"terminal-auth":{"command":%q,"args":["--cli","auth","login","--console"],"label":"Anthropic Console Login"}}}]}}`, bin)
	a := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: scriptedAgentRunner{replies: []string{reply}, starts: &starts}})
	cmd, err := a.SignInCommand(context.Background(), "console-login")
	if err != nil {
		t.Fatal(err)
	}
	releaseLater(t, cmd)
	found := false
	for _, kv := range cmd.Env {
		if kv == "CLAUDE_CONFIG_DIR=/the/hosts/own" {
			found = true
		}
	}
	if !found {
		t.Error("the built-in sign-in keeps the environment it always had")
	}
}

// releaseLater releases a sign-in command's lease when the test ends (a command with
// no lease has nothing to release).
func releaseLater(t *testing.T, cmd SignInCommand) {
	t.Helper()
	if cmd.Release != nil {
		t.Cleanup(cmd.Release)
	}
}
