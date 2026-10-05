package bridge

import (
	"context"
	"fmt"

	"devkit/internal/app"
	"devkit/internal/preferences"
	"devkit/internal/tools"
)

type AppService struct {
	container       *app.Container
	applyTheme      func(preferences.Theme) error
	saveFileDialog  func(suggestedName string) (string, error)
	openDirDialog   func(title, startIn string) (string, error)
	openFilesDialog func(title, startIn string) ([]string, error)

	// sshKeys are the key files this window has been shown or has had chosen for
	// it, so reading one by path is limited to those.
	sshKeys sshKeyPaths
	// serverMode reports the unauthenticated HTTP build. Nil means a desktop one.
	serverMode func() bool
}

func NewAppService(container *app.Container, themeApplier ...func(preferences.Theme) error) *AppService {
	service := &AppService{container: container}
	if len(themeApplier) > 0 {
		service.applyTheme = themeApplier[0]
	}
	return service
}

type ModuleDTO struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Version      string   `json:"version"`
	APIVersion   string   `json:"api_version"`
	Commands     []string `json:"commands"`
	Views        []string `json:"views"`
	Capabilities []string `json:"capabilities"`
}

type ExecuteCommandRequest struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

func (s *AppService) ListModules() []ModuleDTO {
	modules := s.container.Modules.List()
	result := make([]ModuleDTO, 0, len(modules))
	for _, module := range modules {
		result = append(result, ModuleDTO{
			ID:           module.ID,
			Title:        module.Title,
			Version:      module.Version,
			APIVersion:   module.APIVersion,
			Commands:     append([]string(nil), module.Commands...),
			Views:        append([]string(nil), module.Views...),
			Capabilities: append([]string(nil), module.Capabilities...),
		})
	}
	return result
}

func (s *AppService) ExecuteCommand(ctx context.Context, request ExecuteCommandRequest) (any, error) {
	// The capability comes from the command, not from a constant. Every command
	// used to be authorised as tools:hash, which was harmless while hash was
	// the only one and becomes an authorisation bug the moment it is not: a
	// command would run under a grant written for something else.
	capability, known := tools.CapabilityFor(request.ID)
	if !known {
		return nil, fmt.Errorf("command %q is not a tools command", request.ID)
	}
	payload := decodeCommandPayload(request)
	opaqueID := commandID(request.ID)
	return s.call(ctx, tools.SubjectID, capability, "command:"+opaqueID, map[string]any{
		"action":     "execute",
		"command_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Commands.Execute(callCtx, request.ID, payload)
	})
}

// decodeCommandPayload turns the untyped map the webview sends into the request
// shape the handler asserts. Missing keys read as empty rather than failing
// here: the service is what knows which of its fields are required, and it
// already reports that (an unsupported hash algorithm, an unsupported UUID
// version) in terms the caller can act on.
func decodeCommandPayload(request ExecuteCommandRequest) any {
	field := func(name string) string {
		value, ok := request.Payload[name]
		if !ok || value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	if request.ID == tools.CommandHash {
		return tools.HashRequest{
			Algorithm: field("algorithm"),
			Input:     field("input"),
		}
	}
	return tools.UtilRequest{
		Input:   field("input"),
		Version: field("version"),
	}
}
