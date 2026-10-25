package app

import (
	"context"
	"sync"
	"time"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/apiclient"
	"devkit/internal/audit"
	"devkit/internal/auth"
	"devkit/internal/buildinfo"
	"devkit/internal/capabilities"
	"devkit/internal/codegraph"
	"devkit/internal/codexsessions"
	"devkit/internal/commands"
	"devkit/internal/database"
	"devkit/internal/environments"
	gitservice "devkit/internal/git"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/modules"
	"devkit/internal/notes"
	"devkit/internal/notify"
	"devkit/internal/ops"
	"devkit/internal/permissions"
	"devkit/internal/preferences"
	"devkit/internal/prwatch"
	sshservice "devkit/internal/ssh"
	"devkit/internal/syncclient"
	"devkit/internal/syncconflict"
	"devkit/internal/tasks"
	"devkit/internal/telegramcontrol"
	"devkit/internal/terminal"
	"devkit/internal/tools"
	"devkit/internal/uistate"
	"devkit/internal/updater"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
	"devkit/internal/workfolder"
	"devkit/internal/workspace"
	"devkit/internal/workspacememory"
)

// SyncService is the narrow runtime contract exposed to the bridge. Keeping it
// as an interface lets the core container remain usable for offline tests and
// prevents bridge code from depending on HTTP implementation details.
type SyncService interface {
	Connect(ctx context.Context, baseURL, accessToken string) error
	ConnectWithTokenSource(ctx context.Context, baseURL string, tokens syncclient.AccessTokenSource) error
	Disconnect()
	Status() syncclient.Status
	SyncNow(ctx context.Context) (syncclient.Report, error)
	Conflicts(ctx context.Context) ([]syncclient.ConflictInfo, error)
	ListDevices(ctx context.Context, tokens syncclient.AccessTokenSource, baseURL string) ([]syncclient.DeviceInfo, error)
	RevokeDevice(ctx context.Context, tokens syncclient.AccessTokenSource, baseURL, targetDeviceID string) (selfRevoked bool, err error)
}

// AuthService keeps the OIDC browser/keychain lifecycle behind the composition
// root. The bridge can request login but never receives an access token.
type AuthService interface {
	EnsureAuthenticated(ctx context.Context) (auth.Status, error)
	AccessToken(ctx context.Context) (string, time.Time, error)
	Logout() error
	Status() auth.Status
}

// SyncConflictService is the narrow runtime contract for presenting and
// resolving sync conflicts. It is optional (nil in ephemeral/test containers).
type SyncConflictService interface {
	Detail(ctx context.Context, recordID string) (syncconflict.Detail, error)
	Resolve(ctx context.Context, recordID, strategy, mergedText string) error
}

type Container struct {
	Telegram     *telegramcontrol.Service
	Commands     *commands.Registry
	Modules      *modules.Registry
	Audit        *audit.MemorySink
	Permissions  *permissions.Policy
	Capabilities *capabilities.Gateway
	Vault        *vault.Service
	HashTools    *tools.HashService
	UtilTools    *tools.UtilService
	Items        *vaultitem.Store
	Notes        *notes.Service
	Environments *environments.Service
	Preferences  *preferences.Service
	UIState      *uistate.Service
	Database     *database.Service
	SSH          *sshservice.Service
	CodeGraph    *codegraph.Service
	// CodeGraphRegistry keeps other projects' indexes open beside CodeGraph,
	// which is its focused service (workspace.mdx § Registry). Nothing opens a
	// resident service yet — the bridge and MCP still name no service, so
	// they reach CodeGraph exactly as before; step 4 threads servicePath.
	CodeGraphRegistry *codegraph.Registry
	// Terminal is nil in ephemeral/test containers built via
	// NewContainer/NewContainerWithStores — main.go constructs the real one
	// (it needs a Wails-aware output callback container.go doesn't have
	// access to) and wires it into CodeGraph via SetTerminalRegistry.
	Terminal *terminal.Registry
	// ConfigDir is the raw OS user-config directory (os.UserConfigDir()),
	// set by main before use. Empty in ephemeral/test containers built via
	// NewContainer/NewContainerWithStores — callers that need a real path
	// (e.g. CodeGraph's per-project SQLite index) must handle that case,
	// since not every test wants a real filesystem dependency.
	ConfigDir string
	Kafka     *kafka.Service
	// Accounts is the credential store. Built without a *sql.DB in ephemeral
	// containers, which leaves only shared accounts — scoped ones need the
	// local table that exists precisely because they must not sync.
	Accounts *accounts.Service
	// Tasks is the board. Its close gate is wired from main, which owns the
	// SQLite handle the verify queue lives in.
	Tasks *tasks.Service
	// Git is the project's own repository. Probing for a usable git is done
	// once here; a machine without one still gets a Service that reports why.
	Git *gitservice.Service
	// WorkFolder is the folder the app is working in and which modules follow
	// it. Code Graph sets it when a project opens; see WireWorkFolder.
	WorkFolder *workfolder.Service
	// GitBindings holds the guards and remote-to-account bindings the Git
	// module reads but cannot store itself.
	GitBindings *GitBindings
	// PullRequests follows the pull/merge request of a session's branch and
	// backs the pr.* tools. Nil until main wires it (it needs the database).
	PullRequests *prwatch.Service
	// APIClient sends HTTP requests with credentials resolved from Accounts.
	APIClient    *apiclient.Service
	Sync         SyncService
	SyncConflict SyncConflictService
	// Workspace is the multi-repo registry (docs/dev-kit/workspace.mdx).
	// Built with an in-memory repository here; main.go overrides it with a
	// SQL-backed one once the real database is open, same as Preferences
	// and UIState below.
	Workspace *workspace.Service
	// WorkspaceMemory is local shared knowledge with revision history. main wires
	// its SQL repository; ephemeral containers have no memory store.
	WorkspaceMemory *workspacememory.Service
	// WorkspaceCode is Code Graph's view of the workspace's folders: files,
	// search, outline and entry points by (workspace, folder) identity, without
	// a "focused project" (docs/design/codegraph-workspace.md).
	WorkspaceCode *WorkspaceCode
	// PDFView draws a PDF of a workspace folder for the Files viewer. Set by
	// main.go once the PDF engine exists; nil answers "not available".
	PDFView *PDFView
	// CodexSessions stores what an external Codex CLI session reported through
	// its lifecycle hooks — a session DevKit did not spawn and does not run.
	// Nil in ephemeral containers, which leaves it read-only-empty: the MCP
	// tool that writes to it treats a nil service as "log nothing" rather
	// than panicking, and an app without SQLite simply shows no history.
	CodexSessions *codexsessions.Service
	// Interaction is the shared MCP/UI hub for selection and confirmation
	// dialogs. Set from main before NewAppService so bridge and MCP share one
	// instance. Nil in ephemeral tests that do not exercise interaction.
	Interaction *interaction.Hub
	// AgentHub is optional: nil in ephemeral NewContainer tests until main (or a
	// test helper) wires SQLStore + vendor registry from the opened SQLite DB.
	AgentHub *agenthub.Service
	// AgentHubPermissions completes UI permission prompts for AgentHub.
	// Nil when AgentHub is unset.
	AgentHubPermissions *agenthub.PermissionWaiter
	// AgentHubElicitations answers the questions agents put to the user.
	// Nil when AgentHub is unset.
	AgentHubElicitations *agenthub.ElicitationWaiter
	// Updates looks up the newest published release; Version is the release
	// this binary was built from, "dev" when unstamped. Together they answer
	// "is there something newer" and nothing else — DevKit never downloads or
	// installs a release itself.
	Updates *updater.Checker
	Version string
	// AgentNotifications raises Agent Hub's OS notifications. Nil in server
	// mode and in tests that do not set it.
	AgentNotifications *notify.Notifier
	// AgentRuntimes installs and resolves the ACP adapters Agent Hub runs.
	// Nil in ephemeral tests; main roots it at <dataDir>/runtimes.
	AgentRuntimes *agentruntime.Manager
	// AgentCheckpoints snapshots a project's files around each turn. Held here
	// so a preference change can switch it on the service without rebuilding
	// the engine — the shadow repositories it has already warmed are what make
	// the next turn cheap. Nil in ephemeral tests.
	AgentCheckpoints *agenthub.EngineCheckpoints

	// auth and syncBlocked are mutable after startup: the cloudconfig refresh
	// goroutine rewrites them while bridge call handlers read them, so every
	// access goes through the mutex-guarded accessors below.
	// recoverErr is the verify queue recovery failure, if any. Kept rather
	// than fatal: tasks simply stay where they were, which is the safe end.
	recoverErr error

	mu          sync.Mutex
	auth        AuthService
	syncBlocked error
}

// Auth returns the current auth service, if wired.
func (c *Container) Auth() AuthService {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth
}

// SetAuth installs (or replaces) the auth service.
func (c *Container) SetAuth(svc AuthService) {
	c.mu.Lock()
	c.auth = svc
	c.mu.Unlock()
}

// SyncBlockedErr returns the cloud-config or version-gate error that should
// block AuthLogin and SyncConnect, if any.
func (c *Container) SyncBlockedErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syncBlocked
}

// SetSyncBlockedErr records or clears the Sync/AuthLogin block reason.
func (c *Container) SetSyncBlockedErr(err error) {
	c.mu.Lock()
	c.syncBlocked = err
	c.mu.Unlock()
}

// NewContainer wires core services with in-memory stores, suitable for tests
// and ephemeral runs.
func NewContainer() *Container {
	return NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		vaultitem.NewMemoryRepository(),
		environments.NewMemoryRepository(),
	)
}

func NewContainerWithStores(
	keyringStore vault.KeyringStore,
	itemRepo vaultitem.Repository,
	envRepo environments.Repository,
) *Container {
	toolGrants := make([]permissions.Grant, 0, len(tools.Commands))
	for _, command := range tools.Commands {
		toolGrants = append(toolGrants, permissions.Grant{
			SubjectID:  tools.SubjectID,
			Capability: command.Capability,
			Scope:      "command:" + command.ID,
		})
	}

	permissionPolicy := permissions.NewPolicy(append(append(toolGrants, ops.Grants(ops.All()...)...), []permissions.Grant{
		// Tools grants come from tools.Commands so a new command cannot be
		// registered without one — the failure that leaves a command runnable
		// with no matching grant, or granted and never wired.

		{SubjectID: "builtin.environments", Capability: "env:read", Scope: "tag:list"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:new"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:*"},
		// Workspace registry mutations are UI-only; workspace.list reads the
		// session's selected workspace. Shared memory has its own capability
		// and every MCP operation resolves the persisted session workspace.
		{SubjectID: "builtin.workspace", Capability: "ws:read", Scope: "workspace:list"},
		{SubjectID: "builtin.workspace", Capability: "ws:read", Scope: "service:*"},
		{SubjectID: "builtin.workspace", Capability: "ws:write", Scope: "workspace:new"},
		{SubjectID: "builtin.workspace", Capability: "ws:write", Scope: "workspace:*"},
		{SubjectID: "builtin.workspace", Capability: "ws:write", Scope: "folder:choose"},
		// Reindex asks for the folders it names (WorkspaceReindex, WorkspaceReindexService):
		// UI only, like the other registry mutations.
		{SubjectID: "builtin.workspace", Capability: "ws:write", Scope: "service:reindex"},
		{SubjectID: "builtin.workspace-memory", Capability: "workspace-memory:read", Scope: "workspace:*"},
		{SubjectID: "builtin.workspace-memory", Capability: "workspace-memory:write", Scope: "workspace:*"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:list"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:new"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:active"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:*"},
		// Saved queries are scoped by their own id, not the profile's.
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "query:*"},
		// Cancel and terminate act on a server session, not on a profile.
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "session:*"},
		// A console's text is a file in the app's own directory, and reading
		// them is reading what was typed in every window left open — not the
		// same permission as being allowed to ask a database a question.
		{SubjectID: "builtin.database", Capability: "db:console", Scope: "console:list"},
		{SubjectID: "builtin.database", Capability: "db:console", Scope: "console:*"},
		// Choosing where they are kept is a separate scope from reading one:
		// it opens a native picker and moves files.
		{SubjectID: "builtin.database", Capability: "db:console", Scope: "console:directory"},
		{SubjectID: "builtin.ssh", Capability: "terminal:io", Scope: "session:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:list"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:new"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:active"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "forward:list"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "forward:*"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:index", Scope: "project:new"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:read", Scope: "project:active"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:fs", Scope: "project:active"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:fs", Scope: "file:*"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:terminal", Scope: "project:active"},
		// Building an analyzer runs a build tool against the user's machine.
		// Its own capability rather than folding into codegraph:index: a grant
		// that lets a subject read a code index should not also let it start
		// Maven.
		{SubjectID: "builtin.codegraph", Capability: "codegraph:toolchain", Scope: "analyzer:build"},
		{SubjectID: "builtin.codegraph", Capability: "terminal:io", Scope: "session:*"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:plan", Scope: "plan:active"},
		{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:list"},
		{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:new"},
		{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "profile:*"},
		{SubjectID: "builtin.kafka", Capability: "kafka:connect", Scope: "connection"},
		{SubjectID: "builtin.kafka", Capability: "kafka:produce", Scope: "connection"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "session:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "vendor:list"},
		// What the window reads about an agent before any session exists: its
		// model list (the draft picker, Settings' "Models by job") and its
		// environment. The bridge asked for these scopes since they were added
		// and no grant covered them, so both reads were refused with a bare
		// "Permission denied." — the picker's "Models unavailable". The service
		// still answers only the window (ConfigCatalog, Environment).
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "catalog:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "environment:*"},
		// What an agent reads before it picks a child's model: the vendors'
		// model lists and the person's model profiles. Read-only; the profiles
		// are edited through the window, which has its own grants.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "models:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "profiles:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "permission:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "elicitation:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:new"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "permission:complete"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "permission:scoped_approval"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "elicitation:respond"},
		// File checkpoints. Listing is per session and covered by session:*;
		// reading one changed file's two versions is named by the checkpoint,
		// because that is the only id the transcript holds for it. The session
		// behind it is still authorised — CheckpointFileDiff resolves the
		// checkpoint's session and runs authorizeTarget on that.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "checkpoint:*"},
		// Putting a turn's files back. Planning is per session; applying is
		// named by the plan's own token, which the backend issued and holds —
		// the session it belongs to is re-checked from that plan, so the token
		// carries no authority a client could forge.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "restore:*"},
		// Agent runtimes: reading status and the registry, and installing,
		// switching or removing an adapter version. Scoped per agent id.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "runtime:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "runtime:*"},
		// Sign-in: listing an agent's login methods and running one in a
		// terminal the user sees (Claude), or signing in inside the app —
		// status, browser or code, cancel, sign out (Codex). The terminal grant
		// is what lets that tab be typed into, resized and closed, the same
		// grant CodeGraph's shells have. No MCP tool uses auth:*, and the
		// service refuses an agent caller for the in-app sign-in.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:*"},
		// Vendor accounts (Settings → Agents → Accounts & limits). UI only: no MCP tool
		// uses these scopes and every bridge method refuses a caller that is not the window.
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "account:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "account:*"},
		{SubjectID: "builtin.agenthub", Capability: "terminal:io", Scope: "session:*"},
		{SubjectID: "builtin.vault", Capability: "vault:manage", Scope: "vault"},
		{SubjectID: "builtin.search", Capability: "vault:search", Scope: "item:search"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:status"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:login"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:logout"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:connect"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:run"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:disconnect"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:conflicts"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:resolve"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:devices"},
		{SubjectID: "builtin.interaction", Capability: "interaction:respond", Scope: "pending"},
		{SubjectID: "builtin.interaction", Capability: "interaction:respond", Scope: "pending:complete"},
		{SubjectID: "builtin.interaction", Capability: "interaction:respond", Scope: "approvals:list"},
		{SubjectID: "builtin.interaction", Capability: "interaction:respond", Scope: "approval:revoke"},
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:list"},
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:claude-code"},
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:codex"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "telegram"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "telegram"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "theme"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "theme"},
		// The palette is a scope of its own rather than more room inside
		// "theme", matching how the sync and mcp grants above are split one per
		// operation. A scope that is not listed here is denied, and the denial
		// reaches the UI as the palette snapping back — setFamily applies
		// optimistically and reverts when the save rejects.
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "theme_family"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "theme_family"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "update_checks"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "update_checks"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "codegraph_call_visibility"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "codegraph_call_visibility"},
		// The one outbound request an update check makes. Separate from the
		// stored preference so that "off" is enforced where the request is
		// made, not only in the toggle.
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "update_check"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_notifications"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_notifications"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_checkpoints"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_checkpoints"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_sandbox"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_sandbox"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_customization:*"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_customization:*"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_model_profiles"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_model_profiles"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_subagent_limits"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_subagent_limits"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_account_policy"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_account_policy"},
		// Which Accounts token Codex signs in with: the id, never the key.
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_codex_api_key"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_codex_api_key"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "accounts_reveal"},
		// Accounts: read and write are the module's; reveal is a capability the
		// MCP subject is never granted, which is how "an agent cannot read a
		// credential" is enforced rather than merely intended.
		{SubjectID: "builtin.accounts", Capability: "accounts:read", Scope: "account:list"},
		{SubjectID: "builtin.accounts", Capability: "accounts:read", Scope: "account:*"},
		{SubjectID: "builtin.accounts", Capability: "accounts:write", Scope: "account:new"},
		{SubjectID: "builtin.accounts", Capability: "accounts:write", Scope: "account:*"},
		{SubjectID: "builtin.accounts", Capability: "accounts:reveal", Scope: "account:*"},
		{SubjectID: "builtin.accounts", Capability: "accounts:use", Scope: "account:*"},
		// Tasks: admin is board and workflow, deliberately separate so an
		// agent can hold read+write and never change the rule that binds it.
		{SubjectID: "builtin.tasks", Capability: "tasks:read", Scope: "board:list"},
		{SubjectID: "builtin.tasks", Capability: "tasks:read", Scope: "task:list"},
		{SubjectID: "builtin.tasks", Capability: "tasks:read", Scope: "task:search"},
		{SubjectID: "builtin.tasks", Capability: "tasks:read", Scope: "task:*"},
		{SubjectID: "builtin.tasks", Capability: "tasks:write", Scope: "task:new"},
		{SubjectID: "builtin.tasks", Capability: "tasks:write", Scope: "task:*"},
		// An agent may make a board (tasks.board.create): no verify command,
		// bound only into its own session's workspace. Changing one is admin.
		{SubjectID: "builtin.tasks", Capability: "tasks:write", Scope: "board:new"},
		{SubjectID: "builtin.tasks", Capability: "tasks:admin", Scope: "board:new"},
		{SubjectID: "builtin.tasks", Capability: "tasks:admin", Scope: "board:*"},
		{SubjectID: "builtin.tasks", Capability: "tasks:admin", Scope: "task:*"},
		// Git: push is its own capability because it leaves the machine.
		// git:push:force has no grant here and no code path at all.
		{SubjectID: "builtin.git", Capability: "git:read", Scope: "repo:status"},
		{SubjectID: "builtin.git", Capability: "git:read", Scope: "repo:*"},
		{SubjectID: "builtin.git", Capability: "git:write", Scope: "repo:*"},
		{SubjectID: "builtin.git", Capability: "git:push", Scope: "remote:*"},
		// API client: sending is separate from reading or editing a request.
		{SubjectID: "builtin.apiclient", Capability: "api:read", Scope: "request:list"},
		{SubjectID: "builtin.apiclient", Capability: "api:read", Scope: "request:*"},
		{SubjectID: "builtin.apiclient", Capability: "api:read", Scope: "environment:list"},
		// Export is a read the window makes on the person's behalf; no MCP
		// tool asks for it, so an agent cannot write the vault out to a file.
		{SubjectID: "builtin.apiclient", Capability: "api:read", Scope: "export:*"},
		{SubjectID: "builtin.apiclient", Capability: "api:write", Scope: "request:new"},
		{SubjectID: "builtin.apiclient", Capability: "api:write", Scope: "request:*"},
		{SubjectID: "builtin.apiclient", Capability: "api:write", Scope: "environment:*"},
		{SubjectID: "builtin.apiclient", Capability: "api:send", Scope: "request:*"},
		{SubjectID: "builtin.uistate", Capability: "ui:read", Scope: "key:*"},
		{SubjectID: "builtin.uistate", Capability: "ui:write", Scope: "key:*"},
		// codexhook:report is the MCP tool an external Codex CLI's own hooks
		// call; codex:read/write are the UI's read-only view onto what was
		// recorded. Two different capabilities for one feature because they
		// answer to two different callers — Codex over the shared socket,
		// the person over the bridge — and a Codex process should never be
		// able to clear its own history.
		{SubjectID: "builtin.codexhook", Capability: "codexhook:report", Scope: "event"},
		{SubjectID: "builtin.codexhook", Capability: "codex:read", Scope: "session:list"},
		{SubjectID: "builtin.codexhook", Capability: "codex:read", Scope: "session:*"},
		{SubjectID: "builtin.codexhook", Capability: "codex:write", Scope: "session:*"},
	}...))

	vaultService := vault.NewService(keyringStore)
	auditSink := audit.NewMemorySink()
	itemStore := vaultitem.NewStore(itemRepo, vaultService)
	envService := environments.NewService(envRepo, newVaultItemEnvUsage(itemStore))

	sshService := sshservice.NewService(itemStore, envService)
	dbService := database.NewService(itemStore, envService)
	dbService.SetTunnel(sshTunnel{ssh: sshService})
	// In-memory history by default; main.go swaps in the SQL-backed table.
	// History never goes through itemStore — see internal/database/queryhistory.go.
	dbService.SetQueryLog(database.NewMemoryQueryHistory(), vaultService)
	dbService.SetSchemaCache(database.NewMemorySchemaCache())

	c := &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Audit:        auditSink,
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy, auditSink),
		Vault:        vaultService,
		HashTools:    tools.NewHashService(),
		UtilTools:    tools.NewUtilService(),
		Items:        itemStore,
		Notes:        notes.NewService(itemStore),
		Environments: envService,
		Preferences:  preferences.NewService(preferences.NewMemoryRepository()),
		Updates:      &updater.Checker{},
		Version:      buildinfo.Version,
		UIState:      uistate.NewService(uistate.NewMemoryRepository()),
		Database:     dbService,
		SSH:          sshService,
		CodeGraph:    codegraph.NewService(codegraph.NewNilStore(), codegraph.NewSubprocessAnalyzer()),
		Kafka:        kafka.NewService(itemStore, envService, nil),
		// No *sql.DB here: an ephemeral container has no local table, so it
		// holds shared accounts only. main rebuilds this with the real handle.
		Accounts:    accounts.NewService(itemStore, nil, "", vaultService, accounts.NewRegistry(accounts.DefaultProviders()), envService),
		Tasks:       tasks.NewService(itemStore),
		Git:         gitservice.NewService(context.Background()),
		WorkFolder:  workfolder.NewService(),
		GitBindings: NewGitBindings(itemStore),
		APIClient:   apiclient.NewService(itemStore, envService),
		Workspace:   workspace.NewService(workspace.NewMemoryRepository()),
	}
	c.CodeGraphRegistry = codegraph.NewRegistry(c.CodeGraph, -1)
	c.WorkspaceCode = NewWorkspaceCode(c)
	return c
}
