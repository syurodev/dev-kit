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
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/preferences"
	"devkit/internal/sandbox"
)

func openCodeBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func newOpenCodeTestAdapter(t *testing.T, runner SandboxRunner) (*OpenCodeAdapter, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "agent-homes", "opencode")
	return NewOpenCodeAdapter(OpenCodeOptions{Command: openCodeBinary(t), Home: home, Runner: runner}), home
}

func TestOpenCodeSandboxPolicy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/the/user/keeps/config")
	t.Setenv("OPENAI_API_KEY", "sk-must-not-reach-opencode")
	a, home := newOpenCodeTestAdapter(t, nil)
	proj := t.TempDir()
	extra := t.TempDir()

	pol, cmd, err := a.buildSandbox(OpenSpec{Cwd: proj, ExtraReadWritePaths: []string{extra}, Brief: "BRIEF-MARKER"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cmd.Args, []string{"acp"}) || !filepath.IsAbs(cmd.Path) {
		t.Fatalf("cmd = %+v, want the absolute binary with `acp`", cmd)
	}
	if pol.Network != sandbox.NetworkHost || !pol.AllowLoopbackListeners {
		t.Fatalf("policy = %+v: opencode acp serves a private API on loopback", pol)
	}
	if !pol.ReadAncestors {
		t.Fatal("opencode walks up through every directory above the project; a stat-only grant fails every session/new under /Users")
	}

	// Its home is DevKit's, never the user's.
	for key, want := range map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
	} {
		if pol.Env[key] != want {
			t.Errorf("%s = %q, want %q", key, pol.Env[key], want)
		}
	}
	// A private-home session has an empty home of DevKit's, not the host's.
	if pol.Env["HOME"] != filepath.Join(home, "home") {
		t.Errorf("HOME = %q, want DevKit's empty home under %s", pol.Env["HOME"], home)
	}
	if pol.Env["OPENCODE_DISABLE_CLAUDE_CODE"] != "1" {
		t.Error("opencode may read the user's Claude Code files")
	}
	if _, leaked := pol.Env["OPENAI_API_KEY"]; leaked {
		t.Error("a provider key from the host reached a private-home session")
	}
	if !slices.Contains(pol.ReadWritePaths, home) || !slices.Contains(pol.ReadWritePaths, extra) {
		t.Errorf("ReadWritePaths = %v, want the home and the extra folder", pol.ReadWritePaths)
	}
	// What steers opencode must not be writable by it.
	for _, dir := range []string{filepath.Join(home, "config", "opencode"), filepath.Join(home, "prompts")} {
		if !slices.Contains(pol.DenyWritePaths, dir) {
			t.Errorf("DenyWritePaths = %v, missing %s", pol.DenyWritePaths, dir)
		}
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v, want a 0700 directory", dir, info, err)
		}
	}

	// DevKit's settings ride on the highest-precedence layer.
	var cfg struct {
		Update       string              `json:"update"`
		Share        string              `json:"share"`
		Instructions []string            `json:"instructions"`
		Permissions  []map[string]string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(pol.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if cfg.Update != "disable" || cfg.Share != "disabled" {
		t.Errorf("update=%q share=%q: opencode must not update itself or publish a session", cfg.Update, cfg.Share)
	}
	asked := map[string]string{}
	for _, rule := range cfg.Permissions {
		asked[rule["action"]] = rule["effect"]
	}
	if asked["shell"] != "ask" || asked["edit"] != "ask" {
		t.Errorf("permissions = %v, want shell and edit to ask", cfg.Permissions)
	}
	if len(cfg.Instructions) != 1 || filepath.Dir(cfg.Instructions[0]) != filepath.Join(home, "prompts") {
		t.Fatalf("instructions = %v, want one file in the prompts directory", cfg.Instructions)
	}
	prompt, err := os.ReadFile(cfg.Instructions[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "BRIEF-MARKER") || !strings.Contains(string(prompt), "tools.devkit.") {
		t.Errorf("the instructions file lacks the brief or opencode's tool names:\n%s", prompt)
	}
}

func TestOpenCodeSharedHomeKeepsGhAndGlabLogins(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, dir := range []string{"gh", "glab"} {
		if err := os.MkdirAll(filepath.Join(host, ".config", dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := newOpenCodeTestAdapter(t, nil)
	shared := false
	pol, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir(), PrivateHome: &shared})
	if err != nil {
		t.Fatal(err)
	}
	// XDG_CONFIG_HOME is moved for opencode; the CLIs must still find their login.
	if pol.Env["GH_CONFIG_DIR"] != filepath.Join(host, ".config", "gh") || pol.Env["GLAB_CONFIG_DIR"] != filepath.Join(host, ".config", "glab") {
		t.Fatalf("gh/glab dirs = %q %q", pol.Env["GH_CONFIG_DIR"], pol.Env["GLAB_CONFIG_DIR"])
	}

	private, _, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := private.Env["GH_CONFIG_DIR"]; set {
		t.Fatal("a private home was given the host's gh login")
	}
	// A shared home keeps the host's HOME (the agent's tools expect their config there).
	if pol.Env["HOME"] == private.Env["HOME"] || strings.Contains(pol.Env["HOME"], "agent-homes") {
		t.Fatalf("shared HOME = %q, private HOME = %q", pol.Env["HOME"], private.Env["HOME"])
	}
}

func TestOpenCodeWithNothingToRunIsNotInstalled(t *testing.T) {
	a := NewOpenCodeAdapter(OpenCodeOptions{Home: filepath.Join(t.TempDir(), "h")})
	_, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("err = %v, want ErrAgentNotInstalled", err)
	}
	var vendor *VendorError
	if !errors.As(err, &vendor) || vendor.Agent != AgentTypeOpenCode {
		t.Fatalf("err = %v, want it to name opencode", err)
	}

	a = NewOpenCodeAdapter(OpenCodeOptions{Home: filepath.Join(t.TempDir(), "h"), Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{}, agentruntime.ErrNotInstalled
	}})
	if _, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()}); !errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("err = %v, want ErrAgentNotInstalled for a runtime that is not installed", err)
	}
}

func TestOpenCodeRuntimeLaunchRunsAcp(t *testing.T) {
	bin := openCodeBinary(t)
	released := 0
	a := NewOpenCodeAdapter(OpenCodeOptions{Home: filepath.Join(t.TempDir(), "h"), Runtime: func() (agentruntime.Resolved, error) {
		return agentruntime.Resolved{Command: bin, ReadOnlyPaths: []string{filepath.Dir(bin)}, Release: func() { released++ }}, nil
	}})
	pol, cmd, err := a.buildSandbox(OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != bin || !slices.Equal(cmd.Args, []string{"acp"}) {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !slices.Contains(pol.ReadOnlyPaths, filepath.Dir(bin)) {
		t.Fatalf("ReadOnlyPaths = %v, want the version directory", pol.ReadOnlyPaths)
	}
	if released != 1 {
		t.Fatalf("lease released %d times, want once", released)
	}
}

// recordingRunner is an agent that answers each request with the next canned
// reply and writes every request it receives to a file.
type recordingRunner struct {
	replies []string
	log     string
}

func (r recordingRunner) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	var script strings.Builder
	for _, reply := range r.replies {
		fmt.Fprintf(&script, "read line; printf '%%s\\n' \"$line\" >> '%s'; printf '%%s\\n' '%s'; ", r.log, reply)
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

func requestsOf(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("request %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func methodOf(m map[string]json.RawMessage) string {
	var s string
	_ = json.Unmarshal(m["method"], &s)
	return s
}

const openCodeInit = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":false},"promptCapabilities":{"embeddedContext":true,"image":true},"sessionCapabilities":{"additionalDirectories":{},"close":{},"delete":{},"fork":{},"list":{},"resume":{}}},"authMethods":[{"id":"opencode-login","name":"Login with opencode","description":"Run opencode auth login","type":"terminal","args":["--login"]}],"agentInfo":{"name":"OpenCode","version":"9.9.9"}}}`

const openCodeOptionsWire = `[{"id":"model","name":"Model","category":"model","type":"select","currentValue":"anthropic/claude","options":[{"value":"anthropic/claude","name":"anthropic/Claude"},{"value":"openai/gpt","name":"openai/GPT"}]},` +
	`{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"default","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"},{"value":"default","name":"default"}]},` +
	`{"id":"mode","name":"Session Mode","category":"mode","type":"select","currentValue":"build","options":[{"value":"build","name":"build"},{"value":"plan","name":"plan"}]}]`

func TestOpenCodeOpenSendsWhatItShould(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "requests")
	a, _ := newOpenCodeTestAdapter(t, recordingRunner{log: logFile, replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_1","configOptions":` + openCodeOptionsWire + `}}`,
	}})
	proj, extra := t.TempDir(), t.TempDir()
	rt, err := a.Open(context.Background(), OpenSpec{
		Cwd:                 proj,
		ExtraReadWritePaths: []string{extra, "/not/absolute/*"},
		McpServers:          []acp.McpServerStdio{{Name: "devkit", Command: "/bin/devkit-mcp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	if rt.ACPSessionID() != "ses_1" {
		t.Fatalf("session id = %q", rt.ACPSessionID())
	}

	reqs := requestsOf(t, logFile)
	if len(reqs) != 2 || methodOf(reqs[0]) != "initialize" || methodOf(reqs[1]) != "session/new" {
		t.Fatalf("requests = %v", reqs)
	}
	var init struct {
		ClientCapabilities map[string]any `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(reqs[0]["params"], &init); err != nil {
		t.Fatal(err)
	}
	if _, claimed := init.ClientCapabilities["session"]; claimed {
		t.Error("session.compaction is declared, but DevKit has no card for compaction_update")
	}
	if _, ok := init.ClientCapabilities["elicitation"]; !ok {
		t.Error("elicitation.form is not declared: opencode's question tool would be told nobody can answer")
	}
	var created struct {
		Cwd         string   `json:"cwd"`
		Directories []string `json:"additionalDirectories"`
		MCP         []struct {
			Name string `json:"name"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(reqs[1]["params"], &created); err != nil {
		t.Fatal(err)
	}
	if created.Cwd != proj || len(created.MCP) != 1 || created.MCP[0].Name != "devkit" {
		t.Fatalf("session/new = %+v", created)
	}
	if !slices.Equal(created.Directories, []string{extra}) {
		t.Fatalf("additionalDirectories = %v, want only the absolute, wildcard-free %s", created.Directories, extra)
	}

	// DevKit's modes, not opencode's agents.
	modes := rt.(RuntimeModes).Modes()
	if modes.CurrentModeID != "ask" || len(modes.AvailableModes) != 3 {
		t.Fatalf("modes = %+v", modes)
	}
	// opencode's own "mode" option is left out of the settings.
	for _, o := range parseConfigOptions(rt.(*openCodeRuntime).configOptions) {
		if o.ID == "mode" || o.Category == "mode" {
			t.Fatalf("opencode's mode option reached the settings: %+v", o)
		}
	}
	if len(parseConfigOptions(rt.(*openCodeRuntime).configOptions)) != 2 {
		t.Fatal("model and effort should both survive")
	}
}

func TestOpenCodeLoadResumesWithoutReplay(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "requests")
	a, _ := newOpenCodeTestAdapter(t, recordingRunner{log: logFile, replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"result":{"configOptions":` + openCodeOptionsWire + `}}`,
	}})
	rt, err := a.Load(context.Background(), OpenSpec{Cwd: t.TempDir()}, "ses_old")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	reqs := requestsOf(t, logFile)
	if got := methodOf(reqs[1]); got != "session/resume" {
		t.Fatalf("load sent %q, want session/resume (session/load replays the whole history to nobody)", got)
	}
	if rt.ACPSessionID() != "ses_old" {
		t.Fatalf("session id = %q", rt.ACPSessionID())
	}
}

func TestOpenCodeLoadFallsBackToSessionLoadWithoutResume(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "requests")
	noResume := strings.Replace(openCodeInit, `,"resume":{}`, "", 1)
	a, _ := newOpenCodeTestAdapter(t, recordingRunner{log: logFile, replies: []string{
		noResume,
		`{"jsonrpc":"2.0","id":2,"result":{"configOptions":` + openCodeOptionsWire + `}}`,
	}})
	rt, err := a.Load(context.Background(), OpenSpec{Cwd: t.TempDir()}, "ses_old")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	if got := methodOf(requestsOf(t, logFile)[1]); got != "session/load" {
		t.Fatalf("load sent %q, want session/load for an agent that offers no resume", got)
	}
}

func TestOpenCodeLeavesOutDirectoriesTheAgentDoesNotTake(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "requests")
	noDirs := strings.Replace(openCodeInit, `"additionalDirectories":{},`, "", 1)
	a, _ := newOpenCodeTestAdapter(t, recordingRunner{log: logFile, replies: []string{
		noDirs,
		`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_1","configOptions":[]}}`,
	}})
	rt, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir(), ExtraReadWritePaths: []string{t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	if strings.Contains(string(requestsOf(t, logFile)[1]["params"]), "additionalDirectories") {
		t.Fatal("additionalDirectories sent to an agent that does not advertise them")
	}
}

func TestOpenCodeMapsAuthRequired(t *testing.T) {
	a, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"Authentication required"}}`,
	}})
	_, err := a.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
	if !errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want a sign-in to offer, not a crash", err)
	}
	var vendor *VendorError
	if !errors.As(err, &vendor) || vendor.Agent != AgentTypeOpenCode {
		t.Fatalf("err = %v, want it to name opencode", err)
	}
}

func TestOpenCodeProbeNeedsAWayToReopenASession(t *testing.T) {
	bin := openCodeBinary(t)
	rt := agentruntime.Resolved{Command: bin}
	good, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{openCodeInit}})
	if err := good.Probe(context.Background(), rt); err != nil {
		t.Fatalf("probe of a good agent: %v", err)
	}
	bad, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"promptCapabilities":{}}}}`,
	}})
	if err := bad.Probe(context.Background(), rt); !errors.Is(err, ErrProcessFailed) || !strings.Contains(err.Error(), "cannot reopen") {
		t.Fatalf("probe of an agent that cannot resume: %v", err)
	}
}

func TestOpenCodeSignInIsTheTerminalLoginInDevKitsHome(t *testing.T) {
	a, home := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{openCodeInit}})
	methods, err := a.AuthMethods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0].ID != "opencode-login" || !slices.Equal(methods[0].Args, []string{"acp", "--login"}) {
		t.Fatalf("methods = %+v, want `opencode acp --login`", methods)
	}
	cmd, err := a.SignInCommand(context.Background(), "opencode-login")
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "XDG_DATA_HOME="+filepath.Join(home, "data")) || !strings.Contains(env, "XDG_CONFIG_HOME="+filepath.Join(home, "config")) {
		t.Fatalf("the login does not run in DevKit's opencode home:\n%s", env)
	}
	if _, err := a.SignInCommand(context.Background(), "nope"); !errors.Is(err, ErrAuthMethodUnknown) {
		t.Fatalf("an unknown method: %v", err)
	}
}

func TestOpenCodeConfigCatalogReadsModelsAndDeletesItsScratchSession(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "requests")
	a, _ := newOpenCodeTestAdapter(t, recordingRunner{log: logFile, replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_scratch","configOptions":` + openCodeOptionsWire + `}}`,
		`{"jsonrpc":"2.0","id":3,"result":{}}`,
	}})
	options, err := a.ConfigCatalog(context.Background(), OpenSpec{Cwd: "/ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0].ID != "model" || options[0].Category != "model" || len(options[0].Options) != 2 {
		t.Fatalf("catalog = %+v", options)
	}
	current := options[0].Options[0]
	if current.Value != "anthropic/claude" || current.DefaultEffort != "default" || len(current.SupportedEfforts) != 3 {
		t.Fatalf("current model = %+v, want its effort levels", current)
	}
	if len(options[0].Options[1].SupportedEfforts) != 0 {
		t.Fatal("a model the agent is not on cannot have its efforts read without switching to it")
	}
	reqs := requestsOf(t, logFile)
	if len(reqs) != 3 || methodOf(reqs[2]) != "session/delete" {
		t.Fatalf("requests = %v, want the scratch session deleted", reqs)
	}
}

// capturePolicyRunner records the policy each process is started under.
type capturePolicyRunner struct {
	recordingRunner
	policies *[]sandbox.Policy
}

func (r capturePolicyRunner) StartPiped(ctx context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	*r.policies = append(*r.policies, p)
	return r.recordingRunner.StartPiped(ctx, p, c)
}

func TestOpenCodeConfigCatalogRunsWithItsOwnTempDirectory(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("bwrap owns Linux temp isolation")
	}
	var policies []sandbox.Policy
	a, _ := newOpenCodeTestAdapter(t, capturePolicyRunner{policies: &policies, recordingRunner: recordingRunner{log: filepath.Join(t.TempDir(), "requests"), replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_scratch","configOptions":` + openCodeOptionsWire + `}}`,
		`{"jsonrpc":"2.0","id":3,"result":{}}`,
	}}})
	shared := false
	if _, err := a.ConfigCatalog(context.Background(), OpenSpec{PrivateHome: &shared}); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].SessionTempDir == "" {
		t.Fatalf("policies = %+v, want one with a session temp directory", policies)
	}
	dir := policies[0].SessionTempDir
	if policies[0].Env["TMPDIR"] != dir {
		t.Fatalf("TMPDIR = %q, want %q: a shared home would inherit the host's", policies[0].Env["TMPDIR"], dir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the catalog read left %s behind: %v", dir, err)
	}
}

func TestOpenCodeConfigCatalogWithNoModelsSaysToSignIn(t *testing.T) {
	a, _ := newOpenCodeTestAdapter(t, scriptedAgentRunner{replies: []string{
		openCodeInit,
		`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"ses_scratch","configOptions":[]}}`,
		`{"jsonrpc":"2.0","id":3,"result":{}}`,
	}})
	if _, err := a.ConfigCatalog(context.Background(), OpenSpec{}); !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("err = %v, want ErrModelsUnavailable", err)
	}
}

// permission tests --------------------------------------------------------

func permissionRequest(kind string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"sessionId":"s","toolCall":{"toolCallId":"c","kind":%q},"options":[`+
		`{"optionId":"once","kind":"allow_once","name":"Allow once"},`+
		`{"optionId":"always","kind":"allow_always","name":"Always allow"},`+
		`{"optionId":"reject","kind":"reject_once","name":"Reject"}]}`, kind))
}

func answeredOption(t *testing.T, answer json.RawMessage) string {
	t.Helper()
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.Outcome.Outcome != "selected" {
		t.Fatalf("answer = %s, %v", answer, err)
	}
	return res.Outcome.OptionID
}

func TestOpenCodeModesDecideWhoAnswers(t *testing.T) {
	for _, tc := range []struct {
		mode, kind string
		auto       bool
	}{
		{openCodeModeAsk, "edit", false},
		{openCodeModeAsk, "execute", false},
		{openCodeModeAuto, "edit", true},
		{openCodeModeAuto, "execute", false},
		{openCodeModeAuto, "other", false},
		{openCodeModeFull, "execute", true},
		{openCodeModeFull, "edit", true},
		{openCodeModeFull, "other", true},
	} {
		rt := &openCodeRuntime{acpRuntime: &acpRuntime{}, mode: tc.mode}
		rewritten, answer := rt.permissionBeforeAsk(context.Background(), permissionRequest(tc.kind))
		if tc.auto {
			if answer == nil || answeredOption(t, answer) != "once" {
				t.Errorf("%s/%s: want DevKit to allow once itself, got rewritten=%s answer=%s", tc.mode, tc.kind, rewritten, answer)
			}
			continue
		}
		if answer != nil {
			t.Errorf("%s/%s: DevKit answered for the person: %s", tc.mode, tc.kind, answer)
		}
		if strings.Contains(string(rewritten), "allow_always") || !strings.Contains(string(rewritten), "allow_once") || !strings.Contains(string(rewritten), "reject_once") {
			t.Errorf("%s/%s: the person was shown %s, want allow-once and reject without allow-always", tc.mode, tc.kind, rewritten)
		}
	}
}

func TestOpenCodeNeverKeepsARuleForTheProject(t *testing.T) {
	always := openCodeSelected("always")
	if got := answeredOption(t, openCodeNeverAlways(always)); got != "once" {
		t.Fatalf("an always answer became %q, want once", got)
	}
	if got := answeredOption(t, openCodeNeverAlways(openCodeSelected("reject"))); got != "reject" {
		t.Fatalf("a reject became %q", got)
	}
	cancelled := json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)
	if got := openCodeNeverAlways(cancelled); string(got) != string(cancelled) {
		t.Fatalf("a cancelled question was rewritten: %s", got)
	}
}

func TestOpenCodeSetModeTellsTheWindowAndRefusesUnknown(t *testing.T) {
	rt := &openCodeRuntime{acpRuntime: &acpRuntime{}, mode: openCodeModeAsk}
	var mu sync.Mutex
	var updates []string
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u json.RawMessage, _ json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, string(u))
	}})
	if err := rt.SetMode(context.Background(), "plan"); !errors.Is(err, ErrInvalidConfigOption) {
		t.Fatalf("SetMode(plan) = %v: opencode's plan agent is not a DevKit mode (it does not stop writes)", err)
	}
	if err := rt.SetMode(context.Background(), openCodeModeFull); err != nil {
		t.Fatal(err)
	}
	if rt.Modes().CurrentModeID != openCodeModeFull {
		t.Fatalf("mode = %q", rt.Modes().CurrentModeID)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 1 || !strings.Contains(updates[0], `"current_mode_update"`) || !strings.Contains(updates[0], openCodeModeFull) {
		t.Fatalf("updates = %v", updates)
	}
}

func TestOpenCodeSetModeOnAClosedRuntime(t *testing.T) {
	rt := &openCodeRuntime{acpRuntime: &acpRuntime{closed: true}, mode: openCodeModeAsk}
	if err := rt.SetMode(context.Background(), openCodeModeAuto); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestOpenCodeFirstPromptWaitsForMCPAndHonoursCancel(t *testing.T) {
	rt := &openCodeRuntime{acpRuntime: &acpRuntime{closed: true}, mode: openCodeModeAsk, readyAt: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "hi"}}, nil)
	if !errors.Is(err, ErrCancelled) || time.Since(start) > time.Second {
		t.Fatalf("Prompt = %v after %s, want it to stop waiting when cancelled", err, time.Since(start))
	}

	// With the wait over, the prompt reaches the generic runtime (closed here).
	rt.readyAt = time.Now().Add(-time.Second)
	if _, err := rt.Prompt(context.Background(), []acp.ContentBlock{{Type: "text", Text: "hi"}}, nil); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Prompt after the wait = %v, want the generic runtime's answer", err)
	}
}

func TestOpenCodeHasReadsPresenceNotTruth(t *testing.T) {
	caps := map[string]any{
		"loadSession":         true,
		"sessionCapabilities": map[string]any{"resume": map[string]any{}, "off": false, "nothing": nil},
	}
	for path, want := range map[string]bool{
		"loadSession":                 true,
		"sessionCapabilities.resume":  true, // an empty object is how opencode says yes
		"sessionCapabilities.off":     false,
		"sessionCapabilities.nothing": false,
		"sessionCapabilities.absent":  false,
		"absent.deeper":               false,
	} {
		if got := openCodeHas(caps, strings.Split(path, ".")...); got != want {
			t.Errorf("openCodeHas(%s) = %v, want %v", path, got, want)
		}
	}
}

func TestOpenCodeNamingMatchesCodeMode(t *testing.T) {
	if got := opencodeNaming.tool("git.status"); got != "`tools.devkit.git_status`" {
		t.Fatalf("tool name = %s, want the Code Mode spelling opencode lists", got)
	}
	if namingFor(AgentTypeOpenCode).tool("pdf.list") != opencodeNaming.tool("pdf.list") {
		t.Fatal("opencode sessions do not get opencode's naming")
	}
	prompt := devkitPrompt(opencodeNaming, "")
	if !strings.Contains(prompt, "execute") || strings.Contains(prompt, "mcp__devkit__") {
		t.Fatalf("the prompt does not teach opencode's way of calling a tool:\n%s", prompt)
	}
}

// A profile switched on as a subagent for a vendor DevKit has no translator for
// must read as skipped, not active: the adapter would drop it without a word.
func TestSubagentProfilesForOpenCodeAreSkippedWithAReason(t *testing.T) {
	profiles := []preferences.AgentModelProfile{{ID: "reviewer", Name: "Reviewer", Vendor: "opencode", Model: "anthropic/claude", Subagent: true}}
	specs, states := planSubagents(profiles, AgentTypeOpenCode, nil, nil)
	if len(specs) != 0 || states[0].Status != SubagentSkipped || !strings.Contains(states[0].Reason, "opencode") {
		t.Fatalf("specs=%v states=%+v, want the profile skipped and named", specs, states)
	}
}

// opencode walks up through every directory above the project and stats what is
// inside each. On macOS the jail can only let that through for the home folder
// when the home is shared; a private-home session in a project inside it says so
// instead of failing session/new with "Agent process failed".
func TestOpenCodeInPrivateHomeNeedsAProjectOutsideTheHomeOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the restriction is Seatbelt's")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	inside := filepath.Join(home, "Workspace", "app")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	shared, private := false, true

	for name, tc := range map[string]struct {
		cwd     string
		private *bool
		refused bool
	}{
		"private home, project inside the home":  {inside, &private, true},
		"default (private), project inside":      {inside, nil, true},
		"shared home, project inside the home":   {inside, &shared, false},
		"private home, project outside the home": {outside, &private, false},
		// The jail grants the whole working directory, so the home as the project
		// is readable (and is not "inside" itself).
		"the home folder itself": {home, &private, false},
	} {
		err := openCodeNeedsSharedHome(tc.cwd, OpenSpec{Cwd: tc.cwd, PrivateHome: tc.private})
		if tc.refused {
			if !errors.Is(err, ErrProcessFailed) || !strings.Contains(err.Error(), "Share home folder") {
				t.Errorf("%s: err = %v, want a refusal that says what to turn on", name, err)
			}
		} else if err != nil {
			t.Errorf("%s: err = %v, want none", name, err)
		}
	}

	// Open refuses before it starts anything.
	a, _ := newOpenCodeTestAdapter(t, nil)
	if _, err := a.Open(context.Background(), OpenSpec{Cwd: inside, PrivateHome: &private}); !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Open = %v", err)
	}
}
