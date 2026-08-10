package app

import (
	"context"
	"time"

	"changeme/internal/audit"
	"changeme/internal/auth"
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/database"
	"changeme/internal/environments"
	"changeme/internal/modules"
	"changeme/internal/notes"
	"changeme/internal/permissions"
	sshservice "changeme/internal/ssh"
	"changeme/internal/syncclient"
	"changeme/internal/syncconflict"
	"changeme/internal/tools"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
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
	Items        *vaultitem.Store
	Notes        *notes.Service
	Environments *environments.Service
	Database     *database.Service
	SSH          *sshservice.Service
	Sync         SyncService
	Auth         AuthService
	SyncConflict SyncConflictService
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
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "command:tools.hash"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:new"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:list"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:search"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:*"},
		{SubjectID: "builtin.environments", Capability: "env:read", Scope: "tag:list"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:new"},
		{SubjectID: "builtin.environments", Capability: "env:write", Scope: "tag:*"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:list"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:new"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:list"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:new"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "profile:*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "forward:list"},
		{SubjectID: "builtin.ssh", Capability: "ssh:forward", Scope: "forward:*"},
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
	})

	vaultService := vault.NewService(keyringStore)
	auditSink := audit.NewMemorySink()
	itemStore := vaultitem.NewStore(itemRepo, vaultService)
	envService := environments.NewService(envRepo)

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Audit:        auditSink,
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy, auditSink),
		Vault:        vaultService,
		HashTools:    tools.NewHashService(),
		Items:        itemStore,
		Notes:        notes.NewService(itemStore),
		Environments: envService,
		Database:     database.NewService(itemStore, envService),
		SSH:          sshservice.NewService(itemStore, envService),
	}
}
