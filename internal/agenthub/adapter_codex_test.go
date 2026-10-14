package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/codexapp"
	"devkit/internal/sandbox"
)

// TestCodexFakeAppServer is not a test. It is the app-server fakeCodexRunner
// starts: this test binary run again with DEVKIT_FAKE_CODEX naming a scenario.
// Every frame DevKit sends is appended to DEVKIT_FAKE_CODEX_LOG.
func TestCodexFakeAppServer(t *testing.T) {
	scenario := os.Getenv("DEVKIT_FAKE_CODEX")
	if scenario == "" {
		t.Skip("helper process for the Codex adapter tests")
	}
	runFakeCodex(scenario, os.Getenv("DEVKIT_FAKE_CODEX_LOG"))
	os.Exit(0)
}

func runFakeCodex(scenario, logPath string) {
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	var outMu sync.Mutex
	send := func(v any) {
		b, _ := json.Marshal(v)
		outMu.Lock()
		defer outMu.Unlock()
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	notify := func(method string, params any) { send(map[string]any{"method": method, "params": params}) }

	var mu sync.Mutex
	answers := map[string]chan json.RawMessage{}
	ask := func(id, method string, params any) json.RawMessage {
		ch := make(chan json.RawMessage, 1)
		mu.Lock()
		answers[id] = ch
		mu.Unlock()
		send(map[string]any{"id": id, "method": method, "params": params})
		return <-ch
	}
	var thread, turn string

	playTurn := func() {
		cmdItem := map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "/bin/zsh -lc 'touch x'",
			"cwd": "/work", "status": "inProgress", "commandActions": []any{map[string]any{"type": "unknown", "command": "touch x"}}}
		done := func(status string) {
			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": status, "error": nil}})
		}
		switch scenario {
		case "approval":
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			answer := ask("srv-1", "item/commandExecution/requestApproval", map[string]any{"threadId": thread, "turnId": turn,
				"itemId": "cmd-1", "command": "touch x", "availableDecisions": []any{"accept", "cancel"}})
			var d codexapp.ApprovalResponse
			_ = json.Unmarshal(answer, &d)
			cmdItem["status"] = map[bool]string{true: "completed", false: "declined"}[d.Decision == "accept"]
			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			done("completed")
		case "mcp":
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{
				"type": "mcpToolCall", "id": "call-1", "server": "devkit", "tool": "db.query", "status": "inProgress", "arguments": map[string]any{}}})
			ask("srv-2", "mcpServer/elicitation/request", map[string]any{"threadId": thread, "turnId": turn, "serverName": "devkit",
				"mode": "form", "message": "Allow db.query?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "persist": []string{"session", "always"}}})
			done("completed")
		case "interrupt":
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			// Nothing more until turn/interrupt: the command "runs".
		case "unauthorized":
			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "failed",
				"error": map[string]any{"message": "401 Unauthorized", "codexErrorInfo": "unauthorized", "additionalDetails": nil}}})
		case "usageLimit":
			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "failed",
				"error": map[string]any{"message": "You've hit your usage limit. Try again at 3:10 PM.", "codexErrorInfo": "usageLimitExceeded", "additionalDetails": nil}}})
		case "errorOnly":
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			notify("error", map[string]any{"threadId": thread, "turnId": turn, "willRetry": true,
				"error": map[string]any{"message": "reconnecting", "codexErrorInfo": nil}})
			notify("error", map[string]any{"threadId": thread, "turnId": turn, "willRetry": false,
				"error": map[string]any{"message": "usage limit", "codexErrorInfo": "usageLimitExceeded"}})
		case "die":
			os.Exit(3)
		case "garbage":
			// A line the client cannot read, with codex still running.
			outMu.Lock()
			_, _ = os.Stdout.Write([]byte("oops\n"))
			outMu.Unlock()
		case "stubborn":
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			// turn/interrupt is answered but the turn never ends.
		case "staleError":
			time.Sleep(300 * time.Millisecond)
			done("completed")
		default:
			// Another thread on the same connection finishing first must not end
			// this one.
			notify("turn/completed", map[string]any{"threadId": "th-other", "turn": map[string]any{"id": turn, "status": "completed"}})
			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			cmdItem["status"], cmdItem["exitCode"], cmdItem["aggregatedOutput"] = "completed", 0, ""
			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
			notify("item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turn, "itemId": "msg-1", "delta": "done"})
			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": "done"}})
			total := map[string]any{"inputTokens": 100, "outputTokens": 20, "totalTokens": 120}
			if scenario == "resumeUsage" {
				// A resumed thread's first report counts the turns before it.
				total = map[string]any{"inputTokens": 1100, "outputTokens": 220, "totalTokens": 1320}
			}
			notify("thread/tokenUsage/updated", map[string]any{"threadId": thread, "turnId": turn, "tokenUsage": map[string]any{
				"total": total,
				"last":  map[string]any{"inputTokens": 100, "outputTokens": 20, "totalTokens": 120}, "modelContextWindow": 1000}})
			notify("remoteControl/status/changed", map[string]any{"serverName": "secret-host", "installationId": "id"})
			done("completed")
		}
	}

	in := bufio.NewReader(os.Stdin)
	turns := 0
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		_, _ = logFile.Write(line)
		var m codexapp.Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.IsResponse() {
			mu.Lock()
			ch := answers[strings.Trim(string(m.ID), `"`)]
			mu.Unlock()
			if ch != nil {
				ch <- m.Result
			}
			continue
		}
		if m.IsNotification() {
			continue
		}
		reply := func(result any) { send(map[string]any{"id": m.ID, "result": result}) }
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch m.Method {
		case "initialize":
			if scenario == "dieAtInitialize" {
				os.Exit(3)
			}
			reply(map[string]any{"userAgent": "fake", "codexHome": os.Getenv("CODEX_HOME")})
		case "account/read":
			if scenario == "signedout" {
				reply(map[string]any{"account": nil, "requiresOpenaiAuth": true})
			} else {
				reply(map[string]any{"account": map[string]any{"type": "chatgpt", "email": "me@example.com", "planType": "plus"}, "requiresOpenaiAuth": true})
			}
		case "config/read":
			if scenario == "configReloadFails" {
				send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32603, "message": "failed to synchronize managed preferences"}})
			} else {
				reply(map[string]any{"config": map[string]any{}})
			}
		case "account/login/start":
			var login codexapp.LoginStartParams
			_ = json.Unmarshal(m.Params, &login)
			switch login.Type {
			case "chatgpt":
				reply(map[string]any{"type": "chatgpt", "loginId": "login-1", "authUrl": "https://auth.example/authorize"})
			case "chatgptDeviceCode":
				reply(map[string]any{"type": "chatgptDeviceCode", "loginId": "login-1", "verificationUrl": "https://auth.example/device", "userCode": "ABCD-1234"})
			default:
				if scenario == "badkey" {
					send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32000, "message": "invalid api key"}})
					continue
				}
				reply(map[string]any{"type": "apiKey"})
				continue
			}
			switch scenario {
			case "login":
				go func() {
					time.Sleep(50 * time.Millisecond)
					notify("account/login/completed", map[string]any{"loginId": "login-1", "success": true, "error": nil})
				}()
			case "loginDies":
				go func() {
					time.Sleep(50 * time.Millisecond)
					os.Exit(3)
				}()
			}
		case "account/login/cancel", "account/logout":
			reply(map[string]any{})
		case "thread/start":
			thread = "th-new"
			reply(map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-5.5"})
		case "thread/resume":
			thread = p.ThreadID
			reply(map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-5.5"})
		case "model/list":
			if scenario == "effort" {
				reply(map[string]any{"data": []any{
					map[string]any{"id": "gpt-5.5", "model": "gpt-5.5", "displayName": "GPT-5.5", "isDefault": true, "defaultReasoningEffort": "high", "supportedReasoningEfforts": []any{
						map[string]any{"reasoningEffort": "low"}, map[string]any{"reasoningEffort": "high"},
					}},
					map[string]any{"id": "gpt-5.5-mini", "model": "gpt-5.5-mini", "defaultReasoningEffort": "low", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "low"}}},
				}, "nextCursor": nil})
				continue
			}
			reply(map[string]any{"data": []any{
				map[string]any{"id": "gpt-5.5", "model": "gpt-5.5", "displayName": "GPT-5.5"},
				map[string]any{"id": "gpt-5.5-mini", "model": "gpt-5.5-mini", "displayName": "GPT-5.5 mini"},
				map[string]any{"id": "internal", "model": "internal", "hidden": true},
			}, "nextCursor": nil})
		case "turn/start":
			turns++
			turn = "tu-" + string(rune('0'+turns))
			switch scenario {
			case "startFails":
				time.Sleep(200 * time.Millisecond)
				send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32000, "message": "no turn for you"}})
				continue
			case "staleError":
				// An older turn's fatal error lands before this turn's id does.
				notify("error", map[string]any{"threadId": thread, "turnId": "tu-old", "willRetry": false,
					"error": map[string]any{"message": "stale failure", "codexErrorInfo": nil}})
			}
			reply(map[string]any{"turn": map[string]any{"id": turn, "status": "inProgress"}})
			go playTurn()
		case "turn/interrupt":
			reply(map[string]any{})
			if scenario != "stubborn" {
				notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "interrupted"}})
			}
		default:
			send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "fake: " + m.Method}})
		}
	}
}

// fakeCodexRunner starts TestCodexFakeAppServer in place of the jail, after
// checking the policy the way the real runner would.
type fakeCodexRunner struct {
	scenario string
	log      string

	mu     sync.Mutex
	policy sandbox.Policy
	cmd    sandbox.Cmd
	procs  []*exec.Cmd
}

func (r *fakeCodexRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.policy, r.cmd = p, c
	r.mu.Unlock()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCodexFakeAppServer$")
	// GORACE: under -race the helper would otherwise sleep a second on exit,
	// and every Close now waits for the process to exit on its own.
	cmd.Env = append(os.Environ(), "DEVKIT_FAKE_CODEX="+r.scenario, "DEVKIT_FAKE_CODEX_LOG="+r.log, "CODEX_HOME="+p.Env["CODEX_HOME"], "GORACE=atexit_sleep_ms=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	proc := sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{})
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.procs = append(r.procs, cmd)
	r.mu.Unlock()
	return proc, nil
}

// reaped reports whether every process the runner started has been waited
// for: a Kill with no Wait leaves a zombie for the life of the app.
func (r *fakeCodexRunner) reaped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cmd := range r.procs {
		if cmd.ProcessState == nil {
			return false
		}
	}
	return len(r.procs) > 0
}

func (r *fakeCodexRunner) started() (sandbox.Policy, sandbox.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.policy, r.cmd
}

// frames reads back what DevKit sent the fake, in order.
func (r *fakeCodexRunner) frames(t *testing.T) []codexapp.Message {
	t.Helper()
	raw, err := os.ReadFile(r.log)
	if err != nil {
		t.Fatalf("nothing was sent: %v", err)
	}
	var out []codexapp.Message
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m codexapp.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("frame %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func (r *fakeCodexRunner) sent(t *testing.T, method string) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, m := range r.frames(t) {
		if m.Method == method {
			out = append(out, m.Params)
		}
	}
	return out
}

type codexFixture struct {
	adapter *CodexAdapter
	runner  *fakeCodexRunner
	home    string
	cwd     string
}

func newCodexFixture(t *testing.T, scenario string) codexFixture {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(dir, "project")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCodexRunner{scenario: scenario, log: filepath.Join(dir, "sent.jsonl")}
	home := filepath.Join(dir, "data", "agent-homes", "codex")
	return codexFixture{
		adapter: NewCodexAdapter(CodexOptions{Command: bin, Home: home, Runner: runner, Version: "1.2.3"}),
		runner:  runner,
		home:    home,
		cwd:     cwd,
	}
}

func (f codexFixture) open(t *testing.T) SessionRuntime {
	t.Helper()
	rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

// updateLog collects what a runtime hands its OnUpdate.
type updateLog struct {
	mu      sync.Mutex
	updates []json.RawMessage
}

func (u *updateLog) add(update, _ json.RawMessage) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updates = append(u.updates, append(json.RawMessage(nil), update...))
}

func (u *updateLog) kinds() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []string
	for _, up := range u.updates {
		out = append(out, string(ParseSessionUpdate(up).Kind))
	}
	return out
}

func (u *updateLog) joined() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var b strings.Builder
	for _, up := range u.updates {
		b.Write(up)
		b.WriteByte('\n')
	}
	return b.String()
}

// The jail and the command line a session gets (spec §4.3): DevKit's own
// CODEX_HOME, read-write except the policy files; the user's ~/.codex
// hidden in a private session; data hidden but for this home; native features
// left available; the session's MCP server on the command line.
func TestCodexOpenJailsTheAppServer(t *testing.T) {
	f := newCodexFixture(t, "turn")
	data := filepath.Dir(filepath.Dir(f.home))
	secret := filepath.Join(data, "devkit.db")
	if err := os.MkdirAll(filepath.Join(data, "agent-homes", "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(t.TempDir(), "devkit-mcp")
	rt, err := f.adapter.Open(context.Background(), OpenSpec{
		Cwd:           f.cwd,
		DenyReadPaths: []string{secret, filepath.Join(data, "agent-homes")},
		McpServers:    []acp.McpServerStdio{{Name: "devkit", Command: proxy, Args: []string{"--socket", "/tmp/s.sock"}, Env: []acp.EnvVariable{}}},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rt.Close(context.Background())
	pol, cmd := f.runner.started()
	if !slices.Equal(pol.ReadPreferenceDomains, []string{"com.openai.codex"}) {
		t.Fatalf("Codex must read only its managed preferences: %v", pol.ReadPreferenceDomains)
	}

	if pol.Env["CODEX_HOME"] != f.home {
		t.Errorf("CODEX_HOME = %q, want %q", pol.Env["CODEX_HOME"], f.home)
	}
	if !slices.Contains(pol.ReadWritePaths, f.home) {
		t.Errorf("the home must be writable: %v", pol.ReadWritePaths)
	}
	for _, name := range []string{"config.toml", "rules", "AGENTS.md", "AGENTS.override.md"} {
		if !slices.Contains(pol.DenyWritePaths, filepath.Join(f.home, name)) {
			t.Errorf("%s must be deny-write: %v", name, pol.DenyWritePaths)
		}
	}
	host, _ := os.UserHomeDir()
	for _, hidden := range []string{filepath.Join(host, ".codex"), secret, filepath.Join(data, "agent-homes", "claude")} {
		if !slices.Contains(pol.DenyReadPaths, hidden) {
			t.Errorf("%s must be hidden: %v", hidden, pol.DenyReadPaths)
		}
	}
	if slices.Contains(pol.DenyReadPaths, filepath.Join(data, "agent-homes")) {
		t.Errorf("agent-homes must be carved out for Codex's own home: %v", pol.DenyReadPaths)
	}
	if pol.Network != sandbox.NetworkHost {
		t.Errorf("network = %q", pol.Network)
	}
	if len(pol.ListenLoopbackPorts) != 0 {
		t.Errorf("a session serves nothing: %v", pol.ListenLoopbackPorts)
	}
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY"} {
		if _, ok := pol.Env[key]; ok {
			t.Errorf("%s must not reach the jail", key)
		}
	}

	argv := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"app-server",
		`-c cli_auth_credentials_store="file"`,
		"-c check_for_update_on_startup=false",
		`-c mcp_servers.devkit.command="` + proxy + `"`,
		`-c mcp_servers.devkit.args=["--socket","/tmp/s.sock"]`,
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv lacks %s: %s", want, argv)
		}
	}

	var init codexapp.InitializeParams
	_ = json.Unmarshal(f.runner.sent(t, "initialize")[0], &init)
	if init.ClientInfo.Name != "devkit" || init.ClientInfo.Version != "1.2.3" {
		t.Errorf("clientInfo = %+v", init.ClientInfo)
	}
	if len(f.runner.sent(t, "initialized")) != 1 {
		t.Error("initialized must follow initialize")
	}
	start := f.runner.sent(t, "thread/start")
	if len(start) != 1 {
		t.Fatalf("thread/start sent %d times", len(start))
	}
	if strings.Contains(string(start[0]), "sandbox") {
		t.Errorf("thread/start must not send a sandbox mode: %s", start[0])
	}
	var ts codexapp.ThreadStartParams
	_ = json.Unmarshal(start[0], &ts)
	if ts.ApprovalPolicy != "on-request" || ts.ApprovalsReviewer != "user" || ts.Cwd != f.cwd {
		t.Errorf("thread/start = %+v", ts)
	}
	if ts.DeveloperInstructions != devkitPrompt(codexNaming, "") {
		t.Error("thread/start did not carry DevKit's developer instructions")
	}
	if rt.ACPSessionID() != "th-new" {
		t.Errorf("ACPSessionID = %q", rt.ACPSessionID())
	}
}

func TestCodexOpenRefusesWhenSignedOut(t *testing.T) {
	f := newCodexFixture(t, "signedout")
	_, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	if !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("Open = %v, want ErrAgentAuthRequired", err)
	}
	if len(f.runner.sent(t, "thread/start")) != 0 {
		t.Fatal("a signed-out session must not start a thread")
	}
}

// An API key signs in over the wire, with nothing kept: the store is
// ephemeral, and the key is on neither the command line nor in the env.
func TestCodexAPIKeyGoesOverTheWireOnly(t *testing.T) {
	f := newCodexFixture(t, "turn")
	f.adapter.opts.APIKey = func(context.Context) (string, error) { return "sk-test-123", nil }
	f.open(t)
	pol, cmd := f.runner.started()
	argv := strings.Join(cmd.Args, " ")
	if !strings.Contains(argv, `cli_auth_credentials_store="ephemeral"`) {
		t.Errorf("an API-key session must keep no credential: %s", argv)
	}
	if strings.Contains(argv, "sk-test-123") {
		t.Fatal("the key is on the command line")
	}
	for k, v := range pol.Env {
		if strings.Contains(v, "sk-test-123") {
			t.Fatalf("the key is in the jail's env as %s", k)
		}
	}
	login := f.runner.sent(t, "account/login/start")
	if len(login) != 1 || !strings.Contains(string(login[0]), `"apiKey":"sk-test-123"`) || !strings.Contains(string(login[0]), `"type":"apiKey"`) {
		t.Fatalf("account/login/start = %s", login)
	}
}

// One turn end to end: the translated updates arrive before Prompt returns,
// every turn/start carries the per-turn policy (spec §5.1), and the turn's
// token use comes back as its usage.
func TestCodexPromptRunsATurn(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})

	out, err := rt.Prompt(context.Background(), TextPrompt("make x"), nil)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q", out.StopReason)
	}
	kinds := log.kinds()
	for _, want := range []string{string(UpdateToolCall), string(UpdateToolCallUpdate), string(UpdateAgentMessageChunk), string(UpdateUsage)} {
		if !slices.Contains(kinds, want) {
			t.Errorf("no %s among %v", want, kinds)
		}
	}
	if strings.Contains(log.joined(), "secret-host") {
		t.Fatal("remoteControl/status/changed reached the transcript")
	}

	var ts codexapp.TurnStartParams
	_ = json.Unmarshal(f.runner.sent(t, "turn/start")[0], &ts)
	if ts.ThreadID != "th-new" || ts.ApprovalPolicy != "on-request" || ts.ApprovalsReviewer != "user" {
		t.Errorf("turn/start = %+v", ts)
	}
	if ts.SandboxPolicy == nil || ts.SandboxPolicy.Type != "externalSandbox" || ts.SandboxPolicy.NetworkAccess != "enabled" {
		t.Errorf("sandboxPolicy = %+v, want externalSandbox with network", ts.SandboxPolicy)
	}
	if len(ts.Input) != 1 || !strings.Contains(string(ts.Input[0]), `"text":"make x"`) {
		t.Errorf("input = %s", ts.Input)
	}

	usage, err := f.adapter.NormalizeUsage(out.Meta)
	if err != nil || usage.InputTokens != 100 || usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v, %v (meta %s)", usage, err, out.Meta)
	}
}

// Modes are DevKit's: each is an approvalPolicy on the next turn/start, and
// Full auto keeps the id the composer's bypass confirmation keys on.
func TestCodexModesSetTheNextTurnsPolicy(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	withModes := rt.(RuntimeModes)
	if m := withModes.Modes(); m.CurrentModeID != "auto" || len(m.AvailableModes) != 3 || m.AvailableModes[2].ID != "bypassPermissions" {
		t.Fatalf("Modes = %+v", m)
	}
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	configurator := rt.(RuntimeConfigurator)
	for _, mode := range []string{"auto", "bypassPermissions", "ask"} {
		if err := configurator.SetMode(context.Background(), mode); err != nil {
			t.Fatalf("SetMode(%s): %v", mode, err)
		}
		if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); err != nil {
			t.Fatalf("Prompt: %v", err)
		}
	}
	var policies []string
	for _, raw := range f.runner.sent(t, "turn/start") {
		var ts codexapp.TurnStartParams
		_ = json.Unmarshal(raw, &ts)
		policies = append(policies, ts.ApprovalPolicy)
	}
	if strings.Join(policies, ",") != "on-request,never,untrusted" {
		t.Fatalf("policies = %v", policies)
	}
	if !strings.Contains(log.joined(), `"currentModeId":"bypassPermissions"`) {
		t.Fatal("a mode change must be announced as current_mode_update")
	}
	if err := configurator.SetMode(context.Background(), "yolo"); !errors.Is(err, ErrInvalidConfigOption) {
		t.Fatalf("unknown mode: %v", err)
	}
}

// The model picker is announced once handlers arrive, lists what model/list
// offers (hidden ones left out), and a choice rides the next turn/start.
func TestCodexModelPicker(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	announced := log.joined()
	if !strings.Contains(announced, `"id":"model"`) || !strings.Contains(announced, "gpt-5.5-mini") || strings.Contains(announced, `"internal"`) {
		t.Fatalf("config options = %s", announced)
	}
	opt := ParseSessionUpdate(log.updates[0]).ConfigOptions
	if len(opt) != 1 || string(opt[0].CurrentValue) != `"gpt-5.5"` {
		t.Fatalf("parsed options = %+v", opt)
	}
	configurator := rt.(RuntimeConfigurator)
	if err := configurator.SetConfigOption(context.Background(), "model", "gpt-5.5-mini"); err != nil {
		t.Fatal(err)
	}
	if err := configurator.SetConfigOption(context.Background(), "model", "internal"); !errors.Is(err, ErrInvalidConfigOption) {
		t.Fatalf("a hidden model: %v", err)
	}
	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); err != nil {
		t.Fatal(err)
	}
	var ts codexapp.TurnStartParams
	_ = json.Unmarshal(f.runner.sent(t, "turn/start")[0], &ts)
	if ts.Model != "gpt-5.5-mini" {
		t.Fatalf("model = %q", ts.Model)
	}
}

func TestCodexEffortFollowsModelAndReachesTheFirstTurn(t *testing.T) {
	f := newCodexFixture(t, "effort")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	opts := ParseSessionUpdate(log.updates[0]).ConfigOptions
	if len(opts) != 2 || opts[0].DefaultValue != "gpt-5.5" || opts[1].DefaultValue != "high" || len(opts[0].Options[0].SupportedEfforts) != 2 {
		t.Fatalf("model/effort metadata lost: %+v", opts)
	}
	config := rt.(RuntimeConfigurator)
	if err := config.SetConfigOption(context.Background(), codexEffortOption, "medium"); !errors.Is(err, ErrInvalidConfigOption) {
		t.Fatalf("unsupported effort accepted: %v", err)
	}
	if err := config.SetConfigOption(context.Background(), codexEffortOption, "low"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); err != nil {
		t.Fatal(err)
	}
	var turn codexapp.TurnStartParams
	_ = json.Unmarshal(f.runner.sent(t, "turn/start")[0], &turn)
	if turn.Effort != "low" {
		t.Fatalf("first turn effort = %q", turn.Effort)
	}
	if err := config.SetConfigOption(context.Background(), codexEffortOption, "high"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetConfigOption(context.Background(), codexModelOption, "gpt-5.5-mini"); err != nil {
		t.Fatal(err)
	}
	opts = ParseSessionUpdate(log.updates[len(log.updates)-1]).ConfigOptions
	if string(opts[1].CurrentValue) != `"low"` || len(opts[1].Options) != 1 {
		t.Fatalf("effort did not follow model: %+v", opts)
	}
}

func TestCodexCatalogDoesNotCreateAThreadOrTurn(t *testing.T) {
	f := newCodexFixture(t, "effort")
	opts, err := f.adapter.ConfigCatalog(context.Background(), OpenSpec{})
	if err != nil || len(opts) != 2 {
		t.Fatalf("catalog = %+v, %v", opts, err)
	}
	if len(f.runner.sent(t, "thread/start")) != 0 || len(f.runner.sent(t, "turn/start")) != 0 {
		t.Fatal("catalog created a conversation or inference")
	}
}

// A command approval reaches the permission gate as a card, and the answer
// goes back as Codex's decision; with no decline on offer, Deny is cancel.
func TestCodexApprovalGoesThroughThePermissionGate(t *testing.T) {
	for option, want := range map[string]string{codexOptionAllow: "accept", codexOptionDeny: "cancel"} {
		t.Run(option, func(t *testing.T) {
			f := newCodexFixture(t, "approval")
			rt := f.open(t)
			var asked json.RawMessage
			rt.SetHandlers(RuntimeHandlers{
				OnUpdate: func(json.RawMessage, json.RawMessage) {},
				OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
					asked = params
					return selected(option), nil
				},
			})
			if _, err := rt.Prompt(context.Background(), TextPrompt("touch x"), nil); err != nil {
				t.Fatal(err)
			}
			if p := decodePermission(t, asked); p.toolCall.ToolCallID != "cmd-1" || p.SessionID != "th-new" {
				t.Fatalf("card = %+v", p)
			}
			var got string
			for _, m := range f.runner.frames(t) {
				if m.IsResponse() && string(m.ID) == `"srv-1"` {
					var d codexapp.ApprovalResponse
					_ = json.Unmarshal(m.Result, &d)
					got = d.Decision
				}
			}
			if got != want {
				t.Fatalf("decision = %q, want %q", got, want)
			}
		})
	}
}

// With nobody to ask — no permission handler — an approval fails closed.
func TestCodexApprovalWithNobodyToAskIsRefused(t *testing.T) {
	f := newCodexFixture(t, "approval")
	rt := f.open(t)
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(json.RawMessage, json.RawMessage) {}})
	if _, err := rt.Prompt(context.Background(), TextPrompt("touch x"), nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range f.runner.frames(t) {
		if m.IsResponse() && string(m.ID) == `"srv-1"` && !strings.Contains(string(m.Result), `"cancel"`) {
			t.Fatalf("answer = %s, want cancel", m.Result)
		}
	}
}

// Codex asks before a DevKit MCP tool runs; the person answers on a card.
func TestCodexMcpToolApprovalGoesThroughThePermissionGate(t *testing.T) {
	f := newCodexFixture(t, "mcp")
	rt := f.open(t)
	var card json.RawMessage
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: func(json.RawMessage, json.RawMessage) {},
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			card = params
			return selected(codexOptionAllowSession), nil
		},
	})
	if _, err := rt.Prompt(context.Background(), TextPrompt("query"), nil); err != nil {
		t.Fatal(err)
	}
	if id := decodePermission(t, card).toolCall.ToolCallID; id != "call-1" {
		t.Fatalf("card is for %q, want the open call", id)
	}
	for _, m := range f.runner.frames(t) {
		if m.IsResponse() && string(m.ID) == `"srv-2"` {
			if want := `{"action":"accept","content":null,"_meta":{"persist":"session"}}`; string(m.Result) != want {
				t.Fatalf("answer = %s, want %s", m.Result, want)
			}
			return
		}
	}
	t.Fatal("the elicitation was never answered")
}

// Stop is turn/interrupt. Codex ends the turn interrupted without completing
// the command it was running, so DevKit closes that card itself.
func TestCodexCancelInterruptsTheTurn(t *testing.T) {
	f := newCodexFixture(t, "interrupt")
	rt := f.open(t)
	var log updateLog
	started := make(chan struct{}, 1)
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u, m json.RawMessage) {
		log.add(u, m)
		if ParseSessionUpdate(u).Kind == UpdateToolCall {
			started <- struct{}{}
		}
	}})
	type result struct {
		out PromptOutcome
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := rt.Prompt(context.Background(), TextPrompt("sleep"), nil)
		done <- result{out, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}
	if err := rt.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Prompt did not return after Cancel")
	}
	if r.err != nil || r.out.StopReason != "cancelled" {
		t.Fatalf("Prompt = %+v, %v; want a cancelled stop", r.out, r.err)
	}
	if len(f.runner.sent(t, "turn/interrupt")) != 1 {
		t.Fatal("Cancel must send turn/interrupt")
	}
	if !strings.Contains(log.joined(), `"toolCallId":"cmd-1","status":"failed"`) {
		t.Fatalf("the running command's card was left open:\n%s", log.joined())
	}
	if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
		t.Fatal("an interrupt must not kill the process")
	}
}

func TestCodexFailedTurnAsksForSignIn(t *testing.T) {
	f := newCodexFixture(t, "unauthorized")
	rt := f.open(t)
	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); !errors.Is(err, ErrAgentAuthRequired) {
		t.Fatalf("Prompt = %v, want ErrAgentAuthRequired", err)
	}
}

// A usage limit is Codex refusing the turn, not Codex failing: the error
// says so (and carries Codex's own sentence, which says when it lifts), the
// session hears it as the failure banner Claude's sessionFailure fills, and
// the process is left running for the prompt after the limit resets. It used
// to reach the screen as "connection_failed — Agent process failed".
func TestCodexUsageLimitIsAFailureNotACrash(t *testing.T) {
	f := newCodexFixture(t, "usageLimit")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if !errors.Is(err, ErrAgentUsageLimit) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Prompt = %v, want ErrAgentUsageLimit and not ErrProcessFailed", err)
	}
	if got := ErrorVendor(err); got != AgentTypeCodex {
		t.Fatalf("vendor = %q, want codex", got)
	}
	if got := ErrorVendorMessage(err); got != "You've hit your usage limit. Try again at 3:10 PM." {
		t.Fatalf("vendor message = %q", got)
	}

	var failure *SessionFailure
	log.mu.Lock()
	for _, raw := range log.updates {
		if up := ParseSessionUpdate(raw); up.Failure != nil {
			failure = up.Failure
		}
	}
	log.mu.Unlock()
	if failure == nil {
		t.Fatalf("no session failure was reported:\n%s", log.joined())
	}
	if failure.ID != "codex-usage-limit" || failure.Category != "usage_limit" || failure.Severity != "error" ||
		!strings.Contains(failure.Details, "Try again at 3:10 PM") || len(failure.Actions) != 1 || failure.Actions[0] != "retry" {
		t.Fatalf("failure = %+v", *failure)
	}
	if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
		t.Fatal("a usage limit must not kill the process")
	}
}

// Each way Codex can end a turn with an error maps to the kind the UI acts
// on, with Codex's message kept for the person reading it.
func TestCodexTurnErrorKinds(t *testing.T) {
	turn := func(kind, message string) *codexapp.TurnError {
		return &codexapp.TurnError{Message: message, CodexErrorInfo: json.RawMessage(kind)}
	}
	cases := []struct {
		name    string
		in      *codexapp.TurnError
		want    error
		failure string
		action  string
	}{
		{"usage limit", turn(`"usageLimitExceeded"`, "limit"), ErrAgentUsageLimit, "codex-usage-limit", "retry"},
		{"context window", turn(`"contextWindowExceeded"`, "too long"), ErrTurnFailed, "codex-context-window", "new_session"},
		{"budget", turn(`"sessionBudgetExceeded"`, "spent"), ErrTurnFailed, "codex-session-budget", "new_session"},
		{"object-shaped kind", turn(`{"usageLimitExceeded":{"resetsAt":1}}`, "limit"), ErrAgentUsageLimit, "codex-usage-limit", "retry"},
		{"anything else", turn(`"internalServerError"`, "boom"), ErrTurnFailed, "codex-turn-failed", "retry"},
		{"no error at all", nil, ErrTurnFailed, "codex-turn-failed", "retry"},
		{"sign-in", turn(`"unauthorized"`, "401"), ErrAgentAuthRequired, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := codexTurnError(tc.in)
			if !errors.Is(err, tc.want) || errors.Is(err, ErrProcessFailed) {
				t.Fatalf("codexTurnError = %v, want %v", err, tc.want)
			}
			failure := codexTurnFailure(tc.in, 42)
			if tc.failure == "" {
				if failure != nil {
					t.Fatalf("a sign-in has its own card; got failure %+v", *failure)
				}
				return
			}
			if failure == nil || failure.ID != tc.failure || failure.Revision != 42 ||
				len(failure.Actions) != 1 || failure.Actions[0] != tc.action {
				t.Fatalf("codexTurnFailure = %+v, want id %s action %s", failure, tc.failure, tc.action)
			}
			if tc.in != nil && failure.Details != tc.in.Message {
				t.Fatalf("details = %q, want Codex's message %q", failure.Details, tc.in.Message)
			}
		})
	}
}

// An error Codex will retry is only logged; one it will not ends the turn,
// even when no turn/completed follows it.
func TestCodexErrorWithoutCompletionEndsTheTurn(t *testing.T) {
	defer func(old time.Duration) { codexErrorGrace = old }(codexErrorGrace)
	codexErrorGrace = 100 * time.Millisecond
	f := newCodexFixture(t, "errorOnly")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if !errors.Is(err, ErrAgentUsageLimit) || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("Prompt = %v", err)
	}
	if !strings.Contains(log.joined(), `"toolCallId":"cmd-1","status":"failed"`) {
		t.Fatalf("the open command was left open:\n%s", log.joined())
	}
}

func TestCodexLoadResumesTheThread(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, "th-old")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	if rt.ACPSessionID() != "th-old" {
		t.Fatalf("ACPSessionID = %q", rt.ACPSessionID())
	}
	resume := f.runner.sent(t, "thread/resume")
	if len(resume) != 1 || len(f.runner.sent(t, "thread/start")) != 0 {
		t.Fatalf("resume = %s", resume)
	}
	var p codexapp.ThreadResumeParams
	_ = json.Unmarshal(resume[0], &p)
	if p.ThreadID != "th-old" || p.ApprovalPolicy != "on-request" || p.ApprovalsReviewer != "user" {
		t.Fatalf("thread/resume = %+v", p)
	}
	if p.DeveloperInstructions != devkitPrompt(codexNaming, "") {
		t.Error("thread/resume did not carry DevKit's developer instructions")
	}
	if _, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, ""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Load with no id = %v", err)
	}
}

// A process that dies mid-turn fails the turn and reports itself dead, so
// Service resumes the session on a fresh one.
func TestCodexDeadProcessFailsTheTurn(t *testing.T) {
	f := newCodexFixture(t, "die")
	rt := f.open(t)
	_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Prompt = %v, want ErrProcessFailed", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the runtime never reported itself dead")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCodexOpenFailsFastWhenTheProcessDies(t *testing.T) {
	f := newCodexFixture(t, "dieAtInitialize")
	start := time.Now()
	_, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	if !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Open = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("Open waited out the handshake timeout for a dead process")
	}
}

// Probe verifies an install in a throwaway home: the real one — and the
// sign-in in it — is never touched.
func TestCodexProbeUsesAThrowawayHome(t *testing.T) {
	f := newCodexFixture(t, "turn")
	bin := f.adapter.opts.Command
	f.adapter.opts.Command = ""
	if err := f.adapter.Probe(context.Background(), agentruntimeResolved(bin)); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, err := os.Stat(f.home); !os.IsNotExist(err) {
		t.Fatalf("Probe touched the real home: %v", err)
	}
	pol, _ := f.runner.started()
	if pol.Env["CODEX_HOME"] == f.home {
		t.Fatal("Probe ran with the real CODEX_HOME")
	}
}

func TestCodexProbeRejectsConfigReloadFailure(t *testing.T) {
	f := newCodexFixture(t, "configReloadFails")
	err := f.adapter.Probe(context.Background(), agentruntimeResolved(f.adapter.opts.Command))
	if !errors.Is(err, ErrProcessFailed) || !strings.Contains(err.Error(), "config/read") {
		t.Fatalf("Probe accepted a runtime that cannot reload config: %v", err)
	}
}

// Settings reach Codex as TOML on the command line: quoted, so a path with
// spaces or quotes cannot break out into another key.
func TestCodexArgsQuoteTOML(t *testing.T) {
	args := strings.Join(codexArgs("file", []acp.McpServerStdio{
		{Name: "devkit", Command: `/Applications/Dev Kit.app/bin/devkit-mcp`, Args: []string{`--socket`, `/tmp/a "b".sock`},
			Env: []acp.EnvVariable{{Name: "B", Value: "2"}, {Name: "A", Value: `x"y`}}},
		{Name: "bad.name", Command: "/bin/x"},
	}), "\n")
	for _, want := range []string{
		`mcp_servers.devkit.command="/Applications/Dev Kit.app/bin/devkit-mcp"`,
		`mcp_servers.devkit.args=["--socket","/tmp/a \"b\".sock"]`,
		`mcp_servers.devkit.env={"A"="x\"y","B"="2"}`,
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %s in\n%s", want, args)
		}
	}
	if strings.Contains(args, "bad.name") {
		t.Fatal("a server name that is not a bare key must not be passed")
	}
}

func TestCodexInputCarriesTextImagesAndFiles(t *testing.T) {
	in := codexInput([]acp.ContentBlock{
		{Type: "text", Text: "look"},
		{Type: "image", Data: "aGk=", MimeType: "image/png"},
		{Type: "resource", Resource: &acp.ResourceContents{URI: "file:///a.go", Text: "package a"}},
	})
	if len(in) != 2 {
		t.Fatalf("input = %s", in)
	}
	if !strings.Contains(string(in[0]), `"text":"look\n\nfile:///a.go:\npackage a"`) {
		t.Errorf("text = %s", in[0])
	}
	if string(in[1]) != `{"type":"image","url":"data:image/png;base64,aGk="}` {
		t.Errorf("image = %s", in[1])
	}
	if codexInput([]acp.ContentBlock{{Type: "image"}}) != nil {
		t.Error("nothing readable must be nothing")
	}
}

func agentruntimeResolved(bin string) agentruntime.Resolved {
	return agentruntime.Resolved{Command: bin, ReadOnlyPaths: []string{filepath.Dir(bin)}}
}

func waitSignIn(t *testing.T, results <-chan SignInResult) SignInResult {
	t.Helper()
	select {
	case r := <-results:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in never finished")
		return SignInResult{}
	}
}

// Browser sign-in (spec §6.1): Codex's own login, started in a short-lived
// app-server under the session jail with the ChatGPT store, which ends the
// moment the login completes.
func TestCodexBrowserSignIn(t *testing.T) {
	f := newCodexFixture(t, "login")
	results := make(chan SignInResult, 1)
	f.adapter.opts.OnSignInCompleted = func(r SignInResult) { results <- r }
	flow, err := f.adapter.StartBrowserSignIn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if flow.Method != SignInBrowser || flow.AuthURL != "https://auth.example/authorize" || flow.LoginID != "login-1" {
		t.Fatalf("flow = %+v", flow)
	}
	if r := waitSignIn(t, results); !r.Success || r.LoginID != "login-1" || r.Agent != AgentTypeCodex {
		t.Fatalf("result = %+v", r)
	}
	pol, cmd := f.runner.started()
	if !strings.Contains(strings.Join(cmd.Args, " "), `cli_auth_credentials_store="file"`) || pol.Env["CODEX_HOME"] != f.home {
		t.Fatalf("sign-in must write the ChatGPT store in DevKit's home: %v %v", cmd.Args, pol.Env["CODEX_HOME"])
	}
	if pol.Network != sandbox.NetworkHost {
		t.Fatal("the browser callback reaches the jail over the host loopback")
	}
	if !slices.Equal(pol.ListenLoopbackPorts, []int{1455}) {
		t.Fatalf("the sign-in must be able to serve the callback on 1455, and nothing else: %v", pol.ListenLoopbackPorts)
	}
	// account/read on the process that just signed in says "no account"
	// (Phase 0); status is read by a fresh one.
	if n := len(f.runner.sent(t, "account/read")); n != 0 {
		t.Fatalf("account/read sent %d times on the sign-in process", n)
	}
}

func TestCodexDeviceSignInShowsTheCode(t *testing.T) {
	f := newCodexFixture(t, "login")
	f.adapter.opts.OnSignInCompleted = func(SignInResult) {}
	flow, err := f.adapter.StartDeviceSignIn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if flow.Method != SignInDeviceCode || flow.UserCode != "ABCD-1234" || flow.VerificationURL == "" {
		t.Fatalf("flow = %+v", flow)
	}
}

// Cancel withdraws the login and stops the process; a timeout does the same
// on its own.
func TestCodexSignInCancelAndTimeout(t *testing.T) {
	defer func(old time.Duration) { codexSignInTimeout = old }(codexSignInTimeout)
	for _, how := range []string{"cancel", "timeout"} {
		t.Run(how, func(t *testing.T) {
			if how == "timeout" {
				codexSignInTimeout = 100 * time.Millisecond
			} else {
				codexSignInTimeout = time.Minute
			}
			f := newCodexFixture(t, "loginWait")
			results := make(chan SignInResult, 1)
			f.adapter.opts.OnSignInCompleted = func(r SignInResult) { results <- r }
			flow, err := f.adapter.StartBrowserSignIn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if how == "cancel" {
				if err := f.adapter.CancelSignIn(context.Background(), flow.LoginID); err != nil {
					t.Fatal(err)
				}
			}
			if r := waitSignIn(t, results); r.Success || r.Error == "" {
				t.Fatalf("result = %+v", r)
			}
			if cancels := f.runner.sent(t, "account/login/cancel"); len(cancels) != 1 || !strings.Contains(string(cancels[0]), "login-1") {
				t.Fatalf("account/login/cancel = %s", cancels)
			}
		})
	}
}

func TestCodexSignInStatusAndSignOut(t *testing.T) {
	f := newCodexFixture(t, "turn")
	status, err := f.adapter.SignInStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != (SignInStatus{SignedIn: true, Method: "chatgpt", Email: "me@example.com", Plan: "plus"}) {
		t.Fatalf("status = %+v", status)
	}
	if err := f.adapter.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.sent(t, "account/logout")) != 1 {
		t.Fatal("SignOut must send account/logout")
	}

	out := newCodexFixture(t, "signedout")
	if status, err := out.adapter.SignInStatus(context.Background()); err != nil || status.SignedIn {
		t.Fatalf("signed out: %+v, %v", status, err)
	}

	// An API key is what sessions sign in with, so it is the status; no
	// process is needed to say so.
	key := newCodexFixture(t, "signedout")
	key.adapter.opts.APIKey = func(context.Context) (string, error) { return "sk-1", nil }
	if status, err := key.adapter.SignInStatus(context.Background()); err != nil || status != (SignInStatus{SignedIn: true, Method: "apiKey"}) {
		t.Fatalf("api key: %+v, %v", status, err)
	}
}

// Service routes sign-in to the vendor that supports it and says plainly
// when one does not.
func TestServiceSignInRoutesByVendor(t *testing.T) {
	f := newCodexFixture(t, "turn")
	reg := NewRegistry()
	reg.Register(f.adapter)
	reg.Register(NewMockAdapter())
	svc := &Service{reg: reg}
	if status, err := svc.SignInStatus(context.Background(), AgentTypeCodex); err != nil || !status.SignedIn {
		t.Fatalf("codex: %+v, %v", status, err)
	}
	if _, err := svc.SignInStatus(context.Background(), AgentTypeMock); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("mock: %v, want ErrUnsupported", err)
	}
	if _, err := svc.StartSignIn(context.Background(), AgentTypeCodex, "telepathy"); !errors.Is(err, ErrAuthMethodUnknown) {
		t.Fatalf("unknown method: %v", err)
	}
	for _, a := range ForServerMode(f.adapter) {
		reg.Register(a)
	}
	if _, err := svc.StartSignIn(context.Background(), AgentTypeCodex, SignInBrowser); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("server mode: %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Every process DevKit starts is reaped: a closed session, a status check, a
// sign-out, a probe.
func TestCodexProcessesAreReaped(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.adapter.SignInStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "every process to be reaped", f.runner.reaped)
}

// A line the read loop cannot read ends the connection while codex is still
// running. The process must not outlive it: it is stopped and reaped, and
// the session reports itself dead so Service resumes it on a fresh one.
func TestCodexReadLoopFailureStopsTheProcess(t *testing.T) {
	f := newCodexFixture(t, "garbage")
	rt := f.open(t)
	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Prompt = %v, want ErrProcessFailed", err)
	}
	waitFor(t, "the runtime to report itself dead", func() bool { dead, _ := rt.(RuntimeLiveness).Dead(); return dead })
	waitFor(t, "the process to be reaped", f.runner.reaped)
}

// Stop on a turn Codex will not end: the watchdog kills the process, and the
// turn is still a Stop — cancelled, not a crash.
func TestCodexStopThatHasToKillIsStillAStop(t *testing.T) {
	defer func(old time.Duration) { codexInterruptWait = old }(codexInterruptWait)
	codexInterruptWait = 200 * time.Millisecond
	f := newCodexFixture(t, "stubborn")
	rt := f.open(t)
	started := make(chan struct{}, 1)
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u, _ json.RawMessage) {
		if ParseSessionUpdate(u).Kind == UpdateToolCall {
			started <- struct{}{}
		}
	}})
	errs := make(chan error, 1)
	go func() {
		_, err := rt.Prompt(context.Background(), TextPrompt("sleep"), nil)
		errs <- err
	}()
	<-started
	if err := rt.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("Prompt = %v, want ErrCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Prompt did not return")
	}
	waitFor(t, "the process to be reaped", f.runner.reaped)
}

// A fatal error about an older turn, arriving before turn/start answers,
// must not fail the turn that is starting.
func TestCodexStaleErrorDoesNotFailTheNewTurn(t *testing.T) {
	defer func(old time.Duration) { codexErrorGrace = old }(codexErrorGrace)
	codexErrorGrace = 100 * time.Millisecond
	f := newCodexFixture(t, "staleError")
	rt := f.open(t)
	out, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if err != nil || out.StopReason != "end_turn" {
		t.Fatalf("Prompt = %+v, %v; want the turn to complete", out, err)
	}
}

// Stop pressed while turn/start is in flight, and turn/start then fails:
// there is no turn to stop, and the watchdog must not kill a healthy process.
func TestCodexStopBeforeTheTurnStartedLeavesTheProcessAlone(t *testing.T) {
	defer func(old time.Duration) { codexInterruptWait = old }(codexInterruptWait)
	codexInterruptWait = 300 * time.Millisecond
	f := newCodexFixture(t, "startFails")
	rt := f.open(t)
	errs := make(chan error, 1)
	go func() {
		_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
		errs <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := rt.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; !errors.Is(err, ErrProcessFailed) {
		t.Fatalf("Prompt = %v, want the turn/start failure", err)
	}
	time.Sleep(codexInterruptWait + 300*time.Millisecond)
	if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
		t.Fatal("the watchdog killed a process with no turn running")
	}
}

// After a resume, the first usage report counts the whole thread; the turn is
// billed only for itself.
func TestCodexUsageAfterAResumeCountsOnlyTheTurn(t *testing.T) {
	f := newCodexFixture(t, "resumeUsage")
	rt, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, "th-old")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(context.Background())
	out, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := f.adapter.NormalizeUsage(out.Meta)
	if usage.InputTokens != 100 || usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v, want this turn's 100/20 (meta %s)", usage, out.Meta)
	}
}

// A sign-in whose process dies ends at once, not at the 15-minute timeout.
func TestCodexSignInEndsWhenItsProcessDies(t *testing.T) {
	f := newCodexFixture(t, "loginDies")
	results := make(chan SignInResult, 1)
	f.adapter.opts.OnSignInCompleted = func(r SignInResult) { results <- r }
	if _, err := f.adapter.StartBrowserSignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := waitSignIn(t, results); r.Success || !strings.Contains(r.Error, "exited") {
		t.Fatalf("result = %+v", r)
	}
}
