package app

// R2 fixture infrastructure: real vendor processes (Claude Code through
// claude-agent-acp, `codex app-server`, opencode) driven through the existing
// adapters, the existing sandbox launcher and the composed harness rig, with NO
// credentials and NO route to any network.
//
// What is added here and nothing else:
//   - a runner wrapper (e2eRunner) that tightens the policy an adapter builds:
//     NetworkNone (bwrap --unshare-net, so the process has only an empty
//     loopback), a private home, a refusal to start if the environment carries a
//     secret-looking or provider-looking variable;
//   - a loopback-to-unix relay that runs INSIDE the jail so a vendor's provider
//     base URL (http://127.0.0.1:PORT) reaches a model fixture that listens on a
//     unix socket of the host. That socket is the only thing the jail can reach;
//   - an MCP stdio tap that sits in front of the real devkit-mcp proxy and logs
//     JSON-RPC method names, tool names and result sizes, never bodies;
//   - scripted model servers speaking the Anthropic Messages, OpenAI Responses
//     and OpenAI chat wires.
//
// The helper modes live in this test binary (TestMain re-execs it), so there is
// no second runner and nothing outside _test.go files.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devkit/internal/sandbox"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__e2e_relay":
			os.Exit(e2eRelayMain(os.Args[2:]))
		case "__e2e_netprobe":
			os.Exit(e2eNetProbeMain())
		case "__e2e_tap":
			os.Exit(e2eTapMain(os.Args[2:]))
		}
	}
	os.Exit(m.Run())
}

// ---- helper modes (run inside the jail) --------------------------------------

// e2eRelayMain: __e2e_relay LISTEN UNIXSOCK -- CMD ARGS... It serves a loopback
// TCP listener that forwards every connection to a unix socket, then runs CMD.
func e2eRelayMain(args []string) int {
	if len(args) < 4 || args[2] != "--" {
		fmt.Fprintln(os.Stderr, "relay: usage")
		return 2
	}
	listen, sock, cmdline := args[0], args[1], args[3:]
	runtime.LockOSThread() // Pdeathsig is tied to the creating thread
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "relay: listen:", err)
		return 2
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				u, err := net.Dial("unix", sock)
				if err != nil {
					return
				}
				defer u.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(u, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, u); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "relay: start:", err)
		return 127
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// e2eNetProbeMain prints what a process in the jail can see of the network:
// its interfaces and the outcome of attempts to leave. Names and errors only.
func e2eNetProbeMain() int {
	out := map[string]any{}
	var ifaces []string
	if is, err := net.Interfaces(); err == nil {
		for _, i := range is {
			ifaces = append(ifaces, i.Name)
		}
	}
	out["interfaces"] = ifaces
	attempts := map[string]string{}
	for _, target := range []string{"1.1.1.1:443", "8.8.8.8:53", "[2606:4700:4700::1111]:443", "192.168.0.1:80"} {
		c, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err == nil {
			c.Close()
			attempts[target] = "CONNECTED"
			continue
		}
		attempts[target] = unwrapDialErr(err)
	}
	if _, err := net.LookupHost("api.anthropic.com"); err == nil {
		attempts["dns:api.anthropic.com"] = "RESOLVED"
	} else {
		attempts["dns:api.anthropic.com"] = "failed"
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:9", time.Second); err == nil {
		c.Close()
		attempts["127.0.0.1:9"] = "CONNECTED"
	} else {
		attempts["127.0.0.1:9"] = unwrapDialErr(err)
	}
	out["attempts"] = attempts
	var env []string
	for _, kv := range os.Environ() {
		env = append(env, strings.SplitN(kv, "=", 2)[0])
	}
	sort.Strings(env)
	out["envNames"] = env
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return 0
}

func unwrapDialErr(err error) string {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

// e2eTapMain: __e2e_tap LOG REALPROXY ARGS... Runs the real devkit-mcp proxy and
// copies stdio through it, appending one JSON line of method/tool/size facts per
// JSON-RPC message to LOG. It never records message bodies.
func e2eTapMain(args []string) int {
	if len(args) < 2 {
		return 2
	}
	logPath, real, rest := args[0], args[1], args[2:]
	sock := ""
	for i, a := range rest {
		if a == "--socket" && i+1 < len(rest) {
			sock = filepath.Base(rest[i+1])
		}
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tap:", err)
		return 2
	}
	defer f.Close()
	var logMu sync.Mutex
	note := func(e map[string]any) {
		e["sock"] = sock
		e["ms"] = time.Now().UnixMilli()
		b, _ := json.Marshal(e)
		logMu.Lock()
		_, _ = f.Write(append(b, '\n'))
		logMu.Unlock()
	}
	cmd := exec.Command(real, rest...)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	toChild, err := cmd.StdinPipe()
	if err != nil {
		return 2
	}
	fromChild, err := cmd.StdoutPipe()
	if err != nil {
		return 2
	}
	if err := cmd.Start(); err != nil {
		return 127
	}
	method := map[string]string{} // request id → method, to label responses
	var mm sync.Mutex
	pump := func(dir string, src io.Reader, dst io.Writer, done chan<- struct{}) {
		r := bufio.NewReaderSize(src, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = dst.Write(line)
				var m struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.Unmarshal(line, &m) == nil {
					e := map[string]any{"d": dir, "bytes": len(line)}
					id := string(m.ID)
					switch {
					case m.Method != "":
						e["m"] = m.Method
						if m.Params.Name != "" {
							e["tool"] = m.Params.Name
						}
						if id != "" {
							mm.Lock()
							method[id] = m.Method
							mm.Unlock()
						}
					case id != "":
						mm.Lock()
						meth := method[id]
						mm.Unlock()
						e["m"] = "result:" + meth
						e["rpcError"] = len(m.Error) > 0
						var res struct {
							Tools []struct {
								Name string `json:"name"`
							} `json:"tools"`
							IsError bool `json:"isError"`
						}
						if json.Unmarshal(m.Result, &res) == nil {
							if meth == "tools/list" {
								var names []string
								for _, t := range res.Tools {
									names = append(names, t.Name)
								}
								e["tools"] = names
							}
							if meth == "tools/call" {
								e["isError"] = res.IsError
							}
						}
					}
					note(e)
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}
	done := make(chan struct{}, 2)
	go pump("c2s", os.Stdin, toChild, done)
	go func() { pump("s2c", fromChild, os.Stdout, done) }()
	<-done
	_ = toChild.Close()
	_ = cmd.Wait()
	return 0
}

// ---- the runner wrapper ---------------------------------------------------------

type e2eLaunch struct {
	Command      string   `json:"command"`
	Network      string   `json:"network"`
	PrivateHome  bool     `json:"privateHome"`
	HomeUnderTmp bool     `json:"homeUnderTmp"`
	EnvNames     []string `json:"envNames"`
}

type e2eRunner struct {
	inner     *sandbox.Runner
	testBin   string
	modelSock string
	relay     string // loopback address the relay listens on inside the jail
	extraRO   []string
	extraRW   []string
	// extraArgs is appended to the vendor's command line (Codex: -c key=value).
	extraArgs []string

	mu       sync.Mutex
	launches []e2eLaunch
	procs    []*sandbox.Proc
}

// KillAll ends every vendor process this runner started. The evidence "restarts"
// the app inside one OS process, so the vendors the old Agent Hub started would
// otherwise outlive it; in the real app they die with it (RecoverStaleSessions
// documents that no ACP process survives a restart). It only models that exit and
// is not an explicit session Close.
func (r *e2eRunner) KillAll() {
	r.mu.Lock()
	procs := r.procs
	r.procs = nil
	r.mu.Unlock()
	for _, p := range procs {
		_ = p.Kill()
	}
	for _, p := range procs {
		select {
		case <-p.Exited():
		case <-time.After(5 * time.Second):
		}
	}
}

var secretish = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|AUTH|PROXY|_URL|ENDPOINT)`)

// fixtureEnvOK are the only variable names a vendor may carry that look like
// provider configuration: they point at the loopback relay or hold a dummy value.
var fixtureEnvOK = map[string]bool{
	"ANTHROPIC_BASE_URL": true, "ANTHROPIC_API_KEY": true,
	"OPENAI_API_KEY": true, "CODEX_API_KEY": true, "OPENAI_BASE_URL": true,
}

func (r *e2eRunner) StartPiped(ctx context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	home := p.Env["HOME"]
	if u, err := user.Current(); err == nil && home == u.HomeDir {
		return nil, errors.New("e2e: refusing to start a vendor with the real home")
	}
	underTmp := strings.HasPrefix(home, os.TempDir()+"/") || strings.HasPrefix(home, "/tmp/")
	if !underTmp {
		return nil, fmt.Errorf("e2e: HOME for a vendor must be a temporary directory, got %q", filepath.Base(home))
	}
	env := map[string]string{}
	var names []string
	for k, v := range p.Env {
		if strings.HasPrefix(k, "DEVKIT_R2_") {
			continue // the evidence rig's own configuration is not the vendor's business
		}
		if secretish.MatchString(k) && !fixtureEnvOK[k] {
			return nil, fmt.Errorf("e2e: refusing to start a vendor with a secret-looking variable %s", k)
		}
		if fixtureEnvOK[k] && !strings.HasPrefix(v, "http://127.0.0.1:") && strings.Contains(k, "URL") {
			return nil, fmt.Errorf("e2e: %s must point at the loopback relay", k)
		}
		env[k] = v
		names = append(names, k)
	}
	sort.Strings(names)
	t := true
	p.Env = env
	p.Network = sandbox.NetworkNone
	p.AllowLoopbackListeners = false
	p.PrivateHome = &t
	p.AllowUnixSockets = append(append([]string(nil), p.AllowUnixSockets...), r.modelSock)
	p.ReadOnlyPaths = append(append([]string(nil), p.ReadOnlyPaths...), append([]string{r.testBin}, r.extraRO...)...)
	p.ReadWritePaths = append(append([]string(nil), p.ReadWritePaths...), r.extraRW...)
	args := []string{"__e2e_relay", r.relay, r.modelSock, "--", c.Path}
	args = append(args, c.Args...)
	args = append(args, r.extraArgs...)
	r.mu.Lock()
	r.launches = append(r.launches, e2eLaunch{Command: filepath.Base(c.Path), Network: string(p.Network), PrivateHome: true, HomeUnderTmp: underTmp, EnvNames: names})
	r.mu.Unlock()
	proc, err := r.inner.StartPiped(ctx, p, sandbox.Cmd{Path: r.testBin, Args: args, Dir: c.Dir})
	if err == nil {
		r.mu.Lock()
		r.procs = append(r.procs, proc)
		r.mu.Unlock()
	}
	return proc, err
}

func (r *e2eRunner) Launches() []e2eLaunch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]e2eLaunch(nil), r.launches...)
}

// ---- guards the tests run first -------------------------------------------------

// e2eRequireIsolation refuses to run in a process that could hand a vendor
// anything real: the real home, a provider variable, a proxy.
func e2eRequireIsolation(t *testing.T) {
	t.Helper()
	home := os.Getenv("HOME")
	if u, err := user.Current(); err == nil && home == u.HomeDir {
		t.Skip("R2 evidence runs only with a temporary HOME (use env -i with HOME=<tmp>); the real home is never used")
	}
	for _, kv := range os.Environ() {
		k := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(k, "DEVKIT_R2_") {
			continue
		}
		if secretish.MatchString(k) || strings.Contains(strings.ToUpper(k), "ANTHROPIC") || strings.Contains(strings.ToUpper(k), "OPENAI") {
			t.Skipf("R2 evidence refuses to run with %s in the environment", k)
		}
	}
	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
}

func e2eEnvPath(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("set %s to run the real-runtime evidence for this vendor", name)
	}
	if !filepath.IsAbs(v) {
		t.Fatalf("%s must be absolute", name)
	}
	if _, err := os.Stat(v); err != nil {
		t.Skipf("%s: %v", name, err)
	}
	return v
}

// e2eShortDir is a short directory under /tmp (unix sockets are length limited).
func e2eShortDir(t *testing.T, prefix string) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// e2eProveNoNetwork runs a probe through the very wrapper every vendor launch
// goes through, asking for the host network the adapters ask for, and fails the
// test unless the process saw only a loopback and could reach nothing outside.
func e2eProveNoNetwork(t *testing.T, r *e2eRunner) map[string]any {
	t.Helper()
	dir := e2eShortDir(t, "e2eprobe")
	// a vendor-shaped policy, asking for the host network like the adapters do
	t0 := false
	pol := sandbox.Policy{WorkDir: dir, Network: sandbox.NetworkHost, AllowLoopbackListeners: true, PrivateHome: &t0,
		Env: map[string]string{"HOME": dir, "PATH": "/usr/bin:/bin"}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	proc, err := r.probe(ctx, pol)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(proc, &res); err != nil {
		t.Fatalf("probe output %q: %v", proc, err)
	}
	ifs, _ := res["interfaces"].([]any)
	if len(ifs) != 1 || ifs[0] != "lo" {
		t.Fatalf("the jail saw interfaces %v, want only lo", ifs)
	}
	for target, outcome := range res["attempts"].(map[string]any) {
		if target == "127.0.0.1:9" {
			continue // nothing listens on the jail's own loopback; refused is expected
		}
		if outcome == "CONNECTED" || outcome == "RESOLVED" {
			t.Fatalf("the jail reached %s", target)
		}
	}
	return res
}

// probe starts the netprobe mode under the same policy rewrite StartPiped applies.
func (r *e2eRunner) probe(ctx context.Context, p sandbox.Policy) ([]byte, error) {
	t := true
	p.Network = sandbox.NetworkNone
	p.AllowLoopbackListeners = false
	p.PrivateHome = &t
	p.ReadOnlyPaths = append(append([]string(nil), p.ReadOnlyPaths...), r.testBin)
	proc, err := r.inner.StartPiped(ctx, p, sandbox.Cmd{Path: r.testBin, Args: []string{"__e2e_netprobe"}})
	if err != nil {
		return nil, err
	}
	out, _ := io.ReadAll(proc.StdoutReader())
	_ = proc.Wait()
	return bytes.TrimSpace(out), nil
}

// ---- the tap log ----------------------------------------------------------------

type tapEntry struct {
	Dir      string   `json:"d"`
	Method   string   `json:"m"`
	Tool     string   `json:"tool"`
	Tools    []string `json:"tools"`
	IsError  bool     `json:"isError"`
	RPCError bool     `json:"rpcError"`
	Bytes    int      `json:"bytes"`
	Sock     string   `json:"sock"`
	Ms       int64    `json:"ms"`
}

type e2eTap struct {
	log    string
	script string
}

// newE2ETap writes the log file and a one-line proxy script. The SessionHost
// hands the script to the vendors as the devkit-mcp command: it runs the test
// binary's tap mode, which runs the real proxy.
func newE2ETap(t *testing.T, testBin, realProxy string) *e2eTap {
	t.Helper()
	dir := e2eShortDir(t, "e2etap")
	log := filepath.Join(dir, "tap.jsonl")
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "devkit-mcp")
	body := fmt.Sprintf("#!/bin/sh\nexec %q __e2e_tap %q %q \"$@\"\n", testBin, log, realProxy)
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return &e2eTap{log: log, script: script}
}

func (p *e2eTap) entries() []tapEntry {
	b, _ := os.ReadFile(p.log)
	var out []tapEntry
	for _, line := range bytes.Split(b, []byte("\n")) {
		var e tapEntry
		if len(line) > 0 && json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// count of entries with the given direction and method since index from.
func (p *e2eTap) count(from int, dir, method string) int {
	n := 0
	es := p.entries()
	for i := from; i < len(es); i++ {
		if es[i].Dir == dir && es[i].Method == method {
			n++
		}
	}
	return n
}

// ---- scripted model servers ------------------------------------------------------

type e2eStep struct {
	Tool string // matched against the end of an offered tool name, case and punctuation folded
	Args map[string]any
	// Raw makes Codex run this JavaScript instead of the generated call.
	Raw string
}

type e2eResult struct {
	Tool    string `json:"tool"`
	IsError bool   `json:"isError"`
	Excerpt string `json:"excerpt"`
	Bytes   int    `json:"bytes"`
	Tail    string `json:"tail,omitempty"`
	Text    string `json:"-"` // bounded, for the rig's own parsing; never written to evidence rows
}

type e2eRequest struct {
	Tools   []string    `json:"tools"`
	Calls   int         `json:"calls"`
	Results []e2eResult `json:"newResults"`
	Step    string      `json:"step"`
}

// e2eModel is one scripted model on a unix socket. kind is anthropic, responses
// or chat. It records, for each main request, the tool names the runtime offered
// and the tool results it sent back.
type e2eModel struct {
	kind string
	sock string
	srv  *http.Server

	mu       sync.Mutex
	steps    []e2eStep
	final    string
	baseline int
	seen     map[string]bool
	callName map[string]string
	reqs     []e2eRequest
	all      int // every request, including side requests
	nextID   int
	searched string // the step a ToolSearch was already issued for in this turn (Claude)
}

func newE2EModel(t *testing.T, kind string) *e2eModel {
	t.Helper()
	m := &e2eModel{kind: kind, seen: map[string]bool{}, callName: map[string]string{}, baseline: -1}
	dir := e2eShortDir(t, "e2emodel")
	m.sock = filepath.Join(dir, "m.sock")
	ln, err := net.Listen("unix", m.sock)
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "anthropic":
		m.srv = &http.Server{Handler: m.anthropicHandler()}
	case "responses":
		m.srv = &http.Server{Handler: m.responsesHandler()}
	default:
		m.srv = &http.Server{Handler: m.chatHandler()}
	}
	go func() { _ = m.srv.Serve(ln) }()
	t.Cleanup(func() { _ = m.srv.Close() })
	return m
}

func (m *e2eModel) script(final string, steps ...e2eStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.steps, m.final, m.baseline, m.searched = steps, final, -1, ""
}

func (m *e2eModel) requests() []e2eRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]e2eRequest(nil), m.reqs...)
}

func (m *e2eModel) id(prefix string) string {
	m.nextID++
	return fmt.Sprintf("%s_r2_%d", prefix, m.nextID)
}

// next picks the step for a conversation that already holds `done` tool calls.
func (m *e2eModel) next(done int) (e2eStep, bool) {
	if m.baseline < 0 {
		m.baseline = done
	}
	if i := done - m.baseline; i < len(m.steps) {
		return m.steps[i], true
	}
	return e2eStep{}, false
}

func fold(s string) string {
	return strings.ToLower(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(s, "_"))
}

func e2ePick(tools []string, want string) string {
	w := fold(want)
	for _, t := range tools {
		if strings.HasSuffix(fold(t), w) {
			return t
		}
	}
	return ""
}

func capText(s string) string {
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}

func (m *e2eModel) record(tools []string, calls int, results []e2eResult, step string) {
	m.reqs = append(m.reqs, e2eRequest{Tools: tools, Calls: calls, Results: results, Step: step})
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sseEvent(w http.ResponseWriter, event string, data any) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonOf(data))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// ---- Anthropic Messages ---------------------------------------------------------

func (m *e2eModel) anthropicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":100}`))
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &req)
		var tools []string
		for _, t := range req.Tools {
			tools = append(tools, t.Name)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.all++
		if !(e2ePick(tools, "bash") != "" || e2ePick(tools, "devkit") != "" || containsFold(tools, "devkit")) {
			anthropicText(w, req.Stream, "ok")
			return
		}
		calls := 0
		var results []e2eResult
		for _, rawMsg := range req.Messages {
			var msg struct {
				Content json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(rawMsg, &msg)
			var blocks []map[string]any
			if json.Unmarshal(msg.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b["type"] {
				case "tool_use":
					id, _ := b["id"].(string)
					name, _ := b["name"].(string)
					if name != "ToolSearch" { // loading a deferred tool is not one of the scripted calls
						calls++
					}
					m.callName[id] = name
				case "tool_result":
					id, _ := b["tool_use_id"].(string)
					if m.seen[id] {
						continue
					}
					m.seen[id] = true
					isErr, _ := b["is_error"].(bool)
					text := anthropicText2(b["content"])
					results = append(results, e2eResult{Tool: m.callName[id], IsError: isErr, Excerpt: excerpt(text), Bytes: len(text), Text: capText(text)})
				}
			}
		}
		step, ok := m.next(calls)
		if !ok {
			m.record(tools, calls, results, "final")
			anthropicText(w, req.Stream, m.finalText())
			return
		}
		name := e2ePick(tools, step.Tool)
		if name == "" && containsFold(tools, "toolsearch") && m.searched != step.Tool {
			// Claude Code keeps MCP tools deferred: they reach the model's tool list only after a
			// ToolSearch select. Ask for the one tool; if it is still absent afterwards it is not offered.
			m.searched = step.Tool
			m.record(tools, calls, results, "search:"+step.Tool)
			anthropicToolUse(w, "ToolSearch", map[string]any{"query": "select:mcp__devkit__" + fold(step.Tool), "max_results": 1}, m.id("toolu"))
			return
		}
		if name == "" {
			m.record(tools, calls, results, "not-offered:"+step.Tool)
			anthropicText(w, req.Stream, "E2E:NOT-OFFERED "+step.Tool)
			return
		}
		m.record(tools, calls, results, "call:"+step.Tool)
		anthropicToolUse(w, name, step.Args, m.id("toolu"))
	})
	return mux
}

func containsFold(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}

func (m *e2eModel) finalText() string {
	if m.final == "" {
		return "done"
	}
	return m.final
}

func anthropicText2(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, it := range c {
			if b, ok := it.(map[string]any); ok {
				if s, ok := b["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func anthropicStart(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	sseEvent(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_r2", "type": "message", "role": "assistant", "model": "claude-r2", "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 50, "output_tokens": 1}}})
}

func anthropicText(w http.ResponseWriter, stream bool, text string) {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonOf(map[string]any{"id": "msg_r2", "type": "message", "role": "assistant", "model": "claude-r2",
			"content": []any{map[string]any{"type": "text", "text": text}}, "stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 5, "output_tokens": 1}})))
		return
	}
	anthropicStart(w)
	sseEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	sseEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	sseEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sseEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 5}})
	sseEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func anthropicToolUse(w http.ResponseWriter, name string, args map[string]any, id string) {
	anthropicStart(w)
	sseEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}})
	sseEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": jsonOf(args)}})
	sseEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sseEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 20}})
	sseEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

// ---- OpenAI Responses (Codex) ----------------------------------------------------

// codexScript runs one tool through Codex's code mode: it folds names the same
// way the model fixture does, and prints a bounded sentinel line.
func codexScript(step e2eStep) string {
	args := jsonOf(step.Args)
	if step.Args == nil {
		args = "{}"
	}
	return `const want = ` + jsonOf(fold(step.Tool)) + `;
const fold = s => s.toLowerCase().replace(/[^a-z0-9]+/g, "_");
text("E2E:TOOLS " + ALL_TOOLS.map(t => t.name).filter(n => /devkit/i.test(n)).map(n => n.replace(/^mcp__devkit__/, "")).join(","));
const hit = ALL_TOOLS.map(t => t.name).find(n => fold(n).endsWith(want));
if (!hit) { text("E2E:NOT-OFFERED " + want); }
else { try { const r = await tools[hit](` + args + `); const body = (r.content || []).filter(c => c.type === "text").map(c => c.text).join(""); text("E2E:LEN " + body.length); text("E2E:TAIL " + body.slice(-200).replace(/\s+/g, " ")); text("E2E:RESULT " + JSON.stringify(r).slice(0, 600)); } catch (e) { text("E2E:THROW " + String(e).slice(0, 300)); } }`
}

// openCodeScript runs one tool through opencode's Code Mode. DevKit's tools are
// `tools.devkit.<name>`; the script lists what the namespace holds, then calls the
// tool only if the runtime has it, and prints bounded sentinel lines.
func openCodeScript(step e2eStep) string {
	args := jsonOf(step.Args)
	if step.Args == nil {
		args = "{}"
	}
	return `const want = ` + jsonOf(fold(step.Tool)) + `;
const fold = s => s.toLowerCase().replace(/[^a-z0-9]+/g, "_");
const ns = (tools && tools.devkit) || {};
const names = Object.keys(ns);
const out = ["E2E:TOOLS " + names.join(",")];
const hit = names.find(n => fold(n) === want);
if (!hit) { out.push("E2E:NOT-OFFERED " + want); }
else { try { const r = await ns[hit](` + args + `); const body = JSON.stringify(r); out.push("E2E:LEN " + body.length); out.push("E2E:TAIL " + body.slice(-200).replace(/\s+/g, " ")); out.push("E2E:RESULT " + body.slice(0, 600)); } catch (e) { out.push("E2E:THROW " + String(e).slice(0, 300)); } }
return out.join("\n");`
}

func (m *e2eModel) responsesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Header.Get("Upgrade") == "websocket" {
			http.Error(w, "websocket transport is not offered", http.StatusUpgradeRequired)
			return
		}
		if !(r.Method == http.MethodPost && strings.HasSuffix(path, "/responses")) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(path, "/models") {
				_, _ = w.Write([]byte(`{"models":[],"data":[],"object":"list"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input []json.RawMessage `json:"input"`
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		var tools []string
		for _, t := range req.Tools {
			if t.Name != "" {
				tools = append(tools, t.Name)
			}
		}
		// Codex 0.147 delivers its MCP tools as an input item, nested in namespaces.
		for _, rawItem := range req.Input {
			var item struct {
				Type  string          `json:"type"`
				Tools json.RawMessage `json:"tools"`
			}
			if json.Unmarshal(rawItem, &item) == nil && item.Type == "additional_tools" {
				tools = append(tools, nestedToolNames(item.Tools)...)
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.all++
		calls := 0
		var results []e2eResult
		for _, rawItem := range req.Input {
			var item map[string]any
			_ = json.Unmarshal(rawItem, &item)
			switch item["type"] {
			case "function_call", "custom_tool_call":
				calls++
				id, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				m.callName[id] = name
			case "function_call_output", "custom_tool_call_output":
				id, _ := item["call_id"].(string)
				if m.seen[id] {
					continue
				}
				m.seen[id] = true
				text := responsesOutputText(item["output"])
				results = append(results, e2eResult{Tool: m.callName[id], IsError: false, Excerpt: excerpt(text), Bytes: len(text), Text: capText(text)})
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		step, ok := m.next(calls)
		if !ok {
			m.record(tools, calls, results, "final")
			responsesText(w, m.finalText())
			return
		}
		if e2ePick(tools, "exec") == "" {
			m.record(tools, calls, results, "no-exec")
			responsesText(w, "E2E:NO-EXEC")
			return
		}
		m.record(tools, calls, results, "call:"+step.Tool)
		code := step.Raw
		if code == "" {
			code = codexScript(step)
		}
		responsesCustomCall(w, "exec", code, m.id("call"))
	})
}

func responsesOutputText(v any) string {
	switch o := v.(type) {
	case string:
		return o
	case []any:
		var parts []string
		for _, it := range o {
			if b, ok := it.(map[string]any); ok {
				if s, ok := b["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return responsesOutputText(o["content"])
	}
	return ""
}

func nestedToolNames(raw json.RawMessage) []string {
	var tools []struct {
		Name  string          `json:"name"`
		Tools json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(raw, &tools)
	var out []string
	for _, t := range tools {
		if t.Name != "" {
			out = append(out, t.Name)
		}
		if len(t.Tools) > 0 {
			out = append(out, nestedToolNames(t.Tools)...)
		}
	}
	return out
}

func responsesCreated(w http.ResponseWriter) {
	sseEvent(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_r2", "object": "response", "status": "in_progress", "model": "gpt-r2", "output": []any{}}})
}

func responsesCompleted(w http.ResponseWriter, item map[string]any) {
	sseEvent(w, "response.completed", map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "resp_r2", "object": "response", "status": "completed", "model": "gpt-r2", "output": []any{item},
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
			"input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}}})
}

func responsesText(w http.ResponseWriter, text string) {
	responsesCreated(w)
	sseEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": "msg_r2", "role": "assistant", "content": []any{}, "status": "in_progress"}})
	sseEvent(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_r2", "output_index": 0, "content_index": 0, "delta": text})
	done := map[string]any{"type": "message", "id": "msg_r2", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text}}}
	sseEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	responsesCompleted(w, done)
}

func responsesCustomCall(w http.ResponseWriter, name, input, id string) {
	responsesCreated(w)
	added := map[string]any{"type": "custom_tool_call", "id": "ctc_" + id, "call_id": id, "name": name, "input": "", "status": "in_progress"}
	sseEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": added})
	done := map[string]any{"type": "custom_tool_call", "id": "ctc_" + id, "call_id": id, "name": name, "input": input, "status": "completed"}
	sseEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	responsesCompleted(w, done)
}

// ---- OpenAI chat completions (opencode) ------------------------------------------

func (m *e2eModel) chatHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-1","object":"model"}]}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role      string          `json:"role"`
				Content   json.RawMessage `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		var tools []string
		for _, t := range req.Tools {
			tools = append(tools, t.Function.Name)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.all++
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(delta map[string]any, finish any) {
			sseEvent(w, "", map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "mock-1",
				"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}})
		}
		text := func(s string) {
			send(map[string]any{"role": "assistant", "content": s}, nil)
			send(map[string]any{}, "stop")
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
		if len(tools) == 0 {
			text("ok")
			return
		}
		calls := 0
		var results []e2eResult
		for _, msg := range req.Messages {
			for _, tc := range msg.ToolCalls {
				calls++
				m.callName[tc.ID] = tc.Function.Name
			}
			if msg.Role == "tool" && !m.seen[msg.ToolCallID] {
				m.seen[msg.ToolCallID] = true
				var s string
				if json.Unmarshal(msg.Content, &s) != nil {
					s = string(msg.Content)
				}
				results = append(results, e2eResult{Tool: m.callName[msg.ToolCallID], Excerpt: excerpt(s), Bytes: len(s), Text: capText(s)})
			}
		}
		step, ok := m.next(calls)
		if !ok {
			m.record(tools, calls, results, "final")
			text(m.finalText())
			return
		}
		name := e2ePick(tools, step.Tool)
		if name == "" && e2ePick(tools, "execute") != "" {
			// opencode 2.x keeps MCP tools behind its Code Mode `execute` tool, as `tools.<server>.<name>`.
			m.record(tools, calls, results, "call:"+step.Tool)
			send(map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"index": 0, "id": m.id("call"), "type": "function",
				"function": map[string]any{"name": "execute", "arguments": jsonOf(map[string]any{"code": openCodeScript(step)})}}}}, nil)
			send(map[string]any{}, "tool_calls")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if name == "" {
			m.record(tools, calls, results, "not-offered:"+step.Tool)
			text("E2E:NOT-OFFERED " + step.Tool)
			return
		}
		m.record(tools, calls, results, "call:"+step.Tool)
		send(map[string]any{"role": "assistant", "tool_calls": []map[string]any{{"index": 0, "id": m.id("call"), "type": "function",
			"function": map[string]any{"name": name, "arguments": jsonOf(step.Args)}}}}, nil)
		send(map[string]any{}, "tool_calls")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
}
