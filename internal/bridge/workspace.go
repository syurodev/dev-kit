package bridge

import (
	"context"

	"devkit/internal/workspace"
)

// WorkspaceList returns every registered workspace.
func (s *AppService) WorkspaceList() ([]workspace.Workspace, error) {
	result, err := s.call(context.Background(), "builtin.workspace", "ws:read", "workspace:list", map[string]any{
		"action": "list_workspaces",
	}, func(context.Context) (any, error) {
		return s.container.Workspace.ListWorkspaces()
	})
	if err != nil {
		return nil, err
	}
	return result.([]workspace.Workspace), nil
}

// WorkspaceListServices returns every service registered under workspaceID.
func (s *AppService) WorkspaceListServices(workspaceID string) ([]workspace.WorkspaceService, error) {
	opaqueID := recordID(workspaceID)
	result, err := s.call(context.Background(), "builtin.workspace", "ws:read", "service:"+opaqueID, map[string]any{
		"action":       "list_services",
		"workspace_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Workspace.ListServices(workspaceID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]workspace.WorkspaceService), nil
}

// WorkspaceChooseFolder opens the native folder picker for a workspace's
// root. Same shape as TasksBoardChooseProject: cancelling returns "" and is
// not an error, since backing out of a picker is not a failure the user
// caused.
func (s *AppService) WorkspaceChooseFolder(startIn string) (string, error) {
	result, err := s.call(context.Background(), "builtin.workspace", "ws:write", "folder:choose", map[string]any{
		"action": "choose_folder",
	}, func(context.Context) (any, error) {
		dialog := s.openDirDialog
		if dialog == nil {
			dialog = defaultOpenDirectoryDialog
		}
		return dialog("Add workspace", startIn)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// WorkspaceAddResult is what AddWorkspace hands back: the new workspace and
// the services auto-discovery split it into.
type WorkspaceAddResult struct {
	Workspace workspace.Workspace          `json:"workspace"`
	Services  []workspace.WorkspaceService `json:"services"`
}

// WorkspaceAdd registers root as a new workspace. name may be empty — see
// workspace.Service.AddWorkspace for the folder-name fallback and the
// auto-discovery split this runs.
func (s *AppService) WorkspaceAdd(name, root string) (WorkspaceAddResult, error) {
	result, err := s.call(context.Background(), "builtin.workspace", "ws:write", "workspace:new", map[string]any{
		"action": "add_workspace",
	}, func(context.Context) (any, error) {
		ws, services, err := s.container.Workspace.AddWorkspace(name, root)
		if err != nil {
			return nil, err
		}
		return WorkspaceAddResult{Workspace: ws, Services: services}, nil
	})
	if err != nil {
		return WorkspaceAddResult{}, err
	}
	return result.(WorkspaceAddResult), nil
}

// WorkspaceDelete removes a workspace and every service under it from the
// registry. It never touches the underlying folders.
func (s *AppService) WorkspaceDelete(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.workspace", "ws:write", "workspace:"+opaqueID, map[string]any{
		"action":       "delete_workspace",
		"workspace_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Workspace.DeleteWorkspace(id)
	})
	return err
}
