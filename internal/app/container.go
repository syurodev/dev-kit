package app

import (
	"context"

	"changeme/internal/audit"
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/database"
	"changeme/internal/modules"
	"changeme/internal/notes"
	"changeme/internal/permissions"
	sshservice "changeme/internal/ssh"
	"changeme/internal/syncclient"
	"changeme/internal/syncstore"
	"changeme/internal/tools"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
)

// SyncService is the narrow runtime contract exposed to the bridge. Keeping it
// as an interface lets the core container remain usable for offline tests and
// prevents bridge code from depending on HTTP implementation details.
type SyncService interface {
	Connect(ctx context.Context, baseURL, accessToken string) error
	Disconnect()
	Status() syncclient.Status
	SyncNow(ctx context.Context) (syncclient.Report, error)
	Conflicts(ctx context.Context) ([]syncclient.ConflictInfo, error)
}

// ConflictReader reads the captured "theirs" side of a sync conflict so the
// bridge can present ours-vs-theirs and resolve. AccountReader supplies the
// bound account for offline resolution, and ConflictResolver applies the chosen
// strategy atomically. These are optional (nil in ephemeral/test containers).
type ConflictReader interface {
	Get(ctx context.Context, accountID, recordID string) (syncstore.ConflictRecord, bool, error)
}

type ConflictResolver interface {
	ResolveUseTheirs(ctx context.Context, accountID, recordID string) error
	ResolveUseLocal(ctx context.Context, accountID, recordID string, res syncstore.LocalResolution) error
}

type AccountReader interface {
	BoundAccountID(ctx context.Context) (string, bool, error)
}

type Container struct {
	Commands         *commands.Registry
	Modules          *modules.Registry
	Audit            *audit.MemorySink
	Permissions      *permissions.Policy
	Capabilities     *capabilities.Gateway
	Vault            *vault.Service
	HashTools        *tools.HashService
	Items            *vaultitem.Store
	Notes            *notes.Service
	Database         *database.Service
	SSH              *sshservice.Service
	Sync             SyncService
	ConflictStore    ConflictReader
	ConflictResolver ConflictResolver
	AccountReader    AccountReader
}

// NewContainer wires core services with in-memory stores, suitable for tests
// and ephemeral runs.
func NewContainer() *Container {
	return NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		vaultitem.NewMemoryRepository(),
	)
}

func NewContainerWithStores(
	keyringStore vault.KeyringStore,
	itemRepo vaultitem.Repository,
) *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "command:tools.hash"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:new"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "note:*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:list"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:search"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "note:*"},
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
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:connect"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:run"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:disconnect"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:conflicts"},
		{SubjectID: "builtin.sync", Capability: "sync:manage", Scope: "sync:resolve"},
	})

	vaultService := vault.NewService(keyringStore)
	auditSink := audit.NewMemorySink()
	itemStore := vaultitem.NewStore(itemRepo, vaultService)

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
		Database:     database.NewService(itemStore),
		SSH:          sshservice.NewService(itemStore),
	}
}
