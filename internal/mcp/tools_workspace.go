package mcp

import (
	"context"
	"path/filepath"
	"strings"

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
	Cwd        string          `json:"cwd,omitempty"`
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

func (s *Server) registerWorkspace(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name: "workspace.list",
		Description: "List DevKit workspaces and their folders (one per service/repository), primary first. " +
			"Called from an Agent Hub session, it returns only the workspace(s) the session's folders belong to, " +
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
		granted = append([]string{session.ProjectPath}, session.ExtraReadWritePaths...)
	}

	all, err := s.deps.Workspace.ListWorkspaces()
	if err != nil {
		return WorkspaceListResult{}, err
	}
	out := WorkspaceListResult{Cwd: cwd, Workspaces: []WorkspaceInfo{}}
	for _, ws := range all {
		services, err := s.deps.Workspace.ListServices(ws.ID)
		if err != nil {
			return WorkspaceListResult{}, err
		}
		info := WorkspaceInfo{ID: ws.ID, Name: ws.Name, Folders: make([]WorkspaceFolder, 0, len(services))}
		mine := false
		for _, svc := range services {
			folder := WorkspaceFolder{Name: svc.Name, Path: svc.Path, Primary: svc.Path == ws.Root}
			if caller.IsAgent() {
				folder.Granted = coveredBy(svc.Path, granted)
				if folder.Granted || within(cwd, svc.Path) {
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
	return out, nil
}

// coveredBy reports whether path is one of roots or inside one.
func coveredBy(path string, roots []string) bool {
	for _, root := range roots {
		if root != "" && within(path, root) {
			return true
		}
	}
	return false
}

// within reports whether path is root or inside it — a prefix of a folder
// name (kong-plugin-v2 against kong-plugin) is not.
func within(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
