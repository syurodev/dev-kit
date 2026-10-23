package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"slices"
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

// serveExitGrace is how long a failed handshake call waits to find out whether
// the vendor process died, before concluding it is alive but unresponsive.
//
// Short because it is only closing a race between two notifications of the
// same event, not waiting for anything to happen: the write already failed,
// and the serve loop is either about to return or is not going to.
const serveExitGrace = 250 * time.Millisecond

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
	// FileChangeReports says whether to declare the agentFileChangeReport
	// capability. Read on every Open and Load, never captured, because the user
	// can turn checkpoints off between two sessions.
	//
	// Behind a switch because declaring it is not free: it turns on Claude's
	// own native checkpointing for every session in the process, which writes
	// file history under ~/.claude. A user who turned checkpoints off should
	// get no file history at all, from DevKit or from the vendor.
	FileChangeReports func() bool
}

var (
	_ SessionLoader        = (*ClaudeAdapter)(nil)
	_ RuntimeForker        = (*ClaudeAdapter)(nil)
	_ RuntimeDeleter       = (*claudeRuntime)(nil)
	_ VendorSessionDeleter = (*claudeRuntime)(nil)
	_ VendorConfigCatalog  = (*ClaudeAdapter)(nil)
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

	// cliTimeout and cliGrace override claudeCLITimeout / claudeCLIExitGrace (tests).
	cliTimeout time.Duration
	cliGrace   time.Duration
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
	pol, cmd, l, err := a.resolveSandbox(spec)
	l.releaseLease()
	return pol, cmd, err
}

// resolveSandbox retains the resolved artifact lease for open to transfer to
// the process. buildSandbox is inspection-only and releases it immediately.
func (a *ClaudeAdapter) resolveSandbox(spec OpenSpec) (sandbox.Policy, sandbox.Cmd, launch, error) {
	workDir, err := filepath.Abs(spec.Cwd)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, launch{}, fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		return sandbox.Policy{}, sandbox.Cmd{}, launch{}, fmt.Errorf("%w: project path", ErrProcessFailed)
	}

	l, err := a.resolveCommand()
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, launch{}, err
	}
	pol, cmd := sandboxFor(workDir, l, spec)
	return pol, cmd, l, nil
}

// launch is a resolved agent command and what the jail needs for it to run.
type launch struct {
	path string
	args []string
	ro   []string
	// env is merged over claudeEnvAllowlist; a key here wins.
	env     map[string]string
	release func()
}

func (l launch) releaseLease() {
	if l.release != nil {
		l.release()
	}
}

// sandboxFor is the one place the Claude jail policy is written, shared by
// Open, Load and Probe so an install is verified under the policy its sessions
// will actually get.
func sandboxFor(workDir string, l launch, spec OpenSpec) (sandbox.Policy, sandbox.Cmd) {
	env := claudeEnvAllowlist()
	inheritToolEnvironment(env, spec)
	for key, value := range l.env {
		env[key] = value
	}
	for key, value := range spec.ExtraEnv {
		env[key] = value
	}
	completeToolPath(env, spec)
	var readWrite, denyRead []string
	if spec.AccountHome != "" {
		// An added account is a config directory of its own. The machine's own
		// ~/.claude and ~/.claude.json hold the built-in account's login, so they
		// are not granted here (a shared $HOME still shows them, as it always did).
		env["CLAUDE_CONFIG_DIR"] = spec.AccountHome
		keep := append([]string{spec.AccountHome}, spec.AccountSharedPaths...)
		readWrite = append(keep, spec.ExtraReadWritePaths...)
		denyRead = carveOutAll(spec.DenyReadPaths, keep...)
	} else {
		readWrite = append(claudeCredentialPaths(env["HOME"]), spec.ExtraReadWritePaths...)
		denyRead = append([]string(nil), spec.DenyReadPaths...)
	}
	pol := sandbox.Policy{
		WorkDir:                workDir,
		Network:                sandbox.NetworkHost,
		AllowLoopbackListeners: true,
		Env:                    env,
		ReadOnlyPaths:          uniqueAbsPaths(append(append([]string(nil), l.ro...), spec.ReadOnlyPaths...)...),
		ReadWritePaths:         uniqueAbsPaths(readWrite...),
		// Subscription login keeps the OAuth token in the macOS keychain
		// (service "Claude Code-credentials-*"), not in ~/.claude.json.
		AllowKeychain:    true,
		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
		DenyReadPaths:    denyRead,
		// nil keeps sandbox.Policy's own default (private home on); false
		// when AgentSandboxSettings.ShareHome is on, which is its default.
		PrivateHome: spec.PrivateHome,
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
		return launch{}, vendorError(AgentTypeClaude, ErrAgentNotInstalled)
	}
	rt, err := a.opts.Runtime()
	if errors.Is(err, agentruntime.ErrNotInstalled) {
		return launch{}, vendorError(AgentTypeClaude, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err))
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
		path:    rt.Command,
		args:    append([]string(nil), rt.Args...),
		ro:      uniqueAbsPaths(append([]string{rt.Command}, rt.ReadOnlyPaths...)...),
		env:     rt.Env,
		release: rt.Release,
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
func DevkitClientCapabilitiesForTest(withFileChangeReport bool) map[string]any {
	return devkitClientCapabilities(withFileChangeReport)
}

// fileChangeReports reports whether this adapter should ask for the vendor's
// file-change report. Absent hook means no: a build that never wired
// checkpoints must not switch on the vendor's file history.
func (a *ClaudeAdapter) fileChangeReports() bool {
	return a.opts.FileChangeReports != nil && a.opts.FileChangeReports()
}

// devkitClientCapabilities is the declaration with the optional extensions
// resolved. withFileChangeReport adds the one capability that has a side effect
// on the user's disk, so it is passed in rather than read from anywhere.
func devkitClientCapabilities(withFileChangeReport bool) map[string]any {
	caps := devkitBaseCapabilities()
	if withFileChangeReport {
		air := caps["_meta"].(map[string]any)["jetbrains"].(map[string]any)["air"].(map[string]any)
		air["capabilities"] = append(air["capabilities"].([]string), "agentFileChangeReport")
	}
	return caps
}

func devkitBaseCapabilities() map[string]any {
	return map[string]any{
		// The client understands updates that come from a subagent. DevKit
		// renders them in the parent transcript rather than as their own
		// session; nesting is a UI question, losing the text was a wire one.
		"subagents": map[string]any{},
		// Terminal sign-in: DevKit runs the agent's own login command in a
		// terminal tab (AuthMethods → AgentHubStartSignIn), so it is served now.
		// Without it claude-agent-acp offers no authMethods at all (measured on
		// 0.74.0; the same gate is in 0.78.0's initialize), and a
		// signed-out user had no way in but typing a command DevKit had to
		// describe. `auth.terminal` is the ACP field the adapter reads;
		// _meta["terminal-auth"] is the older gate that also makes it attach the
		// full command line to each method (resolveAuthMethods decides whether
		// to trust that).
		"auth": map[string]any{"terminal": true},
		// Questions: elicitation/create is answered by claudeConnHandler.Elicit.
		// Without `form`, claude-agent-acp (0.74.0 and 0.78.0) puts AskUserQuestion in
		// disallowedTools (acp-agent.js, where it reads
		// clientCapabilities.elicitation.form) — the model never sees the tool
		// and writes its question as prose that ends the turn. The same gate
		// forwards MCP servers' elicitations and the refusal-fallback prompt, all
		// through the one method, so the UI has to render any flat form schema,
		// not just AskUserQuestion's.
		//
		// `url` is deliberately absent: DevKit serves neither the browser
		// handoff nor elicitation/complete.
		"elicitation": map[string]any{"form": map[string]any{}},
		// The structured subagent events sit behind a second gate: an
		// extension declared under _meta.jetbrains.air, which claude-agent-acp
		// honours. Declaring `subagents` alone forwards a subagent's text but
		// not the fact that one exists — measured: with the block below, the
		// same prompt also produced subagent_spawned (with the subagent's id,
		// name and task) and subagent_state_update.
		//
		// The namespace is JetBrains' and the vendor's, not something DevKit
		// chose. asyncTasks and sessionFailure are declared alongside because
		// they are the same extension and the same version; their updates are
		// parsed (UpdateAsyncTask*), which is what makes asking for them honest.
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

// sessionNewMeta keeps Claude's normal user, project and local customization
// sources. Skills, plugins, hooks and permission preferences belong to the
// user's working environment. DevKit's own tools still enforce their gates
// after dispatch; the programmatic Git hook remains in place.
func sessionNewMeta(hookCommand, prompt string, gateOwnedTools ...string) json.RawMessage {
	options := map[string]any{"settingSources": []string{"user", "project", "local"}}
	if len(gateOwnedTools) > 0 {
		options["allowedTools"] = gateOwnedTools
	}
	body := map[string]any{
		"systemPrompt": map[string]any{
			"type":   "preset",
			"preset": "claude_code",
			"append": prompt,
		},
	}
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
	body["claudeCode"] = map[string]any{"options": options}
	encoded, err := json.Marshal(body)
	if err != nil {
		// Only static, marshalable values go in above; a failure here would be
		// a programming error, and dropping the hook silently is worse than
		// dropping the whole _meta loudly at the next line.
		return json.RawMessage(`{"claudeCode":{"options":{"settingSources":[]}}}`)
	}
	return encoded
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

// openPlan is which of the three ways a process gets a session: a fresh one, a
// resumed one, or a branch of another one. They share everything up to the last
// call or two of the handshake — same jail, same credentials, same _meta —
// because a session that came back a different way must still run under the
// policy the original did.
type openPlan struct {
	// resume is a vendor session id to load.
	resume string
	// forkFrom and forkAt branch that session at that message, then load the
	// branch. Both empty means no fork.
	forkFrom string
	forkAt   string
}

// Open starts a sandboxed Claude ACP process, connects over stdio, and creates a session.
func (a *ClaudeAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	return a.open(ctx, spec, openPlan{})
}

// Fork branches a session at one of its messages, satisfying RuntimeForker.
//
// One process does both calls. session/fork does not leave the new session
// loaded — measured (spike S1): forking from a process that never saw the
// source works, and the branch has to be loaded before it will take a prompt.
// Doing it here rather than in two adapter calls means a failed load kills the
// same process that made the fork, instead of leaving one running.
func (a *ClaudeAdapter) Fork(ctx context.Context, spec OpenSpec, srcACPSessionID, messageID string) (SessionRuntime, error) {
	if srcACPSessionID == "" || messageID == "" {
		return nil, fmt.Errorf("%w: fork needs a session and a message", ErrSessionNotFound)
	}
	return a.open(ctx, spec, openPlan{forkFrom: srcACPSessionID, forkAt: messageID})
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
// Measured against claude-agent-acp 0.74.0 and again on 0.78.0
// (TestClaudeACPResumeAcrossProcesses): the replay arrives as
// user_message_chunk and agent_message_chunk, and a prompt afterwards recalls
// what was said before the process died.
func (a *ClaudeAdapter) Load(ctx context.Context, spec OpenSpec, acpSessionID string) (SessionRuntime, error) {
	if acpSessionID == "" {
		return nil, fmt.Errorf("%w: load needs the vendor session id", ErrSessionNotFound)
	}
	return a.open(ctx, spec, openPlan{resume: acpSessionID})
}

func (a *ClaudeAdapter) open(ctx context.Context, spec OpenSpec, plan openPlan) (SessionRuntime, error) {
	resume := plan.resume
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pol, cmd, l, err := a.resolveSandbox(spec)
	if err != nil {
		return nil, err
	}
	customization := spec.Customizations[string(AgentTypeClaude)]
	plugin, releaseCustomization, err := claudeCustomization(customization)
	if err != nil {
		l.releaseLease()
		return nil, err
	}
	oldRelease := l.release
	l.release = func() {
		releaseCustomization()
		if oldRelease != nil {
			oldRelease()
		}
	}
	if plugin != "" {
		pol.ReadOnlyPaths = append(pol.ReadOnlyPaths, plugin)
	}
	pol.ReadOnlyPaths = append(pol.ReadOnlyPaths, customization.SkillFolders...)
	pol.DenyWritePaths = append(pol.DenyWritePaths, customization.SkillFolders...)

	owned := true
	defer func() {
		if owned {
			l.releaseLease()
		}
	}()
	// What the jail is about to run, for the log: a session that fails to open
	// says only "Agent process failed" to the UI, and without this there is no
	// way to tell a wrong node from a missing grant. Paths only — the env
	// carries API keys and is never logged.
	log.Printf("agenthub: claude: starting %s %s in %s (read-only %v, sockets %v, resume %q)",
		cmd.Path, strings.Join(cmd.Args, " "), pol.WorkDir, pol.ReadOnlyPaths, pol.AllowUnixSockets, resume)
	if plan.forkFrom != "" {
		log.Printf("agenthub: claude: forking session %s at message %s", plan.forkFrom, plan.forkAt)
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
	// Confirms the jail actually exec'd the agent, not just that bwrap itself
	// forked — a "starting" that never leaves that state and never logs
	// anything else again is otherwise indistinguishable from bwrap silently
	// failing to reach the exec at all.
	log.Printf("agenthub: claude: sandboxed process started, pid=%d", proc.PID())
	owned = false

	stdin := proc.StdinWriter()
	stdout := proc.StdoutReader()
	if stdin == nil || stdout == nil {
		_ = proc.Kill()
		_ = proc.Wait()
		l.releaseLease()
		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
	}

	rw := &stdioPipe{r: stdout, w: stdin}
	rt := &claudeRuntime{
		environment: captureEnvironment(AgentTypeClaude, spec, pol.Env, cmd.Path, ""),
		proc:        proc,
		cwd:         pol.WorkDir,
		killer:      &procKiller{proc: proc},
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
		_ = conn.Close()
		_ = proc.Kill()
		_ = proc.Wait()
		l.releaseLease()
		rt.noteServeExit(serveErr)
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
			return vendorError(AgentTypeClaude, fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err))
		}
		// A grace period rather than a peek. A vendor that died mid-handshake
		// fails this call two ways at once — the write hits a closed pipe, and
		// the serve loop notices EOF — and which arrives first is a race the
		// scheduler decides. Checking serveDone instantaneously lost that race
		// often enough to report "write |1: broken pipe", which tells the user
		// nothing about their agent binary having exited. The two are the same
		// event microseconds apart, so waiting briefly for the one that can
		// explain itself costs nothing on a path that has already failed.
		select {
		case <-rt.serveDone:
			reason := stderrReason(proc)
			logStderrTail("claude "+stage, proc)
			return fmt.Errorf("%w: %s: agent process exited during handshake%s", ErrProcessFailed, stage, reason)
		case <-time.After(serveExitGrace):
			// The process is still alive: this is handshakeTimeout firing on a
			// vendor that started but never answered, or a failure that is
			// genuinely not about the process at all. Whatever it wrote to
			// stderr before going silent — a keyring/D-Bus error, a missing
			// path inside the jail — is otherwise lost the moment cleanup()
			// kills it below, and "starting" forever with no further log line
			// is exactly what that loss looks like from the outside.
			logStderrTail("claude "+stage+" (still running, "+err.Error()+")", proc)
			return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
		}
	}

	var initRes acp.InitializeResult
	if err := conn.Call(hsCtx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: devkitClientCapabilities(a.fileChangeReports()),
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &initRes); err != nil {
		herr := handshakeErr("initialize", err)
		cleanup()
		return nil, herr
	}

	rt.steering, rt.goalControl = parseAgentExtensions(initRes.Meta)

	// Non-nil even with no endpoints: mcpServers is a required field.
	mcpServers := make([]any, 0, len(spec.McpServers))
	for _, srv := range spec.McpServers {
		if srv.Env == nil {
			srv.Env = []acp.EnvVariable{} // schema requires env; nil would serialise as null
		}
		mcpServers = append(mcpServers, srv)
	}
	for _, server := range customization.Servers {
		if server.Enabled && server.URL != "" {
			caps, _ := initRes.AgentCapabilities["mcpCapabilities"].(map[string]any)
			if supported, _ := caps["http"].(bool); !supported {
				cleanup()
				return nil, invalidCustomization("This Claude runtime does not support HTTP MCP servers. Update the runtime or use a stdio server.")
			}
		}
	}
	mcpServers = append(mcpServers, customizationMCP(customization, pol.Env)...)
	meta := claudeSubagentsMeta(claudeCustomizationMeta(sessionNewMeta(devkitHookCommand(spec), devkitPrompt(claudeNaming, spec.Brief), delegatedClaudeTools(spec)...), customization, plugin), spec.Subagents)

	if plan.forkFrom != "" {
		var forkRes acp.SessionForkResult
		if err := conn.Call(hsCtx, "session/fork", acp.SessionForkRequest{
			SessionID:  plan.forkFrom,
			Cwd:        pol.WorkDir,
			McpServers: mcpServers,
			Meta:       withForkMeta(meta, plan.forkAt),
		}, &forkRes); err != nil {
			herr := handshakeErr("session/fork", err)
			cleanup()
			return nil, herr
		}
		if forkRes.SessionID == "" || forkRes.SessionID == plan.forkFrom {
			// A vendor that answered with the source id has not branched
			// anything, and loading it would attach this session to the
			// conversation the rewind is trying to leave behind.
			cleanup()
			return nil, fmt.Errorf("%w: session/fork returned %q", ErrProcessFailed, forkRes.SessionID)
		}
		resume = forkRes.SessionID
	}

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
		rt.modes = loadRes.Modes
		rt.setHandshakeOptions(loadRes.ConfigOptions)
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
	// The handshake is the half of "starting" DevKit can see. Without this
	// line, a UI still showing "Starting" is unreadable: a session that never
	// opened and one that opened and is waiting on its first prompt reply
	// look identical in the log.
	log.Printf("agenthub: claude: session ready (acp session %s)", newRes.SessionID)
	rt.acpSessionID = newRes.SessionID
	rt.modes = newRes.Modes
	rt.setHandshakeOptions(newRes.ConfigOptions)
	return rt, nil
}

// withForkMeta adds the branch point to the session `_meta`.
//
// Merged into what session/new would send rather than sent alone: a fork makes
// a new vendor session, so it needs the same settingSources cut-off every other
// session gets. The two live under different top-level keys — `claudeCode` and
// `jetbrains` — so neither has to know about the other.
func withForkMeta(base json.RawMessage, messageID string) json.RawMessage {
	body := map[string]any{}
	if len(base) > 0 {
		if err := json.Unmarshal(base, &body); err != nil {
			body = map[string]any{}
		}
	}
	body["jetbrains"] = map[string]any{
		"air": map[string]any{
			"fork": map[string]any{"version": 1, "messageId": messageID},
		},
	}
	merged, err := json.Marshal(body)
	if err != nil {
		return base
	}
	return merged
}

// probeTimeout bounds Probe's initialize. Shorter than handshakeTimeout:
// nothing downloads here, the adapter only has to start and answer.
const probeTimeout = 20 * time.Second

// Probe starts an installed runtime under the jail a real session gets, sends
// ACP initialize and stops it. It is agentruntime.Manager.Verifiers[ClaudeACP]:
// the proof that node, the entry and the read grants are enough, before a
// version is activated. A static look at the files cannot say that — a
// missing grant looks exactly like a present file until the jail refuses to
// open it.
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
	defer l.releaseLease()
	return a.authMethodsFor(ctx, l)
}

// SignInCommand resolves one method to the process to start, with the env a
// login needs. The method is looked up again rather than taken from the
// caller, so the UI can only ever name an id, never a command.
func (a *ClaudeAdapter) SignInCommand(ctx context.Context, methodID string) (SignInCommand, error) {
	return a.signInCommand(ctx, methodID, "")
}

// SignInCommandFor is SignInCommand for an added account: the login runs with the
// account's home as its config directory, so the credential lands there. The
// home's value beats the host's own CLAUDE_CONFIG_DIR (signInEnv lets a launch
// variable replace the process environment's, whatever its spelling). home must be
// absolute: a relative one would put the login wherever the terminal happens to be.
// Nothing the login prints or stores passes through DevKit.
func (a *ClaudeAdapter) SignInCommandFor(ctx context.Context, methodID, home string) (SignInCommand, error) {
	if home == "" || !filepath.IsAbs(home) {
		return SignInCommand{}, fmt.Errorf("%w: the account's home must be an absolute path", ErrAccountHome)
	}
	return a.signInCommand(ctx, methodID, home)
}

var _ AccountAuthProvider = (*ClaudeAdapter)(nil)

func (a *ClaudeAdapter) signInCommand(ctx context.Context, methodID, home string) (SignInCommand, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return SignInCommand{}, err
	}
	owned := true
	defer func() {
		if owned {
			l.releaseLease()
		}
	}()
	methods, err := a.authMethodsFor(ctx, l)
	if err != nil {
		return SignInCommand{}, err
	}
	for _, m := range methods {
		if m.ID == methodID {
			env := make(map[string]string, len(l.env)+1)
			for k, v := range l.env {
				env[k] = v
			}
			if home != "" {
				// Whatever spelling the launch used, the account's home is the only one.
				for k := range env {
					if strings.EqualFold(k, "CLAUDE_CONFIG_DIR") {
						delete(env, k)
					}
				}
				env["CLAUDE_CONFIG_DIR"] = home
			}
			owned = false
			return SignInCommand{Release: l.release, Path: m.Command, Args: m.Args, Env: signInEnv(env), Label: m.Label}, nil
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
		_ = proc.Wait()
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
		_ = proc.Wait()
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
		ClientCapabilities: devkitClientCapabilities(a.fileChangeReports()),
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
	// vendor names whose agent this ACP runtime talks to; empty means Claude.
	// The runtime is the generic ACP one (opencode reuses it), and only its
	// messages and logs need to know which vendor they are about.
	vendor AgentType
	// permissionBefore and permissionAfter let a vendor adapter set the policy
	// around the person's answer: Before may rewrite the request or answer it
	// itself (answer non-nil), After may adjust what the person chose. Both nil
	// for Claude, whose own modes decide what is asked.
	permissionBefore func(ctx context.Context, params json.RawMessage) (rewritten, answer json.RawMessage)
	permissionAfter  func(answer json.RawMessage) json.RawMessage

	environment  *environmentSnapshot
	conn         *acp.Conn
	proc         *sandbox.Proc
	killer       acp.ProcessKiller
	acpSessionID string
	// modes is the handshake's answer, guarded by mu: set before the runtime is
	// returned, then only its CurrentModeID follows a successful SetMode (a
	// copy is swapped in, never mutated), so Modes() names the mode the agent
	// is in now rather than the one it started in. steering and goal are read from the same answer:
	// both are extensions the agent advertises in initialize's top-level _meta,
	// and calling either without that is a method the agent would refuse.
	modes *acp.SessionModeState
	// configOptions is what session/new said the agent's settings are, as the
	// agent wrote it. ConfigCatalog reads it for the effort levels it starts
	// from; options (below) is the live copy.
	configOptions json.RawMessage
	steering      bool
	goalControl   string
	cwd           string
	cancelServe   context.CancelFunc
	serveDone     chan struct{}

	// lastRetry is the provider failure the agent last said it was retrying
	// during the running prompt (opencode's retry notice).
	retryMu   sync.Mutex
	lastRetry *ProviderRetry

	mu       sync.Mutex
	handlers RuntimeHandlers
	closed   bool
	// options is the agent's settings kept current: what the handshake said,
	// changed by every setConfigOption answer. claude-agent-acp sends the model
	// list once, in the answer to session/new or session/load, and never again
	// until something changes — so DevKit has to keep it itself, or a session
	// that has not been prompted yet has no model choices anywhere.
	options []ConfigOption
	// announced: the first update handler has been given options.
	announced bool
	// closing records that Close asked the serve loop to stop. Without it the
	// only evidence of a deliberate shutdown was Serve's error, and Serve
	// reports a clean EOF as no error at all.
	closing bool
	exitErr error
}

// noteServeExit records that the serve loop ended. Everything after this is a
// runtime with no process behind it: Prompt and the settings calls refuse, and
// Dead tells Service to resume the session on a fresh one.
// vendorType is the vendor this runtime reports errors for.
func (r *claudeRuntime) vendorType() AgentType {
	if r.vendor == "" {
		return AgentTypeClaude
	}
	return r.vendor
}

func (r *claudeRuntime) noteServeExit(serveErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	// A deliberate Close cancels the serve context first; only record the exit
	// as abnormal when Serve failed for another reason.
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) && r.exitErr == nil {
		r.exitErr = serveErr
	}
}

// Dead reports whether the agent process went away without Close asking it to
// — a crash, an OOM kill, the shutdown ladder Cancel runs, or the agent simply
// exiting. Service.liveRuntime evicts a dead runtime and resumes the session on
// a fresh process, so Retry never prompts the corpse.
//
// The test is "the serve loop ended and nobody asked it to", NOT "it ended with
// an error": acp.Conn.Serve reports a clean EOF as a nil error, which is what a
// process exiting on its own looks like from here. Reading the error instead
// left the corpse in the live map for good — every later prompt on that session
// answered agenthub.sessionNotFound, and no retry could clear it, because
// nothing ever evicted the runtime that was refusing.
func (r *claudeRuntime) Dead() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed && !r.closing, r.exitErr
}

func (r *claudeRuntime) ACPSessionID() string { return r.acpSessionID }

// Modes satisfies RuntimeModes.
func (r *claudeRuntime) Modes() *acp.SessionModeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.modes
}

func (r *claudeRuntime) SetMode(ctx context.Context, modeID string) error {
	r.mu.Lock()
	conn, sid, closed := r.conn, r.acpSessionID, r.closed
	r.mu.Unlock()
	if closed {
		return ErrSessionNotFound
	}
	var result json.RawMessage
	if err := conn.Call(ctx, "session/set_mode", acp.SessionSetModeRequest{SessionID: sid, ModeID: modeID}, &result); err != nil {
		return fmt.Errorf("%w: session/set_mode: %v", ErrProcessFailed, err)
	}
	r.mu.Lock()
	if r.modes != nil {
		next := *r.modes
		next.CurrentModeID = modeID
		r.modes = &next
	}
	r.mu.Unlock()
	return nil
}

func (r *claudeRuntime) SetConfigOption(ctx context.Context, optionID, value string) error {
	// The draft picker is vendor-neutral and says reasoning_effort; Claude's own
	// name for it is effort.
	if optionID == codexEffortOption {
		optionID = claudeEffortOption
	}
	_, err := r.setConfigOption(ctx, optionID, value)
	return err
}

// setConfigOption answers with the agent's settings after the change: the
// effort levels on offer follow the model, so this is how a model's levels are
// read without running a turn.
func (r *claudeRuntime) setConfigOption(ctx context.Context, optionID, value string) ([]ConfigOption, error) {
	r.mu.Lock()
	conn, sid, closed := r.conn, r.acpSessionID, r.closed
	r.mu.Unlock()
	if closed {
		return nil, ErrSessionNotFound
	}
	var result struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := conn.Call(ctx, "session/set_config_option", acp.SessionSetConfigOptionRequest{SessionID: sid, ConfigID: optionID, Value: value}, &result); err != nil {
		return nil, fmt.Errorf("%w: session/set_config_option: %v", ErrProcessFailed, err)
	}
	answer := parseConfigOptions(result.ConfigOptions)
	r.noteConfigChange(optionID, value, answer)
	return answer, nil
}

// SetHandlers installs the callbacks. The first time an update handler arrives
// the agent's settings are announced through it, as Codex's runtime does: the
// handshake carried them, no config_option_update ever will, and a session
// opened by an agent has nobody listening until Service attaches.
func (r *claudeRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	r.handlers = h
	var announce json.RawMessage
	if h.OnUpdate != nil && !r.announced && len(r.options) > 0 {
		r.announced = true
		announce = r.configOptionsUpdateLocked()
	}
	r.mu.Unlock()
	if announce != nil {
		h.OnUpdate(announce, nil)
	}
}

// RefreshConfigOptions publishes the settings the runtime holds again, for a
// reader that missed the announcement (a window opened later). Nothing is
// asked of the agent: it has no call that lists them without changing one.
func (r *claudeRuntime) RefreshConfigOptions(context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	var update json.RawMessage
	if len(r.options) > 0 {
		update = r.configOptionsUpdateLocked()
	}
	onUpdate := r.handlers.OnUpdate
	r.mu.Unlock()
	if update != nil && onUpdate != nil {
		onUpdate(update, nil)
	}
	return nil
}

func (r *claudeRuntime) configOptionsUpdateLocked() json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateConfigOptions),
		"configOptions": r.options,
	})
}

// setHandshakeOptions records what session/new, session/load or session/fork
// answered with the settings.
func (r *claudeRuntime) setHandshakeOptions(raw json.RawMessage) {
	r.configOptions = raw
	parsed := parseConfigOptions(raw)
	r.mu.Lock()
	r.options = parsed
	r.mu.Unlock()
}

// noteConfigChange folds a session/set_config_option answer into the held
// settings. The answer lists the agent's settings after the change — the effort
// levels on offer follow the model — but only what it names is replaced, so an
// answer that leaves the rest out cannot erase it. The changed option's value
// is set from the request, since an answer that omits it still means it took.
func (r *claudeRuntime) noteConfigChange(optionID, value string, answer []ConfigOption) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chosen, _ := json.Marshal(value)
	for i := range r.options {
		if r.options[i].ID == optionID {
			r.options[i].CurrentValue = chosen
		}
	}
	r.options = mergeConfigOptions(r.options, answer)
}

// mergeConfigOptions replaces the options base has under the ids update names
// and appends the ones it does not have, keeping base's order.
func mergeConfigOptions(base, update []ConfigOption) []ConfigOption {
	out := append([]ConfigOption(nil), base...)
	for _, u := range update {
		replaced := false
		for i := range out {
			if out[i].ID == u.ID {
				out[i] = u
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, u)
		}
	}
	return out
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
	return r.DeleteSession(ctx, r.acpSessionID)
}

// DeleteSession asks this process's agent to forget any session, not just its
// own — what a rewind needs for the conversation it replaced, whose own process
// is already gone.
func (r *claudeRuntime) DeleteSession(ctx context.Context, acpSessionID string) error {
	if acpSessionID == "" {
		return nil
	}
	var out json.RawMessage
	if err := r.conn.Call(ctx, "session/delete", acp.SessionDeleteRequest{
		SessionID: acpSessionID,
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

func (r *claudeRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (PromptOutcome, error) {
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

	// session/prompt has no timeout on purpose — a turn takes as long as the
	// work does — so a turn that never answers is indistinguishable from one
	// still thinking. These two lines bracket the call: "sent" with nothing
	// after it says the agent has the prompt and is not replying, which is a
	// different bug from never having sent it at all.
	log.Printf("agenthub: %s session/prompt sent (session %s)", r.vendorType(), sid)
	r.retryMu.Lock()
	r.lastRetry = nil
	r.retryMu.Unlock()
	var res acp.PromptResult
	err := conn.Call(ctx, "session/prompt", acp.PromptRequest{
		SessionID: sid,
		Prompt:    prompt,
		Meta:      meta,
	}, &res)
	if err == nil {
		log.Printf("agenthub: %s session/prompt returned (session %s, stop %q)", r.vendorType(), sid, res.StopReason)
	}
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
				logStderrTail(string(r.vendorType())+" session/prompt", r.proc)
			}
		default:
		}
		log.Printf("agenthub: %s session/prompt (session %s): %v%s", r.vendorType(), sid, err, reason)
		if isAuthRequired(err) {
			return PromptOutcome{}, vendorError(r.vendorType(), fmt.Errorf("%w: %v", ErrAgentAuthRequired, err))
		}
		if r.refusedByLiveAgent(err) {
			return PromptOutcome{}, r.turnRefusal(err)
		}
		return PromptOutcome{}, fmt.Errorf("%w: session/prompt: %v%s", ErrProcessFailed, err, reason)
	}
	return PromptOutcome{StopReason: res.StopReason, Meta: res.Meta}, nil
}

// refusedByLiveAgent is whether a failed session/prompt was an answer from a
// process that is still running — a JSON-RPC error response — rather than a
// process that went away. The first is a turn the agent ended (its provider
// failed, its context is full); calling it a crash put "the agent stopped
// unexpectedly" and an error state on a session that was fine, and hid the
// sentence the agent gave. A transport error (closed pipe, EOF) or a serve
// loop that has already ended stays a process failure.
func (r *claudeRuntime) refusedByLiveAgent(err error) bool {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	select {
	case <-r.serveDone:
		return false
	default:
		return true
	}
}

// turnRefusal is a turn the live agent ended, as ErrTurnFailed carrying its own
// sentence, and says it in the session first: the banner is what the
// transcript keeps, where the error only reaches the caller of this prompt
// (the same order as codexTurnFailure).
func (r *claudeRuntime) turnRefusal(err error) error {
	message := rpcSentence(err)
	if message == "" {
		message = r.lastRetrySentence()
	}
	failure := SessionFailure{
		ID:       "agent-turn-failed",
		Revision: int(time.Now().Unix()),
		Category: "turn_failed",
		Severity: "error",
		Title:    "The turn didn't finish",
		Details:  message,
		Actions:  []string{"retry"},
	}
	if on := r.handlersSnapshot().OnUpdate; on != nil {
		on(sessionFailureUpdate(failure), nil)
	}
	failed := vendorMessageError(r.vendorType(), fmt.Errorf("%w: session/prompt: %s", ErrTurnFailed, message), message)
	r.retryMu.Lock()
	retry := r.lastRetry
	r.retryMu.Unlock()
	return WithStopHint(failed, retry.stopHint())
}

// lastRetrySentence is what the provider said the last time the agent retried
// during this prompt, or "". An agent that retried its provider until it gave
// up answers with a bare "Internal error"; the provider's own sentence is the
// one it showed while retrying.
func (r *claudeRuntime) lastRetrySentence() string {
	r.retryMu.Lock()
	retry := r.lastRetry
	r.retryMu.Unlock()
	if retry == nil {
		return ""
	}
	return retry.Sentence()
}

// noteRetry remembers the agent's retry notice of the running prompt. A null
// notice (the next attempt began) leaves the last one in place: it is what the
// final failure was about.
func (r *claudeRuntime) noteRetry(update json.RawMessage) {
	if !bytes.Contains(update, []byte("opencode/retry")) {
		return
	}
	if parsed := ParseSessionUpdate(update); parsed.Retry != nil {
		r.retryMu.Lock()
		r.lastRetry = parsed.Retry
		r.retryMu.Unlock()
	}
}

// rpcSentence is the part of a JSON-RPC error worth showing a person: the
// error's message, or — when that is the generic "Internal error" — the reason
// the agent put in data. "" when there is nothing more specific than
// "Internal error".
func rpcSentence(err error) string {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		return ""
	}
	generic := func(s string) bool {
		switch strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ".")) {
		case "", "internal error", "error":
			return true
		}
		return false
	}
	// opencode words it "Internal error: <reason>" (measured against 2.0.32 with
	// a provider answering 503): the prefix is JSON-RPC's code, not information.
	message := strings.TrimSpace(strings.TrimPrefix(oneLine(rpcErr.Message), "Internal error:"))
	if !generic(message) {
		return clipRunes(message, 300)
	}
	if len(rpcErr.Data) > 0 {
		var text string
		if json.Unmarshal(rpcErr.Data, &text) == nil && !generic(text) {
			return clipRunes(oneLine(text), 300)
		}
		var fields struct {
			Details string `json:"details"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(rpcErr.Data, &fields) == nil {
			for _, candidate := range []string{fields.Details, fields.Message, fields.Error} {
				if !generic(candidate) {
					return clipRunes(oneLine(candidate), 300)
				}
			}
		}
	}
	return ""
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
	r.closing = true
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
	if before := h.rt.permissionBefore; before != nil {
		rewritten, answer := before(ctx, params)
		if answer != nil {
			return answer, nil
		}
		if rewritten != nil {
			params = rewritten
		}
	}
	answer, err := handlers.OnPermission(ctx, params)
	if err == nil && h.rt.permissionAfter != nil {
		answer = h.rt.permissionAfter(answer)
	}
	return answer, err
}

// Elicit implements acp.ElicitationHandler, so Conn answers elicitation/create
// off its read loop.
//
// A runtime with no OnElicit declines: the person skipped. Never an error — the
// vendor would read that as a failure to present the question and abort the
// tool — and never a wait, because nothing would ever end it.
func (h *claudeConnHandler) Elicit(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	handlers := h.rt.handlersSnapshot()
	if handlers.OnElicit == nil {
		return append(json.RawMessage(nil), declinedElicitation...), nil
	}
	return handlers.OnElicit(ctx, params)
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
	update := p.Update
	h.rt.noteRetry(update)
	// Handlers are only installed after open() has set acpSessionID, so reading
	// it here — after the non-nil snapshot above — is ordered behind that write.
	if p.SessionID != "" && p.SessionID != h.rt.acpSessionID {
		update = markOrigin(update, p.SessionID)
	}
	handlers.OnUpdate(update, p.Meta)
	return nil
}

// markOrigin tags an update that came from a session other than the runtime's
// own. claude-agent-acp streams a subagent's messages on the same connection
// under the subagent's sessionId; without the tag they are recorded exactly
// like the parent's, and a rewind that picked one of their message ids would
// fork at a message the parent thread does not contain. An update that is not
// a JSON object is passed on unchanged.
func markOrigin(update json.RawMessage, origin string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(update, &fields); err != nil || fields == nil {
		return update
	}
	tag, err := json.Marshal(map[string]string{"origin": origin})
	if err != nil {
		return update
	}
	fields[DevKitWireKey] = tag
	out, err := json.Marshal(fields)
	if err != nil {
		return update
	}
	return out
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
	if _, known := knownUnservedNotifications[method]; known {
		return nil, fmt.Errorf("unsupported method %q", method)
	}
	if h.onUnknownMethod != nil && markMethodSeen(method) {
		h.onUnknownMethod(method, params)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

// knownUnservedNotifications are methods DevKit has looked at and decided not
// to act on, so they are not reported as unknown. A notification's error goes
// nowhere; the entry only keeps the "unimplemented ACP method" log for things
// nobody has looked at yet.
//
//   - _auth/status_update (claude-agent-acp 0.75+): the adapter re-checks the
//     sign-in in the background and pushes the result. DevKit learns a lapsed
//     sign-in from the error the next session/new or prompt returns
//     (ErrAgentAuthRequired), which is when it can do something about it.
var knownUnservedNotifications = map[string]struct{}{
	"_auth/status_update": {},
}

// unknownMethodSeen dedupes the report. Process-wide rather than per session:
// the question these answer is "does this vendor use this method at all", and
// the second occurrence adds nothing to it.
var unknownMethodSeen sync.Map

func markMethodSeen(method string) bool {
	_, loaded := unknownMethodSeen.LoadOrStore(method, struct{}{})
	return !loaded
}

// parseAgentExtensions reads the two extensions DevKit drives from the agent's
// initialize answer: steering (inject a message into the running turn) and
// goal (an objective the session works towards).
//
// Both are advertised in the top-level _meta rather than in agentCapabilities,
// because both are extensions rather than ACP proper. The goal's own control
// method comes with it instead of being assumed.
func parseAgentExtensions(meta json.RawMessage) (steering bool, goalControl string) {
	if len(meta) == 0 {
		return false, ""
	}
	var parsed struct {
		Steering struct {
			Supported bool `json:"supported"`
		} `json:"steering"`
		Goal struct {
			ControlMethod string   `json:"controlMethod"`
			Actions       []string `json:"actions"`
		} `json:"goal"`
	}
	if err := json.Unmarshal(meta, &parsed); err != nil {
		return false, ""
	}
	control := parsed.Goal.ControlMethod
	if control != "" && !slices.Contains(parsed.Goal.Actions, "set") {
		// Advertised without the action DevKit would use: treat it as absent
		// rather than calling a method the agent will refuse.
		control = ""
	}
	return parsed.Steering.Supported, control
}

// Steer injects a message into the turn that is running, instead of queueing
// it behind that turn. The agent's own answer says what became of it:
// "injected" into the running turn, "startedNewTurn" when it had already
// finished, or "promptRequired" — the opt-in DevKit asks for — which means the
// session was idle and the message is still the caller's to send.
func (r *claudeRuntime) Steer(ctx context.Context, prompt []acp.ContentBlock) (string, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", ErrSessionNotFound
	}
	conn, sid, supported := r.conn, r.acpSessionID, r.steering
	r.mu.Unlock()
	if !supported {
		return "", ErrUnsupported
	}

	var res struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	// idleBehavior promptRequired: without it an idle session starts a turn of
	// its own that nothing here owns — no turn record, no checkpoint, and the
	// composer would show nothing running while it ran.
	req := map[string]any{
		"sessionId": sid,
		"prompt":    prompt,
		"_meta":     map[string]any{"steering": map[string]any{"idleBehavior": "promptRequired"}},
	}
	if err := conn.Call(ctx, "_session/steering", req, &res); err != nil {
		return "", err
	}
	return res.Outcome, nil
}

// SetGoal sets or clears the session's objective through the goal extension.
// An empty objective clears it.
func (r *claudeRuntime) SetGoal(ctx context.Context, objective string) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	conn, sid, control := r.conn, r.acpSessionID, r.goalControl
	r.mu.Unlock()
	if control == "" {
		return ErrUnsupported
	}

	req := map[string]any{"sessionId": sid, "action": "clear"}
	if objective != "" {
		req = map[string]any{"sessionId": sid, "action": "set", "objective": objective}
	}
	var res struct{}
	return conn.Call(ctx, control, req, &res)
}
