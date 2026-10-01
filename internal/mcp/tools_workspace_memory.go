package mcp

import (
	"context"
	"errors"
	"strings"

	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/workspacememory"
	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// WorkspaceMemoryAPI is the workspace-scoped, versioned memory surface.
type WorkspaceMemoryAPI interface {
	List(context.Context, string, string, int, int) (workspacememory.Page, error)
	Get(context.Context, string, string) (workspacememory.Entry, error)
	Create(context.Context, string, workspacememory.Input, workspacememory.Source) (workspacememory.Entry, error)
	Update(context.Context, string, string, int64, workspacememory.Input, workspacememory.Source) (workspacememory.Entry, error)
	Delete(context.Context, string, string, int64, workspacememory.Source) (workspacememory.Entry, error)
	History(context.Context, string, string, int, int) ([]workspacememory.Revision, error)
	Restore(context.Context, string, string, int64, int64, workspacememory.Source) (workspacememory.Entry, error)
}

type memoryListInput struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	Query       string `json:"query,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}
type memoryGetInput struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	ID          string `json:"id"`
}
type memoryCreateInput struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body"`
}
type memoryUpdateInput struct {
	WorkspaceID      string `json:"workspaceId,omitempty"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	Body             string `json:"body"`
}
type memoryDeleteInput struct {
	WorkspaceID      string `json:"workspaceId,omitempty"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
}
type memoryHistoryInput struct {
	WorkspaceID string `json:"workspaceId,omitempty"`
	ID          string `json:"id"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}
type memoryRestoreInput struct {
	WorkspaceID      string `json:"workspaceId,omitempty"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
	TargetRevision   int64  `json:"targetRevision"`
}

func (s *Server) registerWorkspaceMemory(ms *sdk.Server) {
	scope := " Agent Hub sessions always use their selected workspace; terminal callers must supply workspaceId."
	read := &sdk.ToolAnnotations{ReadOnlyHint: true}
	write := &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)}
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.list", Description: "List shared workspace memory summaries with pagination." + scope, Annotations: read}, s.toolMemoryList)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.search", Description: "Search shared workspace memory by query, returning summaries with pagination." + scope, Annotations: read}, s.toolMemorySearch)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.get", Description: "Read a shared workspace memory entry including its full body and revision. Deleted entries return a tombstone with deleted=true for recovery." + scope, Annotations: read}, s.toolMemoryGet)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.create", Description: "Save durable shared workspace knowledge. Kind is decision, convention, knowledge or handoff. Provide a concise summary for turn context and detail in body. Authorship comes from your session." + scope, Annotations: write}, s.toolMemoryCreate)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.update", Description: "Replace a memory entry using expectedRevision from its last read. On a revision conflict, refetch before editing." + scope, Annotations: write}, s.toolMemoryUpdate)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.delete", Description: "Soft-delete a memory entry using expectedRevision. History remains available for recovery." + scope, Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(true)}}, s.toolMemoryDelete)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.history", Description: "Read version history, including deleted entries, with pagination." + scope, Annotations: read}, s.toolMemoryHistory)
	sdk.AddTool(ms, &sdk.Tool{Name: "workspace.memory.restore", Description: "Restore targetRevision as a new revision, including deleted entries. Supply the current expectedRevision from history." + scope, Annotations: write}, s.toolMemoryRestore)
}

// memoryScope never derives an identity from cwd, UI selection or tool input.
func (s *Server) memoryScope(ctx context.Context, requested string) (string, workspacememory.Source, error) {
	deny := func() (string, workspacememory.Source, error) {
		return "", workspacememory.Source{}, apperrors.New("memory.workspaceDenied", "Select a workspace for this session; terminal callers must supply workspaceId.", true)
	}
	caller, ok := identity.FromContext(ctx)
	if !ok {
		return deny()
	}
	source := workspacememory.Source{Caller: caller.String()}
	switch caller.Kind {
	case identity.KindAgent:
		if caller.SessionID == "" || s.deps.AgentHub == nil {
			return deny()
		}
		session, err := s.deps.AgentHub.GetStatus(ctx, caller.SessionID)
		if err != nil || session.ID != caller.SessionID || session.WorkspaceID == "" {
			return deny()
		}
		if requested != "" && requested != session.WorkspaceID {
			return deny()
		}
		source.SessionID, source.AgentType = session.ID, string(session.AgentType)
		return session.WorkspaceID, source, nil
	case identity.KindUserTerminal:
		if caller.SessionID != "" || strings.TrimSpace(requested) == "" {
			return deny()
		}
		return requested, source, nil
	default:
		return deny()
	}
}

func memoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workspacememory.ErrNotFound), errors.Is(err, workspacememory.ErrWorkspaceNotFound):
		return apperrors.New("memory.notFound", "Workspace memory was not found.", true)
	case errors.Is(err, workspacememory.ErrConflict):
		return apperrors.New("memory.revisionConflict", "Workspace memory changed. Refetch the current entry or history before editing.", true)
	case errors.Is(err, workspacememory.ErrInvalidInput):
		return apperrors.New("validation_failed", "Invalid workspace memory input. Check kind, text limits, pagination and revisions.", true)
	default:
		return apperrors.New("internal_error", "Workspace memory is not available.", true)
	}
}

func (s *Server) memoryCall(ctx context.Context, ws, action, id string, revision int64, write bool, fn func(context.Context, string, workspacememory.Source) (any, error)) (*sdk.CallToolResult, any, error) {
	selected, source, err := s.memoryScope(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.WorkspaceMemory == nil || s.deps.Caller == nil {
		return nil, nil, apperrors.New("internal_error", "Workspace memory is not available.", true)
	}
	capability := "workspace-memory:read"
	if write {
		capability = "workspace-memory:write"
	}
	details := map[string]any{"workspaceId": recordID(selected)}
	if id != "" {
		details["id"] = memoryRecordID(id)
	}
	if revision != 0 {
		details["revision"] = revision
	}
	value, err := s.deps.Caller.Call(ctx, "builtin.workspace-memory", capability, "workspace:"+recordID(selected), meta(action, details), func(callCtx context.Context) (any, error) {
		value, err := fn(callCtx, selected, source)
		return value, memoryError(err)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(value), nil
}
func (s *Server) toolMemoryList(ctx context.Context, _ *sdk.CallToolRequest, in memoryListInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_list", "", 0, false, func(c context.Context, ws string, _ workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.List(c, ws, in.Query, in.Limit, in.Offset)
	})
}
func (s *Server) toolMemorySearch(ctx context.Context, _ *sdk.CallToolRequest, in memoryListInput) (*sdk.CallToolResult, any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, nil, apperrors.New("validation_failed", "A search query is required.", true)
	}
	return s.memoryCall(ctx, in.WorkspaceID, "memory_search", "", 0, false, func(c context.Context, ws string, _ workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.List(c, ws, in.Query, in.Limit, in.Offset)
	})
}
func (s *Server) toolMemoryGet(ctx context.Context, _ *sdk.CallToolRequest, in memoryGetInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_get", in.ID, 0, false, func(c context.Context, ws string, _ workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.Get(c, ws, in.ID)
	})
}
func (s *Server) toolMemoryCreate(ctx context.Context, _ *sdk.CallToolRequest, in memoryCreateInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_create", "", 0, true, func(c context.Context, ws string, source workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.Create(c, ws, workspacememory.Input{Kind: in.Kind, Title: in.Title, Summary: in.Summary, Body: in.Body}, source)
	})
}
func (s *Server) toolMemoryUpdate(ctx context.Context, _ *sdk.CallToolRequest, in memoryUpdateInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_update", in.ID, in.ExpectedRevision, true, func(c context.Context, ws string, source workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.Update(c, ws, in.ID, in.ExpectedRevision, workspacememory.Input{Kind: in.Kind, Title: in.Title, Summary: in.Summary, Body: in.Body}, source)
	})
}
func (s *Server) toolMemoryDelete(ctx context.Context, _ *sdk.CallToolRequest, in memoryDeleteInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_delete", in.ID, in.ExpectedRevision, true, func(c context.Context, ws string, source workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.Delete(c, ws, in.ID, in.ExpectedRevision, source)
	})
}
func (s *Server) toolMemoryHistory(ctx context.Context, _ *sdk.CallToolRequest, in memoryHistoryInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_history", in.ID, 0, false, func(c context.Context, ws string, _ workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.History(c, ws, in.ID, in.Limit, in.Offset)
	})
}
func (s *Server) toolMemoryRestore(ctx context.Context, _ *sdk.CallToolRequest, in memoryRestoreInput) (*sdk.CallToolResult, any, error) {
	return s.memoryCall(ctx, in.WorkspaceID, "memory_restore", in.ID, in.ExpectedRevision, true, func(c context.Context, ws string, source workspacememory.Source) (any, error) {
		return s.deps.WorkspaceMemory.Restore(c, ws, in.ID, in.ExpectedRevision, in.TargetRevision, source)
	})
}

// Memory entries use UUIDs; only canonical opaque identifiers enter audit metadata.
func memoryRecordID(id string) string {
	if parsed, err := uuid.Parse(id); err == nil && parsed.String() == id {
		return id
	}
	return recordID(id)
}
