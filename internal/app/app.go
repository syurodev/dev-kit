package app

import (
	"context"
	"fmt"

	"devkit/internal/appmenu"
	"devkit/internal/commands"
	"devkit/internal/modules"
	"devkit/internal/tools"
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
			Commands:     tools.CommandIDs(),
			Views:        tools.CommandIDs(),
			Capabilities: tools.CapabilityIDs(),
		},
		{
			ID:           "notes",
			Title:        "Notes",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"notes.create"},
			Views:        []string{"notes.list", "notes.editor"},
			Capabilities: []string{"notes:read", "notes:write"},
		},
		{
			ID:         "database",
			Title:      "Database",
			Version:    "1.0.0",
			APIVersion: modules.CurrentAPIVersion,
			// The same list the menu bar is built from, so a command cannot
			// exist in one and not the other — which is what "Module metadata
			// is unavailable or out of sync" meant the first time.
			Commands:     appmenu.ModuleCommandIDs("database"),
			Views:        []string{"database.console", "database.table", "database.erd", "database.activity", "database.history", "database.saved"},
			Capabilities: []string{"db:query"},
		},
		{
			ID:           "ssh",
			Title:        "SSH",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"ssh.run"},
			Views:        []string{"ssh.connections"},
			Capabilities: []string{"ssh:connect", "ssh:forward", "terminal:io"},
		},
		{
			ID:           "kafka",
			Title:        "Kafka",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"kafka.publish"},
			Views:        []string{"kafka.explorer"},
			Capabilities: []string{"kafka:connect", "kafka:produce"},
		},
		{
			ID:         "codegraph",
			Title:      "Code Graph",
			Version:    "1.0.0",
			APIVersion: modules.CurrentAPIVersion,
			Commands:   []string{"codegraph.open_project", "codegraph.reindex_project"},
			// Flows, files and the plan are documents of their own (frontend
			// modules/codegraph/documents.ts); the shell compares this list with
			// the manifest on every launch.
			Views:        []string{"codegraph.explorer", "codegraph.flow", "codegraph.file", "codegraph.plan"},
			Capabilities: []string{"codegraph:index", "codegraph:read", "codegraph:toolchain"},
		},
		{
			ID:           "agenthub",
			Title:        "Agent Hub",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     appmenu.ModuleCommandIDs("agenthub"),
			Views:        []string{"agenthub.sessions"},
			Capabilities: []string{"agenthub:read", "agenthub:execute"},
		},
		{
			ID:         "accounts",
			Title:      "Accounts",
			Version:    "1.0.0",
			APIVersion: modules.CurrentAPIVersion,
			Commands:   []string{"accounts.create"},
			Views:      []string{"accounts.list"},
			// accounts:reveal is declared so the module's surface is honest
			// about what exists. The grant that matters is in container.go,
			// and the MCP subject never receives it.
			Capabilities: []string{"accounts:read", "accounts:write", "accounts:reveal", "accounts:use"},
		},
		{
			ID:           "tasks",
			Title:        "Tasks",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"tasks.create"},
			Views:        []string{"tasks.board", "tasks.detail"},
			Capabilities: []string{"tasks:read", "tasks:write", "tasks:admin"},
		},
		{
			ID:         "git",
			Title:      "Git",
			Version:    "1.0.0",
			APIVersion: modules.CurrentAPIVersion,
			Commands:   []string{"git.commit", "git.push"},
			Views:      []string{"git.changes", "git.diff", "git.remotes", "git.history"},
			// git:push is separate from git:write because it leaves the
			// machine. There is no git:push:force, here or anywhere.
			Capabilities: []string{"git:read", "git:write", "git:push"},
		},
		{
			ID:           "apiclient",
			Title:        "API Client",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"apiclient.new_request", "apiclient.import"},
			Views:        []string{"apiclient.request", "apiclient.variables"},
			Capabilities: []string{"api:read", "api:write", "api:send"},
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
		{
			ID:           "settings",
			Title:        "Settings",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{},
			Views:        []string{"settings.environments"},
			Capabilities: []string{"env:read", "env:write", "preferences:read", "preferences:write"},
		},
	}

	for _, manifest := range builtins {
		if err := container.Modules.Register(manifest); err != nil {
			return err
		}
	}

	for _, descriptor := range tools.Commands {
		handler, err := toolHandler(container, descriptor.ID)
		if err != nil {
			return err
		}
		if err := container.Commands.Register(commands.Command{
			ID:          descriptor.ID,
			Title:       descriptor.Title,
			Permissions: []string{descriptor.Capability},
			Handler:     handler,
		}); err != nil {
			return err
		}
	}
	return nil
}

// toolHandler binds one command id to the service call behind it.
//
// Each handler asserts the request shape it was given and reports a mismatch
// rather than returning (nil, nil) as the original hash handler did — a
// silently empty success is indistinguishable from a command that ran and
// produced nothing, and the caller has no way to tell which happened.
func toolHandler(container *Container, id string) (commands.Handler, error) {
	hashed := func(ctx context.Context, input any) (any, error) {
		request, ok := input.(tools.HashRequest)
		if !ok {
			return nil, fmt.Errorf("command %q expects a HashRequest, got %T", id, input)
		}
		return container.HashTools.Hash(request)
	}
	util := func(run func(tools.UtilRequest) (tools.UtilResult, error)) commands.Handler {
		return func(ctx context.Context, input any) (any, error) {
			request, ok := input.(tools.UtilRequest)
			if !ok {
				return nil, fmt.Errorf("command %q expects a UtilRequest, got %T", id, input)
			}
			return run(request)
		}
	}
	text := func(run func(string) (tools.UtilResult, error)) commands.Handler {
		return util(func(request tools.UtilRequest) (tools.UtilResult, error) {
			return run(request.Input)
		})
	}

	switch id {
	case tools.CommandHash:
		return hashed, nil
	case tools.CommandBase64Encode:
		return text(container.UtilTools.Base64Encode), nil
	case tools.CommandBase64Decode:
		return text(container.UtilTools.Base64Decode), nil
	case tools.CommandURLEncode:
		return text(container.UtilTools.URLEncode), nil
	case tools.CommandURLDecode:
		return text(container.UtilTools.URLDecode), nil
	case tools.CommandJSONFormat:
		return text(container.UtilTools.JSONFormat), nil
	case tools.CommandJWTDecode:
		return text(container.UtilTools.JWTDecode), nil
	case tools.CommandUUIDGenerate:
		return util(func(request tools.UtilRequest) (tools.UtilResult, error) {
			return container.UtilTools.UUIDGenerate(request.Version)
		}), nil
	default:
		// A descriptor with no handler would register a command that always
		// fails at call time; failing at startup names the omission instead.
		return nil, fmt.Errorf("no handler for tools command %q", id)
	}
}
