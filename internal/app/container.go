package app

import (
	"changeme/internal/capabilities"
	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/permissions"
	"changeme/internal/services/tools"
	"changeme/internal/vault"
)

type Container struct {
	Commands     *commands.Registry
	Modules      *modules.Registry
	Permissions  *permissions.Policy
	Capabilities *capabilities.Gateway
	Vault        *vault.Service
	HashTools    *tools.HashService
}

// NewContainer wires core services with an in-memory keyring store, suitable
// for tests and ephemeral runs. The desktop/server entrypoint should call
// NewContainerWithKeyringStore with a FileKeyringStore so the Master Password
// set at setup persists across restarts.
func NewContainer() *Container {
	return NewContainerWithKeyringStore(vault.NewMemoryKeyringStore())
}

func NewContainerWithKeyringStore(keyringStore vault.KeyringStore) *Container {
	permissionPolicy := permissions.NewPolicy([]permissions.Grant{
		{SubjectID: "builtin.tools", Capability: "tools:hash", Scope: "*"},
	})

	return &Container{
		Commands:     commands.NewRegistry(),
		Modules:      modules.NewRegistry(),
		Permissions:  permissionPolicy,
		Capabilities: capabilities.NewGateway(permissionPolicy),
		Vault:        vault.NewService(keyringStore),
		HashTools:    tools.NewHashService(),
	}
}
