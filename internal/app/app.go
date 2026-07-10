package app

import (
	"context"

	"changeme/internal/commands"
	"changeme/internal/modules"
	"changeme/internal/services/tools"
)

func RegisterBuiltins(container *Container) error {
	if err := container.Modules.Register(modules.Module{
		ID:       "tools",
		Title:    "Tools",
		Commands: []string{"tools.hash"},
		Views:    []string{"tools.hash"},
	}); err != nil {
		return err
	}

	if err := container.Modules.Register(modules.Module{
		ID:       "notes",
		Title:    "Notes",
		Commands: []string{"notes.create"},
		Views:    []string{"notes.list"},
	}); err != nil {
		return err
	}

	if err := container.Modules.Register(modules.Module{
		ID:       "database",
		Title:    "Database",
		Commands: []string{"database.query"},
		Views:    []string{"database.connections"},
	}); err != nil {
		return err
	}

	if err := container.Modules.Register(modules.Module{
		ID:       "ssh",
		Title:    "SSH",
		Commands: []string{"ssh.run"},
		Views:    []string{"ssh.connections"},
	}); err != nil {
		return err
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
