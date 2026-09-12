package app

import (
	"context"
	"sync"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/audit"
	"devkit/internal/auth"
	"devkit/internal/capabilities"
	"devkit/internal/codegraph"
	"devkit/internal/commands"
	"devkit/internal/database"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/modules"
	"devkit/internal/notes"
	"devkit/internal/permissions"
	"devkit/internal/preferences"
	sshservice "devkit/internal/ssh"
	"devkit/internal/syncclient"
	"devkit/internal/syncconflict"
	"devkit/internal/terminal"
	"devkit/internal/tools"
	"devkit/internal/uistate"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
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
	ConfigDir    string
	Kafka        *kafka.Service
	Sync         SyncService
	SyncConflict SyncConflictService
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

	// auth and syncBlocked are mutable after startup: the cloudconfig refresh
	// goroutine rewrites them while bridge call handlers read them, so every
	// access goes through the mutex-guarded accessors below.
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

	permissionPolicy := permissions.NewPolicy(append(toolGrants, []permissions.Grant{
		// Tools grants come from tools.Commands so a new command cannot be
		// registered without one — the failure that leaves a command runnable
		// with no matching grant, or granted and never wired.
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:new"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:list"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:search"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:*"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note-folder:new"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note-folder:*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note-folder:list"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note-folder:*"},
		{SubjectID: "builtin.environments", Capability: "env:read", Scope: "tag:list"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:new"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:*"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:list"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:new"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:active"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:*"},
		// Saved queries are scoped by their own id, not the profile's.
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "query:*"},
		// Cancel and terminate act on a server session, not on a profile.
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "session:*"},
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
		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "permission:list"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:new"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "session:*"},
		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "permission:complete"},
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
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:list"},
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:claude-code"},
		{SubjectID: "builtin.mcpinstall", Capability: "mcp:install", Scope: "agent:codex"},
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "theme"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "theme"},
		// The palette is a scope of its own rather than more room inside
		// "theme", matching how the sync and mcp grants above are split one per
		// operation. A scope that is not listed here is denied, and the denial
		// reaches the UI as the palette snapping back — setFamily applies
		// optimistically and reverts when the save rejects.
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "theme_family"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "theme_family"},
		{SubjectID: "builtin.uistate", Capability: "ui:read", Scope: "key:*"},
		{SubjectID: "builtin.uistate", Capability: "ui:write", Scope: "key:*"},
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

	return &Container{
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
		UIState:      uistate.NewService(uistate.NewMemoryRepository()),
		Database:     dbService,
		SSH:          sshService,
		CodeGraph:    codegraph.NewService(codegraph.NewNilStore(), codegraph.NewSubprocessAnalyzer()),
		Kafka:        kafka.NewService(itemStore, envService, nil),
	}
}
