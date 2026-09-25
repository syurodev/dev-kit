package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/codexapp"
	"devkit/internal/sandbox"
)

// Codex's credential stores (cli_auth_credentials_store). "file" keeps a
// ChatGPT sign-in in CODEX_HOME/auth.json; "ephemeral" keeps nothing, for a
// session signed in with an API key DevKit hands over the wire.
const (
	codexStoreFile      = "file"
	codexStoreEphemeral = "ephemeral"
)

// The three modes (spec §7.1). The ids are ACP's: bypassPermissions is the id
// the composer already treats as "confirm before choosing", so Full auto gets
// the same confirmation Claude's bypass does.
const (
	codexModeAsk  = "ask"
	codexModeAuto = "auto"
	codexModeFull = "bypassPermissions"
)

var codexModes = []acp.SessionMode{
	{ID: codexModeAsk, Name: "Ask", Description: "Asks before editing files or running a command that changes anything."},
	{ID: codexModeAuto, Name: "Auto", Description: "Works on its own and asks only when it decides it needs to."},
	{ID: codexModeFull, Name: "Full auto", Description: "Never asks. DevKit's sandbox and its prod checks still apply."},
}

// codexModelOption is the one config option a Codex session has.
const codexModelOption = "model"

// codexDisabledFeatures keep outside code out of a session: hooks could
// approve a request before it reaches DevKit (a project's .codex/config.toml
// can declare them), and plugins, apps and computer use pull in tools, MCP
// servers and hooks DevKit never sees (spec §7.3).
var codexDisabledFeatures = []string{"hooks", "plugins", "apps", "computer_use"}

const (
	// codexModelPages bounds model/list paging.
	codexModelPages = 5
)

// codexInterruptWait is how long a turn gets to end after turn/interrupt.
// Longer than that and the process is not answering: it is killed, and the
// session resumes on a fresh one. A var so a test need not wait it out.
var codexInterruptWait = 10 * time.Second

// codexErrorGrace is how long an error that will not be retried waits for
// the turn/completed that normally follows it before it ends the turn
// itself. A var so a test need not wait it out.
var codexErrorGrace = 10 * time.Second

// CodexOptions configures the Codex vendor adapter.
type CodexOptions struct {
	// Command is an absolute codex binary that overrides the managed runtime —
	// DEVKIT_CODEX_COMMAND, for development and CI.
	Command string
	// Runtime resolves the @openai/codex DevKit installed (agentruntime).
	// Called on every Open and Load, like ClaudeOptions.Runtime.
	Runtime func() (agentruntime.Resolved, error)
	// Home is CODEX_HOME: DevKit's own, never ~/.codex (spec §6). The jail
	// may write it, except the policy files prepareCodexHome resets.
	Home string
	// APIKey returns the key a session signs in with, or "" to use the
	// ChatGPT sign-in kept in Home. The key goes over the wire after
	// initialize; it is never written to disk, the environment or argv.
	APIKey func(ctx context.Context) (string, error)
	// Runner overrides sandbox.DefaultRunner(); nil uses the platform default.
	Runner SandboxRunner
	// Version is DevKit's, sent as clientInfo.version.
	Version string
	// OnSignInCompleted reports how an in-app sign-in ended (InAppSignIn).
	OnSignInCompleted func(SignInResult)
}

// CodexAdapter runs `codex app-server` inside DevKit's jail and speaks ACP to
// the rest of Agent Hub: notifications through codexTranslator, approvals
// through PermissionWaiter.
type CodexAdapter struct {
	opts CodexOptions

	signInStart sync.Mutex // held for the whole of a startSignIn
	signInMu    sync.Mutex
	signIn      *codexSignIn
}

// NewCodexAdapter returns the Codex adapter. Every process it starts goes
// through the SandboxRunner — never an unsandboxed exec.
func NewCodexAdapter(opts CodexOptions) *CodexAdapter {
	return &CodexAdapter{opts: opts}
}

var (
	_ VendorAdapter       = (*CodexAdapter)(nil)
	_ SessionLoader       = (*CodexAdapter)(nil)
	_ SessionRuntime      = (*codexRuntime)(nil)
	_ RuntimeConfigurator = (*codexRuntime)(nil)
	_ RuntimeModes        = (*codexRuntime)(nil)
	_ RuntimeLiveness     = (*codexRuntime)(nil)
)

func (a *CodexAdapter) Type() AgentType { return AgentTypeCodex }

func (a *CodexAdapter) Extensions() []string { return nil }

func (a *CodexAdapter) runner() SandboxRunner {
	if a.opts.Runner != nil {
		return a.opts.Runner
	}
	return sandbox.DefaultRunner()
}

// resolveCommand picks the codex binary: the Command override, else the
// installed runtime, else ErrAgentNotInstalled. Same rules as Claude's.
func (a *CodexAdapter) resolveCommand() (launch, error) {
	if cmd := a.opts.Command; cmd != "" {
		if !filepath.IsAbs(cmd) {
			return launch{}, fmt.Errorf("%w: command must be absolute", ErrProcessFailed)
		}
		path := filepath.Clean(cmd)
		return launch{path: path, ro: uniqueAbsPaths(append([]string{path}, codexPackageDir(path)...)...)}, nil
	}
	if a.opts.Runtime == nil {
		return launch{}, vendorError(AgentTypeCodex, ErrAgentNotInstalled)
	}
	rt, err := a.opts.Runtime()
	if errors.Is(err, agentruntime.ErrNotInstalled) {
		return launch{}, vendorError(AgentTypeCodex, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err))
	}
	if err != nil {
		return launch{}, fmt.Errorf("%w: %w", ErrProcessFailed, err)
	}
	return runtimeLaunch(rt), nil
}

// codexPackageDir is the platform package directory a codex binary sits in
// (vendor/<triple>/bin/codex → vendor/<triple>), which also holds
// codex-package.json and codex-path/ with the bundled rg. The binary finds
// both relative to itself and puts codex-path on its children's PATH
// (measured on 0.147.0), so the jail has to be able to read the directory.
func codexPackageDir(bin string) []string {
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(filepath.Dir(resolved))
	if _, err := os.Stat(filepath.Join(dir, "codex-package.json")); err != nil {
		return nil
	}
	return []string{dir}
}

// codexEnv is the jail's environment. No provider key passes through from
// the host: an API key reaches Codex only over the wire (CodexOptions.APIKey).
// CODEX_MANAGED_BY_NPM and CODEX_MANAGED_PACKAGE_ROOT, which the npm launcher
// sets, are left out on purpose: they only tell Codex how to update itself,
// DevKit pins and updates it, and without them the binary still finds its
// bundled rg (spec §4.2, measured). CODEX_HOME is set last so nothing can
// move it.
func codexEnv(l launch, spec OpenSpec, home string) map[string]string {
	env := make(map[string]string)
	for _, key := range []string{"USER", "LOGNAME", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			env[key] = v
		}
	}
	if env["USER"] == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			env["USER"] = u.Username
		}
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	env["PATH"] = path
	for key, value := range l.env {
		env[key] = value
	}
	for key, value := range spec.ExtraEnv {
		env[key] = value
	}
	env["CODEX_HOME"] = home
	return env
}

// jail is the one place a Codex policy is written, shared by sessions,
// sign-in and Probe, so each runs under the jail a session gets.
//
// listen are loopback ports the process may serve: only a sign-in asks, for
// the browser's callback.
func (a *CodexAdapter) jail(workDir string, l launch, spec OpenSpec, store string, listen ...int) (sandbox.Policy, sandbox.Cmd, error) {
	home := a.opts.Home
	denyWrite, err := prepareCodexHome(home)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	// DevKit's data directory is hidden from every session; this vendor's own
	// home inside it is carved back out for this vendor's sessions only.
	denyRead := carveOut(spec.DenyReadPaths, home)
	// The user's own Codex — its sign-in, config and rules — is not this
	// session's. Hidden even under a shared home.
	if host, err := os.UserHomeDir(); err == nil {
		denyRead = append(denyRead, filepath.Join(host, ".codex"))
	}
	pol := sandbox.Policy{
		WorkDir:             workDir,
		Network:             sandbox.NetworkHost,
		Env:                 codexEnv(l, spec, home),
		ReadOnlyPaths:       uniqueAbsPaths(append(append([]string(nil), l.ro...), spec.ReadOnlyPaths...)...),
		ReadWritePaths:      uniqueAbsPaths(append([]string{home}, spec.ExtraReadWritePaths...)...),
		DenyReadPaths:       denyRead,
		DenyWritePaths:      denyWrite,
		AllowUnixSockets:    append([]string(nil), spec.AllowUnixSockets...),
		ListenLoopbackPorts: append([]int(nil), listen...),
		PrivateHome:         spec.PrivateHome,
	}
	args := append(append([]string(nil), l.args...), codexArgs(store, spec.McpServers)...)
	return pol, sandbox.Cmd{Path: l.path, Args: args}, nil
}

// codexArgs is `codex app-server` with DevKit's settings. They go on the
// command line, not in config.toml: the jail cannot change a process's argv,
// and config.toml is read-only to it and rewritten on every spawn anyway.
//
// The session's MCP servers are TOML values under mcp_servers.<name>. Their
// env lands on argv, which any local process can read; DevKit's own endpoint
// carries none (its identity is the socket, mode 0600).
func codexArgs(store string, servers []acp.McpServerStdio) []string {
	args := []string{
		"app-server",
		"-c", "cli_auth_credentials_store=" + tomlString(store),
		"-c", "check_for_update_on_startup=false",
	}
	for _, srv := range servers {
		if !tomlBareKey.MatchString(srv.Name) {
			log.Printf("agenthub: codex: MCP server name %q cannot be a config key; not passed", srv.Name)
			continue
		}
		key := "mcp_servers." + srv.Name
		args = append(args,
			"-c", key+".command="+tomlString(srv.Command),
			"-c", key+".args="+tomlStrings(srv.Args),
		)
		if len(srv.Env) > 0 {
			args = append(args, "-c", key+".env="+tomlEnv(srv.Env))
		}
	}
	for _, feature := range codexDisabledFeatures {
		args = append(args, "--disable", feature)
	}
	return args
}

// tomlBareKey is what `-c` can take as one segment of a dotted key: Codex
// splits the key on dots, so a quoted segment would not survive.
var tomlBareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlString quotes s as a TOML basic string. JSON's escapes are a subset of
// TOML's, and encoding/json never emits the one JSON has that TOML lacks
// (\/).
func tomlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func tomlStrings(list []string) string {
	if list == nil {
		list = []string{}
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func tomlEnv(env []acp.EnvVariable) string {
	sorted := append([]acp.EnvVariable(nil), env...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	parts := make([]string, 0, len(sorted))
	for _, v := range sorted {
		parts = append(parts, tomlString(v.Name)+"="+tomlString(v.Value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// codexProcess is one app-server under the jail and the connection to it.
type codexProcess struct {
	proc        *sandbox.Proc
	conn        *codexapp.Conn
	cancelServe context.CancelFunc
	// serveDone closes once the read loop has ended AND the process has been
	// reaped, so nothing that waits on it can leave a zombie behind.
	serveDone chan struct{}
	serveErr  error // written before serveDone closes
}

// start spawns the app-server and serves its connection with h. The process
// outlives ctx on purpose, as Claude's does: a session's lifetime belongs to
// its runtime, not to the request that opened it.
func (a *CodexAdapter) start(workDir string, l launch, spec OpenSpec, store string, h codexapp.Handler, listen ...int) (*codexProcess, error) {
	pol, cmd, err := a.jail(workDir, l, spec, store, listen...)
	if err != nil {
		return nil, err
	}
	// Paths only: the env is never logged.
	log.Printf("agenthub: codex: starting %s in %s (read-only %v, sockets %v)",
		cmd.Path, pol.WorkDir, pol.ReadOnlyPaths, pol.AllowUnixSockets)
	proc, err := a.runner().StartPiped(context.Background(), pol, cmd)
	if err != nil {
		return nil, mapSandboxErr(err)
	}
	stdin, stdout := proc.StdinWriter(), proc.StdoutReader()
	if stdin == nil || stdout == nil {
		_ = proc.Kill()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}
	p := &codexProcess{proc: proc, serveDone: make(chan struct{})}
	p.conn = codexapp.NewConn(&stdioPipe{r: stdout, w: stdin}, h)
	var serveCtx context.Context
	serveCtx, p.cancelServe = context.WithCancel(context.Background())
	go func() {
		defer close(p.serveDone)
		p.serveErr = p.conn.Serve(serveCtx)
		p.reap()
	}()
	return p, nil
}

// codexExitGrace is how long codex gets to exit on its own once its stdin
// closes, before it is killed.
const codexExitGrace = 2 * time.Second

// reap ends the process however the read loop ended — a Close, an EOF, or a
// line the loop could not read while codex was still running. stdin closes
// first: codex exits on EOF and takes the commands it started with it
// (measured on 0.147.0: gone in 0.08 s, a backgrounded child with it), which
// a SIGKILL on macOS, with no pid namespace, would leave running. Killed if
// it has not gone in codexExitGrace, and waited for either way.
func (p *codexProcess) reap() {
	_ = p.conn.Close()
	exited := make(chan struct{})
	go func() {
		_ = p.proc.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(codexExitGrace):
		_ = p.proc.Kill()
		<-exited
	}
}

// stop ends the process and waits until it is reaped.
func (p *codexProcess) stop() {
	_ = p.conn.Close()
	p.cancelServe()
	<-p.serveDone
}

// bounded is ctx with a timeout that also ends the moment the process does:
// a dead app-server never answers, and waiting out the timeout for it says
// nothing the exit has not already said.
func (p *codexProcess) bounded(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		select {
		case <-p.serveDone:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// failure explains a failed call. A process that exited is reported as such,
// with the line of stderr that says why; the grace closes the race between
// the failed call and the serve loop noticing EOF, as Claude's handshake does.
func (p *codexProcess) failure(stage string, err error) error {
	select {
	case <-p.serveDone:
		reason := stderrReason(p.proc)
		logStderrTail("codex "+stage, p.proc)
		return fmt.Errorf("%w: %s: codex exited%s", ErrProcessFailed, stage, reason)
	case <-time.After(serveExitGrace):
		return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
	}
}

// initialize is the handshake every connection starts with.
func (a *CodexAdapter) initialize(ctx context.Context, p *codexProcess) error {
	version := a.opts.Version
	if version == "" {
		version = "0.0.0"
	}
	var res codexapp.InitializeResponse
	if err := p.conn.Call(ctx, codexapp.MethodInitialize, codexapp.InitializeParams{
		ClientInfo:   codexapp.ClientInfo{Name: "devkit", Title: "DevKit", Version: version},
		Capabilities: &codexapp.InitializeCapabilities{},
	}, &res); err != nil {
		return p.failure("initialize", err)
	}
	if err := p.conn.Notify(codexapp.MethodInitialized, nil); err != nil {
		return p.failure("initialized", err)
	}
	return nil
}

// apiKey is the key this session signs in with, or "".
func (a *CodexAdapter) apiKey(ctx context.Context) (string, error) {
	if a.opts.APIKey == nil {
		return "", nil
	}
	key, err := a.opts.APIKey(ctx)
	if err != nil {
		return "", vendorError(AgentTypeCodex, fmt.Errorf("%w: %w: the Codex API key could not be read: %v", ErrAgentAuthRequired, ErrAgentAPIKeyUnusable, err))
	}
	return strings.TrimSpace(key), nil
}

func codexWorkDir(cwd string) (string, error) {
	workDir, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%w: project path", ErrProcessFailed)
	}
	return workDir, nil
}

// Open starts a sandboxed app-server and a new thread.
func (a *CodexAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	return a.open(ctx, spec, "")
}

// Load resumes a thread in a new process, satisfying SessionLoader. Under the
// same jail as Open, and in Ask: the mode is DevKit's, not Codex's, and a
// resumed session starts from the safe one rather than from whatever the
// last process was in when it died.
func (a *CodexAdapter) Load(ctx context.Context, spec OpenSpec, threadID string) (SessionRuntime, error) {
	if threadID == "" {
		return nil, fmt.Errorf("%w: load needs the Codex thread id", ErrSessionNotFound)
	}
	return a.open(ctx, spec, threadID)
}

func (a *CodexAdapter) open(ctx context.Context, spec OpenSpec, resume string) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workDir, err := codexWorkDir(spec.Cwd)
	if err != nil {
		return nil, err
	}
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	key, err := a.apiKey(ctx)
	if err != nil {
		return nil, err
	}
	store := codexStoreFile
	if key != "" {
		store = codexStoreEphemeral
	}

	rt := &codexRuntime{tr: newCodexTranslator(), mode: codexModeAsk, network: true}
	p, err := a.start(workDir, l, spec, store, &codexConnHandler{rt: rt})
	if err != nil {
		return nil, err
	}
	rt.proc, rt.conn, rt.cancelServe, rt.serveDone = p.proc, p.conn, p.cancelServe, p.serveDone
	go func() {
		<-p.serveDone
		rt.noteServeExit(p.serveErr)
	}()
	log.Printf("agenthub: codex: sandboxed process started, pid=%d (resume %q)", p.proc.PID(), resume)

	hsCtx, cancel := p.bounded(ctx, handshakeTimeout)
	defer cancel()
	fail := func(err error) (SessionRuntime, error) {
		p.stop()
		return nil, err
	}
	if err := a.initialize(hsCtx, p); err != nil {
		return fail(err)
	}
	if key != "" {
		var login codexapp.LoginStartResponse
		if err := p.conn.Call(hsCtx, codexapp.MethodAccountLoginStart, codexapp.LoginStartParams{
			Type: codexapp.LoginAPIKey, APIKey: key,
		}, &login); err != nil {
			// The server's message, never the key: it is not part of err.
			return fail(vendorError(AgentTypeCodex, fmt.Errorf("%w: %w: Codex refused the API key: %v", ErrAgentAuthRequired, ErrAgentAPIKeyUnusable, err)))
		}
	}
	var account codexapp.GetAccountResponse
	if err := p.conn.Call(hsCtx, codexapp.MethodAccountRead, map[string]any{"refreshToken": false}, &account); err != nil {
		return fail(p.failure("account/read", err))
	}
	if account.RequiresOpenAIAuth && account.Account == nil {
		return fail(vendorError(AgentTypeCodex, fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired)))
	}

	var thread codexapp.ThreadStartResponse
	if resume != "" {
		if err := p.conn.Call(hsCtx, codexapp.MethodThreadResume, codexapp.ThreadResumeParams{
			ThreadID:          resume,
			Cwd:               workDir,
			ApprovalPolicy:    codexApprovalPolicy(codexModeAsk),
			ApprovalsReviewer: codexapp.ReviewerUser,
		}, &thread); err != nil {
			return fail(p.failure("thread/resume", err))
		}
		// The id is ours to begin with; a different one is a different
		// conversation, and attaching to it would be worse than failing.
		if thread.Thread.ID != "" && thread.Thread.ID != resume {
			return fail(fmt.Errorf("%w: thread/resume returned %q, asked for %q", ErrProcessFailed, thread.Thread.ID, resume))
		}
		thread.Thread.ID = resume
	} else {
		// No sandbox field: externalSandbox exists only per turn (spec §4.3).
		if err := p.conn.Call(hsCtx, codexapp.MethodThreadStart, codexapp.ThreadStartParams{
			Cwd:               workDir,
			ApprovalPolicy:    codexApprovalPolicy(codexModeAsk),
			ApprovalsReviewer: codexapp.ReviewerUser,
		}, &thread); err != nil {
			return fail(p.failure("thread/start", err))
		}
		if thread.Thread.ID == "" {
			return fail(fmt.Errorf("%w: thread/start returned no thread id", ErrProcessFailed))
		}
	}
	models := codexListModels(hsCtx, p.conn)
	// Under mu: the read loop is already delivering notifications, and it
	// reads the thread id to tell this thread's from another's.
	rt.mu.Lock()
	rt.threadID, rt.model, rt.models = thread.Thread.ID, thread.Model, models
	rt.mu.Unlock()
	rt.tr.setThread(thread.Thread.ID)
	log.Printf("agenthub: codex: session ready (thread %s, model %q)", thread.Thread.ID, thread.Model)
	return rt, nil
}

// codexListModels is the model picker's list. Best effort: without it the
// session still runs on the thread's default model, it just offers no choice.
func codexListModels(ctx context.Context, conn *codexapp.Conn) []codexapp.Model {
	var out []codexapp.Model
	var cursor *string
	for page := 0; page < codexModelPages; page++ {
		var res codexapp.ModelListResponse
		if err := conn.Call(ctx, codexapp.MethodModelList, codexapp.ModelListParams{Cursor: cursor}, &res); err != nil {
			log.Printf("agenthub: codex: model/list: %v", err)
			return out
		}
		for _, m := range res.Data {
			if !m.Hidden && codexModelSlug(m) != "" {
				out = append(out, m)
			}
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			return out
		}
		cursor = res.NextCursor
	}
	return out
}

func codexModelSlug(m codexapp.Model) string {
	if m.Model != "" {
		return m.Model
	}
	return m.ID
}

// codexApprovalPolicy is a mode's approvalPolicy. Anything unknown is Ask:
// the fail-safe end.
func codexApprovalPolicy(mode string) string {
	switch mode {
	case codexModeAuto:
		return codexapp.ApprovalOnRequest
	case codexModeFull:
		return codexapp.ApprovalNever
	}
	return codexapp.ApprovalUntrusted
}

func (a *CodexAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	out := NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}
	if len(meta) == 0 {
		return out, nil
	}
	var root struct {
		Codex *struct {
			Turn *codexapp.TokenUsageBreakdown `json:"turn"`
		} `json:"codex"`
	}
	if err := json.Unmarshal(meta, &root); err != nil {
		return out, err
	}
	if root.Codex != nil && root.Codex.Turn != nil {
		out.InputTokens = int(root.Codex.Turn.InputTokens)
		out.OutputTokens = int(root.Codex.Turn.OutputTokens)
	}
	return out, nil
}

func (a *CodexAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}

// Probe starts an installed codex under a session's jail, sends initialize
// and stops it: agentruntime.Manager.Verifiers[CodexCLI], the proof that the
// binary runs from where it was installed before a version is activated. In
// a throwaway home, so verifying an install never touches the sign-in.
func (a *CodexAdapter) Probe(ctx context.Context, rt agentruntime.Resolved) error {
	root, err := os.MkdirTemp("", "devkit-codex-probe-")
	if err != nil {
		return fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
	}
	defer os.RemoveAll(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		return fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
	}
	probe := &CodexAdapter{opts: a.opts}
	probe.opts.Home = filepath.Join(root, "home")
	p, err := probe.start(work, runtimeLaunch(rt), OpenSpec{}, codexStoreEphemeral, codexNoHandler{})
	if err != nil {
		return err
	}
	defer p.stop()
	callCtx, cancel := p.bounded(ctx, probeTimeout)
	defer cancel()
	return probe.initialize(callCtx, p)
}

// codexNoHandler serves a connection nobody converses on: notifications are
// dropped and every server request refused.
type codexNoHandler struct{}

func (codexNoHandler) Notification(context.Context, string, json.RawMessage) {}

func (codexNoHandler) Request(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
	return nil, codexUnsupportedRequest(method)
}

// codexRuntime is one Codex thread in one app-server process.
type codexRuntime struct {
	conn        *codexapp.Conn
	proc        *sandbox.Proc
	tr          *codexTranslator
	network     bool
	cancelServe context.CancelFunc
	serveDone   chan struct{}
	// approvals numbers MCP approval cards that have no call of their own.
	approvals atomic.Int64

	mu sync.Mutex
	// threadID and models are set once, at the end of the handshake, under
	// mu because the read loop is running by then.
	threadID  string
	models    []codexapp.Model
	handlers  RuntimeHandlers
	announced bool
	mode      string
	model     string
	turn      *codexTurn
	usage     codexapp.TokenUsageBreakdown // the thread's running total
	// usageKnown says usage has been reported by this process at all; a
	// resumed thread's first report already counts the turns before it.
	usageKnown bool
	closed     bool
	closing    bool
	exitErr    error
}

// codexTurn is the turn a Prompt is waiting on. id is empty until turn/start
// answers; a turn/completed that arrives before that waits in early.
type codexTurn struct {
	id    string
	done  chan codexTurnEnd // one end, for Prompt
	over  chan struct{}     // closed at the end, for anyone else
	ended bool
	early map[string]codexTurnEnd
	// earlyErr holds errors that arrived before turn/start answered, by the
	// turn they name, so one about an older turn cannot fail this one.
	earlyErr  map[string]codexapp.TurnError
	failure   *codexapp.TurnError // an error Codex will not retry
	interrupt bool                // Cancel came before the id did
	// stopping says the person pressed Stop: if the process has to be killed
	// to make it so, the turn was cancelled, not crashed.
	stopping   bool
	startUsage codexapp.TokenUsageBreakdown
	baseKnown  bool // startUsage is a real baseline, not a guess
}

type codexTurnEnd struct {
	status string
	err    *codexapp.TurnError
}

// finish ends t once, with the end Prompt returns. Called with the
// runtime's mu held.
func (t *codexTurn) finish(end codexTurnEnd) {
	if t.ended {
		return
	}
	t.ended = true
	t.done <- end
	close(t.over)
}

// abandon ends t without an outcome: Prompt is returning, whatever became
// of the turn. Called with the runtime's mu held.
func (t *codexTurn) abandon() {
	if t.ended {
		return
	}
	t.ended = true
	close(t.over)
}

func (r *codexRuntime) noteServeExit(serveErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) && r.exitErr == nil {
		r.exitErr = serveErr
	}
}

// Dead reports a process that went away without Close asking it to, so
// Service resumes the session on a fresh one (see claudeRuntime.Dead).
func (r *codexRuntime) Dead() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed && !r.closing, r.exitErr
}

// ACPSessionID is the Codex thread id: what Load resumes.
func (r *codexRuntime) ACPSessionID() string { return r.thread() }

func (r *codexRuntime) thread() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threadID
}

func (r *codexRuntime) Modes() *acp.SessionModeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &acp.SessionModeState{CurrentModeID: r.mode, AvailableModes: append([]acp.SessionMode(nil), codexModes...)}
}

// SetMode takes effect from the next turn: approvalPolicy travels on every
// turn/start, and a turn already running keeps the one it started with.
func (r *codexRuntime) SetMode(_ context.Context, modeID string) error {
	known := false
	for _, m := range codexModes {
		known = known || m.ID == modeID
	}
	if !known {
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidConfigOption, modeID)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	r.mode = modeID
	r.mu.Unlock()
	r.emit([]codexUpdate{{Update: marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateCurrentMode),
		"currentModeId": modeID,
	})}})
	return nil
}

// SetConfigOption changes the model, from the next turn on.
func (r *codexRuntime) SetConfigOption(_ context.Context, optionID, value string) error {
	if optionID != codexModelOption {
		return fmt.Errorf("%w: unknown option %q", ErrInvalidConfigOption, optionID)
	}
	r.mu.Lock()
	known := false
	for _, m := range r.models {
		known = known || codexModelSlug(m) == value
	}
	if !known {
		r.mu.Unlock()
		return fmt.Errorf("%w: unknown model %q", ErrInvalidConfigOption, value)
	}
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	r.model = value
	update := r.configOptionsLocked()
	r.mu.Unlock()
	r.emit([]codexUpdate{{Update: update}})
	return nil
}

// configOptionsLocked is the config_option_update for the model picker.
func (r *codexRuntime) configOptionsLocked() json.RawMessage {
	options := make([]map[string]string, 0, len(r.models))
	for _, m := range r.models {
		name := m.DisplayName
		if name == "" {
			name = codexModelSlug(m)
		}
		options = append(options, map[string]string{"value": codexModelSlug(m), "name": name, "description": m.Description})
	}
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateConfigOptions),
		"configOptions": []map[string]any{{
			"id":           codexModelOption,
			"name":         "Model",
			"category":     "model",
			"type":         "select",
			"currentValue": r.model,
			"options":      options,
		}},
	})
}

// SetHandlers installs the callbacks. The first time an update handler
// arrives, the model picker is announced through it: Codex has no
// config_option_update of its own to send, and RuntimeModes carries modes only.
func (r *codexRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	r.handlers = h
	var announce json.RawMessage
	if h.OnUpdate != nil && !r.announced && len(r.models) > 0 {
		r.announced = true
		announce = r.configOptionsLocked()
	}
	r.mu.Unlock()
	if announce != nil {
		h.OnUpdate(announce, nil)
	}
}

func (r *codexRuntime) handlersSnapshot() RuntimeHandlers {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handlers
}

// emit hands translated updates to the session. Before handlers are
// installed — a resume — they are dropped: the transcript is already stored.
func (r *codexRuntime) emit(updates []codexUpdate) {
	if len(updates) == 0 {
		return
	}
	onUpdate := r.handlersSnapshot().OnUpdate
	if onUpdate == nil {
		return
	}
	for _, u := range updates {
		onUpdate(u.Update, u.Meta)
	}
}

// codexInput turns a prompt into turn/start input. Text blocks are joined
// into one text input; a pasted image goes as a data URL; an attached file's
// contents go as text under its path. A link is named, not opened: Codex
// reads files itself, inside the jail.
func codexInput(prompt []acp.ContentBlock) []codexapp.UserInput {
	var text strings.Builder
	var images []codexapp.UserInput
	add := func(s string) {
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(s)
	}
	for _, block := range prompt {
		switch block.Type {
		case "text", "":
			if block.Text != "" {
				add(block.Text)
			}
		case "image":
			if block.Data != "" && block.MimeType != "" {
				b, _ := json.Marshal(map[string]string{"type": "image", "url": "data:" + block.MimeType + ";base64," + block.Data})
				images = append(images, b)
			}
		case "resource":
			if block.Resource != nil && block.Resource.Text != "" {
				add(block.Resource.URI + ":\n" + block.Resource.Text)
			}
		case "resource_link":
			if block.URI != "" {
				add(block.URI)
			}
		}
	}
	var out []codexapp.UserInput
	if text.Len() > 0 {
		out = append(out, codexapp.TextInput(text.String()))
	}
	return append(out, images...)
}

// Prompt runs one turn: turn/start answers at once with the turn in
// progress, and the turn ends on its turn/completed (spec §5.1).
func (r *codexRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, _ json.RawMessage) (PromptOutcome, error) {
	if err := ctx.Err(); err != nil {
		return PromptOutcome{}, err
	}
	input := codexInput(prompt)
	if len(input) == 0 {
		return PromptOutcome{}, fmt.Errorf("%w: nothing Codex can read", ErrInvalidPrompt)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionNotFound
	}
	if r.turn != nil {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionBusy
	}
	t := &codexTurn{
		done:       make(chan codexTurnEnd, 1),
		over:       make(chan struct{}),
		early:      map[string]codexTurnEnd{},
		earlyErr:   map[string]codexapp.TurnError{},
		startUsage: r.usage,
		baseKnown:  r.usageKnown,
	}
	r.turn = t
	sandboxPolicy := codexapp.ExternalSandbox(r.network)
	params := codexapp.TurnStartParams{
		ThreadID:          r.threadID,
		Input:             input,
		ApprovalPolicy:    codexApprovalPolicy(r.mode),
		ApprovalsReviewer: codexapp.ReviewerUser,
		SandboxPolicy:     &sandboxPolicy,
		Model:             r.model,
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		// Ended however Prompt leaves: Cancel's watchdog waits on it, and must
		// not kill a process a turn never got going on.
		t.abandon()
		if r.turn == t {
			r.turn = nil
		}
		r.mu.Unlock()
	}()

	// turn/start answers at once with the turn in progress. Its own bound,
	// not the prompt's ctx: a Stop that lands while it is in flight still
	// needs the turn id it answers with, or Codex runs a turn nobody can
	// interrupt and the next prompt starts beside it.
	log.Printf("agenthub: codex turn/start sent (thread %s, policy %s)", params.ThreadID, params.ApprovalPolicy)
	startCtx, cancelStart := context.WithTimeout(context.Background(), handshakeTimeout)
	go func() {
		select {
		case <-r.serveDone:
			cancelStart()
		case <-startCtx.Done():
		}
	}()
	var res codexapp.TurnStartResponse
	err := r.conn.Call(startCtx, codexapp.MethodTurnStart, params, &res)
	cancelStart()
	if err != nil {
		return PromptOutcome{}, r.callFailure("turn/start", err)
	}
	r.mu.Lock()
	t.id = res.Turn.ID
	for _, key := range []string{t.id, ""} {
		if e, ok := t.earlyErr[key]; ok {
			r.failLocked(t, e)
		}
	}
	if end, ok := t.early[t.id]; ok {
		t.finish(end)
	}
	interrupt := t.interrupt && !t.ended
	r.mu.Unlock()
	if interrupt {
		r.interrupt(t.id)
	}

	select {
	case end := <-t.done:
		log.Printf("agenthub: codex turn %s ended %s (thread %s)", t.id, end.status, params.ThreadID)
		return r.outcome(t, end)
	case <-r.serveDone:
		r.emit(r.tr.closeOpen("Codex stopped before the turn finished."))
		r.mu.Lock()
		stopping := t.stopping
		r.mu.Unlock()
		if stopping {
			// Stop had to kill it: still a Stop, not a crash.
			return PromptOutcome{}, ErrCancelled
		}
		return PromptOutcome{}, r.callFailure("turn", codexapp.ErrClosed)
	case <-ctx.Done():
		r.mu.Lock()
		t.stopping = true
		r.mu.Unlock()
		r.interrupt(t.id)
		select {
		case <-t.over:
		case <-r.serveDone:
		case <-time.After(codexInterruptWait):
			// Codex did not stop. The next prompt must not start a turn
			// beside this one, so the process goes; the session resumes
			// on a fresh one.
			log.Printf("agenthub: codex: turn did not stop after interrupt; killing the process (thread %s)", params.ThreadID)
			_ = r.proc.Kill()
			r.emit(r.tr.closeOpen("Cancelled."))
		}
		return PromptOutcome{}, ErrCancelled
	}
}

// callFailure explains a failed call on a live session's connection.
func (r *codexRuntime) callFailure(stage string, err error) error {
	select {
	case <-r.serveDone:
		reason := ""
		if r.proc != nil {
			reason = stderrReason(r.proc)
			logStderrTail("codex "+stage, r.proc)
		}
		return fmt.Errorf("%w: %s: codex exited%s", ErrProcessFailed, stage, reason)
	default:
		return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
	}
}

// outcome is what Prompt returns for a turn that ended.
func (r *codexRuntime) outcome(t *codexTurn, end codexTurnEnd) (PromptOutcome, error) {
	r.mu.Lock()
	used := codexUsageSince(r.usage, t.startUsage)
	r.mu.Unlock()
	meta := marshalUpdate(map[string]any{"codex": map[string]any{"turnId": t.id, "turn": used}})
	switch end.status {
	case codexapp.TurnCompleted:
		return PromptOutcome{StopReason: "end_turn", Meta: meta}, nil
	case codexapp.TurnInterrupted:
		return PromptOutcome{StopReason: "cancelled", Meta: meta}, nil
	}
	return PromptOutcome{}, codexTurnError(end.err)
}

// codexUsageSince is what was used between two running totals.
func codexUsageSince(now, then codexapp.TokenUsageBreakdown) codexapp.TokenUsageBreakdown {
	return codexapp.TokenUsageBreakdown{
		TotalTokens:           now.TotalTokens - then.TotalTokens,
		InputTokens:           now.InputTokens - then.InputTokens,
		CachedInputTokens:     now.CachedInputTokens - then.CachedInputTokens,
		OutputTokens:          now.OutputTokens - then.OutputTokens,
		ReasoningOutputTokens: now.ReasoningOutputTokens - then.ReasoningOutputTokens,
	}
}

// codexTurnError maps a failed turn's error. unauthorized is a sign-in to
// offer; the budget errors say what ran out; anything else carries Codex's
// own message.
func codexTurnError(e *codexapp.TurnError) error {
	if e == nil {
		return fmt.Errorf("%w: the Codex turn failed", ErrProcessFailed)
	}
	switch codexErrorKind(e.CodexErrorInfo) {
	case "unauthorized":
		return vendorError(AgentTypeCodex, fmt.Errorf("%w: %s", ErrAgentAuthRequired, e.Message))
	case "usageLimitExceeded":
		return fmt.Errorf("%w: Codex usage limit reached: %s", ErrProcessFailed, e.Message)
	case "contextWindowExceeded":
		return fmt.Errorf("%w: the conversation no longer fits the model's context window: %s", ErrProcessFailed, e.Message)
	case "sessionBudgetExceeded":
		return fmt.Errorf("%w: the session's budget is spent: %s", ErrProcessFailed, e.Message)
	}
	return fmt.Errorf("%w: %s", ErrProcessFailed, e.Message)
}

// codexErrorKind reads codexErrorInfo, which is a bare string for most kinds
// and an object keyed by the kind for the ones that carry details.
func codexErrorKind(raw json.RawMessage) string {
	var kind string
	if json.Unmarshal(raw, &kind) == nil {
		return kind
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for k := range obj {
			return k
		}
	}
	return ""
}

// interrupt asks Codex to stop the turn. Best effort: a process that does not
// answer is dealt with by Cancel's watchdog.
func (r *codexRuntime) interrupt(turnID string) {
	if turnID == "" {
		return
	}
	thread := r.thread()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.conn.Call(ctx, codexapp.MethodTurnInterrupt, codexapp.TurnInterruptParams{ThreadID: thread, TurnID: turnID}, nil); err != nil {
		log.Printf("agenthub: codex turn/interrupt (thread %s): %v", thread, err)
	}
}

// Cancel interrupts the running turn (spec §5.1). Codex ends it with
// turn/completed interrupted, and Prompt returns a cancelled stop. A turn that
// does not end in codexInterruptWait is a process that stopped answering:
// it is killed, and the session resumes on a fresh one.
func (r *codexRuntime) Cancel(context.Context) error {
	r.mu.Lock()
	t, closed := r.turn, r.closed
	id := ""
	if t != nil {
		id = t.id
		t.stopping = true
		if id == "" {
			t.interrupt = true
		}
	}
	r.mu.Unlock()
	if closed || t == nil {
		return nil
	}
	go r.interrupt(id)
	wait := codexInterruptWait
	go func() {
		select {
		case <-t.over:
		case <-r.serveDone:
		case <-time.After(wait):
			log.Printf("agenthub: codex: turn did not stop after interrupt; killing the process (thread %s)", r.thread())
			_ = r.proc.Kill()
		}
	}()
	return nil
}

func (r *codexRuntime) Close(context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed, r.closing = true, true
	r.mu.Unlock()
	// Closing stdin ends the read loop, and the loop's reaper does the rest:
	// codex exits on EOF, or is killed after codexExitGrace, and is waited for.
	if r.conn != nil {
		_ = r.conn.Close()
	}
	if r.cancelServe != nil {
		r.cancelServe()
	}
	if r.serveDone != nil {
		select {
		case <-r.serveDone:
		case <-time.After(codexExitGrace + 3*time.Second):
		}
	}
	return nil
}

// codexConnHandler is the runtime's side of the app-server connection.
type codexConnHandler struct{ rt *codexRuntime }

// Notification runs on the read loop, in wire order, so a turn's last updates
// — including the tool calls turnCompleted closes — reach the session before
// Prompt learns the turn is over.
func (h *codexConnHandler) Notification(_ context.Context, method string, params json.RawMessage) {
	r := h.rt
	r.emit(r.tr.Translate(method, params))
	switch method {
	case codexapp.MethodTurnCompleted:
		r.onTurnCompleted(params)
	case codexapp.MethodError:
		r.onError(params)
	case codexapp.MethodTokenUsageUpdated:
		r.onUsage(params)
	default:
		if !codexTranslates(method) {
			codexNoteIgnored(method)
		}
	}
}

// codexIgnoredSeen dedupes the "not translated" log, per method, process-wide.
// The method name only: remoteControl/status/changed carries the host name
// and an installation id, and params are never logged (spec §5.3).
var codexIgnoredSeen sync.Map

func codexNoteIgnored(method string) {
	if _, seen := codexIgnoredSeen.LoadOrStore(method, struct{}{}); !seen {
		log.Printf("agenthub: codex: not translating %s", method)
	}
}

func (r *codexRuntime) ownThread(threadID string) bool {
	return threadID == "" || threadID == r.thread()
}

func (r *codexRuntime) onTurnCompleted(params json.RawMessage) {
	var n codexapp.TurnCompletedNotification
	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turn
	if t == nil {
		return
	}
	end := codexTurnEnd{status: n.Turn.Status, err: n.Turn.Error}
	if end.err == nil && end.status == codexapp.TurnFailed {
		end.err = t.failure
	}
	switch t.id {
	case "":
		t.early[n.Turn.ID] = end
	case n.Turn.ID:
		t.finish(end)
	}
}

// onError handles error. willRetry is Codex retrying on its own: logged, the
// turn goes on. Otherwise the turn is over, and turn/completed normally says
// so right after; if it does not come, the error ends the turn itself.
func (r *codexRuntime) onError(params json.RawMessage) {
	var n codexapp.ErrorNotification
	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
		return
	}
	if n.WillRetry {
		log.Printf("agenthub: codex: retrying after %q (thread %s)", n.Error.Message, n.ThreadID)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turn
	switch {
	case t == nil || t.ended:
	case t.id == "":
		// turn/start has not answered: kept for the turn the error names,
		// which may be an older one.
		t.earlyErr[n.TurnID] = n.Error
	case n.TurnID == "" || n.TurnID == t.id:
		r.failLocked(t, n.Error)
	}
}

// failLocked records an error t will not recover from, and ends t with it if
// the turn/completed that normally follows does not come in codexErrorGrace.
// Called with mu held.
func (r *codexRuntime) failLocked(t *codexTurn, e codexapp.TurnError) {
	failure := e
	t.failure = &failure
	time.AfterFunc(codexErrorGrace, func() {
		r.mu.Lock()
		pending := r.turn == t && !t.ended
		r.mu.Unlock()
		if !pending {
			return
		}
		r.emit(r.tr.closeOpen("The turn failed."))
		r.mu.Lock()
		t.finish(codexTurnEnd{status: codexapp.TurnFailed, err: &failure})
		r.mu.Unlock()
	})
}

func (r *codexRuntime) onUsage(params json.RawMessage) {
	var n codexapp.TokenUsageUpdatedNotification
	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
		return
	}
	r.mu.Lock()
	if t := r.turn; t != nil && !t.baseKnown {
		// The first figure this process reports: after a resume it already
		// counts every earlier turn, so the baseline is the thread's total
		// before this call rather than zero.
		t.startUsage = codexUsageSince(n.TokenUsage.Total, n.TokenUsage.Last)
		t.baseKnown = true
	}
	r.usage, r.usageKnown = n.TokenUsage.Total, true
	r.mu.Unlock()
}

// Request answers the app-server's requests on their own goroutine. Every
// wait selects on ctx, which ends when Codex withdraws the request
// (serverRequest/resolved) or the connection closes — codexapp.Handler's
// contract.
func (h *codexConnHandler) Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
	r := h.rt
	switch method {
	case codexapp.MethodPermissionsApproval:
		response, notice := codexPermissionsRequest(params)
		r.emit(notice)
		return response, nil
	case codexapp.MethodMcpElicitation:
		ref := strconv.FormatInt(r.approvals.Add(1), 10)
		a, ok := r.tr.mcpToolApprovalFor(r.thread(), ref, params)
		if !ok {
			return nil, codexUnsupportedRequest(method)
		}
		return r.ask(ctx, a), nil
	}
	a, ok := r.tr.approvalFor(r.thread(), method, params)
	switch {
	case !ok:
		return nil, codexUnsupportedRequest(method)
	case a.Malformed:
		return a.Response(nil), nil
	}
	return r.ask(ctx, a), nil
}

// ask puts an approval to the person. With nobody to ask — no turn handler
// installed — the answer is the fail-closed one.
func (r *codexRuntime) ask(ctx context.Context, a codexApproval) json.RawMessage {
	onPermission := r.handlersSnapshot().OnPermission
	if onPermission == nil {
		return a.Response(nil)
	}
	// A denial comes back as an outcome and an error together; the outcome
	// is still the answer, and one Response cannot read maps to cancel.
	outcome, _ := onPermission(ctx, a.Params)
	return a.Response(outcome)
}
