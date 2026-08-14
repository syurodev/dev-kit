package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerNotes() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.list",
		Description: "List encrypted notes (metadata only)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolNotesList)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.search",
		Description: "Search notes by title (and body when unlocked)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolNotesSearch)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.get",
		Description: "Get a note by id (decrypts body; requires unlocked vault)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolNotesGet)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.create",
		Description: "Create a new encrypted note",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesCreate)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.update",
		Description: "Update an existing note's title and body",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesUpdate)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.delete",
		Description: "Delete a note by id",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolNotesDelete)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.set_organization",
		Description: "Set a note's folder and tags",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesSetOrganization)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.folder.list",
		Description: "List note folders",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolNotesFolderList)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.folder.create",
		Description: "Create a note folder",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesFolderCreate)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.folder.update",
		Description: "Update a note folder",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesFolderUpdate)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.folder.delete",
		Description: "Delete a note folder",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolNotesFolderDelete)
}

func (s *Server) toolNotesList(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:read", "note:list", meta("list", nil), func(context.Context) (any, error) {
		return s.deps.Notes.List()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesQueryInput struct {
	Query string `json:"query"`
}

func (s *Server) toolNotesSearch(ctx context.Context, _ *sdk.CallToolRequest, in notesQueryInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:read", "note:search", meta("search", nil), func(context.Context) (any, error) {
		return s.deps.Notes.Search(in.Query)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesIDInput struct {
	ID string `json:"id"`
}

func (s *Server) toolNotesGet(ctx context.Context, _ *sdk.CallToolRequest, in notesIDInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:read", "note:"+opaqueID, meta("get", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.Get(in.ID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesCreateInput struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	FolderID string   `json:"folder_id"`
	Tags     []string `json:"tags"`
}

func (s *Server) toolNotesCreate(ctx context.Context, _ *sdk.CallToolRequest, in notesCreateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:new", meta("create", nil), func(context.Context) (any, error) {
		return s.deps.Notes.Create(in.Title, in.Body, in.FolderID, in.Tags)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesUpdateInput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) toolNotesUpdate(ctx context.Context, _ *sdk.CallToolRequest, in notesUpdateInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:"+opaqueID, meta("update", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.Update(in.ID, in.Title, in.Body)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolNotesDelete(ctx context.Context, _ *sdk.CallToolRequest, in notesIDInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:"+opaqueID, meta("delete", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return nil, s.deps.Notes.Delete(in.ID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesSetOrganizationInput struct {
	ID       string   `json:"id"`
	FolderID string   `json:"folder_id"`
	Tags     []string `json:"tags"`
	Color    string   `json:"color"`
}

func (s *Server) toolNotesSetOrganization(ctx context.Context, _ *sdk.CallToolRequest, in notesSetOrganizationInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:"+opaqueID, meta("set_organization", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.SetOrganization(in.ID, in.FolderID, in.Tags, in.Color)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolNotesFolderList(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:read", "note-folder:list", meta("list_folders", nil), func(context.Context) (any, error) {
		return s.deps.Notes.ListFolders()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesFolderCreateInput struct {
	Name  string   `json:"name"`
	Color string   `json:"color"`
	Tags  []string `json:"tags"`
}

func (s *Server) toolNotesFolderCreate(ctx context.Context, _ *sdk.CallToolRequest, in notesFolderCreateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note-folder:new", meta("create_folder", nil), func(context.Context) (any, error) {
		return s.deps.Notes.CreateFolder(in.Name, in.Color, in.Tags)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type notesFolderUpdateInput struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Color string   `json:"color"`
	Tags  []string `json:"tags"`
}

func (s *Server) toolNotesFolderUpdate(ctx context.Context, _ *sdk.CallToolRequest, in notesFolderUpdateInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note-folder:"+opaqueID, meta("update_folder", map[string]any{
		"folder_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.UpdateFolder(in.ID, in.Name, in.Color, in.Tags)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolNotesFolderDelete(ctx context.Context, _ *sdk.CallToolRequest, in notesIDInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note-folder:"+opaqueID, meta("delete_folder", map[string]any{
		"folder_id": opaqueID,
	}), func(context.Context) (any, error) {
		return nil, s.deps.Notes.DeleteFolder(in.ID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

// recordID mirrors bridge.recordID: opaque vault-item ids are 24 hex chars.
// Invalid values become "invalid" so the capability scope never carries raw input.
func recordID(value string) string {
	if len(value) != 24 {
		return "invalid"
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return "invalid"
		}
	}
	return value
}
