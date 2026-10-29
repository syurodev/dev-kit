package mcp

import (
	"context"
	"errors"
	"fmt"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// WorkspaceAPI is the workspace registry as workspace.* tools read it.
type WorkspaceAPI interface {
	ListWorkspaces() ([]workspace.Workspace, error)
	// ListServices lists a workspace's services, the primary first.
	ListServices(workspaceID string) ([]workspace.WorkspaceService, error)
}

// WorkspaceWriteAPI is the owner of folder grants, Agent Hub.
type WorkspaceWriteAPI interface {
	PrepareWorkspaceWrite(ctx context.Context, folder string) (agenthub.WriteGrantPlan, error)
	CommitWorkspaceWrite(ctx context.Context, plan agenthub.WriteGrantPlan) (agenthub.WriteGrantResult, error)
}

// Public codes of workspace.requestWrite. Each is published in bridge keptCodes.
const (
	codeGrantRefused     = "workspace.grantRefused"
	codeGrantDeclined    = "workspace.grantDeclined"
	codeGrantUnavailable = "workspace.grantUnavailable"
)

type workspaceRequestWriteInput struct {
	Folder string `json:"folder" jsonschema:"the folder's name, exactly as workspace.list shows it"`
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
	sdk.AddTool(ms, s.gateOwnedTool(&sdk.Tool{
		Name: "workspace.requestWrite",
		Description: "Ask the person for write access to a folder of your workspace that workspace.list shows with granted:false; pass its name. " +
			"The person is asked every time. Once approved, git tools and sub-sessions you create later get the folder; your own shell and " +
			"file tools get it when DevKit reopens this session after this turn (the answer says when). Top-level sessions only: a sub-session asks its parent.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}), s.toolWorkspaceRequestWrite)
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

func (s *Server) toolWorkspaceRequestWrite(ctx context.Context, _ *sdk.CallToolRequest, in workspaceRequestWriteInput) (*sdk.CallToolResult, any, error) {
	if s.deps.WorkspaceWrite == nil {
		return nil, nil, apperrors.New("internal_error", "Folder access requests are not available.", true)
	}
	folder := []rune(in.Folder)
	if len(folder) > 80 {
		folder = folder[:80]
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.workspace", "ws:grant", "workspace:requestWrite",
		meta("request_workspace_write", map[string]any{"folder": string(folder)}),
		func(callCtx context.Context) (any, error) {
			plan, err := s.deps.WorkspaceWrite.PrepareWorkspaceWrite(callCtx, in.Folder)
			if err != nil {
				return nil, mapGrantError(err)
			}
			if !plan.AlreadyGranted {
				if err := s.grantConfirm(callCtx, plan); err != nil {
					return nil, mapGrantError(err)
				}
			}
			res, err := s.deps.WorkspaceWrite.CommitWorkspaceWrite(callCtx, plan)
			if err != nil {
				return nil, mapGrantError(err)
			}
			return res, nil
		})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// grantConfirm asks every time: no allow-always (EnvBlocks), the same rule as peerConfirm.
func (s *Server) grantConfirm(ctx context.Context, plan agenthub.WriteGrantPlan) error {
	return effectiveHub(s.deps.Hub).Confirm(ctx, interaction.ConfirmRequest{
		Tool: "workspace.requestWrite", Kind: interaction.KindAgent, EnvBlocks: true,
		Summary: fmt.Sprintf("Let this session write in %q (workspace %q)? Path: %s. This lasts for as long as the session does.",
			plan.Folder, plan.WorkspaceName, plan.Path),
	})
}

func mapGrantError(err error) error {
	var refused *agenthub.GrantRefusedError
	switch {
	case errors.As(err, &refused):
		return apperrors.New(codeGrantRefused, refused.Reason, false)
	case errors.Is(err, interaction.ErrConfirmationDenied):
		return apperrors.New(codeGrantDeclined, "The person declined to give this session write access to that folder.", false)
	case errors.Is(err, interaction.ErrConfirmationRequired):
		return apperrors.New(codeGrantUnavailable, "No one can approve this right now: there is no window to ask in.", false)
	default:
		return mapInteractErr(err)
	}
}
