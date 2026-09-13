package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
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
	// Command is an absolute agent binary that overrides the managed runtime —
	// DEVKIT_CLAUDE_ACP_COMMAND, for development and CI. Non-absolute commands
	// are rejected.
	Command string
	// Args are appended after Command (without the binary path).
	Args []string
	// Runtime resolves the adapter DevKit installed (agentruntime.Manager).
	// Called on every Open and Load rather than once at startup, so an
	// install, update or rollback applies to the next session without a
	// restart. Nil with no Command means the agent is not installed.
	Runtime func() (agentruntime.Resolved, error)
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

var (
	_ SessionLoader  = (*ClaudeAdapter)(nil)
	_ RuntimeDeleter = (*claudeRuntime)(nil)
)

// ClaudeAdapter is a VendorAdapter that speaks ACP to Claude inside a sandbox.
type ClaudeAdapter struct {
	opts ClaudeOptions

	// The last AuthMethods probe, keyed by the launch it ran: sign-in lists
	// the methods and then starts one, and a second node start in between buys
	// nothing. A new install or version changes the key.
	authMu      sync.Mutex
	authKey     string
	authMethods []AuthMethod
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

	l, err := a.resolveCommand()
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	pol, cmd := sandboxFor(workDir, l, spec)
	return pol, cmd, nil
}

// launch is a resolved agent command and what the jail needs for it to run.
type launch struct {
	path string
	args []string
	ro   []string
	// env is merged over claudeEnvAllowlist; a key here wins.
	env map[string]string
}

// sandboxFor is the one place the Claude jail policy is written, shared by
// Open, Load and Probe so an install is verified under the policy its sessions
// will actually get.
func sandboxFor(workDir string, l launch, spec OpenSpec) (sandbox.Policy, sandbox.Cmd) {
	env := claudeEnvAllowlist()
	for key, value := range l.env {
		env[key] = value
	}
	pol := sandbox.Policy{
		WorkDir:        workDir,
		Network:        sandbox.NetworkHost,
		Env:            env,
		ReadOnlyPaths:  uniqueAbsPaths(append(append([]string(nil), l.ro...), spec.ReadOnlyPaths...)...),
		ReadWritePaths: claudeCredentialPaths(env["HOME"]),
		// Subscription login keeps the OAuth token in the macOS keychain
		// (service "Claude Code-credentials-*"), not in ~/.claude.json.
		AllowKeychain:    true,
		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
	}
	return pol, sandbox.Cmd{Path: l.path, Args: l.args}
}

// resolveCommand picks what to run: the Command override, else the installed
// runtime, else ErrAgentNotInstalled.
//
// There is no PATH lookup and no npx any more. npx cannot work inside the
// jail — it needs a writable ~/.npm and the whole npm lib tree (measured: it
// died on EPERM before downloading anything) — and a global install was
// whatever version the machine happened to carry, invisible anyway to an app
// opened from the Dock.
func (a *ClaudeAdapter) resolveCommand() (launch, error) {
	if cmd := a.opts.Command; cmd != "" {
		if !filepath.IsAbs(cmd) {
			return launch{}, fmt.Errorf("%w: command must be absolute", ErrProcessFailed)
		}
		path := filepath.Clean(cmd)
		return launch{
			path: path,
			args: append([]string(nil), a.opts.Args...),
			ro:   uniqueAbsPaths(append([]string{path}, nodeModulesRoot(path)...)...),
		}, nil
	}
	if a.opts.Runtime == nil {
		return launch{}, ErrAgentNotInstalled
	}
	rt, err := a.opts.Runtime()
	if errors.Is(err, agentruntime.ErrNotInstalled) {
		return launch{}, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err)
	}
	if err != nil {
		// Both sentinels stay reachable: the bridge names a missing Node
		// before it falls back to "agent process failed".
		return launch{}, fmt.Errorf("%w: %w", ErrProcessFailed, err)
	}
	return runtimeLaunch(rt), nil
}

func runtimeLaunch(rt agentruntime.Resolved) launch {
	return launch{
		path: rt.Command,
		args: append([]string(nil), rt.Args...),
		ro:   uniqueAbsPaths(append([]string{rt.Command}, rt.ReadOnlyPaths...)...),
		env:  rt.Env,
	}
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
// still works but a #!/usr/bin/env node shebang does not; a managed runtime
// replaces PATH with node's directory first. Values must never be logged.
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
// DevkitClientCapabilitiesForTest exposes the declaration to the external test
// package. The capabilities themselves stay unexported: nothing outside this
// adapter should be choosing what DevKit claims to support.
func DevkitClientCapabilitiesForTest() map[string]any { return devkitClientCapabilities() }

func devkitClientCapabilities() map[string]any {
	return map[string]any{
		// The client understands updates that come from a subagent. DevKit
		// renders them in the parent transcript rather than as their own
		// session; nesting is a UI question, losing the text was a wire one.
		"subagents": map[string]any{},
		// Terminal sign-in: DevKit runs the agent's own login command in a
		// terminal tab (AuthMethods → AgentHubStartSignIn), so it is served now.
		// Without it claude-agent-acp 0.74.0 offers no authMethods at all, and a
		// signed-out user had no way in but typing a command DevKit had to
		// describe. `auth.terminal` is the ACP field the adapter reads;
		// _meta["terminal-auth"] is the older gate that also makes it attach the
		// full command line to each method (resolveAuthMethods decides whether
		// to trust that).
		"auth": map[string]any{"terminal": true},
		// The structured subagent events sit behind a second gate: an
		// extension declared under _meta.jetbrains.air, which claude-agent-acp
		// honours. Declaring `subagents` alone forwards a subagent's text but
		// not the fact that one exists — measured: with the block below, the
		// same prompt also produced subagent_spawned (with the subagent's id,
		// name and task) and subagent_state_update.
		//
		// The namespace is JetBrains' and the vendor's, not something DevKit
		// chose. asyncTasks and sessionFailure are declared alongside because
		// they are the same extension and the same version; whether their
		// updates get modelled is a separate question from whether we are
		// allowed to see them.
		"_meta": map[string]any{
			"terminal-auth": true,
			"jetbrains": map[string]any{
				"air": map[string]any{
					"version":      1,
					"capabilities": []string{"nativeSubagentSessions", "asyncTasks", "sessionFailure"},
				},
			},
		},
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
	// What the jail is about to run, for the log: a session that fails to open
	// says only "Agent process failed" to the UI, and without this there is no
	// way to tell a wrong node from a missing grant. Paths only — the env
	// carries API keys and is never logged.
	log.Printf("agenthub: claude: starting %s %s in %s (read-only %v, sockets %v, resume %q)",
		cmd.Path, strings.Join(cmd.Args, " "), pol.WorkDir, pol.ReadOnlyPaths, pol.AllowUnixSockets, resume)

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
		// A signed-out agent answers session/new or session/load with an error
		// and stays alive; that is a sign-in to offer, not a crash.
		if isAuthRequired(err) {
			return fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err)
		}
		select {
		case <-rt.serveDone:
			reason := stderrReason(proc)
			logStderrTail("claude "+stage, proc)
			return fmt.Errorf("%w: %s: agent process exited during handshake%s", ErrProcessFailed, stage, reason)
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
		herr := handshakeErr("initialize", err)
		cleanup()
		return nil, herr
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
			// Judged before cleanup: cleanup kills the process, after which every
			// failure read as "exited during handshake" — including an error the
			// agent answered with while it was still running.
			herr := handshakeErr("session/load", err)
			cleanup()
			return nil, herr
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
		herr := handshakeErr("session/new", err)
		cleanup()
		return nil, herr
	}
	if newRes.SessionID == "" {
		cleanup()
		return nil, fmt.Errorf("%w: empty sessionId from session/new", ErrProcessFailed)
	}
	rt.acpSessionID = newRes.SessionID
	return rt, nil
}

// probeTimeout bounds Probe's initialize. Shorter than handshakeTimeout:
// nothing downloads here, the adapter only has to start and answer.
const probeTimeout = 20 * time.Second

// Probe starts an installed runtime under the jail a real session gets, sends
// ACP initialize and stops it. It is agentruntime.Manager.Verify: the proof
// that node, the entry and the read grants are enough, before a version is
// activated. A static look at the files cannot say that — a missing grant
// looks exactly like a present file until the jail refuses to open it.
//
// No session is created, so neither credentials nor a model are exercised.
func (a *ClaudeAdapter) Probe(ctx context.Context, rt agentruntime.Resolved) error {
	_, err := a.initialize(ctx, runtimeLaunch(rt))
	return err
}

// AuthMethods lists the terminal sign-ins the agent DevKit would launch offers,
// with the command each would run. It needs no session and no login: the
// methods come from initialize, under the same jail as Probe.
func (a *ClaudeAdapter) AuthMethods(ctx context.Context) ([]AuthMethod, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	return a.authMethodsFor(ctx, l)
}

// SignInCommand resolves one method to the process to start, with the env a
// login needs. The method is looked up again rather than taken from the
// caller, so the UI can only ever name an id, never a command.
func (a *ClaudeAdapter) SignInCommand(ctx context.Context, methodID string) (SignInCommand, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return SignInCommand{}, err
	}
	methods, err := a.authMethodsFor(ctx, l)
	if err != nil {
		return SignInCommand{}, err
	}
	for _, m := range methods {
		if m.ID == methodID {
			return SignInCommand{Path: m.Command, Args: m.Args, Env: signInEnv(l.env), Label: m.Label}, nil
		}
	}
	return SignInCommand{}, ErrAuthMethodUnknown
}

func (a *ClaudeAdapter) authMethodsFor(ctx context.Context, l launch) ([]AuthMethod, error) {
	key := l.path + "\x00" + strings.Join(l.args, "\x00")
	// Held across the probe on purpose: two callers asking at once should
	// start one node, not two.
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if a.authKey == key && a.authMethods != nil {
		return a.authMethods, nil
	}
	res, err := a.initialize(ctx, l)
	if err != nil {
		return nil, err
	}
	a.authKey, a.authMethods = key, resolveAuthMethods(res.AuthMethods, l)
	return a.authMethods, nil
}

// initialize starts l under the probe jail, sends ACP initialize and stops it.
func (a *ClaudeAdapter) initialize(ctx context.Context, l launch) (acp.InitializeResult, error) {
	workDir, err := os.MkdirTemp("", "devkit-agent-probe-")
	if err != nil {
		return acp.InitializeResult{}, fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
	}
	defer os.RemoveAll(workDir)
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}

	pol, cmd := sandboxFor(workDir, l, OpenSpec{})
	proc, err := a.runner().StartPiped(ctx, pol, cmd)
	if err != nil {
		return acp.InitializeResult{}, mapSandboxErr(err)
	}
	stdin, stdout := proc.StdinWriter(), proc.StdoutReader()
	if stdin == nil || stdout == nil {
		_ = proc.Kill()
		return acp.InitializeResult{}, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}

	conn := acp.NewConn(&stdioPipe{r: stdout, w: stdin}, &claudeConnHandler{rt: &claudeRuntime{}})
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = conn.Serve(serveCtx)
	}()
	defer func() {
		cancelServe()
		_ = conn.Close()
		_ = proc.Kill()
		<-serveDone
	}()

	// Same shape as open's handshake: the process exiting ends the wait at
	// once, the timeout is only for one that runs and never answers.
	callCtx, cancelCall := context.WithTimeout(ctx, probeTimeout)
	defer cancelCall()
	go func() {
		select {
		case <-serveDone:
			cancelCall()
		case <-callCtx.Done():
		}
	}()

	var res acp.InitializeResult
	err = conn.Call(callCtx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: devkitClientCapabilities(),
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &res)
	if err == nil {
		return res, nil
	}
	if ctx.Err() != nil {
		return acp.InitializeResult{}, ctx.Err()
	}
	select {
	case <-serveDone:
		return acp.InitializeResult{}, fmt.Errorf("%w: the agent exited before answering initialize%s", ErrProcessFailed, stderrReason(proc))
	default:
		return acp.InitializeResult{}, fmt.Errorf("%w: initialize: %v", ErrProcessFailed, err)
	}
}

// acpAuthRequiredCode is RequestError.authRequired() in the ACP SDK: what a
// signed-out agent answers session/new, session/load or session/prompt with.
const acpAuthRequiredCode = -32000

// isAuthRequired reports an agent error that means "sign in": ACP's
// authRequired code, or claude-agent-acp's -32603 with errorKind
// authentication_failed for a login that expired and could not refresh.
func isAuthRequired(err error) bool {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == acpAuthRequiredCode || rpcErrorKind(err) == "authentication_failed"
}

// rpcErrorKind is the errorKind claude-agent-acp puts in a JSON-RPC error's
// data, or "". It is how an expired login is told apart from a crash: both
// arrive as -32603 "Internal error".
func rpcErrorKind(err error) string {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) || len(rpcErr.Data) == 0 {
		return ""
	}
	var data struct {
		ErrorKind string `json:"errorKind"`
	}
	if json.Unmarshal(rpcErr.Data, &data) != nil {
		return ""
	}
	return data.ErrorKind
}

// logStderrTail writes the end of a dead agent's stderr to the log. The reason
// that reaches the error is one line; the lines around it are usually what
// explains it — a stack, the path that could not be opened.
func logStderrTail(stage string, proc *sandbox.Proc) {
	_, stderr := proc.Output()
	tail := strings.TrimSpace(string(stderr))
	if tail == "" {
		log.Printf("agenthub: %s: agent exited with nothing on stderr", stage)
		return
	}
	if len(tail) > 2000 {
		tail = "…" + tail[len(tail)-2000:]
	}
	log.Printf("agenthub: %s: agent stderr:\n%s", stage, tail)
}

// stderrReason is the line of a dead agent's stderr that says why, or "".
// Node ends an uncaught exception with a "Node.js v24…" banner, so the last
// line naming an error is wanted, not the last line.
func stderrReason(proc *sandbox.Proc) string {
	_ = proc.Kill()
	waited := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		return "" // the buffer is still being written; reading it would race
	}
	_, stderr := proc.Output()
	var reason string
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Error") || strings.Contains(line, "error") {
			reason = line
		}
	}
	if reason == "" {
		return ""
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return ": " + reason
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

// Delete asks Claude to remove its own copy of the session, satisfying
// RuntimeDeleter.
//
// DevKit's DeleteSession removed its rows and left this behind: the vendor
// keeps every session as a file under its own directory, so a conversation the
// user deleted "for good" stayed readable there. The dialog says it cannot be
// undone, and until this it could be.
//
// ponytail: only reachable while a runtime is live. Deleting a session whose
// process is gone would mean spawning one to clean up after it — resumeRuntime
// makes that possible if the leftovers ever matter more than the cost.
func (r *claudeRuntime) Delete(ctx context.Context) error {
	if r.acpSessionID == "" {
		return nil
	}
	var out json.RawMessage
	if err := r.conn.Call(ctx, "session/delete", acp.SessionDeleteRequest{
		SessionID: r.acpSessionID,
	}, &out); err != nil {
		return fmt.Errorf("%w: session/delete: %v", ErrProcessFailed, err)
	}
	return nil
}

func (r *claudeRuntime) handlersSnapshot() RuntimeHandlers {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handlers
}

func (r *claudeRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock) (PromptOutcome, error) {
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
		Prompt:    prompt,
	}, &res)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PromptOutcome{}, ErrCancelled
		}
		// A prompt is where the SDK actually starts Claude Code, so an agent that
		// dies here is the common failure after a clean handshake. Its stderr is
		// the only record of why.
		reason := ""
		select {
		case <-r.serveDone:
			if r.proc != nil {
				reason = stderrReason(r.proc)
				logStderrTail("claude session/prompt", r.proc)
			}
		default:
		}
		log.Printf("agenthub: claude session/prompt (session %s): %v%s", sid, err, reason)
		if isAuthRequired(err) {
			return PromptOutcome{}, fmt.Errorf("%w: %v", ErrAgentAuthRequired, err)
		}
		return PromptOutcome{}, fmt.Errorf("%w: session/prompt: %v%s", ErrProcessFailed, err, reason)
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
