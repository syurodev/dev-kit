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
		Name:        "notes.delete",
		Description: "Delete a note by id",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolNotesDelete)
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
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) toolNotesCreate(ctx context.Context, _ *sdk.CallToolRequest, in notesCreateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:new", meta("create", nil), func(context.Context) (any, error) {
		return s.deps.Notes.Create(in.Title, in.Body)
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
