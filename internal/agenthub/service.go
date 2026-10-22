package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"devkit/internal/acp"
	"devkit/internal/identity"
	"devkit/internal/preferences"

	"github.com/google/uuid"
)

// DefaultInvokeTimeout bounds Invoke when the caller context has no deadline
// and the caller is not a person. An agent or terminal CLI that blocks on
// another session needs a bound; a person watching a turn does not — they
// have Stop, and a real task (a build, a refactor) outlasts any fixed limit.
// Overridable in tests.
var DefaultInvokeTimeout = 10 * time.Minute

// invokeUnbounded reports whether ctx belongs to a person using the app, whose
// turns run until they end or the person stops them.
func invokeUnbounded(ctx context.Context) bool {
	caller, ok := identity.FromContext(ctx)
	return ok && (caller.Kind == identity.KindUI || caller.Kind == identity.KindRemoteHuman)
}

// invokeTimeoutFailure is the session failure left when DefaultInvokeTimeout
// cut a turn short. Without it the turn just stops: no turn row, no message,
// an idle session — indistinguishable from the agent giving up.
func invokeTimeoutFailure(limit time.Duration, now time.Time) SessionFailure {
	return SessionFailure{
		ID: "invoke-timeout", Revision: int(now.Unix()), Category: "timeout", Severity: "error",
		Title:   "The turn stopped at its time limit",
		Details: fmt.Sprintf("This turn was stopped after %s. What it already changed is kept. Send a new prompt to continue.", limit),
		Actions: []string{"retry"},
	}
}

// CreateInput is the request to open a new agent session.
type CreateInput struct {
	AgentType       AgentType
	Cwd             string
	WorkspaceID     string
	ParentSessionID string // empty = root session
	Title           *string
	// ExtraReadWritePaths widen the jail beyond Cwd — see OpenSpec. Only the
	// bridge (UI) path sets this; the MCP tool's own input struct has no such
	// field, and CreateSession ignores it for an agent caller. An
	// agent-created session gets its parent's stored list instead
	// (inheritedExtraPaths): never more than the parent may write.
	ExtraReadWritePaths []string
	// InitialConfig is applied before the first turn and recorded by attachUpdates.
	InitialConfig map[string]string
	// Profile names one of the person's model profiles (Settings → Agents) the
	// agent chose by; its model and effort become the InitialConfig. Agent
	// callers only — the window has its own pickers.
	Profile string
	// ModelChoiceApproved says the person agreed to a model or effort above
	// the calling agent's own (PlanModelChoice). Only the MCP tool handler
	// sets it, after asking; its input struct has no field to carry it.
	ModelChoiceApproved bool
}

// cleanExtraReadWritePaths applies the same invariants resolveCwd holds Cwd
// to — absolute, not "/", not bare $HOME — dropping anything that fails
// rather than rejecting the whole create: these are additional grants, not
// the one the session cannot start without.
func cleanExtraReadWritePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	home, _ := os.UserHomeDir()
	homeClean := filepath.Clean(home)
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		clean := filepath.Clean(p)
		if clean == "/" || clean == homeClean || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

// sessionOpenSpec is the starting OpenSpec for respawning an existing
// session's process (resume, rewind): its Cwd plus the jail widening it was
// created with. Cleaned again on the way out — the stored list went through
// cleanExtraReadWritePaths once already, but $HOME can differ from the one
// it was checked against, and re-checking costs nothing.
func sessionOpenSpec(sess AgentSession) OpenSpec {
	return OpenSpec{
		Cwd:                 sess.ProjectPath,
		WorkspaceID:         sess.WorkspaceID,
		ExtraReadWritePaths: cleanExtraReadWritePaths(sess.ExtraReadWritePaths),
	}
}

// applySandboxExtras shapes spec by the user's sandbox settings: the real
// $HOME and Docker by default, extra read-only paths, and DevKit's own data
// directory hidden whatever else is shared. Called at every place an
// OpenSpec is built — CreateSession, the two resume paths, and rewind — so a
// resumed or forked session runs under the same jail an open would give it,
// not a narrower one that happens to be easier to reach from that call site.
func (s *Service) applySandboxExtras(spec *OpenSpec) {
	// Not a sandbox setting, but it needs exactly this function's guarantee —
	// every path that builds an OpenSpec — or a resumed session would come
	// back without knowing where it is.
	if s.SessionBrief != nil {
		spec.Brief = s.SessionBrief(spec.WorkspaceID, spec.Cwd, append([]string{spec.Cwd}, spec.ExtraReadWritePaths...))
	}
	if s.ProtectedPaths != nil {
		spec.DenyReadPaths = append(spec.DenyReadPaths, s.ProtectedPaths()...)
	}
	if s.SandboxExtras == nil {
		return
	}
	extras := s.SandboxExtras()
	spec.Customizations = extras.Normalized().Customizations
	if extras.DockerAllowed() {
		s.applyDocker(spec, extras.DockerSocketPath)
	}
	if len(extras.ExtraReadOnlyPaths) > 0 {
		spec.ReadOnlyPaths = append(spec.ReadOnlyPaths, extras.ExtraReadOnlyPaths...)
	}
	if extras.HomeShared() {
		shared := false
		spec.PrivateHome = &shared
		spec.ReadOnlyPaths = append(spec.ReadOnlyPaths, developmentToolRoots()...)
	}
}

// applyDocker grants the Docker daemon's socket and whatever the CLI needs to
// run. override is the socket path from Settings; empty means found.
func (s *Service) applyDocker(spec *OpenSpec, override string) {
	detect := s.DetectDocker
	if detect == nil {
		detect = func() DockerGrant {
			home, _ := os.UserHomeDir()
			return DetectDocker(home, os.Getenv)
		}
	}
	grant := detect()
	socket := grant.Socket
	if override != "" {
		// Resolved like a found one: Seatbelt matches the path the kernel
		// connects to, and Docker Desktop's /var/run/docker.sock is a
		// symlink into ~/.docker.
		socket = override
		if resolved, err := filepath.EvalSymlinks(override); err == nil {
			socket = resolved
		}
	}
	if spec.ExtraEnv == nil {
		spec.ExtraEnv = map[string]string{}
	}
	if socket != "" {
		spec.AllowUnixSockets = append(spec.AllowUnixSockets, socket)
		// DOCKER_HOST, not the context the CLI would otherwise resolve: it
		// names the socket just granted, whatever ~/.docker says and whether
		// or not home is shared.
		spec.ExtraEnv["DOCKER_HOST"] = "unix://" + socket
	}
	spec.ReadOnlyPaths = append(spec.ReadOnlyPaths, grant.ReadOnly...)
	base := os.Getenv("PATH")
	if base == "" {
		base = "/usr/local/bin:/usr/bin:/bin" // claudeEnvAllowlist's fallback
	}
	if path := pathWith(base, grant.PathDirs); path != "" {
		spec.ExtraEnv["PATH"] = path
	}
}

// pathWith is base with dirs appended that it lacks, or "" when it lacks
// none. Appended, not prepended: the user's own order still decides which
// of two `docker`s runs.
func pathWith(base string, dirs []string) string {
	have := map[string]bool{}
	for _, dir := range filepath.SplitList(base) {
		have[dir] = true
	}
	parts := []string{}
	if base != "" {
		parts = append(parts, base)
	}
	added := false
	for _, dir := range dirs {
		if !have[dir] {
			parts = append(parts, dir)
			have[dir] = true
			added = true
		}
	}
	if !added {
		return ""
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// InvokeInput is a blocking prompt against an existing hub session.
type InvokeInput struct {
	SessionID string
	Prompt    string
	// Attachments are absolute paths DevKit reads and sends with the prompt.
	// Read by DevKit rather than by the agent on purpose: a path is only useful
	// to the agent if the file is inside its jail, and attaching something from
	// elsewhere on the machine is the case a path cannot serve. See
	// attachment.go for the check that keeps that from becoming a way around
	// the sandbox.
	Attachments []string
	// Images are transient pasted/drop images. Data is base64 and is never
	// persisted in the session transcript or credential store.
	Images []ImageAttachment
	// Refs are devkit:// EntityRef URIs (attachedref.go) — cross-module
	// references such as a Code Graph endpoint or a Git commit. Unlike
	// Attachments, DevKit never reads what one points at: it names the MCP
	// tool that resolves it and lets the agent call that tool itself, so the
	// agent always reads live data rather than a snapshot taken at attach
	// time (docs/dev-kit/cross-module.mdx § Primitive 1).
	Refs            []string
	CallerSessionID string // optional; used for CheckCycle on cross-session invoke
}

type ImageAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// InvokeResult is the MCP/UI-facing outcome of a blocking Invoke.
type InvokeResult struct {
	Status       string   `json:"status"` // completed | cancelled | error
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"filesChanged"`
	TurnCount    int      `json:"turnCount"`
	StopReason   string   `json:"stopReason"`
}

// MCPEndpoint is the identity-bound MCP socket handed to one vendor process.
type MCPEndpoint struct {
	SocketPath     string
	Server         acp.McpServerStdio
	Close          func()
	GateOwnedTools []string
	// PreApprovedTools are read-only tools scoped to the session that the vendor
	// may run without a prompt (see OpenSpec.PreApprovedMCP).
	PreApprovedTools []string
}

// MCPEndpoints opens one MCPEndpoint per session. nil means sessions get no
// DevKit MCP server (tests only; production wires mcp.SessionHost).
type MCPEndpoints interface {
	Open(sessionID string) (MCPEndpoint, error)
}

// Service orchestrates vendor adapters, session persistence, and live runtimes.
type Service struct {
	reg         *Registry
	store       Store
	recorder    *Recorder
	unknownSeen map[string]struct{}
	// retrying holds the sessions whose agent has an unanswered provider-retry
	// notice on screen, so the turn that ends can clear it (see endProviderRetry).
	retrying  map[string]struct{}
	runtimes  map[string]SessionRuntime
	endpoints map[string]func() // per-session MCP socket close
	invoking  map[string]struct{}
	// workspaceSent is the workspace context block each session's agent last
	// received (see workspaceContextDue). Not persisted: a restart resumes the
	// runtime, which forgets it, and the block goes out once more.
	workspaceSent map[string]workspaceContextSent
	// resuming marks a session whose runtime is being reopened. A second
	// caller waits on it rather than spawning a second process onto the same
	// MCP socket path, and gets the same outcome.
	resuming              map[string]*resumeCall
	mu                    sync.Mutex
	emit                  func(sessionID string, update json.RawMessage)
	ResolvePermission     PermissionResolver // nil = fail closed (no auto-approve)
	ResolveGatePermission PermissionResolver
	// GateTargetName resolves display metadata at the composition root, never
	// from the model's tool arguments. The gate's ProfileID remains the scope.
	GateTargetName   func(kind, id string) string
	permissionMu     sync.Mutex
	permissionStates map[string]*permissionState
	// CancelPendingPermissions drops what a session is waiting on when its turn
	// is stopped. Set in main.go to PermissionWaiter.CancelSession; nil means a
	// stop leaves pending requests alone, which is the old behaviour.
	CancelPendingPermissions func(sessionID string) int
	// ResolveElicitation puts an agent's question to the person. nil declines
	// every question: the tool runs with no answers, the turn carries on.
	ResolveElicitation ElicitationResolver
	// CancelPendingElicitations cancels a session's open questions when its turn
	// is stopped. Set in main.go to ElicitationWaiter.CancelSession.
	CancelPendingElicitations func(sessionID string) int
	PermissionTimeout         time.Duration // 0 = no deadline: wait for the user
	// MCPEndpoints opens the per-session MCP socket before the vendor spawns.
	MCPEndpoints MCPEndpoints
	// SandboxExtras reads the user's sandbox settings — shared $HOME, Docker,
	// extra read-only directories — beyond what a session grants on its own.
	// Read fresh on every spawn, resume and fork, so a change in Settings
	// applies to the next session without a restart. nil (the default wiring
	// for tests) leaves the adapter's own jail: private home, no Docker.
	SandboxExtras func() preferences.AgentSandboxSettings
	// SessionBrief describes where a session is — its workspace, the folders
	// in it and which of them the session may write — for the prompt, so the
	// agent starts knowing its workspace rather than being asked to go and
	// discover it. nil or "" leaves the prompt telling it to call
	// workspace.list instead.
	SessionBrief func(workspaceID, cwd string, granted []string) string
	// WorkspaceInstructions reads the selected workspace's current rules on each
	// turn. Errors refuse the turn before it records a user message or starts work.
	WorkspaceInstructions func(workspaceID string) (string, error)
	// WorkspaceMemory supplies a bounded snapshot of shared knowledge. It is
	// reference data, never authority for instructions or permissions.
	WorkspaceMemory func(ctx context.Context, workspaceID string) (string, error)
	// MatchingWorkspaces resolves cwd when creating a root session. Ambiguous
	// folders require an explicit choice; UI switching never moves a session.
	MatchingWorkspaces func(cwd string) ([]string, error)
	// ProtectedPaths lists what is hidden from every session whatever
	// SandboxExtras shares — DevKit's data directory (ProtectDataDir). A
	// function, read on every spawn, because the directory's contents are
	// what gets listed and they change. main.go sets it.
	ProtectedPaths func() []string
	// DetectDocker finds the Docker daemon and CLI; nil means DetectDocker on
	// the host's $HOME and environment. A seam for tests.
	DetectDocker func() DockerGrant
	// Documents keeps the PDFs attached to sessions (documents.go). nil leaves PDF
	// attachments refused with that reason.
	Documents *DocumentLibrary
	// OnSessionClosed fires after a session is closed (interaction.Hub forgets its allow_always).
	OnSessionClosed func(sessionID string)
	// OnSessionCreated fires after a session was created by someone other than
	// the window itself — an agent (agenthub.createAgentSession) or the terminal.
	// The window loads its list once, so without this a child an agent starts
	// never shows in the rail, and its tab says "Session not available".
	OnSessionCreated func(AgentSession)
	// OnTurnStarted fires when a turn begins and the session row says running.
	// The window learns of its own turns from the call it made; a turn started
	// by an agent's message or by the terminal reached it only when it ended, so
	// the rail showed a working session as idle.
	OnTurnStarted func(sessionID string)
	// OnTurnFinished fires after the turn's final transcript flush, including
	// cancellation and errors, so shell consumers can clear running state once.
	OnTurnFinished func(sessionID string, status SessionStatus, outcome string)
	// OnUnknownUpdate reports a session/update kind this build does not model,
	// once per kind per session. It exists because the gap used to be wide and
	// silent: of the sixteen kinds claude-agent-acp 0.74 emits this build parsed
	// six, and the other ten — subagent_spawned, async_task_*, session_info_update
	// among them — degraded to UpdateUnknown and vanished without a trace, so
	// nothing on screen or in the log said the vendor was sending something new.
	// All sixteen are modelled now; this stays for the next version's additions.
	// 0.78 has two more — compaction_update and compaction_summary_chunk — but
	// sends them only to a client that declares session.compaction, which
	// DevKit does not; it reports compaction as a `compact` tool call instead.
	//
	// A hook rather than a log call for the same reason OnRecordError is one:
	// internal/agenthub owns no logger.
	OnUnknownUpdate func(sessionID, wireKind string, raw json.RawMessage)

	// OnRecordError reports a transcript write that failed. It is a hook, not
	// an error return: losing a row must not abort a turn the vendor is
	// already running, but it must not be silent either.
	OnRecordError func(sessionID string, err error)

	// OnFilesRestored fires after a restore has written. The UI reloads what it
	// is showing of the project; nothing in the transcript changes.
	OnFilesRestored func(sessionID string, result RestoreResult)

	// OnSessionRewound fires after a session's conversation has been cut. The
	// transcript on screen is now wrong in a way no other event explains, so
	// the UI reloads it from the store.
	OnSessionRewound func(sessionID, promptText string)

	// catalog caches each vendor's model list for callers who may not start a
	// throw-away agent of their own (model_catalog.go).
	catalog modelCatalogCache
	// ModelProfiles reads the person's "which model for which job" list
	// (Settings → Agents). nil means there are none. Read on every call, so an
	// edit applies to the next session an agent starts.
	ModelProfiles func() (preferences.AgentModelProfiles, error)
	// SubagentLimits reads the person's cap on subagents (Settings → Agents →
	// Models & subagents). nil means none. Read when a session's process is
	// started, so a change applies to the next session and to a Reload.
	SubagentLimits func() (preferences.AgentSubagentLimits, error)

	// Accounts holds the vendor accounts a session can run as and what is known
	// of their usage. nil means the feature is off: every session runs as the
	// built-in account, exactly as before. AccountPolicy reads the person's
	// switching rules, AccountSharedHome the built-in account's home for a vendor
	// (where the conversation store an added account links to lives). All set in
	// main.go; read on every turn and every open, so an edit applies at once.
	Accounts          AccountBook
	AccountPolicy     func() (preferences.AgentAccountPolicy, error)
	AccountSharedHome func(vendor AgentType) string

	// All guarded by mu.
	// accountSwitches: when and from where a session last moved.
	// sessionAccount: the account a session's runtime runs as.
	// turnProgress: whether the running turn has produced output or a tool call.
	// usageSeen: the last usage windows written per account, to skip repeats.
	accountSwitches map[string]accountSwitch
	sessionAccount  map[string]string
	turnProgress    map[string]bool
	usageSeen       map[string][]LimitWindow
	// waitHook, when set (tests only, see export_test.go), is called each time
	// a caller is about to wait on a resume or a move in flight: waiter is
	// "move" (moveSession) or "resume" (resumeRuntime). Read under mu.
	waitHook func(waiter, sessionID string)

	// restores and rewinds hold the previews waiting between plan and apply.
	restores restorePlans
	rewinds  rewindPlans

	// peers is the state of messages between sessions (peer.go).
	peers peerState
	// turnWork counts what is still working on the database after a turn: each
	// invoke for its whole length, and the queue drains peer.go starts. For
	// WaitQuiet.
	turnWork atomic.Int64
	// OnPeerMessage reports each outcome of SendMessage, and a queued message's
	// later one. main.go audits it and tells the window. Never carries text.
	OnPeerMessage func(PeerMessageEvent)
	// HasPendingQuestion says an agent of the session is waiting on the person
	// to answer a question. A message is not added to a turn parked on one.
	HasPendingQuestion func(sessionID string) bool

	// checkpoints takes the file snapshots around a turn. Never nil — an
	// unconfigured service holds a no-op, so Invoke has one code path rather
	// than a nil check at each of the four places it would need one. Set with
	// SetCheckpoints.
	checkpoints Checkpoints
}

// NewService wires registry + store. emit may be nil (no-op).
func NewService(reg *Registry, store Store, emit func(sessionID string, update json.RawMessage)) *Service {
	if emit == nil {
		emit = func(string, json.RawMessage) {}
	}
	s := &Service{
		reg:             reg,
		store:           store,
		recorder:        NewRecorder(store),
		runtimes:        make(map[string]SessionRuntime),
		endpoints:       make(map[string]func()),
		invoking:        make(map[string]struct{}),
		resuming:        make(map[string]*resumeCall),
		emit:            emit,
		accountSwitches: make(map[string]accountSwitch),
		sessionAccount:  make(map[string]string),
		turnProgress:    make(map[string]bool),
		usageSeen:       make(map[string][]LimitWindow),
		// Off until main.go says otherwise: a machine with no git, or a user
		// who turned the feature off, runs the same Invoke as everyone else.
		checkpoints: noCheckpoints{},
	}
	// A cadence flush has no caller to return its error to; route it to the
	// same hook Record errors take, read at call time because callers set
	// OnRecordError after NewService.
	s.recorder.OnWriteError = func(sessionID string, err error) {
		if s.OnRecordError != nil {
			s.OnRecordError(sessionID, err)
		}
	}
	return s
}

// SetCheckpoints turns file checkpoints on. nil turns them off again, which is
// what a preference change does: the engine and its shadow repositories stay,
// so turning it back on does not re-pay the first snapshot.
func (s *Service) SetCheckpoints(cp Checkpoints) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cp == nil || !cp.Enabled() {
		s.checkpoints = noCheckpoints{}
		return
	}
	s.checkpoints = cp
}

// currentCheckpoints reads the engine under the lock. A preference change can
// swap it between two turns.
func (s *Service) currentCheckpoints() Checkpoints {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoints
}

// CheckpointsEnabled reports whether a turn would be checkpointed. The adapter
// asks before declaring the vendor capability that switches on Claude's own
// file history.
func (s *Service) CheckpointsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoints.Enabled()
}

// Shutdown lands every session's buffered transcript and stops the recorder.
// The app calls it on quit, before the database closes: the recorder batches
// its writes, so without it a quit mid-stream lost the last cadence of text
// and left buffered tool calls "in progress" on reload.
func (s *Service) Shutdown(ctx context.Context) error {
	return s.recorder.FlushAll(ctx)
}

// RecoverStaleSessions resets sessions whose row still claims a turn is in
// flight (running / awaiting_permission). No ACP process survives the restart
// that runs this, so those rows describe turns that can never finish — and
// because Invoke refuses them with ErrSessionBusy before it ever reaches the
// resume path, a session left mid-turn was stuck forever. Idle is the truth:
// the transcript keeps whatever the turn managed to say, and the next prompt
// resumes the session on a fresh process.
//
// Startup-only by contract, mirroring outbox.RecoverStaleSyncing: calling it
// while turns are actually running would reset live sessions.
func (s *Service) RecoverStaleSessions(ctx context.Context) error {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range list {
		if sess.Status != SessionStatusRunning && sess.Status != SessionStatusAwaitingPermission {
			continue
		}
		interrupted := InterruptedStop(time.Now())
		if err := s.store.UpdateSessionStop(ctx, sess.ID, &interrupted, time.Now().UTC()); err != nil {
			return err
		}
		notice := SessionUpdate{Kind: UpdateSessionInfo, Failure: &SessionFailure{
			ID: "interrupted:" + sess.ID, Revision: 1, Category: "interrupted", Severity: "warning", Title: "Previous turn was interrupted",
			Details: "The app stopped before this turn finished. Its earlier changes may already exist. Resume this conversation, then send your next instruction; no prompt or command is replayed automatically.", Actions: []string{"resume"},
		}}
		if err := s.recorder.Record(ctx, sess.ID, notice); err != nil {
			return err
		}
		if err := s.store.UpdateSessionStatus(ctx, sess.ID, SessionStatusIdle, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

// CreateSession opens a vendor runtime and persists an idle hub session.
//
// Order matters: the session id is generated first because the MCP socket is
// named after it and the sandbox must know that path before the vendor spawns.
func (s *Service) CreateSession(ctx context.Context, in CreateInput) (AgentSession, error) {
	adapter, ok := s.reg.Get(in.AgentType)
	if !ok {
		return AgentSession{}, ErrVendorUnknown
	}

	caller, _ := identity.FromContext(ctx)
	parentID := in.ParentSessionID
	if caller.IsAgent() {
		// An agent never chooses its parent: the parent is the caller.
		if parentID != "" && parentID != caller.SessionID {
			return AgentSession{}, ErrCallerMismatch
		}
		parentID = caller.SessionID
	}

	depth := 0
	var parent *AgentSession
	if parentID != "" {
		p, err := s.store.GetSession(ctx, parentID)
		if err != nil {
			return AgentSession{}, err
		}
		if err := CheckDepth(p.Depth, MaxDepth); err != nil {
			return AgentSession{}, err
		}
		depth = p.Depth + 1
		parent = &p
	}

	cwd, err := resolveCwd(in.Cwd, parent, caller.IsAgent())
	if err != nil {
		return AgentSession{}, err
	}
	if caller.IsAgent() && parent != nil {
		if err := s.checkChildFolderInWorkspace(cwd, *parent); err != nil {
			return AgentSession{}, err
		}
	}
	// An agent's model choice is checked here, not only by the tool that asked
	// the person: this is where it would take effect, and it holds whoever calls.
	inheritedConfig := false
	if caller.IsAgent() {
		plan, err := s.PlanModelChoice(ctx, ModelChoiceInput{Vendor: in.AgentType, Config: in.InitialConfig, Profile: in.Profile})
		if err != nil {
			return AgentSession{}, err
		}
		if plan.NeedsApproval && !in.ModelChoiceApproved {
			return AgentSession{}, fmt.Errorf("%w: %s", ErrModelChoiceNeedsApproval, strings.Join(plan.Why, "; "))
		}
		in.InitialConfig, inheritedConfig = plan.Config, plan.Inherited
	}
	// The mode an agent's child starts in is its parent's current one, read
	// from the parent's recorded state: not from the tool input (it has no mode
	// field) and not from the parent's say-so. See mode_inherit.go.
	var inheritedMode *parentMode
	if caller.IsAgent() && parent != nil {
		if m, ok := s.currentMode(ctx, *parent); ok {
			inheritedMode = &m
		}
	}
	workspaceID := in.WorkspaceID
	if parent != nil {
		if workspaceID != "" && workspaceID != parent.WorkspaceID {
			return AgentSession{}, ErrWorkspaceMismatch
		}
		workspaceID = parent.WorkspaceID
	} else if s.MatchingWorkspaces != nil {
		matches, err := s.MatchingWorkspaces(cwd)
		if err != nil {
			return AgentSession{}, err
		}
		if workspaceID != "" {
			found := false
			for _, id := range matches {
				found = found || id == workspaceID
			}
			if !found {
				return AgentSession{}, ErrWorkspaceMismatch
			}
		} else if len(matches) == 1 {
			workspaceID = matches[0]
		} else if len(matches) > 1 {
			return AgentSession{}, ErrWorkspaceAmbiguous
		}
	} else if workspaceID != "" {
		return AgentSession{}, ErrWorkspaceMismatch
	}

	id := uuid.NewString()
	spec := OpenSpec{Cwd: cwd, WorkspaceID: workspaceID}
	// Belt: the MCP tool handler's own input struct has no field to carry
	// paths, so an agent caller cannot reach it in practice. Suspenders: an
	// agent caller's own list is ignored here too, so the guarantee holds even
	// if that ever changes without whoever changes it noticing this needs to
	// move with it. What an agent's child gets is its parent's stored list.
	if !caller.IsAgent() {
		spec.ExtraReadWritePaths = cleanExtraReadWritePaths(in.ExtraReadWritePaths)
	} else if parent != nil {
		spec.ExtraReadWritePaths = inheritedExtraPaths(*parent)
	}
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(id)
		if err != nil {
			log.Printf("agenthub: %s session: open MCP endpoint: %v", in.AgentType, err)
			return AgentSession{}, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.GateOwnedMCP = map[string][]string{ep.Server.Name: ep.GateOwnedTools}
		spec.PreApprovedMCP = map[string][]string{ep.Server.Name: ep.PreApprovedTools}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}
	s.applySandboxExtras(&spec)
	s.applySubagents(&spec, in.AgentType)

	rt, err := adapter.Open(ctx, spec)
	if err != nil {
		// The bridge redacts this to one sentence; the log keeps the cause.
		log.Printf("agenthub: open %s session in %s: %v", in.AgentType, cwd, err)
		closeEndpoint()
		return AgentSession{}, err
	}
	if configurator, ok := rt.(RuntimeConfigurator); len(in.InitialConfig) > 0 && (!inheritedConfig || ok) {
		if !ok {
			_ = rt.Close(ctx)
			closeEndpoint()
			return AgentSession{}, ErrInvalidConfigOption
		}
		// Model first: it determines which effort values are supported.
		for _, key := range []string{"model", "reasoning_effort"} {
			if value := in.InitialConfig[key]; value != "" {
				if err := configurator.SetConfigOption(ctx, key, value); err != nil {
					if inheritedConfig {
						// The parent's own choice, not the agent's: a child that
						// cannot take it starts on the vendor's default.
						log.Printf("agenthub: %s child session: parent's %s %q not applied: %v", in.AgentType, key, value, err)
						continue
					}
					_ = rt.Close(ctx)
					closeEndpoint()
					return AgentSession{}, err
				}
			}
		}
	}

	if inheritedMode != nil {
		if err := applyInheritedMode(ctx, rt, in.AgentType, *inheritedMode); err != nil {
			_ = rt.Close(ctx)
			closeEndpoint()
			return AgentSession{}, err
		}
	}

	now := time.Now().UTC()
	sess := AgentSession{
		ID:           id,
		AgentType:    in.AgentType,
		ProjectPath:  cwd,
		WorkspaceID:  workspaceID,
		Status:       SessionStatusIdle,
		Depth:        depth,
		Title:        in.Title,
		ACPSessionID: rt.ACPSessionID(),
		CreatedAt:    now,
		UpdatedAt:    now,
		// What the jail was actually built with, after cleaning — resume and
		// rewind rebuild it from this (sessionOpenSpec).
		ExtraReadWritePaths: spec.ExtraReadWritePaths,
	}
	if parentID != "" {
		pid := parentID
		sess.ParentSessionID = &pid
	}

	if err := s.store.InsertSession(ctx, sess); err != nil {
		_ = rt.Close(ctx)
		closeEndpoint()
		return AgentSession{}, err
	}
	// After the insert: a row recorded for a session the store does not have
	// yet would have nowhere to belong.
	s.attachUpdates(sess.ID, rt)

	// Warm the project's shadow repository now, off any turn's path. The first
	// snapshot of a project walks every file — 0.86s on this repository, far
	// more on a monorepo — and every one after it costs 0.04s. Doing it here
	// means the first prompt joins a snapshot that is already the cheap kind.
	s.currentCheckpoints().Baseline(sess.ProjectPath)

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.mu.Unlock()

	// The window upserts the session it created itself from the call's answer.
	if s.OnSessionCreated != nil && !invokeUnbounded(ctx) {
		s.OnSessionCreated(sess)
	}
	return sess, nil
}

// inheritedExtraPaths is the jail widening an agent's child starts with: its
// parent's, read from the parent's stored row (what the person granted at
// creation, or what the parent itself inherited), never from the tool input.
// A chain therefore only ever keeps or narrows what a session may write.
func inheritedExtraPaths(parent AgentSession) []string {
	return cleanExtraReadWritePaths(parent.ExtraReadWritePaths)
}

// resolveCwd applies the cwd rules. Nobody gets "/" or the home directory.
// An agent caller with no cwd inherits its parent's project; with one, it
// must stay inside a folder its parent may write — the parent's project or
// one of the parent's extra read-write paths (symlinks resolved on all
// sides).
func resolveCwd(cwd string, parent *AgentSession, agentCaller bool) (string, error) {
	if agentCaller && cwd == "" && parent != nil {
		cwd = parent.ProjectPath
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", ErrInvalidCwd
	}
	clean := filepath.Clean(cwd)
	if clean == "/" {
		return "", ErrInvalidCwd
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "", ErrInvalidCwd
	}
	if agentCaller && parent != nil {
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return "", ErrInvalidCwd
		}
		parentResolved, err := filepath.EvalSymlinks(parent.ProjectPath)
		if err != nil {
			return "", ErrInvalidCwd
		}
		if !underResolved(resolved, parentResolved) && !underAnyResolved(resolved, parent.ExtraReadWritePaths) {
			return "", ErrCwdOutsideParent
		}
	}
	return clean, nil
}

// underResolved reports whether resolved (already symlink-free) is root or
// inside it; root is the symlink-free path of the folder.
func underResolved(resolved, root string) bool {
	rel, err := filepath.Rel(root, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// underAnyResolved is underResolved against each of roots after resolving
// their symlinks; a root that no longer resolves grants nothing.
func underAnyResolved(resolved string, roots []string) bool {
	for _, root := range cleanExtraReadWritePaths(roots) {
		r, err := filepath.EvalSymlinks(root)
		if err == nil && underResolved(resolved, r) {
			return true
		}
	}
	return false
}

// checkChildFolderInWorkspace holds a child opened outside its parent's
// project (in one of the parent's extra folders) to the parent's workspace:
// the folder must be one of the workspace's own, as a root session's cwd
// must be, not just any path somebody once granted. A cwd inside the parent's
// project is the parent's own ground and is not asked again. No lookup wired,
// or a parent with no workspace, has nothing to check against.
func (s *Service) checkChildFolderInWorkspace(cwd string, parent AgentSession) error {
	if s.MatchingWorkspaces == nil || parent.WorkspaceID == "" {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return ErrInvalidCwd
	}
	if root, err := filepath.EvalSymlinks(parent.ProjectPath); err == nil && underResolved(resolved, root) {
		return nil
	}
	matches, err := s.MatchingWorkspaces(cwd)
	if err != nil {
		return err
	}
	for _, id := range matches {
		if id == parent.WorkspaceID {
			return nil
		}
	}
	return ErrCwdOutsideParent
}

// sessionsByID loads every session keyed by id (for ancestry walks).
func (s *Service) sessionsByID(ctx context.Context) (map[string]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	return byID, nil
}

// authorizeTarget enforces the descendant rule: an agent caller may only act
// on itself or sessions inside its subtree. UI and terminal callers pass.
func (s *Service) authorizeTarget(ctx context.Context, targetID string) error {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return nil
	}
	if targetID == caller.SessionID {
		return nil
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if ancestorContains(byID, targetID, caller.SessionID) {
		return nil
	}
	return ErrNotDescendant
}

// ListSessions returns all persisted hub sessions visible to the caller.
// Agents only see themselves and their descendants.
//
// Note: after a process restart, store rows may outlive in-memory runtimes
// (orphan sessions). Invoke/Cancel return ErrUnavailable for those; callers
// should not assume every listed session has a live runtime.
func (s *Service) ListSessions(ctx context.Context) ([]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return list, nil
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	out := make([]AgentSession, 0, len(list))
	for _, item := range list {
		if item.ID == caller.SessionID || ancestorContains(byID, item.ID, caller.SessionID) {
			out = append(out, item)
		}
	}
	return out, nil
}

const maxContextSuggestions = 100

// contextWalkBudget bounds how many entries one listing reads. A query that
// matches almost nothing would otherwise walk every folder of a large
// workspace on each keystroke the composer sends.
const contextWalkBudget = 50_000

// contextSkipDirs are trees nobody mentions a file from: build output,
// dependencies, caches. Skipped below a root, never as the root itself.
var contextSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true, "dist": true, "build": true,
	".next": true, ".turbo": true, ".cache": true, ".venv": true, "__pycache__": true, ".idea": true,
}

// ListContextFiles returns the files an "@" in the session's composer can
// mention: below the session's folder and every workspace folder it was
// granted (ExtraReadWritePaths), filtered by query. It is deliberately not a
// repository listing: the composer needs useful suggestions for what is being
// typed, while the native picker remains the escape hatch for an exact path.
func (s *Service) ListContextFiles(ctx context.Context, id, query string) ([]ContextFile, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return nil, err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return listContextFiles(ctx, append([]string{sess.ProjectPath}, sess.ExtraReadWritePaths...), query)
}

// ListDraftContextFiles is ListContextFiles for a session not created yet:
// the draft names its folders (the one it will work in, then the workspace's
// others) because there is no session row to read them from. The person's
// only — the folders are the caller's to choose, so a caller stamped as an
// agent or a terminal MCP client, which could name any folder on the machine,
// is refused.
func (s *Service) ListDraftContextFiles(ctx context.Context, roots []string, query string) ([]ContextFile, error) {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return nil, fmt.Errorf("%w: a draft's files are listed for the person, from the app", ErrPermissionDenied)
	}
	return listContextFiles(ctx, roots, query)
}

// listContextFiles walks each root in order. With no query it keeps the first
// maxContextSuggestions files found; with one it keeps every file whose path
// contains it (up to the walk budget) and returns the best matches first — a
// name that starts with the query, then a name that contains it, then a path
// that does — so "sche" finds schema.sql before a file in a schemas/ folder.
// A file's Description is its path below its root, prefixed with the root's
// name when there is more than one root, so two services' README.md differ.
func listContextFiles(ctx context.Context, roots []string, query string) ([]ContextFile, error) {
	roots = contextRoots(roots)
	needle := strings.ToLower(strings.TrimSpace(query))
	type match struct {
		file ContextFile
		rank int
	}
	var matches []match
	// A workspace folder inside another is walked twice; its files are listed once.
	seen := make(map[string]bool)
	visited := 0
	full := func() bool { return needle == "" && len(matches) >= maxContextSuggestions }
	for i, root := range roots {
		if full() || visited >= contextWalkBudget {
			break
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				// One unreadable folder costs only its own subtree. Returning the
				// error failed the whole listing, so a single permission-denied
				// directory left the composer with no suggestions at all.
				if path == root {
					return walkErr
				}
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// SkipAll, not SkipDir: SkipDir returned for a file skips only the
			// rest of that directory, so the walk still went through the
			// whole tree after the cap.
			if full() || visited >= contextWalkBudget {
				return filepath.SkipAll
			}
			visited++
			if entry.IsDir() {
				if path != root && contextSkipDirs[entry.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil || rel == "." || seen[path] {
				return nil
			}
			rel = filepath.ToSlash(rel)
			shown := rel
			if len(roots) > 1 {
				shown = filepath.Base(root) + "/" + rel
			}
			rank := contextMatchRank(entry.Name(), shown, needle)
			if rank < 0 {
				return nil
			}
			seen[path] = true
			matches = append(matches, match{
				file: ContextFile{Path: path, Name: entry.Name(), Description: shown, Kind: "file"},
				rank: rank,
			})
			return nil
		})
		if err != nil {
			// A missing or unreadable primary folder is the listing's failure;
			// one of the workspace's other folders is only its own.
			if i == 0 || ctx.Err() != nil {
				return nil, err
			}
		}
	}
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].rank != matches[b].rank {
			return matches[a].rank < matches[b].rank
		}
		if needle != "" && len(matches[a].file.Description) != len(matches[b].file.Description) {
			return len(matches[a].file.Description) < len(matches[b].file.Description)
		}
		return matches[a].file.Description < matches[b].file.Description
	})
	if len(matches) > maxContextSuggestions {
		matches = matches[:maxContextSuggestions]
	}
	out := make([]ContextFile, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.file)
	}
	return out, nil
}

// contextMatchRank orders a file for a query: 0 its name starts with it, 1 its
// name contains it, 2 only its path does, -1 not a match. Everything is 0 for
// no query.
func contextMatchRank(name, path, needle string) int {
	if needle == "" {
		return 0
	}
	lowerName := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lowerName, needle):
		return 0
	case strings.Contains(lowerName, needle):
		return 1
	case strings.Contains(strings.ToLower(path), needle):
		return 2
	}
	return -1
}

// contextRoots cleans the folders to list: absolute only, never "/" (a walk
// of the whole machine), each once.
func contextRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		clean := filepath.Clean(root)
		if clean != string(filepath.Separator) && !slices.Contains(out, clean) {
			out = append(out, clean)
		}
	}
	return out
}

// ListVendorTypes returns registered adapter types in stable order.
func (s *Service) ListVendorTypes() []AgentType {
	return s.reg.Types()
}

// GetStatus returns the persisted session record by id.
func (s *Service) GetStatus(ctx context.Context, id string) (AgentSession, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return AgentSession{}, err
	}
	return s.store.GetSession(ctx, id)
}

// ToolRawPreviewLimit is how much of a tool call's raw fields a transcript
// page carries. One huge terminal dump used to ride with every page load —
// the full output stays in the row and ToolCallDetail serves it.
const ToolRawPreviewLimit = 8 << 10

// TranscriptPage is the transcript read, bounded: the newest window of a
// session rather than the whole of it. A session with months of history used
// to pay for all of it on every open; the UI opens on the last page and walks
// backwards with beforeSeq from the page it holds.
func (s *Service) TranscriptPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) (TranscriptPage, error) {
	if err := s.authorizeTarget(ctx, sessionID); err != nil {
		return TranscriptPage{}, err
	}
	if limit <= 0 {
		limit = DefaultTranscriptPageSize
	}
	if limit > MaxTranscriptPageSize {
		limit = MaxTranscriptPageSize
	}
	newest := beforeSeq <= 0
	if newest {
		// Opening a session mid-stream: land the tail the recorder still holds,
		// or the page ends at the first chunk of the reply being streamed and
		// the live events carry on from a gap. A failed flush is reported, not
		// fatal — the rows already on disk are still worth showing.
		if err := s.recorder.Flush(ctx, sessionID); err != nil && s.OnRecordError != nil {
			s.OnRecordError(sessionID, err)
		}
	}
	page, err := s.store.ListTranscriptPage(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		return TranscriptPage{}, err
	}
	page.Entries = previewToolPayloads(page.Entries)
	if newest {
		if page.State, err = s.sessionStateAbove(ctx, sessionID, page.Entries); err != nil {
			return TranscriptPage{}, err
		}
	}
	return page, nil
}

// sessionStateAbove returns the session-level rows a page's entries do not
// hold. There is at most one per kind, so this is a handful of indexed reads.
func (s *Service) sessionStateAbove(ctx context.Context, sessionID string, entries []TranscriptEntry) ([]TranscriptEntry, error) {
	var state []TranscriptEntry
	for _, kind := range sessionStateKinds {
		if slices.ContainsFunc(entries, func(e TranscriptEntry) bool { return e.Kind == kind }) {
			continue
		}
		entry, ok, err := s.store.FindTranscriptKind(ctx, sessionID, kind)
		if err != nil {
			return nil, err
		}
		if ok {
			state = append(state, entry)
		}
	}
	return state, nil
}

// previewToolPayloads bounds what a page carries for each tool row: rawInput,
// rawOutput and text content are cut to ToolRawPreviewLimit, a diff too large
// to preview keeps only its path, and Raw is dropped outright — for tool rows
// it is a copy of those same fields, and rows written before the parser
// stopped keeping it would otherwise ship it whole. Anything cut marks the
// call truncated; ToolCallDetail still serves the row intact.
func previewToolPayloads(entries []TranscriptEntry) []TranscriptEntry {
	for i, entry := range entries {
		if entry.ToolCallID == "" || len(entry.Payload) == 0 {
			continue
		}
		var stored SessionUpdate
		if err := json.Unmarshal(entry.Payload, &stored); err != nil || stored.ToolCall == nil {
			continue
		}
		call := stored.ToolCall
		changed := len(stored.Raw) > 0
		stored.Raw = nil
		var inCut, outCut bool
		call.RawInput, inCut = previewRaw(call.RawInput)
		call.RawOutput, outCut = previewRaw(call.RawOutput)
		if contentCut := previewToolContent(call.Content); inCut || outCut || contentCut {
			call.Truncated = true
			changed = true
		}
		if !changed {
			continue
		}
		payload, err := json.Marshal(stored)
		if err != nil {
			continue
		}
		entries[i].Payload = payload
	}
	return entries
}

// previewRaw passes small values through untouched and turns big ones into a
// JSON string of their leading bytes — the field is vendor-shaped JSON of any
// type, and a byte slice of it would not parse. The string is encoded by
// encoding/json from a cut that ends on a rune boundary: strconv.Quote, used
// before, writes \xNN and \U escapes JSON does not have, so a cut through a
// multi-byte character failed the page marshal and sent the row out whole.
// The encoded preview, quotes and escapes included, fits the limit.
func previewRaw(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) <= ToolRawPreviewLimit {
		return raw, false
	}
	text := string(raw)
	n := ToolRawPreviewLimit
	for {
		encoded, _ := json.Marshal(truncateUTF8(text, n))
		if len(encoded) <= ToolRawPreviewLimit {
			return encoded, true
		}
		// Escapes grew it past the limit (HTML-escaped <>&, control
		// characters): shrink the cut in proportion. n strictly decreases,
		// and an empty cut encodes to two bytes, so this ends.
		n = n * ToolRawPreviewLimit / len(encoded)
	}
}

// previewToolContent shortens oversized content items in place and reports
// whether it cut anything. A diff is never cut inside — half an oldText against
// half a newText renders as a change nobody made — so one too large to preview
// keeps its path and is marked truncated; an image's data cannot be half-shown
// either and is dropped the same way.
func previewToolContent(content []ToolCallContent) bool {
	cut := false
	for i := range content {
		item := &content[i]
		if item.Type == "diff" {
			size := len(item.NewText)
			if item.OldText != nil {
				size += len(*item.OldText)
			}
			if size > ToolRawPreviewLimit {
				item.OldText, item.NewText, item.Truncated = nil, "", true
				cut = true
			}
			continue
		}
		block := item.Content
		if block == nil {
			continue
		}
		if len(block.Text) > ToolRawPreviewLimit {
			block.Text = truncateUTF8(block.Text, ToolRawPreviewLimit)
			item.Truncated = true
		}
		if len(block.Data) > ToolRawPreviewLimit {
			block.Data = ""
			item.Truncated = true
		}
		if block.Resource != nil && len(block.Resource.Text) > ToolRawPreviewLimit {
			block.Resource.Text = truncateUTF8(block.Resource.Text, ToolRawPreviewLimit)
			item.Truncated = true
		}
		if block.Resource != nil && len(block.Resource.Blob) > ToolRawPreviewLimit {
			block.Resource.Blob = ""
			item.Truncated = true
		}
		cut = cut || item.Truncated
	}
	return cut
}

// truncateUTF8 returns at most n bytes of s, backed off to the start of the rune
// the cut would split.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := 0; i < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(s[n]); i++ {
		n--
	}
	return s[:n]
}

// ToolCallDetail returns the stored row of one tool call, payload intact. The
// page reader shortens oversized raw fields to a bounded preview so one huge
// terminal dump cannot make a page heavy; this is where the full output still
// lives for the surface that asked to see it.
func (s *Service) ToolCallDetail(ctx context.Context, sessionID, toolCallID string) (TranscriptEntry, error) {
	if err := s.authorizeTarget(ctx, sessionID); err != nil {
		return TranscriptEntry{}, err
	}
	entry, ok, err := s.store.FindTranscriptToolCall(ctx, sessionID, toolCallID)
	if err != nil {
		return TranscriptEntry{}, err
	}
	if !ok {
		return TranscriptEntry{}, ErrTranscriptEntryNotFound
	}
	return entry, nil
}

// CloseSession closes the live runtime (if any) and marks the session closed.
func (s *Service) CloseSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	if _, err := s.store.GetSession(ctx, id); err != nil {
		return err
	}
	if s.CancelPendingPermissions != nil {
		s.CancelPendingPermissions(id)
	}
	if s.CancelPendingElicitations != nil {
		s.CancelPendingElicitations(id)
	}

	s.mu.Lock()
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	s.forgetWorkspaceContextLocked(id)
	s.forgetSessionAccountLocked(id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	// The conversation must be complete on disk before the session becomes
	// history: closing is the one path after which no turn will ever flush.
	// A write that fails is reported, not returned. Returning left the runtime
	// out of the map but running, and a full disk made the session impossible
	// to close — and, through DeleteSession, to delete, which is how the user
	// frees that disk.
	if err := s.recorder.Flush(ctx, id); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}
	// With the runtime gone nothing records for this session any more; land
	// whatever arrived during the close and drop its write state. State that
	// did not write is kept for a later flush; the close goes on either way.
	if err := s.recorder.Forget(ctx, id); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}

	// Serialize closure with permission status restoration: a late gate answer
	// must never turn a closed conversation back into an idle/running one.
	s.permissionMu.Lock()
	closeErr := s.store.UpdateSessionStatus(ctx, id, SessionStatusClosed, time.Now().UTC())
	s.permissionMu.Unlock()
	if closeErr != nil {
		return closeErr
	}
	if s.CancelPendingPermissions != nil {
		s.CancelPendingPermissions(id)
	}
	if s.OnSessionClosed != nil {
		s.OnSessionClosed(id)
	}
	return nil
}

// RenameSession changes the human-authored title without touching the vendor
// runtime or its transcript.
func (s *Service) RenameSession(ctx context.Context, id, title string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 120 {
		return fmt.Errorf("%w: title must be between 1 and 120 characters", ErrInvalidSessionTitle)
	}
	return s.store.UpdateSessionTitle(ctx, id, title, time.Now().UTC())
}

// DeleteSession permanently removes a session, its turns and its transcript,
// along with every descendant session.
//
// The subtree goes with it on purpose. A child session exists only because its
// parent spawned it, and deleting the parent alone would leave children whose
// lineage points at a row that is gone — the sidebar renders those as roots,
// so the swarm would silently reshape itself. Deleting one at a time in the
// right order is work the user should not have to do.
//
// Each session is closed first: the row is what the UI reads, but the process
// is what holds the port, the MCP endpoint and the sandbox. Deleting the row
// while the runtime lives would leave an agent running that nothing can reach
// or stop. A close that fails because the session was already closed is not an
// error here — that is the state we want.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if _, ok := byID[id]; !ok {
		return ErrSessionNotFound
	}

	ids := descendantsOf(byID, id)
	for _, target := range ids {
		// Ask the vendor to forget it before the connection goes away. Best
		// effort on purpose: the user asked DevKit to delete a session, and
		// failing the whole operation because the agent could not tidy its own
		// copy would leave the rows behind too — the worse of the two states.
		s.deleteVendorCopy(ctx, target)
		if err := s.CloseSession(ctx, target); err != nil && !errors.Is(err, ErrSessionNotFound) {
			return err
		}
	}
	if err := s.store.DeleteSessions(ctx, ids); err != nil {
		return err
	}
	// The rows are gone; so are the stored PDFs. A file that will not go is
	// collected by the next start-up sweep, never a reason to keep the session.
	if err := s.Documents.Remove(ids); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}
	// Close keeps a tail that would not write; with its rows gone there is
	// nothing left for a retry to land in.
	for _, target := range ids {
		s.recorder.Drop(target)
		s.peers.forget(target) // messages waiting for a session that no longer exists
		s.mu.Lock()
		s.forgetSessionAccountLocked(target)
		s.mu.Unlock()
	}
	return nil
}

// deleteVendorCopy tells the vendor to destroy its own record of a session.
//
// DevKit's DeleteSession removed its rows and stopped there, so a conversation
// deleted "for good" — the words in the confirmation — stayed readable in the
// vendor's own store. Only reachable while a runtime is live: a session whose
// process already died keeps its vendor-side copy, which is recorded rather
// than hidden.
func (s *Service) deleteVendorCopy(ctx context.Context, sessionID string) {
	rt := s.liveRuntime(ctx, sessionID)

	deleter, ok := rt.(RuntimeDeleter)
	if !ok {
		return
	}
	if err := deleter.Delete(ctx); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sessionID, err)
	}
}

// descendantsOf returns id and everything beneath it// descendantsOf returns id and everything beneath it, deepest last, so callers
// that must respect parent links can walk the slice backwards.
func descendantsOf(byID map[string]AgentSession, root string) []string {
	out := []string{root}
	for i := 0; i < len(out); i++ {
		for childID, child := range byID {
			if child.ParentSessionID != nil && *child.ParentSessionID == out[i] {
				out = append(out, childID)
			}
		}
	}
	return out
}

// Invoke runs a blocking prompt turn on an existing session.
func (s *Service) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	return s.invoke(ctx, in, nil)
}

// invoke is Invoke, and the turn a message from another session starts (peer
// non-nil; peer.go). That turn is not an agent calling Invoke: SendMessage has
// already decided the sender may reach this session, so the descendant rule and
// the cycle check do not apply, nobody waits on it so no time limit does, and
// its echo is the sender's words with their label rather than a user prompt.
func (s *Service) invoke(ctx context.Context, in InvokeInput, peer *peerRun) (result InvokeResult, retErr error) {
	s.turnWork.Add(1)
	defer s.turnWork.Add(-1) // registered first, so it runs after every other defer
	if peer != nil {
		// A turn that never reached the point of running still owes the sender
		// an answer. Only the first settle counts.
		defer func() { peer.settle(retErr) }()
	}
	boundedByDefault := false
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && DefaultInvokeTimeout > 0 && peer == nil && !invokeUnbounded(ctx) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultInvokeTimeout)
		defer cancel()
		boundedByDefault = true
	}

	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeResult{}, err
	}
	if peer == nil {
		if err := s.authorizeTarget(ctx, in.SessionID); err != nil {
			return InvokeResult{}, err
		}
	}
	if caller, _ := identity.FromContext(ctx); peer == nil && caller.IsAgent() {
		if in.CallerSessionID != "" && in.CallerSessionID != caller.SessionID {
			return InvokeResult{}, ErrCallerMismatch
		}
		in.CallerSessionID = caller.SessionID // CheckCycle below runs on the real caller
	}
	// A closed session reopens on the next prompt. Closing parks the process
	// and keeps the conversation; it is not a delete, and the row still carries
	// the vendor's own session id, so resumeRuntime can pick the conversation up
	// exactly as it does after a restart.
	//
	// This used to answer ErrSessionNotFound, which made Close a one-way door:
	// a user whose turn failed pressed Close — the button the error card put
	// next to Retry — and lost the conversation for good. The refusal predates
	// session/load; there is no longer anything to protect.
	if sess.Status == SessionStatusRunning || sess.Status == SessionStatusAwaitingPermission {
		return InvokeResult{}, ErrSessionBusy
	}

	if in.CallerSessionID != "" {
		list, err := s.store.ListSessions(ctx)
		if err != nil {
			return InvokeResult{}, err
		}
		byID := make(map[string]AgentSession, len(list))
		for _, item := range list {
			byID[item.ID] = item
		}
		if err := CheckCycle(byID, in.CallerSessionID, in.SessionID); err != nil {
			return InvokeResult{}, err
		}
	}

	// Validate the whole input before anything records the turn. This used to
	// run after the user echo was written and emitted and the session marked
	// running, so a refused attachment left its prompt in the transcript — and
	// the frontend, which puts a refused prompt back in the queue, showed it
	// twice once it was sent again.
	invoker, _ := identity.FromContext(ctx)
	if peer == nil && invokeUnbounded(ctx) {
		// The person is talking to this session: whatever exchange with other
		// sessions it was in is over, and its replies start counting afresh.
		s.peers.resetChain(in.SessionID)
	}
	// PDFs are not read into the prompt: they are kept in the session's document
	// store and the agent reads them with DevKit's PDF tools. The prompt carries
	// what each is and how to read it.
	docs, files, err := s.importDocuments(ctx, sess, in.Attachments, callerIsAgent(invoker))
	if err != nil {
		return InvokeResult{Status: "error"}, err
	}
	prompt, err := buildPromptBlocks(in.Prompt, files, in.Images, in.Refs, documentsPromptBlock(docs, namingFor(sess.AgentType)), sess.ProjectPath, callerIsAgent(invoker))
	if err != nil {
		return InvokeResult{Status: "error"}, err
	}
	// Read now so a failure refuses the turn before anything is recorded; whether
	// the agent needs it again is decided once the runtime is known, below.
	workspaceContext, err := s.workspaceTurnContext(ctx, sess)
	if err != nil {
		return InvokeResult{Status: "error"}, err
	}
	contextFingerprint := workspaceContextFingerprint(workspaceContext)

	// Claim the session before anything slow. The claim used to come after the
	// resume, so two prompts on a dead or orphaned session both resumed it:
	// two processes onto one MCP socket path, where the second failed with
	// "address already in use" or removed the first one's socket and left
	// that agent without DevKit's tools.
	s.mu.Lock()
	if _, busy := s.invoking[in.SessionID]; busy {
		s.mu.Unlock()
		return InvokeResult{}, ErrSessionBusy
	}
	s.invoking[in.SessionID] = struct{}{}
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		delete(s.invoking, in.SessionID)
		s.mu.Unlock()
	}
	// The turn's one name, from here to the row written when it ends: the
	// prompt's echo carries it, the turn row stores it, and a checkpoint or a
	// rewind names the turn by it. The live stream and a reloaded page agree on
	// nothing else — a transcript item's own id differs between the two.
	promptMessageID := uuid.NewString()
	if peer != nil {
		promptMessageID = peer.msg.id // the id the sender was given
	}

	// The opening snapshot starts here and is joined just before the prompt
	// goes out, so it runs inside the time resuming a process already costs
	// rather than on top of it. A checkpoint must never be the reason a user
	// waits for their agent — if it has not finished by then it is killed and
	// the turn runs without one.
	checkpoints := s.currentCheckpoints()
	cp := checkpoints.Begin(ctx, TurnCheckpointSpec{
		SessionID:       in.SessionID,
		ProjectPath:     sess.ProjectPath,
		PromptMessageID: promptMessageID,
		ParentSessionID: derefString(sess.ParentSessionID),
	})

	// Accounts: the start of a turn is the only moment a session changes
	// account (never inside a running turn). Under the claim, before the
	// runtime is resolved: a move closes the idle process and the resolve below
	// reopens it in the new account's home. A refusal ends here, before the
	// session is marked running or the prompt recorded: nothing ran.
	if err := s.switchAccountForTurn(ctx, &sess); err != nil {
		cp.Finish(context.WithoutCancel(ctx), TurnResult{Cancelled: true})
		release()
		return InvokeResult{}, err
	}

	// An orphan — the row outlived the process that served it, which is what
	// every restart does to every open session, and what a mid-turn crash does
	// too — resumes here. This used to be the end of the conversation.
	rt, err := s.runtimeFor(ctx, sess)
	if err != nil {
		cp.Finish(context.WithoutCancel(ctx), TurnResult{Cancelled: true})
		release()
		return InvokeResult{}, err
	}

	// Past the resume, which forgets what a process had been sent: an agent that
	// has this exact block in its conversation is not sent it again.
	// A slash command is the whole prompt: the block in front would make it a
	// sentence the vendor does not recognise as one (Claude reads a command from
	// the start of the turn; Codex's runtime matches the prompt exactly).
	command, _, _ := slashCommandOf(prompt)
	carriesContext := workspaceContext != "" && command == "" && s.workspaceContextDue(in.SessionID, contextFingerprint)
	if carriesContext {
		prompt = append([]acp.ContentBlock{{Type: "text", Text: workspaceContext}}, prompt...)
	}

	turnOutcome := "error"
	defer func() {
		release()
		// Whatever waited for this turn to end can go now.
		defer s.kickPeers(in.SessionID)
		// Before the flush: an agent that gave up retrying does not always say
		// the notice is over, and a banner left behind would claim a turn that
		// has ended is still trying.
		s.endProviderRetry(context.WithoutCancel(ctx), in.SessionID)
		// The turn is over however it went: land the tail the recorder is
		// still holding. WithoutCancel because the error paths reach here on
		// a context that is already gone — the transcript of a failed turn is
		// exactly the one worth keeping.
		if err := s.recorder.Flush(context.WithoutCancel(ctx), in.SessionID); err != nil && s.OnRecordError != nil {
			s.OnRecordError(in.SessionID, err)
		}
		// Before the event, so the window's refresh of the row sees the title.
		// A peer's message is another agent's, not what the session is about.
		if peer == nil {
			s.nameFromPrompt(context.WithoutCancel(ctx), sess, in.Prompt)
		}
		if s.OnTurnFinished != nil {
			if session, err := s.store.GetSession(context.WithoutCancel(ctx), in.SessionID); err == nil {
				s.OnTurnFinished(in.SessionID, session.Status, turnOutcome)
			}
		}
	}()

	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return InvokeResult{}, ErrVendorUnknown
	}

	// Everything that records what happened has to outlive whatever went
	// wrong. Values (caller identity, capability scope) are kept; only the
	// cancellation is dropped. SQLite's own busy_timeout bounds these writes,
	// so no deadline of ours is needed to stop one hanging forever.
	recordCtx := context.WithoutCancel(ctx)

	if err := s.setSessionStatus(ctx, in.SessionID, SessionStatusRunning); err != nil {
		return InvokeResult{}, err
	}
	if sess.Stop != nil {
		// A new turn is a new attempt: the last one's stop no longer describes
		// the session. Written before the turn runs, so a parent that reads the
		// status mid-turn is not told the session is stopped.
		_ = s.store.UpdateSessionStop(recordCtx, in.SessionID, nil, time.Now().UTC())
	}
	if s.OnTurnStarted != nil {
		s.OnTurnStarted(in.SessionID)
	}

	var (
		lastUpdateMu sync.Mutex
		lastUpdate   json.RawMessage
		permDenied   atomic.Bool
		touched      touchedPaths
		// The agent's last message this turn, for the session preview.
		lastMsg lastAgentMessage
	)
	// writePreview records the session's one-line preview. Its error is
	// ignored on purpose: a preview must never fail a turn.
	writePreview := func(preview string) {
		if preview == "" {
			return
		}
		_ = s.store.UpdateSessionPreview(recordCtx, in.SessionID, preview, time.Now().UTC())
	}
	rt.SetHandlers(RuntimeHandlers{
		// Replaces the handler attachUpdates installed rather than adding to
		// it, so the turn's updates are still emitted and recorded once.
		OnUpdate: func(update json.RawMessage, meta json.RawMessage) {
			lastUpdateMu.Lock()
			lastUpdate = append(json.RawMessage(nil), update...)
			lastUpdateMu.Unlock()
			// The vendor's own account of what it changed. It rides a
			// session_info_update's `_meta` and lands at the same moment as the
			// prompt response (spike S7), so which arrives first is not
			// something this can depend on — AttachReport handles both.
			if requestID, report := fileChangeReport(update, meta); report != nil {
				checkpoints.AttachReport(in.SessionID, requestID, report)
			}
			// recordCtx, not ctx: the turn's context is cancelled the moment
			// the turn goes wrong, and the update that says *why* is the last
			// one to arrive. Writing it under ctx meant the one message worth
			// keeping was the one guaranteed to be dropped — the log read
			// "transcript write failed: context canceled" and the UI drew an
			// empty bubble where the agent's error should have been.
			parsed := s.handleUpdate(recordCtx, in.SessionID, update)
			lastMsg.observe(parsed)
			// Where a tool call says it worked. This is the only evidence a
			// changed file was the agent's doing rather than a Bash script's,
			// short of the vendor's report.
			if parsed.ToolCall != nil {
				for _, location := range parsed.ToolCall.Locations {
					touched.add(location.Path)
				}
			}
		},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			outcome, err := s.mediatePermission(ctx, in.SessionID, params)
			if err != nil || isDeniedPermissionOutcome(outcome) {
				permDenied.Store(true)
				if len(outcome) == 0 {
					outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
				}
				return outcome, ErrPermissionDenied
			}
			return outcome, nil
		},
		// The same handler attachUpdates installs. SetHandlers replaces the set
		// wholesale, so leaving it out here would take the ability to ask away
		// for exactly the time the agent is most likely to use it.
		OnElicit: s.elicitHandler(in.SessionID),
	})

	// Record what the human said before the agent gets a chance to answer.
	// ACP has a user_message_chunk kind but claude-agent-acp never echoes the
	// prompt back — session/update only carries what the agent emits — so a
	// transcript reloaded from the store showed the replies with nothing to
	// reply to. Written under recordCtx like every other transcript write, and
	// best-effort for the same reason: losing the echo must not fail the turn.
	//
	// Every prompt, including one of only images or files. The echo is what
	// starts a turn in the transcript; a prompt without one folded into the turn
	// before it and had no name to rewind to.
	attachments := promptAttachmentsFor(in, files, docs)
	if peer != nil {
		// The sender's words under the sender's label, not the envelope the model
		// reads. Recorded before the claim is reported, so a sender told "started"
		// can rely on the message being in the target's transcript.
		s.recordPeerEcho(recordCtx, in.SessionID, peer.msg)
		peer.settle(nil)
	} else if strings.TrimSpace(in.Prompt) != "" || len(attachments) > 0 {
		userEcho := SessionUpdate{
			Kind:        UpdateUserMessageChunk,
			Content:     &ContentBlock{Type: "text", Text: in.Prompt},
			MessageID:   promptMessageID,
			Attachments: attachments,
		}
		if err := s.recorder.Record(recordCtx, in.SessionID, userEcho); err != nil && s.OnRecordError != nil {
			s.OnRecordError(in.SessionID, err)
		}
		s.emit(in.SessionID, userEchoUpdate(in.Prompt, promptMessageID, attachments, nil))
	}

	// Join the opening snapshot. Past this line the prompt is about to go out,
	// so a snapshot still running is one the turn would be waiting on.
	cp.Wait()

	outcome, promptErr := rt.Prompt(ctx, prompt, fileChangeReportRequest(cp.ID()))

	// The closing snapshot, before anything below decides the session's status.
	// Not in a defer: every path out of here ends with the session idle, idle
	// is the UI's signal that another prompt may be sent, and a tree taken
	// after that point would hold the next turn's writes and blame them on
	// this one.
	cp.Finish(context.WithoutCancel(ctx), TurnResult{
		ToolPaths: touched.list(),
		Cancelled: promptErr != nil,
	})

	// A denial is not a failure. The agent was told no — mediatePermission
	// still fails closed toward it, and its tool call is rejected — and then
	// the turn went on and ended like any other, usually with the agent saying
	// what it will do instead. This used to mark the session errored and fail
	// the turn, which put a red "stopped … Restart" card under a conversation
	// that had not stopped and a session that was still usable: it told the
	// user their own answer had broken something.
	//
	// Status "denied" rather than "completed" so a caller that has to know —
	// an agent that invoked this session through MCP — still can.
	if promptErr != nil {
		timedOut := false
		finalStatus := SessionStatusError
		if boundedByDefault && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			timedOut = true
			failure := invokeTimeoutFailure(DefaultInvokeTimeout, time.Now())
			log.Printf("agenthub: invoke %s session %s: stopped at the %s time limit", sess.AgentType, in.SessionID, DefaultInvokeTimeout)
			s.handleUpdate(recordCtx, in.SessionID, sessionFailureUpdate(failure))
		}
		if errors.Is(promptErr, ErrCancelled) || errors.Is(promptErr, context.Canceled) {
			finalStatus = SessionStatusIdle
			promptErr = ErrCancelled
		}
		// A turn the vendor refused — a usage limit, a full context window —
		// is not a session that broke. The process is up and the next prompt
		// may well work, so the session stays idle: "error" drew a "stopped …
		// Restart" card over a conversation that had not stopped. The vendor's
		// reason is on screen as the session failure the adapter reported.
		refused := errors.Is(promptErr, ErrAgentUsageLimit) || errors.Is(promptErr, ErrTurnFailed)
		if refused {
			finalStatus = SessionStatusIdle
			log.Printf("agenthub: invoke %s session %s: turn refused: %v", sess.AgentType, in.SessionID, promptErr)
		}
		if finalStatus == SessionStatusError {
			log.Printf("agenthub: invoke %s session %s: %v", sess.AgentType, in.SessionID, promptErr)
		}
		// A cancelled turn keeps the previous preview: stopping it was the
		// person's own doing, and what the last finished turn said still holds.
		// So does a busy refusal, which ran nothing.
		if !errors.Is(promptErr, ErrCancelled) && !errors.Is(promptErr, ErrSessionBusy) {
			writePreview(PreviewFromError(promptErr))
		}
		// A cancel is recorded too (kind "cancelled"; the parent is not told: the
		// one who cancelled knows). A busy refusal ran nothing, so it stops nothing.
		if !errors.Is(promptErr, ErrSessionBusy) {
			s.recordStop(recordCtx, sess, promptErr, timedOut, invoker)
		}
		_ = s.setSessionStatus(recordCtx, in.SessionID, finalStatus)
		return InvokeResult{Status: statusForInvokeErr(promptErr)}, promptErr
	}

	s.stopRecovered(in.SessionID)
	if workspaceContext != "" {
		s.workspaceContextDelivered(in.SessionID, contextFingerprint, carriesContext)
	}
	if command == "compact" {
		// What the agent kept of the block is for the vendor to say: a
		// compaction can summarise it away, so the next real prompt carries it again.
		s.mu.Lock()
		s.forgetWorkspaceContextLocked(in.SessionID)
		s.mu.Unlock()
	}

	usage, err := adapter.NormalizeUsage(outcome.Meta)
	if err != nil {
		writePreview(PreviewFromError(err))
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	lastUpdateMu.Lock()
	updateCopy := append(json.RawMessage(nil), lastUpdate...)
	lastUpdateMu.Unlock()
	files, summary, err := adapter.NormalizeToolSummary(outcome.Meta, updateCopy)
	if err != nil {
		writePreview(PreviewFromError(err))
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	if summary == "" {
		summary = "turn completed"
	}

	usageJSON, err := json.Marshal(usage)
	if err != nil {
		writePreview(PreviewFromError(err))
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turn := TurnRecord{
		ID:              uuid.NewString(),
		SessionID:       in.SessionID,
		PromptMessageID: promptMessageID,
		StopReason:      outcome.StopReason,
		UsageJSON:       usageJSON,
		RawMeta:         append(json.RawMessage(nil), outcome.Meta...),
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.InsertTurn(recordCtx, turn); err != nil {
		writePreview(PreviewFromError(err))
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turns, err := s.store.ListTurns(recordCtx, in.SessionID)
	if err != nil {
		writePreview(PreviewFromError(err))
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	writePreview(PreviewFromMessage(lastMsg.String()))

	if err := s.setSessionStatus(recordCtx, in.SessionID, SessionStatusIdle); err != nil {
		return InvokeResult{Status: "error"}, err
	}
	turnOutcome = "completed"

	status := "completed"
	if permDenied.Load() {
		status = "denied"
	}
	return InvokeResult{
		Status:       status,
		Summary:      summary,
		FilesChanged: files,
		TurnCount:    len(turns),
		StopReason:   outcome.StopReason,
	}, nil
}

// handleUpdate emits one session/update and records it.
//
// Emit before persisting: the screen must never wait on SQLite, and the
// recorder batches its writes anyway — a reload lands within one flush cadence
// of what the live event said.
func (s *Service) handleUpdate(ctx context.Context, sessionID string, update json.RawMessage) SessionUpdate {
	logAgentUpdate(sessionID, update)
	s.emit(sessionID, update)
	parsed := ParseSessionUpdate(update)
	if parsed.Kind == UpdateSessionInfo && parsed.Title != "" {
		if err := s.adoptSessionTitle(ctx, sessionID, parsed.Title); err != nil && s.OnRecordError != nil {
			s.OnRecordError(sessionID, err)
		}
	}
	if parsed.Kind == UpdateUnknown && s.OnUnknownUpdate != nil {
		// Once per kind per session: a stream of chunks of one unknown kind is
		// one fact, not a thousand log lines.
		if s.markUnknownSeen(sessionID, parsed.WireKind) {
			s.OnUnknownUpdate(sessionID, parsed.WireKind, parsed.Raw)
		}
	}
	if parsed.Kind == UpdateProviderRetry {
		s.markProviderRetry(sessionID, parsed.Retry != nil)
	}
	if parsed.Kind == UpdateUsage {
		s.observeAccountUsage(ctx, sessionID, parsed)
	}
	// The live event above carries any page picture; the stored row does not.
	stored, _ := slimPageImages(parsed)
	if err := s.recorder.Record(ctx, sessionID, stored); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sessionID, err)
	}
	return parsed
}

// attachUpdates gives a runtime its update handler the moment the service
// holds it. Installing it only when a prompt started left a runtime that a
// settings call had just resumed deaf to the config_option_update and
// current_mode_update the agent answers with: the setting changed and nothing
// recorded or showed it. Invoke replaces this with its own handler for the
// turn, which routes through the same handleUpdate.
//
// No permission handler: outside a turn there is nobody to ask, and a runtime
// without one refuses, which is the fail-closed answer.
//
// A question handler, though. A question authorises nothing, and the agent can
// ask one between turns — a background task, an MCP server — so a missing
// handler here would turn "ask the person" into "decline without asking".
//
// A mode set from the handshake goes through the same handler, so the picker
// gets it live and a reload gets it from the stored state row.
func (s *Service) attachUpdates(sessionID string, rt SessionRuntime) {
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
			s.handleUpdate(context.Background(), sessionID, update)
		},
		OnElicit: s.elicitHandler(sessionID),
	})
	if withModes, ok := rt.(RuntimeModes); ok {
		if update := modeStateUpdate(withModes.Modes()); update != nil {
			s.handleUpdate(context.Background(), sessionID, update)
		}
	}
}

// modeStateUpdate turns a handshake's mode set into a current_mode_update that
// also lists the modes — the agent's own update shape plus the one field only
// the handshake has, so every reader already handling the kind picks it up.
// nil when the agent advertised no modes.
func modeStateUpdate(modes *acp.SessionModeState) json.RawMessage {
	if modes == nil || len(modes.AvailableModes) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"sessionUpdate":  string(UpdateCurrentMode),
		"currentModeId":  modes.CurrentModeID,
		"availableModes": modes.AvailableModes,
	})
	if err != nil {
		return nil
	}
	return body
}

// userEchoUpdate is the same echo in ACP's own wire shape, for the live event.
// The UI parses session/update notifications, not SessionUpdate values, so the
// echo has to arrive looking like one or the open rail would show the prompt
// only after a reload.
func userEchoUpdate(text, messageID string, attachments []PromptAttachment, source *MessageSource) json.RawMessage {
	update := map[string]any{
		"sessionUpdate": string(UpdateUserMessageChunk),
		"content":       map[string]any{"type": "text", "text": text},
		"messageId":     messageID,
	}
	extra := map[string]any{}
	if len(attachments) > 0 {
		extra["attachments"] = attachments
	}
	if source != nil {
		extra["source"] = source
	}
	if len(extra) > 0 {
		update[DevKitWireKey] = extra
	}
	body, err := json.Marshal(update)
	if err != nil {
		return nil
	}
	return body
}

// promptAttachments names what a prompt carried besides text, in the order it
// was given: files by their base name, images by the name they were pasted or
// dropped with, refs by their devkit:// URI. The content is never kept — a
// ref never had any to begin with.
func promptAttachments(in InvokeInput) []PromptAttachment {
	var out []PromptAttachment
	for _, path := range in.Attachments {
		out = append(out, PromptAttachment{Name: filepath.Base(path), Kind: "file"})
	}
	for _, image := range in.Images {
		name := strings.TrimSpace(image.Name)
		if name == "" {
			name = "image"
		}
		out = append(out, PromptAttachment{Name: name, Kind: "image"})
	}
	for _, raw := range in.Refs {
		name := raw
		if ref, err := parseAttachedRef(raw); err == nil {
			name = ref.Label
			if name == "" {
				name = ref.ID
			}
		}
		out = append(out, PromptAttachment{Name: name, Kind: "ref", URI: raw})
	}
	return out
}

// adoptSessionTitle takes the vendor's generated name for a session that has
// none. The agent produces it after the first exchange, from what was actually
// said — which is why sessions read "Session 1d5b9d50" until now: nothing else
// ever names them.
//
// Only fills an empty title, never replaces one. A name passed to CreateSession
// was chosen by a person, and the agent regenerates its own as a conversation
// drifts; letting the later guess overwrite the deliberate choice would make a
// named session quietly rename itself.
func (s *Service) adoptSessionTitle(ctx context.Context, sessionID, title string) error {
	// No event of its own: the same update is already emitted to the UI on the
	// line below this call, carrying the title in its parsed form. A second
	// channel for one string would be a second thing to keep in sync.
	return s.store.AdoptSessionTitle(ctx, sessionID, title, time.Now().UTC())
}

// markProviderRetry records whether the session's retry notice is showing.
func (s *Service) markProviderRetry(sessionID string, showing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !showing {
		delete(s.retrying, sessionID)
		return
	}
	if s.retrying == nil {
		s.retrying = make(map[string]struct{})
	}
	s.retrying[sessionID] = struct{}{}
}

// endProviderRetry clears a retry notice still showing when the turn ends,
// for the window and for the stored row alike: a turn that is over is not
// waiting to try again.
func (s *Service) endProviderRetry(ctx context.Context, sessionID string) {
	s.mu.Lock()
	_, showing := s.retrying[sessionID]
	s.mu.Unlock()
	if !showing {
		return
	}
	s.handleUpdate(ctx, sessionID, marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateSessionInfo),
		"_meta":         map[string]any{"opencode/retry": nil},
	}))
}

// markUnknownSeen reports whether this is the first time the session has seen
// this wire kind, and records it. Keyed by session so a long-lived hub still
// reports a new kind on the next session rather than staying quiet forever.
func (s *Service) markUnknownSeen(sessionID, wireKind string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unknownSeen == nil {
		s.unknownSeen = make(map[string]struct{})
	}
	key := sessionID + "\x00" + wireKind
	if _, ok := s.unknownSeen[key]; ok {
		return false
	}
	s.unknownSeen[key] = struct{}{}
	return true
}

// liveRuntime returns the session's runtime unless its process has died, in
// which case the corpse is evicted — runtime and MCP endpoint both — and nil
// is reported, which sends Invoke down the resume path.
//
// Only CloseSession ever removed a runtime before, so a session whose agent
// crashed kept answering from the map forever: Retry prompted the dead process
// and failed with the crash's error, every time. Dead() is an optional
// interface (RuntimeLiveness) because an in-process runtime cannot die this
// way; one that cannot say simply stays.
func (s *Service) liveRuntime(ctx context.Context, id string) SessionRuntime {
	s.mu.Lock()
	rt := s.runtimes[id]
	liveness, canDie := rt.(RuntimeLiveness)
	dead := false
	if canDie {
		dead, _ = liveness.Dead()
	}
	if !dead {
		s.mu.Unlock()
		return rt
	}
	delete(s.runtimes, id)
	s.forgetWorkspaceContextLocked(id)
	// Nothing runs as an account now; the reopen writes it again. The switch
	// memory stays: the session is still the same one.
	delete(s.sessionAccount, id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	// Outside the lock, same shape as CloseSession: Close parks the process
	// and the endpoint close tears down a socket; neither belongs under s.mu.
	_ = rt.Close(ctx)
	if closeEndpoint != nil {
		closeEndpoint()
	}
	return nil
}

// resumeCall is one resume in flight. err is set before done closes, so a
// caller that waited reads the outcome the resuming caller got.
type resumeCall struct {
	done chan struct{}
	err  error
}

// resumeRuntime reopens a session whose process is gone, using the vendor's own
// session id from the store.
//
// Fails with ErrUnavailable, not something new: to a caller this is still "no
// runtime", and only the reason differs — vendor cannot resume, no id was
// recorded, or the attempt failed. The old behaviour is the floor, so a vendor
// without SessionLoader is exactly as available as before. The one exception is
// a session deleted between the caller's read and the resume: the row is read
// again once the resume owns the session, and its absence is the store's
// ErrSessionNotFound.
//
// A fresh MCP endpoint is opened for the resumed session because the old one
// died with its process, and the identity-bound socket is what ties the agent's
// tool calls back to this session in the audit log.
func (s *Service) resumeRuntime(ctx context.Context, sess AgentSession) (rt SessionRuntime, err error) {
	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return nil, ErrVendorUnknown
	}
	// A session nobody has prompted has no conversation to resume: Codex writes
	// none before the first turn (a resume of such a thread answers "no rollout
	// found" — measured against codex 0.163.0) and Claude keeps its file from the
	// first message. It starts over instead, which loses nothing.
	fresh := s.neverPrompted(ctx, sess)
	loader, isLoader := adapter.(SessionLoader)
	if !fresh {
		if sess.ACPSessionID == "" || !isLoader {
			return nil, ErrUnavailable
		}
	}
	// A restart brings sessions back without going through CreateSession, so
	// this is where an existing project's shadow repository gets warmed. A
	// no-op once the project has been baselined in this process.
	s.currentCheckpoints().Baseline(sess.ProjectPath)

	// One resume per session at a time. Invoke and the settings calls all
	// resume, and two at once would open two MCP endpoints on one socket path.
	s.mu.Lock()
	if call, busy := s.resuming[sess.ID]; busy {
		hook := s.waitHook
		s.mu.Unlock()
		if hook != nil {
			hook("resume", sess.ID)
		}
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// The resume's own error, not ErrUnavailable: a settings call that
		// waited on Invoke's resume must tell the user to sign in or install
		// the agent just as Invoke does.
		if call.err != nil {
			return nil, call.err
		}
		s.mu.Lock()
		rt := s.runtimes[sess.ID]
		s.mu.Unlock()
		if rt == nil {
			return s.resumeRuntime(ctx, sess)
		}
		return rt, nil
	}
	if rt := s.runtimes[sess.ID]; rt != nil {
		// Resumed by someone else between liveRuntime and here.
		s.mu.Unlock()
		return rt, nil
	}
	call := &resumeCall{done: make(chan struct{})}
	s.resuming[sess.ID] = call
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.resuming, sess.ID)
		s.mu.Unlock()
		call.err = err
		close(call.done)
	}()

	// The row, not the caller's copy, names the account: a caller that read the
	// session before an account move (and waited on the move's barrier above)
	// must reopen where the session is bound now, not where it was.
	row, err := s.store.GetSession(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	sess.AccountID = row.AccountID

	spec := sessionOpenSpec(sess)
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(sess.ID)
		if err != nil {
			return nil, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.GateOwnedMCP = map[string][]string{ep.Server.Name: ep.GateOwnedTools}
		spec.PreApprovedMCP = map[string][]string{ep.Server.Name: ep.PreApprovedTools}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}
	s.applySandboxExtras(&spec)
	s.applyAccount(ctx, &spec, sess)
	s.applySubagents(&spec, sess.AgentType)

	if fresh {
		rt, err = adapter.Open(ctx, spec)
	} else {
		rt, err = loader.Load(ctx, spec, sess.ACPSessionID)
	}
	if err != nil {
		log.Printf("agenthub: resume %s session %s: %v", sess.AgentType, sess.ID, err)
		closeEndpoint()
		return nil, err
	}
	if fresh && rt.ACPSessionID() != sess.ACPSessionID {
		if err := s.store.UpdateSessionACPID(ctx, sess.ID, rt.ACPSessionID(), time.Now().UTC()); err != nil {
			_ = rt.Close(ctx)
			closeEndpoint()
			return nil, err
		}
	}
	// Restore the controls before publishing the newly loaded conversation.
	if configurator, ok := rt.(RuntimeConfigurator); ok {
		if entry, found, readErr := s.store.FindTranscriptKind(ctx, sess.ID, UpdateConfigOptions); readErr == nil && found {
			var saved SessionUpdate
			if json.Unmarshal(entry.Payload, &saved) == nil {
				for _, option := range saved.ConfigOptions {
					if option.ID != "model" && option.ID != "reasoning_effort" && option.ID != "effort" {
						continue
					}
					var value string
					if json.Unmarshal(option.CurrentValue, &value) == nil && value != "" {
						_ = configurator.SetConfigOption(ctx, option.ID, value)
					}
				}
			}
		}
		if entry, found, readErr := s.store.FindTranscriptKind(ctx, sess.ID, UpdateCurrentMode); readErr == nil && found {
			var saved SessionUpdate
			if json.Unmarshal(entry.Payload, &saved) == nil && saved.Mode != "" {
				_ = configurator.SetMode(ctx, saved.Mode)
			}
		}
	}
	// Before the runtime is published, so a caller waiting on this resume
	// never gets a runtime that cannot hear the agent.
	s.attachUpdates(sess.ID, rt)

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.forgetWorkspaceContextLocked(sess.ID)
	s.mu.Unlock()
	return rt, nil
}

// neverPrompted reports a session that has had no turn and shows no message of
// the person's: nothing exists on the vendor's side that a resume could lose.
// A store that cannot say counts as prompted — the old path, which fails loudly
// rather than start a conversation over by mistake.
func (s *Service) neverPrompted(ctx context.Context, sess AgentSession) bool {
	turns, err := s.store.ListTurns(ctx, sess.ID)
	if err != nil || len(turns) > 0 {
		return false
	}
	_, found, err := s.store.FindTranscriptKind(ctx, sess.ID, UpdateUserMessageChunk)
	return err == nil && !found
}

// Cancel requests cancellation of the in-flight prompt on a session.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	if _, err := s.store.GetSession(ctx, id); err != nil {
		return err
	}
	// The question goes first. Cancelling the runtime kills the process that
	// asked it, and a card left counting down over a dead agent is the state
	// this used to leave behind.
	if s.CancelPendingPermissions != nil {
		s.CancelPendingPermissions(id)
	}
	// Questions too, for the same reason and in the same place: a card asking
	// something of an agent that is about to be killed. Cancel, not decline —
	// see ElicitationWaiter.CancelSession.
	if s.CancelPendingElicitations != nil {
		s.CancelPendingElicitations(id)
	}

	rt := s.liveRuntime(ctx, id)
	if rt == nil {
		// Nothing is running, which is what the caller asked for. This used to
		// answer ErrUnavailable — surfaced as "Agent Hub is not available", a
		// sentence about the module when the truth was about one session — and
		// that is what a second press of Stop always got, because the first
		// press kills the process and the runtime is evicted with it.
		return nil
	}
	return rt.Cancel(ctx)
}

// runtimeFor returns the session's live runtime, resuming it when its process
// is gone.
func (s *Service) runtimeFor(ctx context.Context, sess AgentSession) (SessionRuntime, error) {
	if rt := s.liveRuntime(ctx, sess.ID); rt != nil {
		return rt, nil
	}
	return s.resumeRuntime(ctx, sess)
}

// SetMode changes the agent's mode without routing it through prompt.
func (s *Service) SetMode(ctx context.Context, id, modeID string) error {
	if strings.TrimSpace(modeID) == "" {
		return fmt.Errorf("%w: mode id is required", ErrInvalidConfigOption)
	}
	configurator, err := s.configurator(ctx, id)
	if err != nil {
		return err
	}
	return configurator.SetMode(ctx, modeID)
}

// SetConfigOption changes one of the agent's advertised settings.
func (s *Service) SetConfigOption(ctx context.Context, id, optionID, value string) error {
	if strings.TrimSpace(optionID) == "" {
		return fmt.Errorf("%w: option id is required", ErrInvalidConfigOption)
	}
	configurator, err := s.configurator(ctx, id)
	if err != nil {
		return err
	}
	return configurator.SetConfigOption(ctx, optionID, value)
}

// ConfigCatalog supplies a draft without creating a persisted session.
func (s *Service) ConfigCatalog(ctx context.Context, vendor AgentType) ([]ConfigOption, error) {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return nil, ErrPermissionDenied
	}
	adapter, ok := s.reg.Get(vendor)
	if !ok {
		return nil, ErrVendorUnknown
	}
	if _, ok := adapter.(VendorConfigCatalog); !ok {
		return []ConfigOption{}, nil
	}
	options, err := s.readCatalog(ctx, vendor)
	if err == nil {
		// What the person just read is the freshest list there is.
		s.catalog.put(vendorModelsOf(vendor, options, nil, s.catalog.clock()))
	}
	return options, err
}

// PrepareSession wakes the selected conversation and refreshes its controls,
// without submitting a prompt or starting inference.
func (s *Service) PrepareSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	rt, err := s.runtimeFor(ctx, sess)
	if err != nil {
		return err
	}
	if refresh, ok := rt.(interface{ RefreshConfigOptions(context.Context) error }); ok {
		if err := refresh.RefreshConfigOptions(ctx); err != nil {
			if errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrSessionNotFound) {
				return err
			}
			log.Printf("agenthub: %s session %s: model list: %v", sess.AgentType, sess.ID, err)
			return vendorError(sess.AgentType, fmt.Errorf("%w: %w", ErrModelsUnavailable, err))
		}
	}
	return nil
}

// SteerOutcome is what became of a steered message; see SessionSteerer.Steer.
type SteerOutcome string

const (
	// SteerInjected: the message went into the turn that was running.
	SteerInjected SteerOutcome = "injected"
	// SteerPromptRequired: nothing was running, so nothing was sent. The
	// caller sends it as an ordinary prompt, which is the turn it deserves —
	// a record, a checkpoint, a Stop button.
	SteerPromptRequired SteerOutcome = "promptRequired"
)

// Steer puts a message into the turn a session is in the middle of, rather
// than queueing it behind that turn. This is what the vendor's own apps do
// when you type while the agent works: the agent reads it at its next step.
//
// The echo is recorded here, because nothing else will: the agent streams the
// answer to a steered message but never the message. Recorded only when it was
// actually injected — a promptRequired outcome sent nothing.
func (s *Service) Steer(ctx context.Context, id, text string) (SteerOutcome, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: prompt is required", ErrInvalidPrompt)
	}
	if err := s.authorizeTarget(ctx, id); err != nil {
		return "", err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	rt, err := s.runtimeFor(ctx, sess)
	if err != nil {
		return "", err
	}
	steerer, ok := rt.(TurnSteerer)
	if !ok {
		return "", fmt.Errorf("%w: agent %q cannot take a message mid-turn", ErrUnsupported, sess.AgentType)
	}
	outcome, err := steerer.Steer(ctx, []acp.ContentBlock{{Type: "text", Text: text}})
	if err != nil {
		return "", err
	}
	if SteerOutcome(outcome) == SteerPromptRequired {
		return SteerPromptRequired, nil
	}

	recordCtx := context.WithoutCancel(ctx)
	echo := SessionUpdate{
		Kind:      UpdateUserMessageChunk,
		Content:   &ContentBlock{Type: "text", Text: text},
		MessageID: uuid.NewString(),
	}
	if err := s.recorder.Record(recordCtx, id, echo); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}
	s.emit(id, userEchoUpdate(text, echo.MessageID, nil, nil))
	if invokeUnbounded(ctx) {
		s.peers.resetChain(id) // the person spoke to it: see invoke
	}
	return SteerOutcome(outcome), nil
}

// SetGoal sets what the session is working towards, or clears it when the
// objective is empty. The agent publishes the goal back on a
// session_info_update, which is what the UI shows — this call only asks.
func (s *Service) SetGoal(ctx context.Context, id, objective string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	rt, err := s.runtimeFor(ctx, sess)
	if err != nil {
		return err
	}
	steerer, ok := rt.(SessionSteerer)
	if !ok {
		return fmt.Errorf("%w: agent %q keeps no goal", ErrUnsupported, sess.AgentType)
	}
	return steerer.SetGoal(ctx, strings.TrimSpace(objective))
}

// configurator resumes the session's runtime the way Invoke does. It used to
// read only the live map, which is empty after every restart, so the first
// model or mode change in a reopened session was refused as "agent does not
// support session mode".
func (s *Service) configurator(ctx context.Context, id string) (RuntimeConfigurator, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return nil, err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	// A prompt reopens a closed session; a settings change does not. Waking a
	// process to set a mode nobody is about to use spends a spawn on nothing,
	// and the mode the reopened session will carry is read from its own
	// snapshot when it starts.
	if sess.Status == SessionStatusClosed {
		return nil, ErrSessionClosed
	}
	rt, err := s.runtimeFor(ctx, sess)
	if err != nil {
		return nil, err
	}
	configurator, ok := rt.(RuntimeConfigurator)
	if !ok {
		return nil, fmt.Errorf("%w: agent %q does not support session settings", ErrUnsupported, sess.AgentType)
	}
	return configurator, nil
}

func (s *Service) setSessionStatus(ctx context.Context, id string, status SessionStatus) error {
	return s.store.UpdateSessionStatus(ctx, id, status, time.Now().UTC())
}

func statusForInvokeErr(err error) string {
	if errors.Is(err, ErrCancelled) {
		return "cancelled"
	}
	return "error"
}
