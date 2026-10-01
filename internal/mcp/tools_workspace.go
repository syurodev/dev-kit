package mcp

import (
	"context"

	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// WorkspaceAPI is the workspace registry as workspace.* tools read it.
type WorkspaceAPI interface {
	ListWorkspaces() ([]workspace.Workspace, error)
	// ListServices lists a workspace's services, the primary first.
	ListServices(workspaceID string) ([]workspace.WorkspaceService, error)
}

// WorkspaceFolder is one folder (service) of a workspace.
type WorkspaceFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Primary is the workspace's main folder — where a whole-workspace
	// session starts.
	Primary bool `json:"primary"`
	// Granted says the calling agent session may write here: its working
	// directory or a folder the session was granted beside it. Only set for
	// an agent; a folder that is not granted may still be readable, or not,
	// depending on the sandbox settings.
	Granted bool `json:"granted,omitempty"`
}

// WorkspaceInfo is a workspace and its folders, primary first.
type WorkspaceInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Folders []WorkspaceFolder `json:"folders"`
}

// WorkspaceListResult is workspace.list's answer.
type WorkspaceListResult struct {
	// Cwd is the calling agent session's working directory; empty for a
	// caller that is not an agent session.
	Cwd string `json:"cwd,omitempty"`
	// SelectedWorkspaceID is the session's context, even if another workspace
	// lists the same folder. Empty for unbound or terminal callers.
	SelectedWorkspaceID string          `json:"selectedWorkspaceId,omitempty"`
	Workspaces          []WorkspaceInfo `json:"workspaces"`
}

func (s *Server) registerWorkspace(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name: "workspace.list",
		Description: "List DevKit workspaces and their folders (one per service/repository), primary first. " +
			"Called from an Agent Hub session, it returns its selected workspace, " +
			"its working directory (cwd), and which folders the session may write (granted).",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolWorkspaceList)
}

func (s *Server) toolWorkspaceList(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	if s.deps.Workspace == nil {
		return nil, nil, apperrors.New("internal_error", "Workspaces are not available.", true)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.workspace", "ws:read", "workspace:list", meta("list_workspaces", nil), func(callCtx context.Context) (any, error) {
		return s.workspaceList(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// workspaceList answers for the caller. An agent session sees the
// workspaces its own folders are in, not every folder registered on the
// machine: it has no use for the rest, and a list of paths is still a list
// of what else is on disk.
func (s *Server) workspaceList(ctx context.Context) (WorkspaceListResult, error) {
	var granted []string
	cwd := ""
	selectedWorkspaceID := ""
	caller, _ := identity.FromContext(ctx)
	if caller.IsAgent() {
		if s.deps.AgentHub == nil {
			return WorkspaceListResult{}, apperrors.New("internal_error", "Agent Hub is not available.", true)
		}
		session, err := s.deps.AgentHub.GetStatus(ctx, caller.SessionID)
		if err != nil {
			return WorkspaceListResult{}, err
		}
		cwd = session.ProjectPath
		selectedWorkspaceID = session.WorkspaceID
		granted = append([]string{session.ProjectPath}, session.ExtraReadWritePaths...)
	}

	all, err := s.deps.Workspace.ListWorkspaces()
	if err != nil {
		return WorkspaceListResult{}, err
	}
	out := WorkspaceListResult{Cwd: cwd, SelectedWorkspaceID: selectedWorkspaceID, Workspaces: []WorkspaceInfo{}}
	for _, ws := range all {
		if caller.IsAgent() && selectedWorkspaceID != "" && ws.ID != selectedWorkspaceID {
			continue
		}
		services, err := s.deps.Workspace.ListServices(ws.ID)
		if err != nil {
			return WorkspaceListResult{}, err
		}
		info := WorkspaceInfo{ID: ws.ID, Name: ws.Name, Folders: make([]WorkspaceFolder, 0, len(services))}
		mine := selectedWorkspaceID != "" && ws.ID == selectedWorkspaceID
		for _, svc := range services {
			folder := WorkspaceFolder{Name: svc.Name, Path: svc.Path, Primary: svc.Path == ws.Root}
			if caller.IsAgent() {
				folder.Granted = workspace.Covered(svc.Path, granted)
				if folder.Granted || workspace.Within(cwd, svc.Path) {
					mine = true
				}
			}
			info.Folders = append(info.Folders, folder)
		}
		if caller.IsAgent() && !mine {
			continue
		}
		out.Workspaces = append(out.Workspaces, info)
	}
	if caller.IsAgent() && selectedWorkspaceID == "" && len(out.Workspaces) > 1 {
		// A legacy session in overlapping workspaces has no recorded choice.
		// Returning both would ask the agent to guess its scope.
		out.Workspaces = []WorkspaceInfo{}
	}
	return out, nil
}
