package bridge

import (
	"context"
	"fmt"

	"devkit/internal/app"
	"devkit/internal/tools"
)

type AppService struct {
	container *app.Container
}

func NewAppService(container *app.Container) *AppService {
	return &AppService{container: container}
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
	var payload any = request.Payload
	if request.ID == "tools.hash" {
		payload = tools.HashRequest{
			Algorithm: fmt.Sprint(request.Payload["algorithm"]),
			Input:     fmt.Sprint(request.Payload["input"]),
		}
	}
	opaqueID := commandID(request.ID)
	return s.call(ctx, "builtin.tools", "tools:hash", "command:"+opaqueID, map[string]any{
		"action":     "execute",
		"command_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Commands.Execute(callCtx, request.ID, payload)
	})
}
