package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/sandbox"
)

// SandboxRunner starts sandboxed piped processes (injected for tests).
type SandboxRunner interface {
	StartPiped(ctx context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error)
}

// handshakeTimeout bounds initialize + session/new. It is a backstop only: a
// dead process is caught immediately through the serve loop, so this fires only
// for a vendor that is running and simply never answers. Generous, because a
// cold npx start can take real time and a false failure here means the user
// cannot open a session at all.
const handshakeTimeout = 60 * time.Second

// ClaudeOptions configures the Claude ACP vendor adapter.
type ClaudeOptions struct {
	// Command is the agent binary. Prefer an absolute path resolved before the jail.
	// Non-absolute commands are rejected unless AllowNpx is set.
	Command string
	// Args are appended after Command (without the binary path).
	Args []string
	// AllowNpx enables the npx -y @agentclientprotocol/claude-agent-acp fallback.
	// Requires resolving npx to an absolute path; still sandboxed via StartPiped.
	AllowNpx bool
	// Runner overrides sandbox.DefaultRunner(); nil uses the platform default.
	Runner SandboxRunner
	// OnUnknownMethod reports an agent→client ACP method this build does not
	// implement, once per method per process.
	//
	// It exists because the answer to "does Claude ever call fs/* or
	// terminal/*" cannot be read off the vendor's bundle — the plumbing is
	// there (methods.client.fs.readTextFile) but no call site was found — and
	// NFR-1 makes it the point where the Capability Gateway is supposed to
	// mediate the agent's file access. Until one of these is logged, DevKit's
	// Gateway sees no file access at all and nobody knows whether that is
	// because the agent never asks or because we never answered.
	OnUnknownMethod func(method string, params json.RawMessage)
}

var _ SessionLoader = (*ClaudeAdapter)(nil)

// ClaudeAdapter is a VendorAdapter that speaks ACP to Claude inside a sandbox.
type ClaudeAdapter struct {
	opts ClaudeOptions
}

// NewClaudeAdapter returns a Claude ACP adapter. Production Open always uses
// sandbox.StartPiped — never acp.StartProcess or unsandboxed exec.
func NewClaudeAdapter(opts ClaudeOptions) *ClaudeAdapter {
	return &ClaudeAdapter{opts: opts}
}

var (
	_ VendorAdapter  = (*ClaudeAdapter)(nil)
	_ SessionRuntime = (*claudeRuntime)(nil)
	_ SandboxRunner  = (*sandbox.Runner)(nil)
)

func (a *ClaudeAdapter) Type() AgentType { return AgentTypeClaude }

func (a *ClaudeAdapter) Extensions() []string { return nil }

func (a *ClaudeAdapter) runner() SandboxRunner {
	if a.opts.Runner != nil {
		return a.opts.Runner
	}
	return sandbox.DefaultRunner()
}

// buildSandbox resolves the agent binary and returns the jail Policy + Cmd.
// Exported for unit tests via same-package access.
func (a *ClaudeAdapter) buildSandbox(spec OpenSpec) (sandbox.Policy, sandbox.Cmd, error) {
	workDir, err := filepath.Abs(spec.Cwd)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		return sandbox.Policy{}, sandbox.Cmd{}, fmt.Errorf("%w: project path", ErrProcessFailed)
	}

	path, args, roPaths, err := a.resolveCommand()
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	roPaths = uniqueAbsPaths(append(roPaths, spec.ReadOnlyPaths...)...)

	env := claudeEnvAllowlist()
	pol := sandbox.Policy{
		WorkDir:        workDir,
		Network:        sandbox.NetworkHost,
		Env:            env,
		ReadOnlyPaths:  roPaths,
		ReadWritePaths: claudeCredentialPaths(env["HOME"]),
		// Subscription login keeps the OAuth token in the macOS keychain
		// (service "Claude Code-credentials-*"), not in ~/.claude.json.
		AllowKeychain:    true,
		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
	}
	cmd := sandbox.Cmd{
		Path: path,
		Args: args,
	}
	return pol, cmd, nil
}

func (a *ClaudeAdapter) resolveCommand() (path string, args []string, ro []string, err error) {
	cmd := a.opts.Command
	args = append([]string(nil), a.opts.Args...)

	if a.opts.AllowNpx {
		if cmd == "" || cmd == "npx" || !filepath.IsAbs(cmd) && filepath.Base(cmd) == "npx" {
			npx, lookErr := exec.LookPath("npx")
			if lookErr != nil {
				return "", nil, nil, fmt.Errorf("%w: npx not found for AllowNpx", ErrProcessFailed)
			}
			path = npx
			if len(args) == 0 {
				args = []string{"-y", "@agentclientprotocol/claude-agent-acp"}
			}
			ro = uniqueAbsPaths(append([]string{path}, nodeModulesRoot(path)...)...)
			return path, args, ro, nil
		}
	}

	if cmd == "" {
		return "", nil, nil, fmt.Errorf("%w: ClaudeOptions.Command required (absolute binary)", ErrProcessFailed)
	}
	if !filepath.IsAbs(cmd) {
		return "", nil, nil, fmt.Errorf("%w: command must be absolute (or enable AllowNpx)", ErrProcessFailed)
	}
	path = filepath.Clean(cmd)
	ro = uniqueAbsPaths(append([]string{path}, nodeModulesRoot(path)...)...)
	return path, args, ro, nil
}

// nodeModulesRoot returns the node_modules tree the command lives in, if any.
// A Node CLI is a thin entry that imports its siblings: granting only the
// entry file gets ERR_MODULE_NOT_FOUND on the first import. The outermost
// node_modules is used so a nested dependency resolves too. Measured against
// @agentclientprotocol/claude-agent-acp, which needs @anthropic-ai/claude-agent-sdk.
func nodeModulesRoot(command string) []string {
	resolved, err := filepath.EvalSymlinks(command)
	if err != nil {
		return nil
	}
	var outermost string
	for dir := filepath.Dir(resolved); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "node_modules" {
			outermost = dir
		}
	}
	if outermost == "" {
		return nil
	}
	return []string{outermost}
}

func uniqueAbsPaths(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

// claudeEnvAllowlist copies API key vars (and a minimal PATH) from the host
// env into the jail. bwrap uses --clearenv, so without PATH absolute Command
// still works but AllowNpx / relative lookups fail. Values must never be logged.
func claudeEnvAllowlist() map[string]string {
	env := make(map[string]string)
	for _, key := range []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"} {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			env[key] = v
		}
	}
	// USER/LOGNAME are how subscription auth resolves the account: without
	// USER the agent completes session/new and then fails session/prompt with
	// "-32000 Authentication required", even with the keychain readable
	// (measured — adding USER alone flips it to a successful turn).
	// Not secrets; the jail already runs as this user.
	for _, key := range []string{"USER", "LOGNAME"} {
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
	return env
}

// devkitClientCapabilities is what DevKit tells the agent it can handle.
//
// Sending none — which is what this did until now — is not neutral. The agent
// reads this to decide what to send back, so an empty object is a claim that
// DevKit understands nothing, and it acted on it: with a swarm prompt that
// spawned two subagents, declaring `subagents` alone took agent_message_chunk
// from 2 to 13 (measured against claude-agent-acp 0.74.0). Everything those
// subagents said was being dropped on the wire, in the module built around
// running several agents at once.
//
// Only what DevKit actually serves goes in here. `fs` and `terminal` are
// deliberately absent: ClientHandler has no implementation for them and
// UnknownMethod refuses the call, so advertising them would invite a request
// DevKit answers with an error. Measurement says this vendor never asks —
// three cases, read in cwd, write outside cwd, and a shell command, produced
// no fs/* or terminal/* call even with both advertised — but the rule is to
// declare what is true, not what is harmless.
func devkitClientCapabilities() map[string]any {
	return map[string]any{
		// The client understands updates that come from a subagent. DevKit
		// renders them in the parent transcript rather than as their own
		// session; nesting is a UI question, losing the text was a wire one.
		"subagents": map[string]any{},
	}
}

// sessionNewMeta cuts the agent off// sessionNewMeta cuts the agent off from the user's own Claude Code settings.
//
// claude-agent-acp passes settingSources: ["user","project","local"] to the
// Agent SDK for every session, so ~/.claude/settings.json, the project's
// .claude/settings.json and settings.local.json all applied inside DevKit's
// jail. Two of the things those files can carry defeat the permission gate
// outright, because the gate is the LAST step of the SDK's evaluation order
// (hooks → deny → ask → mode → allow → the gate):
//
//	permissions.allow      an allow rule approves the call before the gate is
//	                       reached. Measured: with {"allow":["Bash"]} a shell
//	                       command that writes a file ran and the gate was
//	                       never asked.
//	permissions.defaultMode acp-agent reads it and makes it the session's
//	                       permission mode, so acceptEdits or bypassPermissions
//	                       in a user's settings silences the gate for whole
//	                       classes of tool.
//
// An empty settingSources leaves the agent with the programmatic tier only —
// which is what DevKit passes and controls. It does not disable managed
// (MDM/policy) settings, which is correct: an organisation's policy should
// still bind an agent running on its machine.
//
// This is a jailbreak fix, not a preference: the whole premise of
// PermissionWaiter is that it sees every tool call it is meant to gate.
//
// The same _meta carries a PreToolUse hook when we know where DevKit's own
// binary lives inside the jail. settingSources shuts outside settings out;
// the hook is the one rule DevKit puts back in, and it runs before every
// other step of the evaluation order. See runPreToolUseHook.
func sessionNewMeta(hookCommand string) json.RawMessage {
	options := map[string]any{"settingSources": []string{}}
	if hookCommand != "" {
		options["settings"] = map[string]any{
			"hooks": map[string]any{
				"PreToolUse": []any{
					map[string]any{
						"hooks": []any{
							map[string]any{
								"type":    "command",
								"command": hookCommand,
								"args":    []string{"hook", "pretooluse"},
							},
						},
					},
				},
			},
		}
	}
	body, err := json.Marshal(map[string]any{"claudeCode": map[string]any{"options": options}})
	if err != nil {
		// Only static, marshalable values go in above; a failure here would be
		// a programming error, and dropping the hook silently is worse than
		// dropping the whole _meta loudly at the next line.
		return json.RawMessage(`{"claudeCode":{"options":{"settingSources":[]}}}`)
	}
	return body
}

// devkitHookCommand is the path to DevKit's own binary as the agent sees it.
// SessionHost already hands the same path over as the devkit MCP server's
// command, so the jail is guaranteed to have it bound and executable — reading
// it back from the spec beats threading a second copy through OpenSpec.
func devkitHookCommand(spec OpenSpec) string {
	for _, srv := range spec.McpServers {
		if srv.Name == "devkit" && srv.Command != "" {
			return srv.Command
		}
	}
	return ""
}

// claudeCredentialPaths returns Claude Code's own config/credential paths.// claudeCredentialPaths returns Claude Code's own config/credential paths.
//
// The jail hides $HOME, which assumed the agent authenticates with
// ANTHROPIC_API_KEY. Subscription login does not: the credentials live in
// ~/.claude.json and ~/.claude, so without these the agent completes the ACP
// handshake and then dies on the first model call (measured: ~70ms, session
// status error, zero turns).
//
// Read-write, not read-only: Claude Code writes session state and rewrites
// its config. The grant is deliberately two exact paths, never $HOME, and it
// is a real widening — anything running in that jail can read the agent's
// credentials. See sandbox.mdx.
func claudeCredentialPaths(home string) []string {
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return nil
		}
	}
	out := make([]string, 0, 2)
	for _, name := range []string{".claude", ".claude.json"} {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		}
	}
	return out
}

func mapSandboxErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sandbox.ErrUnavailable) || errors.Is(err, sandbox.ErrUnsupportedPlatform) {
		return fmt.Errorf("%w: %v", ErrSandboxUnavailable, err)
	}
	if errors.Is(err, sandbox.ErrInvalidPolicy) {
		return fmt.Errorf("%w: %v", ErrProcessFailed, err)
	}
	return fmt.Errorf("%w: %v", ErrProcessFailed, err)
}

// Open starts a sandboxed Claude ACP process, connects over stdio, and creates a session.
func (a *ClaudeAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	return a.open(ctx, spec, "")
}

// Load reopens a session that outlived the process that created it, satisfying
// SessionLoader. Everything up to the last call is identical to Open — same
// jail, same credentials, same _meta — because a resumed session must run under
// the policy the original did, not a looser one that happens to be easier here.
//
// The vendor replays the session's history as session/update notifications
// while this runs. They are discarded, because the runtime's handlers are still
// nil until Service installs them: the transcript is already in SQLite, and a
// resume that re-recorded it would grow the conversation every restart.
// Measured against claude-agent-acp 0.74.0: the replay arrives as
// user_message_chunk and agent_message_chunk, and a prompt afterwards recalls
// what was said before the process died.
func (a *ClaudeAdapter) Load(ctx context.Context, spec OpenSpec, acpSessionID string) (SessionRuntime, error) {
	if acpSessionID == "" {
		return nil, fmt.Errorf("%w: load needs the vendor session id", ErrSessionNotFound)
	}
	return a.open(ctx, spec, acpSessionID)
}

func (a *ClaudeAdapter) open(ctx context.Context, spec OpenSpec, resume string) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pol, cmd, err := a.buildSandbox(spec)
	if err != nil {
		return nil, err
	}

	// The sandbox process outlives the caller's request: sessions opened over
	// MCP derive ctx from the client connection, and a client disconnect would
	// SIGKILL the agent (exec.CommandContext) while its session row stays
	// alive. Lifetime belongs to the runtime/Close, so start detached; only
	// the handshake below still honors the caller's ctx.
	proc, err := a.runner().StartPiped(context.Background(), pol, cmd)
	if err != nil {
		return nil, mapSandboxErr(err)
	}

	stdin := proc.StdinWriter()
	stdout := proc.StdoutReader()
	if stdin == nil || stdout == nil {
		_ = proc.Kill()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}

	rw := &stdioPipe{r: stdout, w: stdin}
	rt := &claudeRuntime{
		proc:   proc,
		cwd:    pol.WorkDir,
		killer: &procKiller{proc: proc},
	}
	handler := &claudeConnHandler{rt: rt, onUnknownMethod: a.opts.OnUnknownMethod}
	conn := acp.NewConn(rw, handler)
	rt.conn = conn

	serveCtx, cancelServe := context.WithCancel(context.Background())
	rt.cancelServe = cancelServe
	rt.serveDone = make(chan struct{})
	go func() {
		defer close(rt.serveDone)
		serveErr := conn.Serve(serveCtx)
		// Unexpected exit (agent process death, pipe EOF): mark the runtime
		// dead so later Prompt/Cancel calls fail fast instead of hanging on a
		// broken pipe. A deliberate Close() has already set rt.closed.
		rt.mu.Lock()
		rt.closed = true
		// A deliberate Close cancels the serve context first; only record the
		// exit as abnormal when Serve failed for another reason.
		if serveErr != nil && !errors.Is(serveErr, context.Canceled) && rt.exitErr == nil {
			rt.exitErr = serveErr
		}
		rt.mu.Unlock()
	}()

	cleanup := func() {
		cancelServe()
		_ = conn.Close()
		_ = proc.Kill()
		<-rt.serveDone
	}

	// The handshake must not be able to outlive the process it is talking to.
	//
	// acp.Conn.Call blocks in a select on ctx.Done() and the response channel.
	// The caller's ctx has no deadline here (Open deliberately detaches the
	// process from the request that started it), so a vendor that dies without
	// closing stdout left both calls below parked forever: no error, no log,
	// nothing for the UI to render but a spinner. Every ErrProcessFailed branch
	// in this function was unreachable in exactly the case it was written for.
	//
	// serveDone closes when conn.Serve returns, which is what process death and
	// pipe EOF both look like from here, so it is the real signal. The timeout
	// is only a backstop for a process that is alive but never answers.
	hsCtx, cancelHandshake := context.WithTimeout(ctx, handshakeTimeout)
	defer cancelHandshake()
	go func() {
		select {
		case <-rt.serveDone:
			cancelHandshake()
		case <-hsCtx.Done():
		}
	}()
	// Reports what actually went wrong: a cancelled handshake reads as a
	// context error, which says nothing about the process having exited.
	handshakeErr := func(stage string, err error) error {
		select {
		case <-rt.serveDone:
			return fmt.Errorf("%w: %s: agent process exited during handshake", ErrProcessFailed, stage)
		default:
			return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
		}
	}

	var initRes acp.InitializeResult
	if err := conn.Call(hsCtx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: devkitClientCapabilities(),
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &initRes); err != nil {
		cleanup()
		return nil, handshakeErr("initialize", err)
	}

	// Non-nil even with no endpoints: mcpServers is a required field.
	mcpServers := make([]any, 0, len(spec.McpServers))
	for _, srv := range spec.McpServers {
		if srv.Env == nil {
			srv.Env = []acp.EnvVariable{} // schema requires env; nil would serialise as null
		}
		mcpServers = append(mcpServers, srv)
	}
	meta := sessionNewMeta(devkitHookCommand(spec))

	if resume != "" {
		var loadRes acp.SessionLoadResult
		if err := conn.Call(hsCtx, "session/load", acp.SessionLoadRequest{
			SessionID:  resume,
			Cwd:        pol.WorkDir,
			McpServers: mcpServers,
			Meta:       meta,
		}, &loadRes); err != nil {
			cleanup()
			return nil, handshakeErr("session/load", err)
		}
		// The id is ours to begin with, so a vendor that echoes a different one
		// has resumed something else; better to fail than to attach a session
		// to the wrong conversation.
		if loadRes.SessionID != "" && loadRes.SessionID != resume {
			cleanup()
			return nil, fmt.Errorf("%w: session/load returned %q, asked for %q",
				ErrProcessFailed, loadRes.SessionID, resume)
		}
		rt.acpSessionID = resume
		return rt, nil
	}

	var newRes acp.SessionNewResult
	if err := conn.Call(hsCtx, "session/new", acp.SessionNewRequest{
		Cwd:        pol.WorkDir,
		McpServers: mcpServers,
		Meta:       meta,
	}, &newRes); err != nil {
		cleanup()
		return nil, handshakeErr("session/new", err)
	}
	if newRes.SessionID == "" {
		cleanup()
		return nil, fmt.Errorf("%w: empty sessionId from session/new", ErrProcessFailed)
	}
	rt.acpSessionID = newRes.SessionID
	return rt, nil
}

type claudeMetaRoot struct {
	ClaudeCode *struct {
		CostUSD      *float64 `json:"costUsd"`
		InputTokens  *int     `json:"inputTokens"`
		OutputTokens *int     `json:"outputTokens"`
	} `json:"claudeCode"`
}

func (a *ClaudeAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	out := NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}
	if len(meta) == 0 {
		return out, nil
	}
	var root claudeMetaRoot
	if err := json.Unmarshal(meta, &root); err != nil {
		return out, err
	}
	if root.ClaudeCode != nil {
		if root.ClaudeCode.CostUSD != nil {
			out.CostUSD = *root.ClaudeCode.CostUSD
		}
		if root.ClaudeCode.InputTokens != nil {
			out.InputTokens = *root.ClaudeCode.InputTokens
		}
		if root.ClaudeCode.OutputTokens != nil {
			out.OutputTokens = *root.ClaudeCode.OutputTokens
		}
	}
	return out, nil
}

func (a *ClaudeAdapter) NormalizeToolSummary(meta json.RawMessage, update json.RawMessage) ([]string, string, error) {
	_ = meta
	_ = update
	return nil, "", nil
}

// stdioPipe adapts separate stdin writer + stdout reader into one ReadWriteCloser
// for acp.NewConn (write → agent stdin, read ← agent stdout).
type stdioPipe struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *stdioPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *stdioPipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *stdioPipe) Close() error {
	errW := p.w.Close()
	errR := p.r.Close()
	if errW != nil {
		return errW
	}
	return errR
}

// procKiller adapts *sandbox.Proc to acp.ProcessKiller (adds Signal).
type procKiller struct {
	proc *sandbox.Proc
}

func (k *procKiller) PID() int    { return k.proc.PID() }
func (k *procKiller) Kill() error { return k.proc.Kill() }
func (k *procKiller) Wait() error { return k.proc.Wait() }
func (k *procKiller) Signal(sig os.Signal) error {
	pid := k.proc.PID()
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

type claudeRuntime struct {
	conn         *acp.Conn
	proc         *sandbox.Proc
	killer       acp.ProcessKiller
	acpSessionID string
	cwd          string
	cancelServe  context.CancelFunc
	serveDone    chan struct{}

	mu       sync.Mutex
	handlers RuntimeHandlers
	closed   bool
	exitErr  error
}

// Dead reports whether the agent process exited unexpectedly (crash, pipe
// EOF). A dead runtime rejects further calls; the owning session should
// surface status error.
func (r *claudeRuntime) Dead() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed && r.exitErr != nil, r.exitErr
}

func (r *claudeRuntime) ACPSessionID() string { return r.acpSessionID }

func (r *claudeRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = h
}

func (r *claudeRuntime) handlersSnapshot() RuntimeHandlers {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handlers
}

func (r *claudeRuntime) Prompt(ctx context.Context, text string) (PromptOutcome, error) {
	if err := ctx.Err(); err != nil {
		return PromptOutcome{}, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionNotFound
	}
	conn := r.conn
	sid := r.acpSessionID
	r.mu.Unlock()

	var res acp.PromptResult
	err := conn.Call(ctx, "session/prompt", acp.PromptRequest{
		SessionID: sid,
		Prompt: []acp.ContentBlock{{
			Type: "text",
			Text: text,
		}},
	}, &res)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PromptOutcome{}, ErrCancelled
		}
		return PromptOutcome{}, fmt.Errorf("%w: session/prompt: %v", ErrProcessFailed, err)
	}
	return PromptOutcome{StopReason: res.StopReason, Meta: res.Meta}, nil
}

func (r *claudeRuntime) Cancel(ctx context.Context) error {
	r.mu.Lock()
	conn := r.conn
	sid := r.acpSessionID
	killer := r.killer
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil
	}

	// Best-effort ACP cancel, then OS shutdown ladder on the sandbox supervisor PID.
	if conn != nil && sid != "" {
		_ = conn.Notify(context.Background(), "session/cancel", map[string]any{
			"sessionId": sid,
		})
	}
	if killer != nil {
		cfg := acp.ShutdownConfig{
			CancelWait: 5 * time.Second,
			TermWait:   5 * time.Second,
		}
		// Honor caller ctx for overall cancel, but Shutdown escalates even if cancelled.
		_ = ctx
		return acp.Shutdown(context.Background(), killer, cfg)
	}
	return nil
}

func (r *claudeRuntime) Close(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cancel := r.cancelServe
	conn := r.conn
	proc := r.proc
	done := r.serveDone
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if proc != nil {
		_ = proc.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// claudeConnHandler bridges agent→client ACP methods onto RuntimeHandlers.
type claudeConnHandler struct {
	rt              *claudeRuntime
	onUnknownMethod func(method string, params json.RawMessage)
}

func (h *claudeConnHandler) RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	handlers := h.rt.handlersSnapshot()
	if handlers.OnPermission == nil {
		return nil, ErrPermissionDenied
	}
	return handlers.OnPermission(ctx, params)
}

func (h *claudeConnHandler) SessionUpdate(ctx context.Context, params json.RawMessage) error {
	_ = ctx
	handlers := h.rt.handlersSnapshot()
	if handlers.OnUpdate == nil {
		return nil
	}
	var p acp.SessionUpdateParams
	if err := json.Unmarshal(params, &p); err != nil {
		// Forward raw blob if shape is unexpected.
		handlers.OnUpdate(params, nil)
		return nil
	}
	handlers.OnUpdate(p.Update, p.Meta)
	return nil
}

// UnknownMethod refuses agent→client methods this build does not implement,
// and — unlike before — says so somewhere a person can see.
//
// The refusal itself is correct: answering fs/read_text_file with a made-up
// result would be worse than an error. What was wrong is that the error went
// only to the agent, so an ACP surface DevKit is specified to serve could have
// been requested on every turn since Phase 4 without leaving a trace.
func (h *claudeConnHandler) UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	_ = ctx
	if h.onUnknownMethod != nil && markMethodSeen(method) {
		h.onUnknownMethod(method, params)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

// unknownMethodSeen dedupes the report. Process-wide rather than per session:
// the question these answer is "does this vendor use this method at all", and
// the second occurrence adds nothing to it.
var unknownMethodSeen sync.Map

func markMethodSeen(method string) bool {
	_, loaded := unknownMethodSeen.LoadOrStore(method, struct{}{})
	return !loaded
}
