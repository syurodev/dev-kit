package bridge

import (
	"context"

	"devkit/internal/app"
	"devkit/internal/codegraph"
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
		return dialog("Add folder", startIn)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// WorkspaceAddResult is what WorkspaceCreate and WorkspaceUpdate hand back:
// the workspace and its services, primary first.
type WorkspaceAddResult struct {
	Workspace workspace.Workspace          `json:"workspace"`
	Services  []workspace.WorkspaceService `json:"services"`
}

// WorkspaceCreate registers a named workspace over folders — the Create
// project dialog. folders may be empty: a project is named first and given
// its folders afterwards. Each folder goes through auto-discovery; the first
// is the primary. See workspace.Service.CreateWorkspace.
func (s *AppService) WorkspaceCreate(name string, folders []string) (WorkspaceAddResult, error) {
	result, err := s.call(context.Background(), "builtin.workspace", "ws:write", "workspace:new", map[string]any{
		"action":  "create_workspace",
		"folders": len(folders),
	}, func(context.Context) (any, error) {
		ws, services, err := s.container.Workspace.CreateWorkspace(name, folders)
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

// WorkspaceUpdate renames a workspace and sets its service folders to
// paths, in order — the Edit project dialog's Save. Paths already registered
// keep their rows; new ones are discovered and queued for indexing; missing
// ones are dropped. The first is the primary.
func (s *AppService) WorkspaceUpdate(id, name string, paths []string) (WorkspaceAddResult, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.workspace", "ws:write", "workspace:"+opaqueID, map[string]any{
		"action":       "update_workspace",
		"workspace_id": opaqueID,
		"folders":      len(paths),
	}, func(context.Context) (any, error) {
		ws, services, err := s.container.Workspace.UpdateWorkspace(id, name, paths)
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

// WorkspaceIndexStatus reports where each service of a workspace stands
// with its index: pending, indexing, indexed (with when), failed (and
// whether a missing toolchain is why) or unsupported — the states
// workspace.mdx § "Trạng thái index" names. Live changes arrive as
// codegraph:indexStatus events.
func (s *AppService) WorkspaceIndexStatus(workspaceID string) ([]codegraph.ServiceIndexStatus, error) {
	opaqueID := recordID(workspaceID)
	result, err := s.call(context.Background(), "builtin.workspace", "ws:read", "service:"+opaqueID, map[string]any{
		"action":       "index_status",
		"workspace_id": opaqueID,
	}, func(context.Context) (any, error) {
		services, err := s.container.Workspace.ListServices(workspaceID)
		if err != nil {
			return nil, err
		}
		out := make([]codegraph.ServiceIndexStatus, 0, len(services))
		for _, svc := range services {
			out = append(out, app.ServiceIndexStatus(s.container, svc))
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.ServiceIndexStatus), nil
}

// WorkspaceReindexService queues one service for indexing again — Retry on a
// failed one, or a refresh. Returns at once; the outcome arrives as
// codegraph:indexStatus events.
func (s *AppService) WorkspaceReindexService(path string) error {
	_, err := s.call(context.Background(), "builtin.workspace", "ws:write", "service:reindex", map[string]any{
		"action": "reindex_service",
	}, func(context.Context) (any, error) {
		if s.container.CodeGraphRegistry == nil {
			return nil, codegraph.ErrNoActiveProject
		}
		s.container.CodeGraphRegistry.Enqueue(path)
		return nil, nil
	})
	return err
}
