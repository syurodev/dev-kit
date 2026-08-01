package app

import (
	"changeme/internal/audit"
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/permissions"
	"changeme/internal/services/database"
	"changeme/internal/services/notes"
	sshservice "changeme/internal/services/ssh"
	"changeme/internal/services/tools"
	"changeme/internal/services/vaultitem"
	"changeme/internal/vault"
)

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
	Database     *database.Service
	SSH          *sshservice.Service
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
