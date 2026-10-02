package bridge

import (
	"context"
	"errors"

	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/workspacememory"
	"github.com/google/uuid"
)

func memoryAuditID(id string) string {
	if len(id) == 36 {
		if _, err := uuid.Parse(id); err == nil {
			return id
		}
	}
	return recordID(id)
}

// Workspace memory management is a UI boundary. MCP derives workspace and source
// independently from its stamped session identity; neither accepts authorship input.
func (s *AppService) workspaceMemoryCall(ctx context.Context, ws, action, id string, write bool, fn func(context.Context, *workspacememory.Service) (any, error)) (any, error) {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return nil, apperrors.New(codePermissionDenied, "Workspace memory management requires the DevKit UI.", true)
	}
	capability := "workspace-memory:read"
	if write {
		capability = "workspace-memory:write"
	}
	metadata := map[string]any{"action": action, "workspace_id": recordID(ws)}
	if id != "" {
		metadata["entry_id"] = memoryAuditID(id)
	}
	return s.call(ctx, "builtin.workspace-memory", capability, "workspace:"+recordID(ws), metadata, func(c context.Context) (any, error) {
		if s.container.WorkspaceMemory == nil {
			return nil, apperrors.New(codeInternalError, "Workspace memory is unavailable.", true)
		}
		return fn(c, s.container.WorkspaceMemory)
	})
}

func (s *AppService) WorkspaceMemoryBrowse(ctx context.Context, workspaceID string, in workspacememory.BrowseInput) (workspacememory.BrowsePage, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_browse", "", false, func(c context.Context, m *workspacememory.Service) (any, error) { return m.Browse(c, workspaceID, in) })
	if e != nil {
		return workspacememory.BrowsePage{}, e
	}
	return v.(workspacememory.BrowsePage), nil
}
func (s *AppService) WorkspaceMemoryGet(ctx context.Context, workspaceID, id string) (workspacememory.Entry, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_get", id, false, func(c context.Context, m *workspacememory.Service) (any, error) { return m.Get(c, workspaceID, id) })
	if e != nil {
		return workspacememory.Entry{}, e
	}
	return v.(workspacememory.Entry), nil
}
func (s *AppService) WorkspaceMemoryCreate(ctx context.Context, workspaceID string, in workspacememory.Input) (workspacememory.Entry, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_create", "", true, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.Create(c, workspaceID, in, workspacememory.Source{Caller: identity.UI().String()})
	})
	if e != nil {
		return workspacememory.Entry{}, e
	}
	return v.(workspacememory.Entry), nil
}
func (s *AppService) WorkspaceMemoryUpdate(ctx context.Context, workspaceID, id string, expectedRevision int64, in workspacememory.Input) (workspacememory.Entry, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_update", id, true, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.Update(c, workspaceID, id, expectedRevision, in, workspacememory.Source{Caller: identity.UI().String()})
	})
	if e != nil {
		return workspacememory.Entry{}, e
	}
	return v.(workspacememory.Entry), nil
}
func (s *AppService) WorkspaceMemoryDelete(ctx context.Context, workspaceID, id string, expectedRevision int64) (workspacememory.Entry, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_delete", id, true, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.Delete(c, workspaceID, id, expectedRevision, workspacememory.Source{Caller: identity.UI().String()})
	})
	if e != nil {
		return workspacememory.Entry{}, e
	}
	return v.(workspacememory.Entry), nil
}
func (s *AppService) WorkspaceMemoryRestore(ctx context.Context, workspaceID, id string, expectedRevision, targetRevision int64) (workspacememory.Entry, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_restore", id, true, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.Restore(c, workspaceID, id, expectedRevision, targetRevision, workspacememory.Source{Caller: identity.UI().String()})
	})
	if e != nil {
		return workspacememory.Entry{}, e
	}
	return v.(workspacememory.Entry), nil
}
func (s *AppService) WorkspaceMemoryHistoryPage(ctx context.Context, workspaceID, id, cursor string, limit int) (workspacememory.HistoryPage, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_history", id, false, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.HistoryPage(c, workspaceID, id, cursor, limit)
	})
	if e != nil {
		return workspacememory.HistoryPage{}, e
	}
	return v.(workspacememory.HistoryPage), nil
}
func (s *AppService) WorkspaceMemoryReadRevision(ctx context.Context, workspaceID, id string, revision int64) (workspacememory.Revision, error) {
	v, e := s.workspaceMemoryCall(ctx, workspaceID, "memory_revision", id, false, func(c context.Context, m *workspacememory.Service) (any, error) {
		return m.ReadRevision(c, workspaceID, id, revision)
	})
	if e != nil {
		return workspacememory.Revision{}, e
	}
	return v.(workspacememory.Revision), nil
}

// Keep domain errors stable for the manager's conflict-preserving editor.
func workspaceMemoryPublicError(err error) *apperrors.AppError {
	switch {
	case errors.Is(err, workspacememory.ErrConflict):
		return apperrors.New("memory.revisionConflict", "This memory changed. Reload the latest version before saving.", true)
	case errors.Is(err, workspacememory.ErrNotFound), errors.Is(err, workspacememory.ErrWorkspaceNotFound):
		return apperrors.New("memory.notFound", "Workspace memory was not found.", true)
	case errors.Is(err, workspacememory.ErrInvalidInput):
		return apperrors.New(codeValidationFailed, "Invalid memory input. Check text limits, category, cursor and revisions.", true)
	}
	return nil
}
