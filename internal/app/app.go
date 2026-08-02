package app

import (
	"context"

	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/services/tools"
)

func RegisterBuiltins(container *Container) error {
	// Built-ins use the same declarative descriptor shape as future extensions.
	// The capability list is descriptive metadata only; grants remain owned by
	// the permission policy in the application container.
	builtins := []modules.ModuleManifest{
		{
			ID:           "tools",
			Title:        "Tools",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"tools.hash"},
			Views:        []string{"tools.hash"},
			Capabilities: []string{"tools:hash"},
		},
		{
			ID:           "notes",
			Title:        "Notes",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"notes.create"},
			Views:        []string{"notes.list"},
			Capabilities: []string{"notes:read", "notes:write"},
		},
		{
			ID:           "database",
			Title:        "Database",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"database.query"},
			Views:        []string{"database.connections"},
			Capabilities: []string{"db:query"},
		},
		{
			ID:           "ssh",
			Title:        "SSH",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"ssh.run"},
			Views:        []string{"ssh.connections"},
			Capabilities: []string{"ssh:connect", "ssh:forward"},
		},
		{
			ID:           "sync",
			Title:        "Sync",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"sync.run"},
			Views:        []string{"sync.status"},
			Capabilities: []string{"sync:manage"},
		},
	}

	for _, manifest := range builtins {
		if err := container.Modules.Register(manifest); err != nil {
			return err
		}
	}

	return container.Commands.Register(commands.Command{
		ID:    "tools.hash",
		Title: "Hash text",
		Handler: func(ctx context.Context, input any) (any, error) {
			request, ok := input.(tools.HashRequest)
			if !ok {
				return nil, nil
			}
			return container.HashTools.Hash(request)
		},
	})
}
