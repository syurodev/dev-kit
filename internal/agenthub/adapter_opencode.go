package agenthub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agentruntime"
	"devkit/internal/sandbox"
)

// opencode (https://opencode.ai), driven as `opencode acp`.
//
// What this adapter is and is not, measured against opencode 2.0.32 with a
// scripted model and a fake HOME (see docs/superpowers/specs/ and the live
// test in adapter_opencode_live_test.go):
//
//   - It is ACP over stdio, like Claude, and reuses the generic ACP runtime
//     (acpRuntime): handshake, prompt, cancel, config options, session
//     delete, steering and goal when — and only when — the agent advertises them.
//   - It has no version pin. DevKit installs npm's latest and works out what
//     the agent can do from what it says (initialize, the config options it
//     publishes, the errors it answers with), never from a version number.
//   - Its permission modes are DevKit's, not opencode's. opencode's own "mode"
//     is an agent (build/plan), and its default is to run everything, so DevKit
//     tells it to ask (OPENCODE_CONFIG_CONTENT) and answers the questions
//     itself according to the session's mode. Nothing here relies on opencode's
//     `plan` agent being read-only: measured, it is not.
//   - Its home is DevKit's, as Codex's is: opencode keeps its sign-in, session
//     database and config under the XDG directories, so those four point into
//     <data>/agent-homes/opencode. The user's own ~/.config/opencode and
//     ~/.local/share/opencode are never read or written.

// DevKit's permission modes for opencode. The ids are the ones Codex uses so a
// child session inherits its parent's mode across the two (mode_inherit.go).
const (
	openCodeModeAsk  = "ask"
	openCodeModeAuto = "auto"
	openCodeModeFull = "bypassPermissions"
)

var openCodeModes = []acp.SessionMode{
	{ID: openCodeModeAsk, Name: "Ask", Description: "Asks before editing files or running a command."},
	{ID: openCodeModeAuto, Name: "Auto", Description: "Edits files on its own and asks before running a command."},
	{ID: openCodeModeFull, Name: "Full auto", Description: "Never asks. DevKit's sandbox and its prod checks still apply."},
}

// openCodeMCPSettle is how long after a session is created its first prompt
// waits when DevKit gave it MCP servers. opencode registers a session's MCP
// servers asynchronously: a prompt sent right after session/new is built
// without their tools (measured, 5 of 5 immediate prompts; a 100 ms wait after
// the server answered tools/list was enough, 4 of 4; upstream issue #53299).
// A person's first message rarely arrives this fast, so the wait is usually
// already over. Waiting for the MCP host to see tools/list would be exact; a
// bound is simple and costs nothing in the common case.
var openCodeMCPSettle = 1500 * time.Millisecond

// openCodeCancelGrace is how long a cancelled turn gets to end before the
// process is stopped. opencode answers session/cancel itself (measured: the
// turn ended "cancelled" in about the time the model took to stop streaming),
// so unlike Claude's the process is not killed on every cancel.
var openCodeCancelGrace = 15 * time.Second

// OpenCodeOptions configures the opencode adapter.
type OpenCodeOptions struct {
	// Command is an absolute opencode binary that overrides the managed runtime
	// (DEVKIT_OPENCODE_COMMAND), for development and CI.
	Command string
	// Home is DevKit's own opencode home: the XDG roots of the process.
	Home string
	// Runtime resolves the runtime DevKit installed, on every Open and Load so
	// an install or rollback applies to the next session without a restart.
	Runtime func() (agentruntime.Resolved, error)
	// Runner overrides sandbox.DefaultRunner(); nil uses the platform default.
	Runner SandboxRunner
	// OnUnknownMethod reports an agent→client method this build does not serve,
	// once per method per process.
	OnUnknownMethod func(method string, params json.RawMessage)
}

var (
	_ VendorAdapter       = (*OpenCodeAdapter)(nil)
	_ SessionLoader       = (*OpenCodeAdapter)(nil)
	_ VendorConfigCatalog = (*OpenCodeAdapter)(nil)
	_ AuthProvider        = (*OpenCodeAdapter)(nil)
	_ SessionRuntime      = (*openCodeRuntime)(nil)
	_ RuntimeConfigurator = (*openCodeRuntime)(nil)
	_ RuntimeModes        = (*openCodeRuntime)(nil)
	_ RuntimeLiveness     = (*openCodeRuntime)(nil)
	_ RuntimeDeleter      = (*openCodeRuntime)(nil)
)

// OpenCodeAdapter is a VendorAdapter that speaks ACP to opencode in a sandbox.
type OpenCodeAdapter struct {
	opts OpenCodeOptions
	// homeMu serialises preparing the home: two sessions opening at once must
	// not reset it under each other.
	homeMu sync.Mutex

	authMu      sync.Mutex
	authKey     string
	authMethods []AuthMethod
}

// NewOpenCodeAdapter returns the opencode adapter. Every process it starts goes
// through the SandboxRunner — never an unsandboxed exec.
func NewOpenCodeAdapter(opts OpenCodeOptions) *OpenCodeAdapter {
	return &OpenCodeAdapter{opts: opts}
}

func (a *OpenCodeAdapter) Type() AgentType { return AgentTypeOpenCode }

func (a *OpenCodeAdapter) Extensions() []string { return nil }

func (a *OpenCodeAdapter) runner() SandboxRunner {
	if a.opts.Runner != nil {
		return a.opts.Runner
	}
	return sandbox.DefaultRunner()
}

// openCodeRoots are the four XDG directories opencode derives its own from, and
// the two DevKit keeps out of the agent's hands.
type openCodeRoots struct {
	home     string
	config   string // <home>/config      → XDG_CONFIG_HOME
	data     string // <home>/data        → XDG_DATA_HOME
	state    string // <home>/state       → XDG_STATE_HOME
	cache    string // <home>/cache       → XDG_CACHE_HOME
	prompts  string // <home>/prompts     DevKit's instructions, read-only to the jail
	userHome string // <home>/home      the $HOME of a private-home session
}

func (r openCodeRoots) userConfigDir() string { return filepath.Join(r.config, "opencode") }

// prepareOpenCodeHome creates the home and returns its layout and the paths the
// jail must not write. The home must be a real directory: a symlink there would
// move the whole grant somewhere else.
//
// The config directory is not reset (unlike Codex's config.toml): it is where a
// person can put providers DevKit does not know, and OPENCODE_CONFIG_CONTENT
// is layered over it, so it cannot loosen what DevKit sets. It is read-only to
// the agent, so a session cannot rewrite it for the next one.
func prepareOpenCodeHome(home string) (openCodeRoots, []string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return openCodeRoots{}, nil, fmt.Errorf("%w: opencode home must be an absolute path", ErrProcessFailed)
	}
	roots := openCodeRoots{
		home:     home,
		config:   filepath.Join(home, "config"),
		data:     filepath.Join(home, "data"),
		state:    filepath.Join(home, "state"),
		cache:    filepath.Join(home, "cache"),
		prompts:  filepath.Join(home, "prompts"),
		userHome: filepath.Join(home, "home"),
	}
	for _, dir := range []string{home, roots.config, roots.userConfigDir(), roots.data, roots.state, roots.cache, roots.prompts, roots.userHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return openCodeRoots{}, nil, fmt.Errorf("%w: opencode home: %v", ErrProcessFailed, err)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return openCodeRoots{}, nil, fmt.Errorf("%w: opencode home %s is not a directory", ErrProcessFailed, dir)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return openCodeRoots{}, nil, fmt.Errorf("%w: opencode home: %v", ErrProcessFailed, err)
		}
	}
	return roots, []string{roots.userConfigDir(), roots.prompts}, nil
}

// writeOpenCodePrompt stores the DevKit prompt where opencode reads
// instructions from and returns its path. Content-named, so a session that is
// still running keeps the file it started with while a later one gets its own;
// files nothing has touched for a day are swept.
func writeOpenCodePrompt(dir, text string) (string, error) {
	sum := sha256.Sum256([]byte(text))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".md")
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return "", fmt.Errorf("%w: opencode prompt: %v", ErrProcessFailed, err)
		}
	} else {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			info, err := e.Info()
			if err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return path, nil
}

// openCodeConfig is what DevKit tells opencode about itself, as the JSON for
// OPENCODE_CONFIG_CONTENT. Measured: this layer wins over the global config
// file, so nothing in the home's config directory can undo it.
//
//   - permissions: ask before running a command or editing. What DevKit does
//     with the question depends on the session's mode (openCodeRuntime.decide).
//     MCP tools are not listed: DevKit's own gate asks after dispatch, and
//     asking here as well would put every DevKit tool call to the person twice.
//   - update: opencode never installs itself; DevKit installs it.
//   - share: a session is never published to opencode's web.
//   - instructions: DevKit's prompt (the workspace brief and how to call DevKit's tools).
func openCodeConfig(promptFile string) string {
	cfg := map[string]any{
		"update": "disable",
		"share":  "disabled",
		"permissions": []map[string]string{
			{"action": "shell", "resource": "*", "effect": "ask"},
			{"action": "edit", "resource": "*", "effect": "ask"},
		},
	}
	if promptFile != "" {
		cfg["instructions"] = []string{promptFile}
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// openCodeEnv is the environment of the opencode process. Like Codex's it is
// built from a small allowlist plus the managed runtime's PATH; with a shared
// real $HOME the host's CLI environment comes along (inheritToolEnvironment).
func openCodeEnv(l launch, spec OpenSpec, roots openCodeRoots, config string) map[string]string {
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
	inheritToolEnvironment(env, spec)
	for key, value := range l.env {
		env[key] = value
	}
	for key, value := range spec.ExtraEnv {
		env[key] = value
	}
	completeToolPath(env, spec)
	applySessionTempEnv(env, spec)
	// Pointing XDG_CONFIG_HOME away from the real one also moves where gh and
	// glab look for their login, which the agent's shell would then not find.
	// With the real home shared, say where they are.
	if shared := spec.PrivateHome != nil && !*spec.PrivateHome; shared {
		if host, err := os.UserHomeDir(); err == nil {
			for _, tool := range []struct{ env, dir string }{{"GH_CONFIG_DIR", "gh"}, {"GLAB_CONFIG_DIR", "glab"}} {
				if _, set := env[tool.env]; set {
					continue
				}
				dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), tool.dir)
				if os.Getenv("XDG_CONFIG_HOME") == "" {
					dir = filepath.Join(host, ".config", tool.dir)
				}
				if info, err := os.Stat(dir); err == nil && info.IsDir() {
					env[tool.env] = dir
				}
			}
		}
	}
	// A private-home session gets an empty home of its own. This is not only
	// tidiness: opencode looks at ~/.claude and ~/.agents when it opens a
	// session, and where the jail hides the real home an existing ~/.claude it
	// may not read makes that lookup fail (FileSystem.realPath), and with it
	// every session/new (measured on macOS; an absent directory is fine).
	if spec.PrivateHome == nil || *spec.PrivateHome {
		env["HOME"] = roots.userHome
	}
	// opencode reads Claude Code's CLAUDE.md and skills as its own; a session
	// that is meant to run on DevKit's configuration should not pick up another
	// agent's.
	env["OPENCODE_DISABLE_CLAUDE_CODE"] = "1"
	env["XDG_CONFIG_HOME"] = roots.config
	env["XDG_DATA_HOME"] = roots.data
	env["XDG_STATE_HOME"] = roots.state
	env["XDG_CACHE_HOME"] = roots.cache
	env["OPENCODE_CONFIG_CONTENT"] = config
	return env
}

// resolveCommand picks the opencode binary: the Command override, else the
// installed runtime, else ErrAgentNotInstalled. `acp` is part of the launch.
func (a *OpenCodeAdapter) resolveCommand() (launch, error) {
	if cmd := a.opts.Command; cmd != "" {
		if !filepath.IsAbs(cmd) {
			return launch{}, fmt.Errorf("%w: command must be absolute", ErrProcessFailed)
		}
		path := filepath.Clean(cmd)
		return launch{path: path, args: []string{"acp"}, ro: uniqueAbsPaths(path)}, nil
	}
	if a.opts.Runtime == nil {
		return launch{}, vendorError(AgentTypeOpenCode, ErrAgentNotInstalled)
	}
	rt, err := a.opts.Runtime()
	if errors.Is(err, agentruntime.ErrNotInstalled) {
		return launch{}, vendorError(AgentTypeOpenCode, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err))
	}
	if err != nil {
		return launch{}, fmt.Errorf("%w: %w", ErrProcessFailed, err)
	}
	l := runtimeLaunch(rt)
	l.args = append(l.args, "acp")
	return l, nil
}

// openCodeNeedsSharedHome refuses, with the reason, a private-home session in a
// project inside the user's home folder on macOS. opencode resolves every
// directory above the project and what is directly inside each (~/.claude among
// them); Seatbelt answers a stat of a directory the jail hides with EPERM, which
// fails session/new, and the only way to let it through is to make those
// directories readable — in a private home, every folder of it. Measured: with
// the home shared, or a project outside it, it opens. Better said here than as
// "Agent process failed".
func openCodeNeedsSharedHome(workDir string, spec OpenSpec) error {
	if runtime.GOOS != "darwin" || (spec.PrivateHome != nil && !*spec.PrivateHome) {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	rel, err := filepath.Rel(resolve(home), resolve(workDir))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("%w: opencode cannot open a project inside your home folder while \"Share home folder\" is off — "+
		"macOS only lets it look at the folders above the project when the home is shared. "+
		"Turn it on in Settings → Agents → Safety, or open a project outside your home folder", ErrProcessFailed)
}

// jail is the one place an opencode policy is written, shared by sessions,
// sign-in and Probe, so each runs under the jail a session gets.
func (a *OpenCodeAdapter) jail(workDir string, l launch, spec OpenSpec) (sandbox.Policy, sandbox.Cmd, error) {
	a.homeMu.Lock()
	defer a.homeMu.Unlock()
	roots, denyWrite, err := prepareOpenCodeHome(a.opts.Home)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	promptFile, err := writeOpenCodePrompt(roots.prompts, devkitPrompt(opencodeNaming, spec.Brief))
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	// DevKit's data directory is hidden from every session; this vendor's own
	// home inside it is carved back out for this vendor's sessions only.
	denyRead := carveOut(spec.DenyReadPaths, roots.home)
	pol := sandbox.Policy{
		SessionTempDir: spec.SessionTempDir,
		WorkDir:        workDir,
		Network:        sandbox.NetworkHost,
		// `opencode acp` serves its own private HTTP API on a loopback port and
		// talks to it; the session's dev servers need the same.
		AllowLoopbackListeners: true,
		// opencode resolves every directory above the project when it opens a
		// session; a stat-only grant on them fails every session/new under /Users.
		ReadAncestors:    true,
		Env:              openCodeEnv(l, spec, roots, openCodeConfig(promptFile)),
		ReadOnlyPaths:    uniqueAbsPaths(append(append([]string(nil), l.ro...), spec.ReadOnlyPaths...)...),
		ReadWritePaths:   uniqueAbsPaths(append([]string{roots.home}, spec.ExtraReadWritePaths...)...),
		DenyReadPaths:    denyRead,
		DenyWritePaths:   denyWrite,
		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
		// gh/glab and other CLIs may keep their login in the macOS keychain.
		// ShareHome opts into the user's CLI environment, just as for Codex.
		AllowKeychain: spec.PrivateHome != nil && !*spec.PrivateHome,
		PrivateHome:   spec.PrivateHome,
	}
	return pol, sandbox.Cmd{Path: l.path, Args: append([]string(nil), l.args...)}, nil
}

// buildSandbox resolves the binary and returns the jail Policy + Cmd, for tests.
func (a *OpenCodeAdapter) buildSandbox(spec OpenSpec) (sandbox.Policy, sandbox.Cmd, error) {
	workDir, err := filepath.Abs(spec.Cwd)
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	l, err := a.resolveCommand()
	if err != nil {
		return sandbox.Policy{}, sandbox.Cmd{}, err
	}
	defer l.releaseLease()
	return a.jail(workDir, l, spec)
}

// openCodeClientCapabilities is what DevKit declares. Only what it serves:
//
//   - auth.terminal: sign-in runs `opencode acp --login` in a terminal tab.
//   - elicitation.form: opencode's question tool becomes elicitation/create,
//     answered by acpConnHandler.Elicit. Without it the model is told no
//     one can answer.
//   - the child-session extension: a subagent's tool calls arrive tagged on the
//     parent's session instead of vanishing into its result.
//
// session.compaction is not declared: DevKit has no card for compaction_update,
// and without it opencode reports a compaction as a plain marker.
func openCodeClientCapabilities() map[string]any {
	return map[string]any{
		"auth":        map[string]any{"terminal": true},
		"elicitation": map[string]any{"form": map[string]any{}},
		// "terminal-auth" is not declared: auth.terminal is enough for opencode to
		// offer `acp --login`, and the older gate would only add a command line
		// ("opencode", not the absolute path DevKit launches) that is discarded.
		"_meta": map[string]any{
			"opencode/child-session-updates": true,
		},
	}
}

// openCodeDirs are the additional directories sent with a session: the
// absolute, wildcard-free subset of what DevKit grants. opencode refuses the
// whole request over one bad entry, so a path it would refuse is left out
// here — the sandbox is the boundary either way.
func openCodeDirs(spec OpenSpec, cwd string) []string {
	var out []string
	for _, dir := range uniqueAbsPaths(spec.ExtraReadWritePaths...) {
		if dir == cwd || strings.ContainsAny(dir, "*?") {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// Open starts a sandboxed `opencode acp`, connects over stdio and creates a session.
func (a *OpenCodeAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	return a.open(ctx, spec, "")
}

// Load reopens a session that outlived its process, satisfying SessionLoader.
// session/resume is used when the agent has it: unlike session/load it does not
// replay the whole history to be thrown away (the transcript is in SQLite).
func (a *OpenCodeAdapter) Load(ctx context.Context, spec OpenSpec, acpSessionID string) (SessionRuntime, error) {
	if acpSessionID == "" {
		return nil, fmt.Errorf("%w: load needs the vendor session id", ErrSessionNotFound)
	}
	return a.open(ctx, spec, acpSessionID)
}

func (a *OpenCodeAdapter) open(ctx context.Context, spec OpenSpec, resume string) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workDir, err := filepath.Abs(spec.Cwd)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: project path", ErrProcessFailed)
	}
	if err := openCodeNeedsSharedHome(workDir, spec); err != nil {
		return nil, err
	}
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			l.releaseLease()
		}
	}()
	pol, cmd, err := a.jail(workDir, l, spec)
	if err != nil {
		return nil, err
	}
	// Paths only: the env carries the config and is never logged.
	log.Printf("agenthub: opencode: starting %s %s in %s (read-only %v, resume %q)",
		cmd.Path, strings.Join(cmd.Args, " "), pol.WorkDir, pol.ReadOnlyPaths, resume)

	// The process outlives the caller's request, as Claude's does: lifetime
	// belongs to the runtime. Only the handshake below honours ctx.
	started, err := (Launcher{Runner: a.runner()}).Start(context.Background(), pol, cmd)
	if err != nil {
		return nil, err
	}
	proc, stdin, stdout := started.Proc, started.Stdin, started.Stdout
	owned = false

	base := &acpRuntime{
		hooks:       acpRuntimeHooks{Type: AgentTypeOpenCode},
		environment: captureEnvironment(AgentTypeOpenCode, spec, pol.Env, cmd.Path, a.opts.Home),
		proc:        proc,
		cwd:         pol.WorkDir,
		killer:      &procKiller{proc: proc},
	}
	rt := &openCodeRuntime{acpRuntime: base, mode: openCodeModeAsk}
	base.hooks.PermissionBefore = rt.permissionBeforeAsk
	base.hooks.PermissionAfter = openCodeNeverAlways
	conn := acp.NewConn(&stdioPipe{r: stdout, w: stdin}, &acpConnHandler{rt: base, onUnknownMethod: a.opts.OnUnknownMethod})
	base.conn = conn

	serveCtx, cancelServe := context.WithCancel(context.Background())
	base.cancelServe = cancelServe
	base.serveDone = make(chan struct{})
	go func() {
		defer close(base.serveDone)
		serveErr := conn.Serve(serveCtx)
		_ = conn.Close()
		_ = proc.Kill()
		_ = proc.Wait()
		l.releaseLease()
		base.noteServeExit(serveErr)
	}()
	cleanup := func() {
		cancelServe()
		_ = conn.Close()
		_ = proc.Kill()
		<-base.serveDone
	}

	// The handshake must not outlive the process it talks to (see Claude's open).
	hsCtx, cancelHandshake := context.WithTimeout(ctx, handshakeTimeout)
	defer cancelHandshake()
	go func() {
		select {
		case <-base.serveDone:
			cancelHandshake()
		case <-hsCtx.Done():
		}
	}()
	handshakeErr := func(stage string, err error) error {
		if isAuthRequired(err) {
			return vendorError(AgentTypeOpenCode, fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err))
		}
		select {
		case <-base.serveDone:
			reason := stderrReason(proc)
			logStderrTail("opencode "+stage, proc)
			return fmt.Errorf("%w: %s: agent process exited during handshake%s", ErrProcessFailed, stage, reason)
		case <-time.After(serveExitGrace):
			logStderrTail("opencode "+stage+" (still running, "+err.Error()+")", proc)
			return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
		}
	}

	var initRes acp.InitializeResult
	if err := conn.Call(hsCtx, "initialize", acp.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: openCodeClientCapabilities(),
		ClientInfo:         map[string]any{"name": "devkit", "version": "0.0.0"},
	}, &initRes); err != nil {
		herr := handshakeErr("initialize", err)
		cleanup()
		return nil, herr
	}
	base.steering, base.goalControl = parseAgentExtensions(initRes.Meta)

	mcpServers := make([]any, 0, len(spec.McpServers))
	for _, srv := range spec.McpServers {
		if srv.Env == nil {
			srv.Env = []acp.EnvVariable{} // schema requires env; nil would serialise as null
		}
		mcpServers = append(mcpServers, srv)
	}
	dirs := []string(nil)
	if openCodeHas(initRes.AgentCapabilities, "sessionCapabilities", "additionalDirectories") {
		dirs = openCodeDirs(spec, pol.WorkDir)
	}

	var options json.RawMessage
	if resume != "" {
		method := "session/load"
		if openCodeHas(initRes.AgentCapabilities, "sessionCapabilities", "resume") {
			method = "session/resume"
		}
		var loadRes acp.SessionLoadResult
		if err := conn.Call(hsCtx, method, acp.SessionLoadRequest{
			SessionID: resume, Cwd: pol.WorkDir, McpServers: mcpServers, AdditionalDirectories: dirs,
		}, &loadRes); err != nil {
			herr := handshakeErr(method, err)
			cleanup()
			return nil, herr
		}
		if loadRes.SessionID != "" && loadRes.SessionID != resume {
			cleanup()
			return nil, fmt.Errorf("%w: %s returned %q, asked for %q", ErrProcessFailed, method, loadRes.SessionID, resume)
		}
		log.Printf("agenthub: opencode: session resumed (acp session %s)", resume)
		base.acpSessionID = resume
		options = loadRes.ConfigOptions
	} else {
		var newRes acp.SessionNewResult
		if err := conn.Call(hsCtx, "session/new", acp.SessionNewRequest{
			Cwd: pol.WorkDir, McpServers: mcpServers, AdditionalDirectories: dirs,
		}, &newRes); err != nil {
			herr := handshakeErr("session/new", err)
			cleanup()
			return nil, herr
		}
		if newRes.SessionID == "" {
			cleanup()
			return nil, fmt.Errorf("%w: empty sessionId from session/new", ErrProcessFailed)
		}
		log.Printf("agenthub: opencode: session ready (acp session %s)", newRes.SessionID)
		base.acpSessionID = newRes.SessionID
		options = newRes.ConfigOptions
	}
	base.setHandshakeOptions(openCodeOptionsJSON(options))
	if len(mcpServers) > 0 {
		rt.readyAt = time.Now().Add(openCodeMCPSettle)
	}
	return rt, nil
}

// openCodeHas reads agentCapabilities.<path…> as "present": opencode writes an
// empty object for a capability it has, so presence — not a true value — is the
// signal.
func openCodeHas(caps map[string]any, path ...string) bool {
	var cur any = caps
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[key]; !ok {
			return false
		}
	}
	if b, isBool := cur.(bool); isBool {
		return b
	}
	return cur != nil
}

// openCodeOptionsJSON leaves out opencode's own "mode" option (its build/plan
// agents). DevKit's modes are about what may run unasked, which that option is
// not, and offering it next to them would let the person pick "plan" believing
// it forbids writes — measured, it does not.
func openCodeOptionsJSON(raw json.RawMessage) json.RawMessage {
	var options []map[string]json.RawMessage
	if json.Unmarshal(raw, &options) != nil {
		return raw
	}
	kept := options[:0]
	for _, o := range options {
		var id, category string
		_ = json.Unmarshal(o["id"], &id)
		_ = json.Unmarshal(o["category"], &category)
		if id == "mode" || category == "mode" {
			continue
		}
		kept = append(kept, o)
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return raw
	}
	return out
}

// openCodeRuntime is the generic ACP runtime with opencode's differences:
// DevKit's permission modes, a first-prompt wait for MCP, and a cancel that
// does not kill the process.
type openCodeRuntime struct {
	*acpRuntime

	pmu     sync.Mutex
	mode    string
	readyAt time.Time
	// turnDone is closed when the prompt in flight returns; nil when none is.
	turnDone chan struct{}
}

// Modes satisfies RuntimeModes with DevKit's modes.
func (r *openCodeRuntime) Modes() *acp.SessionModeState {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	return &acp.SessionModeState{CurrentModeID: r.mode, AvailableModes: append([]acp.SessionMode(nil), openCodeModes...)}
}

// SetMode takes effect at the next permission question: it changes how DevKit
// answers, not anything opencode was started with, so it applies mid-turn too.
func (r *openCodeRuntime) SetMode(_ context.Context, modeID string) error {
	known := false
	for _, m := range openCodeModes {
		known = known || m.ID == modeID
	}
	if !known {
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidConfigOption, modeID)
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return ErrSessionNotFound
	}
	r.pmu.Lock()
	r.mode = modeID
	r.pmu.Unlock()
	if on := r.handlersSnapshot().OnUpdate; on != nil {
		on(marshalUpdate(map[string]any{
			"sessionUpdate": string(UpdateCurrentMode),
			"currentModeId": modeID,
		}), nil)
	}
	return nil
}

// SetConfigOption is the generic one, then drops opencode's "mode" option again:
// every answer lists the settings after the change, mode included.
func (r *openCodeRuntime) SetConfigOption(ctx context.Context, optionID, value string) error {
	if err := r.acpRuntime.SetConfigOption(ctx, optionID, value); err != nil {
		return err
	}
	r.mu.Lock()
	kept := r.options[:0]
	for _, o := range r.options {
		if o.ID != "mode" && o.Category != "mode" {
			kept = append(kept, o)
		}
	}
	r.options = kept
	r.mu.Unlock()
	return nil
}

func (r *openCodeRuntime) currentMode() string {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	return r.mode
}

// openCodePermission is the part of session/request_permission this reads.
type openCodePermission struct {
	ToolCall struct {
		Kind string `json:"kind"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// permissionBeforeAsk decides what happens to a question opencode asks.
//
// opencode is configured to ask before a command or an edit. Full auto answers
// "allow once" for everything; Auto answers it for edits and puts commands (and
// anything else) to the person; Ask puts everything to the person.
//
// The question the person sees never offers "always": opencode would keep that
// rule for the whole project, past this session and against its mode (measured:
// a later session in the same directory ran the command unasked).
func (r *openCodeRuntime) permissionBeforeAsk(_ context.Context, params json.RawMessage) (rewritten, answer json.RawMessage) {
	var req openCodePermission
	if json.Unmarshal(params, &req) != nil {
		return nil, nil
	}
	mode := r.currentMode()
	if mode == openCodeModeFull || (mode == openCodeModeAuto && req.ToolCall.Kind == "edit") {
		for _, o := range req.Options {
			if o.Kind == "allow_once" {
				return nil, openCodeSelected(o.OptionID)
			}
		}
	}
	// Drop allow_always from what the person is shown.
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return nil, nil
	}
	var options []map[string]any
	if json.Unmarshal(fields["options"], &options) != nil {
		return nil, nil
	}
	kept := options[:0]
	for _, o := range options {
		if kind, _ := o["kind"].(string); kind != "allow_always" {
			kept = append(kept, o)
		}
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, nil
	}
	fields["options"] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, nil
	}
	return out, nil
}

func openCodeSelected(optionID string) json.RawMessage {
	out, _ := json.Marshal(map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}})
	return out
}

// openCodeNeverAlways turns an "always" answer into "once", in case something
// other than the card chose it.
func openCodeNeverAlways(answer json.RawMessage) json.RawMessage {
	var res struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(answer, &res) != nil || res.Outcome.Outcome != "selected" || res.Outcome.OptionID != "always" {
		return answer
	}
	return openCodeSelected("once")
}

// Prompt waits out the MCP settle time, then prompts.
func (r *openCodeRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (PromptOutcome, error) {
	if wait := time.Until(r.readyAt); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return PromptOutcome{}, ErrCancelled
		}
	}
	done := make(chan struct{})
	r.pmu.Lock()
	r.turnDone = done
	r.pmu.Unlock()
	defer func() {
		r.pmu.Lock()
		if r.turnDone == done {
			r.turnDone = nil
		}
		r.pmu.Unlock()
		close(done)
	}()
	return r.acpRuntime.Prompt(ctx, prompt, meta)
}

// Cancel asks opencode to stop the turn and gives it openCodeCancelGrace to do
// so. Only a turn that does not end is stopped with the process ladder; the
// session then resumes on a fresh process like any dead runtime.
func (r *openCodeRuntime) Cancel(_ context.Context) error {
	base := r.acpRuntime
	base.mu.Lock()
	conn, sid, killer, closed := base.conn, base.acpSessionID, base.killer, base.closed
	base.mu.Unlock()
	if closed {
		return nil
	}
	if conn != nil && sid != "" {
		_ = conn.Notify(context.Background(), "session/cancel", map[string]any{"sessionId": sid})
	}
	r.pmu.Lock()
	done := r.turnDone
	r.pmu.Unlock()
	if done == nil || killer == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-time.After(openCodeCancelGrace):
	}
	log.Printf("agenthub: opencode: turn still running %s after cancel; stopping the process", openCodeCancelGrace)
	return acp.Shutdown(context.Background(), killer, acp.ShutdownConfig{CancelWait: 2 * time.Second, TermWait: 5 * time.Second})
}

// Probe starts an installed runtime under the jail a real session gets, sends
// ACP initialize and stops it: the proof, before a version is activated, that
// the binary starts under DevKit's policy and still offers what DevKit relies
// on. There is no version to compare with, so what is checked is the agent's
// own answer: it must be able to reopen a session (without that a restart loses
// every conversation).
//
// What this does not prove — that a permission question really reaches DevKit
// and a refusal really stops the command — needs a model; the live test in
// adapter_opencode_live_test.go checks it against a scripted one.
func (a *OpenCodeAdapter) Probe(ctx context.Context, rt agentruntime.Resolved) error {
	l := runtimeLaunch(rt)
	l.args = append(l.args, "acp")
	res, err := a.initialize(ctx, l)
	if err != nil {
		return err
	}
	if !openCodeHas(res.AgentCapabilities, "loadSession") && !openCodeHas(res.AgentCapabilities, "sessionCapabilities", "resume") {
		return fmt.Errorf("%w: this opencode cannot reopen a session (it offers neither loadSession nor session/resume)", ErrProcessFailed)
	}
	return nil
}

// initialize starts l under the probe jail, sends ACP initialize and stops it.
func (a *OpenCodeAdapter) initialize(ctx context.Context, l launch) (acp.InitializeResult, error) {
	workDir, err := os.MkdirTemp("", "devkit-agent-probe-")
	if err != nil {
		return acp.InitializeResult{}, fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
	}
	defer os.RemoveAll(workDir)
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	pol, cmd, err := a.jail(workDir, l, OpenSpec{})
	if err != nil {
		return acp.InitializeResult{}, err
	}
	started, err := (Launcher{Runner: a.runner()}).Start(ctx, pol, cmd)
	if err != nil {
		return acp.InitializeResult{}, err
	}
	proc, stdin, stdout := started.Proc, started.Stdin, started.Stdout
	conn := acp.NewConn(&stdioPipe{r: stdout, w: stdin}, &acpConnHandler{rt: &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeOpenCode}}})
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
		ClientCapabilities: openCodeClientCapabilities(),
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

// AuthMethods lists the terminal sign-ins opencode offers. opencode has one,
// `opencode acp --login`, which runs its own `auth login`; its credentials land
// in DevKit's opencode home because the login runs with the same XDG roots.
func (a *OpenCodeAdapter) AuthMethods(ctx context.Context) ([]AuthMethod, error) {
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	defer l.releaseLease()
	return a.authMethodsFor(ctx, l)
}

// SignInCommand resolves one method to the process to start. The method is
// looked up again rather than taken from the caller, so the UI can only name an id.
func (a *OpenCodeAdapter) SignInCommand(ctx context.Context, methodID string) (SignInCommand, error) {
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
		if m.ID != methodID {
			continue
		}
		a.homeMu.Lock()
		roots, _, herr := prepareOpenCodeHome(a.opts.Home)
		a.homeMu.Unlock()
		if herr != nil {
			return SignInCommand{}, herr
		}
		env := map[string]string{}
		for k, v := range l.env {
			env[k] = v
		}
		env["XDG_CONFIG_HOME"], env["XDG_DATA_HOME"] = roots.config, roots.data
		env["XDG_STATE_HOME"], env["XDG_CACHE_HOME"] = roots.state, roots.cache
		owned = false
		return SignInCommand{Release: l.release, Path: m.Command, Args: m.Args, Env: signInEnv(env), Label: m.Label}, nil
	}
	return SignInCommand{}, ErrAuthMethodUnknown
}

func (a *OpenCodeAdapter) authMethodsFor(ctx context.Context, l launch) ([]AuthMethod, error) {
	key := l.path + "\x00" + strings.Join(l.args, "\x00")
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

// ConfigCatalog reads the model and effort choices before there is a
// conversation: a short-lived agent in a scratch folder that is never sent a
// prompt. opencode lists "provider/model" for every model of every provider
// that has credentials — possibly hundreds — and only the model it is on lists
// its effort levels, so those are read for the current model alone rather than
// by switching to each (every switch reloads the agent).
//
// The scratch session is deleted afterwards: opencode persists a session the
// moment it is created, and each catalog read would leave an empty one behind.
func (a *OpenCodeAdapter) ConfigCatalog(ctx context.Context, spec OpenSpec) ([]ConfigOption, error) {
	work, err := os.MkdirTemp("", "devkit-opencode-catalog-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	// Not resolved through symlinks: on macOS the temp directory is
	// /var/folders/…, and its real spelling (/private/var/…) puts /private among
	// the folders opencode resolves on the way up from the working directory — a
	// folder the jail does not let it look at, which fails session/new.
	spec.Cwd = work
	spec.McpServers = nil
	opened, err := a.open(ctx, spec, "")
	if err != nil {
		return nil, err
	}
	rt, ok := opened.(*openCodeRuntime)
	if !ok {
		_ = opened.Close(ctx)
		return nil, fmt.Errorf("%w: unexpected runtime %T", ErrProcessFailed, opened)
	}
	defer func() {
		delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = rt.Delete(delCtx)
		_ = rt.Close(ctx)
	}()
	options := parseConfigOptions(rt.configOptions)
	model, effort := findConfigOption(options, claudeModelOption), findConfigOption(options, claudeEffortOption)
	if model == nil || len(model.Options) == 0 {
		return nil, fmt.Errorf("%w: opencode returned no models; sign in or add a provider", ErrModelsUnavailable)
	}
	var current string
	_ = json.Unmarshal(model.CurrentValue, &current)
	model.DefaultValue = current
	if effort != nil {
		var def string
		_ = json.Unmarshal(effort.CurrentValue, &def)
		for i := range model.Options {
			if model.Options[i].Value != current {
				continue
			}
			model.Options[i].DefaultEffort = def
			for _, level := range effort.Options {
				model.Options[i].SupportedEfforts = append(model.Options[i].SupportedEfforts,
					ConfigSelectOption{Value: level.Value, Name: level.Value, Description: level.Description})
			}
		}
	}
	model.Category = "model"
	return []ConfigOption{*model}, nil
}

func (a *OpenCodeAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	return NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}, nil
}

func (a *OpenCodeAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
