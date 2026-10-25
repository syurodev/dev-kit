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
	_ RuntimeDeleter       = (*acpRuntime)(nil)
	_ VendorSessionDeleter = (*acpRuntime)(nil)
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
	_ SessionRuntime = (*acpRuntime)(nil)
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
	applySessionTempEnv(env, spec)
	if spec.SessionTempDir != "" {
		// Claude Code takes its scratch directory from this variable and falls
		// back to a literal /tmp, ignoring TMPDIR; the jail has no shared /tmp.
		env["CLAUDE_CODE_TMPDIR"] = spec.SessionTempDir
	}
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
		SessionTempDir:         spec.SessionTempDir,
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
		// Questions: elicitation/create is answered by acpConnHandler.Elicit.
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
	started, err := (Launcher{Runner: a.runner()}).Start(context.Background(), pol, cmd)
	if err != nil {
		return nil, err
	}
	proc, stdin, stdout := started.Proc, started.Stdin, started.Stdout
	// Confirms the jail actually exec'd the agent, not just that bwrap itself
	// forked — a "starting" that never leaves that state and never logs
	// anything else again is otherwise indistinguishable from bwrap silently
	// failing to reach the exec at all.
	log.Printf("agenthub: claude: sandboxed process started, pid=%d", proc.PID())
	owned = false

	rw := &stdioPipe{r: stdout, w: stdin}
	rt := &acpRuntime{
		hooks:       acpRuntimeHooks{Type: AgentTypeClaude, PromptFailure: claudePromptFailure},
		environment: captureEnvironment(AgentTypeClaude, spec, pol.Env, cmd.Path, ""),
		proc:        proc,
		cwd:         pol.WorkDir,
		killer:      &procKiller{proc: proc},
	}
	handler := &acpConnHandler{rt: rt, onUnknownMethod: a.opts.OnUnknownMethod}
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
		rt.mu.Lock()
		rt.acpSessionID = resume
		rt.modes = loadRes.Modes
		rt.mu.Unlock()
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
	rt.mu.Lock()
	rt.acpSessionID = newRes.SessionID
	rt.modes = newRes.Modes
	rt.mu.Unlock()
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
	started, err := (Launcher{Runner: a.runner()}).Start(ctx, pol, cmd)
	if err != nil {
		return acp.InitializeResult{}, err
	}
	proc, stdin, stdout := started.Proc, started.Stdin, started.Stdout

	conn := acp.NewConn(&stdioPipe{r: stdout, w: stdin}, &acpConnHandler{rt: &acpRuntime{}})
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

type claudeMetaRoot struct {
	ClaudeCode *struct {
		CostUSD      *float64 `json:"costUsd"`
		InputTokens  *int     `json:"inputTokens"`
		OutputTokens *int     `json:"outputTokens"`
	} `json:"claudeCode"`
	Quota *struct {
		TokenCount *struct {
			TotalTokens           *int `json:"totalTokens"`
			InputTokens           *int `json:"inputTokens"`
			CachedInputTokens     *int `json:"cachedInputTokens"`
			CachedWriteTokens     *int `json:"cachedWriteTokens"`
			OutputTokens          *int `json:"outputTokens"`
			ReasoningOutputTokens *int `json:"reasoningOutputTokens"`
		} `json:"token_count"`
	} `json:"quota"`
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
	if q := root.Quota; q != nil && q.TokenCount != nil {
		// Keep the reported counters separate: this vendor's inputTokens excludes
		// cache reads and writes, so adding them would change the vendor's totals.
		t := q.TokenCount
		out.TotalTokens = copyInt(t.TotalTokens)
		if t.InputTokens != nil {
			out.InputTokens = *t.InputTokens
		}
		if t.OutputTokens != nil {
			out.OutputTokens = *t.OutputTokens
		}
		out.CachedInputTokens = copyInt(t.CachedInputTokens)
		out.CachedWriteTokens = copyInt(t.CachedWriteTokens)
		out.ReasoningOutputTokens = copyInt(t.ReasoningOutputTokens)
	}
	return out, nil
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// claudePromptFailure is Claude's result-side AIR failure hook. A warning is
// status information and must not turn an otherwise successful ACP response
// into a failed turn. A missing/null failure is likewise not terminal.
func claudePromptFailure(meta json.RawMessage) *SessionFailure {
	failure := parseSessionFailure(meta)
	if failure == nil || failure.Severity != "error" || strings.TrimSpace(failure.Title) == "" {
		return nil
	}
	return failure
}

func (a *ClaudeAdapter) NormalizeToolSummary(meta json.RawMessage, update json.RawMessage) ([]string, string, error) {
	_ = meta
	_ = update
	return nil, "", nil
}
