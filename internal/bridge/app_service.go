package bridge

import (
	"context"
	"fmt"

	"changeme/internal/app"
	"changeme/internal/services/tools"
)

type AppService struct {
	container *app.Container
}

func NewAppService(container *app.Container) *AppService {
	return &AppService{container: container}
}

func (s *AppService) ListModules() []ModuleDTO {
	modules := s.container.Modules.List()
	result := make([]ModuleDTO, 0, len(modules))
	for _, module := range modules {
		result = append(result, ModuleDTO{
			ID:       module.ID,
			Title:    module.Title,
			Commands: module.Commands,
			Views:    module.Views,
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
