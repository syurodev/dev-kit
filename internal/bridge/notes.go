package bridge

import (
	"context"

	"devkit/internal/notes"
)

func (s *AppService) NotesList() ([]notes.Summary, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:read", "note:list", map[string]any{
		"action": "list",
	}, func(context.Context) (any, error) {
		return s.container.Notes.List()
	})
	if err != nil {
		return nil, err
	}
	return result.([]notes.Summary), nil
}

func (s *AppService) NotesSearch(query string) ([]notes.Summary, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:read", "note:search", map[string]any{
		"action": "search",
	}, func(context.Context) (any, error) {
		return s.container.Notes.Search(query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]notes.Summary), nil
}

func (s *AppService) NotesCreate(title string, body string) (notes.Summary, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:new", map[string]any{
		"action": "create",
	}, func(context.Context) (any, error) {
		return s.container.Notes.Create(title, body, "", nil)
	})
	if err != nil {
		return notes.Summary{}, err
	}
	return result.(notes.Summary), nil
}

func (s *AppService) NotesUpdate(id string, title string, body string) (notes.Note, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:"+opaqueID, map[string]any{
		"action":  "update",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.Update(id, title, body)
	})
	if err != nil {
		return notes.Note{}, err
	}
	return result.(notes.Note), nil
}

func (s *AppService) NotesGet(id string) (notes.Note, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:read", "note:"+opaqueID, map[string]any{
		"action":  "get",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.Get(id)
	})
	if err != nil {
		return notes.Note{}, err
	}
	return result.(notes.Note), nil
}

func (s *AppService) NotesDelete(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:"+opaqueID, map[string]any{
		"action":  "delete",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Notes.Delete(id)
	})
	return err
}
