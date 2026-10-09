package bridge

import (
	"context"
	"os"

	"devkit/internal/app"
	"devkit/internal/codegraph"
	"devkit/internal/workspace"
	"devkit/internal/wssearch"
)

// Code Graph over a workspace's folders (docs/design/codegraph-workspace.md).
//
// Every method here names its owner — (workspaceId, folderId) — and none reads
// the "focused" Code Graph project, so concurrent calls for different folders
// cannot race and a folder that cannot be analysed does not stop another's
// files from being read. The path of a folder never crosses the bridge in this
// direction: Go resolves it from the workspace's membership.
//
// Audit metadata names the folder and the relative path and never the content
// or the search query.

func (s *AppService) workspaceCode() (*app.WorkspaceCode, error) {
	if s.container.WorkspaceCode == nil {
		return nil, codegraph.ErrNoActiveProject
	}
	return s.container.WorkspaceCode, nil
}

// WorkspaceFolderStates reports, for every folder of the workspace, whether it
// is there and what git says: branch and number of changed files.
func (s *AppService) WorkspaceFolderStates(ctx context.Context, workspaceID string) ([]app.FolderState, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"workspace_id": recordID(workspaceID),
		"action":       "folder_states",
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.FolderStates(ctx, workspaceID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]app.FolderState), nil
}

// WorkspaceFileList lists a folder's files and directories, path-sorted.
func (s *AppService) WorkspaceFileList(ctx context.Context, workspaceID, folderID string) (app.FileList, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"workspace_id": recordID(workspaceID),
		"folder_id":    recordID(folderID),
		"action":       "list_files",
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.ListFiles(ctx, workspaceID, folderID)
	})
	if err != nil {
		return app.FileList{}, err
	}
	return result.(app.FileList), nil
}

// WorkspaceFileRead reads a text file of a folder with the revision of the
// bytes read — the base a later save is checked against.
func (s *AppService) WorkspaceFileRead(workspaceID, folderID, relativePath string) (codegraph.FileContent, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "read_file",
		"relative_path": relativePath,
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.ReadFile(workspaceID, folderID, relativePath)
	})
	if err != nil {
		return codegraph.FileContent{}, err
	}
	return result.(codegraph.FileContent), nil
}

// WorkspaceFileStat says what is on disk at a path — exists, revision, why it
// cannot be opened as text — without its content. The editor reconciles an open
// buffer with this after a change notification, a focus return or a save.
func (s *AppService) WorkspaceFileStat(workspaceID, folderID, relativePath string) (codegraph.FileStatus, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "stat_file",
		"relative_path": relativePath,
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.StatFile(workspaceID, folderID, relativePath)
	})
	if err != nil {
		return codegraph.FileStatus{}, err
	}
	return result.(codegraph.FileStatus), nil
}

// WorkspaceFileWrite saves a buffer into its owner folder. expectedRevision is
// the revision the buffer was based on ("absent" for a new file, "*" to
// overwrite what is there on purpose). A file that has another revision on disk
// is refused as file_conflict with nothing written. The response carries the
// revision of the bytes written.
func (s *AppService) WorkspaceFileWrite(workspaceID, folderID, relativePath, content, expectedRevision string) (codegraph.FileContent, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "write_file",
		"relative_path": relativePath,
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.WriteFile(workspaceID, folderID, relativePath, content, expectedRevision)
	})
	if err != nil {
		return codegraph.FileContent{}, err
	}
	return result.(codegraph.FileContent), nil
}

// WorkspaceGitStatus maps a folder's changed files to M/A/D/R.
func (s *AppService) WorkspaceGitStatus(ctx context.Context, workspaceID, folderID string) (map[string]string, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"workspace_id": recordID(workspaceID),
		"folder_id":    recordID(folderID),
		"action":       "git_status",
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.GitStatus(ctx, workspaceID, folderID)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]string), nil
}

// WorkspaceEntryPoints lists a folder's entry points from its index — the open
// Service's when it has one, else the index on disk — with the state that
// explains an empty list. It never opens a project.
func (s *AppService) WorkspaceEntryPoints(ctx context.Context, workspaceID, folderID string) (codegraph.EntryPointsView, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"workspace_id": recordID(workspaceID),
		"folder_id":    recordID(folderID),
		"action":       "workspace_entry_points",
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.EntryPoints(ctx, workspaceID, folderID)
	})
	if err != nil {
		return codegraph.EntryPointsView{}, err
	}
	return result.(codegraph.EntryPointsView), nil
}

// WorkspaceFileOutline is the declarations of one file as the index last saw
// them: the saved file as of the index time, not an unsaved buffer.
func (s *AppService) WorkspaceFileOutline(ctx context.Context, workspaceID, folderID, relativePath string) (codegraph.FileOutline, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"workspace_id":  recordID(workspaceID),
		"folder_id":     recordID(folderID),
		"action":        "file_outline",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.Outline(ctx, workspaceID, folderID, relativePath)
	})
	if err != nil {
		return codegraph.FileOutline{}, err
	}
	return result.(codegraph.FileOutline), nil
}

// WorkspaceSearch finds text in the files of the workspace's folders (all of
// them, or the folders named). ctx is the call's own: cancelling the frontend's
// promise cancels the search, and WorkspaceSearchCancel does the same by
// request ID for a caller that no longer holds the promise. A newer search with
// the same request ID supersedes the older one.
func (s *AppService) WorkspaceSearch(ctx context.Context, req app.SearchRequest) (wssearch.Result, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		// The query and patterns are what the person is looking for; they are
		// not recorded.
		"workspace_id": recordID(req.WorkspaceID),
		"folders":      len(req.FolderIDs),
		"action":       "search",
		"regex":        req.Regex,
	}, func(ctx context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.Search(ctx, req)
	})
	if err != nil {
		return wssearch.Result{}, err
	}
	return result.(wssearch.Result), nil
}

// WorkspaceSearchCancel stops the search with this request ID.
func (s *AppService) WorkspaceSearchCancel(requestID string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action": "search_cancel",
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		code.CancelSearch(requestID)
		return nil, nil
	})
	return err
}

// WorkspaceSetPrimary makes a folder the workspace's primary: where an agent
// session is spawned by default. Only the workspace's root changes; folders
// keep their IDs and nothing is re-indexed.
func (s *AppService) WorkspaceSetPrimary(workspaceID, folderID string) (workspace.Workspace, error) {
	opaque := recordID(workspaceID)
	result, err := s.call(context.Background(), "builtin.workspace", "ws:write", "workspace:"+opaque, map[string]any{
		"action":       "set_primary",
		"workspace_id": opaque,
		"folder_id":    recordID(folderID),
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.SetPrimary(workspaceID, folderID)
	})
	if err != nil {
		return workspace.Workspace{}, err
	}
	return result.(workspace.Workspace), nil
}

// WorkspaceReindex queues folders of the workspace (all when folderIDs is
// empty) to be analysed again; cancel withdraws them from the queue instead.
// Returns at once: progress arrives as codegraph:indexStatus events.
func (s *AppService) WorkspaceReindex(workspaceID string, folderIDs []string, cancel bool) error {
	opaque := recordID(workspaceID)
	_, err := s.call(context.Background(), "builtin.workspace", "ws:write", "service:reindex", map[string]any{
		"action":       "reindex_folders",
		"workspace_id": opaque,
		"folders":      len(folderIDs),
		"cancel":       cancel,
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return nil, code.Reindex(workspaceID, folderIDs, cancel)
	})
	return err
}

// WorkspaceWatchDocuments sets the documents the editor has open, so a change to
// one on disk arrives as a workspace:fileChanged event. It replaces the previous
// set. The returned keys are documents that could not be watched; the editor
// looks at those when the window regains focus.
func (s *AppService) WorkspaceWatchDocuments(docs []app.WatchedDocument) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action":    "watch_documents",
		"documents": len(docs),
	}, func(context.Context) (any, error) {
		code, err := s.workspaceCode()
		if err != nil {
			return nil, err
		}
		return code.WatchDocuments(docs), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

// WorkspaceSaveDraftCopy writes text the editor could not save — a buffer whose
// folder left the workspace — to a file the person chooses in the native save
// dialog. "" means the dialog was cancelled. Desktop only: there is no dialog in
// server mode, where the call fails.
func (s *AppService) WorkspaceSaveDraftCopy(suggestedName, content string) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:draft-copy", map[string]any{
		"action": "save_draft_copy",
	}, func(context.Context) (any, error) {
		dialog := s.saveFileDialog
		if dialog == nil {
			dialog = defaultSaveFileDialog
		}
		path, err := dialog(suggestedName)
		if err != nil || path == "" {
			return "", err
		}
		// 0o600: it is source the person was editing, in a place they chose,
		// not a file for other accounts on the machine.
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", err
		}
		return path, nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
