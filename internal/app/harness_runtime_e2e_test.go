package app

// R2: real-runtime protocol evidence for the harness controls.
//
// Each test starts the ACTUAL vendor process (Claude Code through
// claude-agent-acp, `codex app-server`, opencode) through the existing adapter,
// the existing sandbox launcher and the composed harness rig (ComposeHarness, the
// real gateway and grants, the real SessionHost and MCP server, the real
// devkit-mcp stdio proxy). The model is a scripted fixture on a unix socket, and
// the vendor runs in a jail with no network at all. See harness_runtime_fixture_test.go.
//
// They skip cleanly unless the vendor is configured (CI never has them):
//
//	DEVKIT_R2_PROXY_BIN    absolute devkit-mcp (go build ./cmd/devkit-mcp)
//	DEVKIT_R2_CODEX_BIN    absolute native codex binary
//	DEVKIT_R2_CLAUDE_DIR   directory holding node_modules/@agentclientprotocol/claude-agent-acp
//	DEVKIT_R2_NODE         absolute node (Claude only)
//	DEVKIT_R2_OPENCODE_BIN absolute native opencode binary
//	DEVKIT_R2_OUT          optional directory that receives <vendor>.jsonl evidence rows
//
// Run from a scrubbed environment (the tests refuse to run with the real HOME or
// a provider/secret-looking variable):
//
//	go test -c -o $T/app.test ./internal/app && cd internal/app &&
//	  env -i HOME=$T/home PATH=/usr/bin:/bin DEVKIT_R2_... $T/app.test -test.run TestR2 -test.v

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/sandbox"
)

const e2eRelayAddr = "127.0.0.1:18080"

type permEntry struct {
	Session string   `json:"session"`
	Title   string   `json:"title"`
	Kind    string   `json:"kind"`
	Options []string `json:"options"`
}

// permLog answers the vendors' own permission requests (allow once) and keeps
// metadata only: the tool title/kind and the option kinds offered.
type permLog struct {
	mu      sync.Mutex
	entries []permEntry
	deny    bool
}

func (p *permLog) resolve(_ context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		ToolCall struct {
			Title string `json:"title"`
			Kind  string `json:"kind"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(params, &req)
	e := permEntry{Session: sessionID, Title: excerpt(req.ToolCall.Title), Kind: req.ToolCall.Kind}
	allow := ""
	for _, o := range req.Options {
		e.Options = append(e.Options, o.Kind)
		if o.Kind == "allow_once" && allow == "" {
			allow = o.OptionID
		}
	}
	if os.Getenv("DEVKIT_R2_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "R2DEBUG permission params: %.1500s\n", params)
	}
	p.mu.Lock()
	p.entries = append(p.entries, e)
	deny := p.deny
	p.mu.Unlock()
	if allow == "" || deny {
		return json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`), agenthub.ErrPermissionDenied
	}
	return json.RawMessage(fmt.Sprintf(`{"outcome":{"outcome":"selected","optionId":%q}}`, allow)), nil
}

func (p *permLog) setDeny(v bool) {
	p.mu.Lock()
	p.deny = v
	p.mu.Unlock()
}

func (p *permLog) all() []permEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]permEntry(nil), p.entries...)
}

type e2eVendor struct {
	name    string
	typ     agenthub.AgentType
	model   *e2eModel
	adapter agenthub.VendorAdapter
	runner  *e2eRunner
	tap     *e2eTap
	extra   map[string]string // InitialConfig
}

func claudeRuntime(node, entry, dir, base string) agentruntime.Resolved {
	return agentruntime.Resolved{
		Command:       node,
		Args:          []string{entry},
		ReadOnlyPaths: []string{filepath.Dir(filepath.Dir(node)), dir},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       base,
			"ANTHROPIC_API_KEY":                        "sk-ant-r2-dummy",
			"DISABLE_TELEMETRY":                        "1",
			"DISABLE_AUTOUPDATER":                      "1",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		},
	}
}

func e2eNewVendor(t *testing.T, name string) *e2eVendor {
	t.Helper()
	e2eRequireIsolation(t)
	proxy := e2eEnvPath(t, "DEVKIT_R2_PROXY_BIN")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	v := &e2eVendor{name: name}
	switch name {
	case "codex":
		v.typ, v.model = agenthub.AgentTypeCodex, newE2EModel(t, "responses")
	case "claude":
		v.typ, v.model = agenthub.AgentTypeClaude, newE2EModel(t, "anthropic")
	case "opencode":
		v.typ, v.model = agenthub.AgentTypeOpenCode, newE2EModel(t, "chat")
	}
	v.tap = newE2ETap(t, self, proxy)
	v.runner = &e2eRunner{inner: sandbox.DefaultRunner(), testBin: self, modelSock: v.model.sock, relay: e2eRelayAddr,
		extraRO: []string{proxy}, extraRW: []string{v.tap.log}}
	base := "http://" + e2eRelayAddr
	switch name {
	case "codex":
		bin := e2eEnvPath(t, "DEVKIT_R2_CODEX_BIN")
		v.runner.extraArgs = []string{"-c", `openai_base_url="` + base + `/v1"`}
		v.adapter = agenthub.NewCodexAdapter(agenthub.CodexOptions{Command: bin, Home: e2eShortDir(t, "r2codexhome"),
			APIKey: func(context.Context) (string, error) { return "sk-r2-dummy", nil }, Runner: v.runner, Version: "r2"})
	case "claude":
		dir := e2eEnvPath(t, "DEVKIT_R2_CLAUDE_DIR")
		node := e2eEnvPath(t, "DEVKIT_R2_NODE")
		entry := filepath.Join(dir, "node_modules", "@agentclientprotocol", "claude-agent-acp", "dist", "index.js")
		v.adapter = agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{Runner: v.runner, Runtime: func() (agentruntime.Resolved, error) {
			return claudeRuntime(node, entry, dir, base), nil
		}})
	case "opencode":
		bin := e2eEnvPath(t, "DEVKIT_R2_OPENCODE_BIN")
		home := e2eShortDir(t, "r2ochome")
		cfg := filepath.Join(home, "config", "opencode")
		if err := os.MkdirAll(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		provider := fmt.Sprintf(`{"providers":{"mock":{"name":"Mock","package":"@opencode/ai/providers/openai-compatible",
		"settings":{"baseURL":%q,"apiKey":"test"},"models":{"mock-1":{"name":"Mock 1"}}}}}`, base+"/v1")
		if err := os.WriteFile(filepath.Join(cfg, "opencode.jsonc"), []byte(provider), 0o600); err != nil {
			t.Fatal(err)
		}
		v.extra = map[string]string{"model": "mock/mock-1"}
		v.adapter = agenthub.NewOpenCodeAdapter(agenthub.OpenCodeOptions{Command: bin, Home: home, Runner: v.runner})
	}
	return v
}

// e2eTurn is everything one prompt did, as counters and names.
type e2eTurn struct {
	Status   string       `json:"status"`
	Requests []e2eRequest `json:"requests"`
	Tap      []tapEntry   `json:"tap"`
	Perms    []permEntry  `json:"perms"`
	Gates    []permEntry  `json:"gates"`
	Prompts  []string     `json:"prompts"` // interaction kinds put to the person
	Handled  int32        `json:"handled"` // gateway handlers that actually ran
	Err      string       `json:"err,omitempty"`
}

type e2eCase struct {
	t     *testing.T
	v     *e2eVendor
	r     *compRig
	perms *permLog
	gates *permLog
	fault *fault
	out   *os.File
}

func newE2ECase(t *testing.T, name string) *e2eCase {
	t.Helper()
	v := e2eNewVendor(t, name)
	perms, gates := &permLog{}, &permLog{}
	c := &e2eCase{t: t, v: v, perms: perms, gates: gates}
	c.r = newCompRig(t, compOptions{controls: true, liveStatus: true, realAdapters: map[agenthub.AgentType]agenthub.VendorAdapter{v.typ: v.adapter},
		proxyPath: v.tap.script, resolvePermission: perms.resolve, resolveGate: gates.resolve,
		wrapStore: func(p harnessPersistence) harnessPersistence {
			c.fault = &fault{harnessPersistence: p}
			return c.fault
		}})
	if dir := os.Getenv("DEVKIT_R2_OUT"); dir != "" {
		if f, err := os.OpenFile(filepath.Join(dir, name+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			c.out = f
			t.Cleanup(func() { f.Close() })
		}
	}
	return c
}

// row records one sanitized evidence row (counters, names, excerpts of OUR fixture's
// results; never prompts, transcripts or environment values).
func (c *e2eCase) row(id string, facts map[string]any) {
	c.t.Helper()
	facts["row"], facts["vendor"] = id, c.v.name
	b, _ := json.Marshal(facts)
	c.t.Logf("R2ROW %s", b)
	if c.out != nil {
		_, _ = c.out.Write(append(b, '\n'))
	}
}

func (c *e2eCase) create() agenthub.AgentSession {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.r.ui(), 2*time.Minute)
	defer cancel()
	sess, err := c.r.agents.CreateSession(ctx, agenthub.CreateInput{AgentType: c.v.typ, Cwd: c.r.root, WorkspaceID: c.r.wsID, InitialConfig: c.v.extra})
	if err != nil {
		c.t.Fatalf("create %s session: %v", c.v.name, err)
	}
	c.t.Cleanup(func() { _ = c.r.agents.CloseSession(context.Background(), sess.ID) })
	return sess
}

func (c *e2eCase) snapshot() (reqs, tap, perms, prompts int, handled int32, gates int) {
	return len(c.v.model.requests()), len(c.v.tap.entries()), len(c.perms.all()), len(kinds(c.r.prompter)), c.r.gate.handled.Load(), len(c.gates.all())
}

func (c *e2eCase) delta(a0, t0, p0, k0 int, h0 int32, g0 int, status string, err error) e2eTurn {
	k := kinds(c.r.prompter)
	var kk []string
	for _, x := range k[k0:] {
		kk = append(kk, string(x))
	}
	out := e2eTurn{Status: status, Requests: c.v.model.requests()[a0:], Tap: c.v.tap.entries()[t0:], Perms: c.perms.all()[p0:], Gates: c.gates.all()[g0:], Prompts: kk,
		Handled: c.r.gate.handled.Load() - h0}
	if err != nil {
		out.Err = err.Error()
	}
	return out
}

// turn runs one prompt against a script and reports what happened.
func (c *e2eCase) turn(sess agenthub.AgentSession, final string, steps ...e2eStep) e2eTurn {
	c.t.Helper()
	c.v.model.script(final, steps...)
	a0, t0, p0, k0, h0, g0 := c.snapshot()
	ctx, cancel := context.WithTimeout(c.r.ui(), 3*time.Minute)
	defer cancel()
	res, err := c.r.agents.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "r2 fixture turn"})
	time.Sleep(300 * time.Millisecond) // let the tap flush trailing notifications
	return c.delta(a0, t0, p0, k0, h0, g0, res.Status, err)
}

// offered is the DevKit tools the runtime made available to the model: the tool
// list of its last request, or (Codex, whose MCP tools live behind code mode) the
// list the fixture's own script read from the runtime's ALL_TOOLS.
func (t e2eTurn) offered() []string {
	for _, r := range t.results() {
		if _, list, ok := strings.Cut(r.Text, "E2E:TOOLS "); ok {
			line, _, _ := strings.Cut(list, "\n")
			var out []string
			for _, n := range strings.Split(strings.TrimSpace(line), ",") {
				if n != "" {
					out = append(out, "mcp__devkit__"+n)
				}
			}
			return out
		}
	}
	for i := len(t.Requests) - 1; i >= 0; i-- {
		if len(t.Requests[i].Tools) > 0 {
			return t.Requests[i].Tools
		}
	}
	return nil
}

func (t e2eTurn) results() []e2eResult {
	var out []e2eResult
	for _, r := range t.Requests {
		out = append(out, r.Results...)
	}
	return out
}

func (t e2eTurn) tapCalls(tool string) (calls, errs int) {
	for _, e := range t.Tap {
		if e.Dir == "c2s" && e.Method == "tools/call" && e.Tool == tool {
			calls++
		}
		if e.Dir == "s2c" && e.Method == "result:tools/call" && e.IsError {
			errs++
		}
	}
	return
}

func (t e2eTurn) tapMethods(dir string) map[string]int {
	m := map[string]int{}
	for _, e := range t.Tap {
		if e.Dir == dir {
			m[e.Method]++
		}
	}
	return m
}

func devkitOffered(tools []string, group string) []string {
	var out []string
	for _, n := range tools {
		if strings.Contains(strings.ToLower(n), "devkit") && strings.Contains(fold(n), fold(group)) {
			out = append(out, n)
		}
	}
	return out
}

func offeredDevkit(tools []string) int {
	n := 0
	for _, t := range tools {
		if strings.Contains(strings.ToLower(t), "devkit") || strings.Contains(fold(t), "harness_profile") {
			n++
		}
	}
	return n
}

func (c *e2eCase) summary(t e2eTurn) map[string]any {
	perms := []string{}
	for _, p := range t.Perms {
		perms = append(perms, p.Title)
	}
	res := []string{}
	for _, r := range t.results() {
		res = append(res, fmt.Sprintf("%s err=%v %dB: %s", r.Tool, r.IsError, r.Bytes, r.Excerpt))
	}
	// A bounded sample of the names the runtime offered the model, so a vendor that names
	// MCP tools differently shows up as a naming finding instead of "nothing offered".
	sample := t.offered()
	if len(sample) > 40 {
		sample = sample[:40]
	}
	return map[string]any{"status": t.Status, "err": t.Err, "sentinels": sentinels(t), "offeredNamesSample": sample, "offeredTotal": len(t.offered()), "offeredDevkit": offeredDevkit(t.offered()), "offeredVault": len(devkitOffered(t.offered(), "vault")),
		"offeredPropose": len(devkitOffered(t.offered(), "harness_profile_propose")), "steps": stepsOf(t), "results": res, "vendorPermissionAsks": perms,
		"gateConfirmations": gateTitles(t), "hubPrompts": t.Prompts, "gatewayHandlersRan": t.Handled, "mcpC2S": t.tapMethods("c2s"), "mcpS2C": t.tapMethods("s2c")}
}

var sentinelRE = regexp.MustCompile(`E2E:(LEN|NOT-OFFERED|THROW) [^\n]{0,200}`)

// sentinels are the fixture script's own bounded result lines: the length of a tool's
// result, a name the runtime did not have, or a refusal. Result bodies are not kept.
func sentinels(t e2eTurn) []string {
	out := []string{}
	for _, r := range t.results() {
		out = append(out, sentinelRE.FindAllString(r.Text, -1)...)
	}
	return out
}

func gateTitles(t e2eTurn) []string {
	out := []string{}
	for _, g := range t.Gates {
		out = append(out, g.Title)
	}
	return out
}

func stepsOf(t e2eTurn) []string {
	var s []string
	for _, r := range t.Requests {
		s = append(s, r.Step)
	}
	return s
}

// ---- the evidence ---------------------------------------------------------------------

func TestR2CodexRealRuntime(t *testing.T)    { runR2(t, "codex") }
func TestR2ClaudeRealRuntime(t *testing.T)   { runR2(t, "claude") }
func TestR2OpenCodeRealRuntime(t *testing.T) { runR2(t, "opencode") }

func mergeFacts(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}
