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
	Notes        *notes.Service
	Database     *database.Service
	SSH          *sshservice.Service
}

// NewContainer wires core services with in-memory stores, suitable for tests
// and ephemeral runs. The desktop/server entrypoint should call
// NewContainerWithStores with file-backed stores so the Master Password set at
// setup and the notes persist across restarts.
func NewContainer() *Container {
	return NewContainerWithStores(
		vault.NewMemoryKeyringStore(),
		notes.NewMemoryRepository(),
		database.NewMemoryRepository(),
		sshservice.NewMemoryRepository(),
	)
}

func NewContainerWithStores(
	keyringStore vault.KeyringStore,
	notesRepo notes.Repository,
	dbRepo database.Repository,
	sshRepo sshservice.Repository,
) *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
		{SubjectID: "builtin.notes", Capability: "notes:write", Scope: "*"},
		{SubjectID: "builtin.notes", Capability: "notes:read", Scope: "*"},
		{SubjectID: "builtin.database", Capability: "db:query", Scope: "*"},
		{SubjectID: "builtin.ssh", Capability: "ssh:connect", Scope: "*"},
	})

	vaultService := vault.NewService(keyringStore)
	auditSink := audit.NewMemorySink()

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Audit:        auditSink,
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy, auditSink),
		Vault:        vaultService,
		HashTools:    tools.NewHashService(),
		Notes:        notes.NewService(notesRepo, vaultService),
		Database:     database.NewService(dbRepo, vaultService),
		SSH:          sshservice.NewService(sshRepo, vaultService),
	}
}
