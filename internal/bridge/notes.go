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

func (s *AppService) NotesCreate(title string, body string, folderID string, tags []string) (notes.Summary, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:new", map[string]any{
		"action": "create",
	}, func(context.Context) (any, error) {
		return s.container.Notes.Create(title, body, folderID, tags)
	})
	if err != nil {
		return notes.Summary{}, err
	}
	return result.(notes.Summary), nil
}

func (s *AppService) NotesSetOrganization(id, folderID string, tags []string) (notes.Summary, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:"+opaqueID, map[string]any{
		"action":  "set_organization",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.SetOrganization(id, folderID, tags)
	})
	if err != nil {
		return notes.Summary{}, err
	}
	return result.(notes.Summary), nil
}

func (s *AppService) NotesFolderList() ([]notes.Folder, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:read", "note-folder:list", map[string]any{
		"action": "list_folders",
	}, func(context.Context) (any, error) {
		return s.container.Notes.ListFolders()
	})
	if err != nil {
		return nil, err
	}
	return result.([]notes.Folder), nil
}

func (s *AppService) NotesFolderCreate(name, color string, tags []string) (notes.Folder, error) {
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note-folder:new", map[string]any{
		"action": "create_folder",
	}, func(context.Context) (any, error) {
		return s.container.Notes.CreateFolder(name, color, tags)
	})
	if err != nil {
		return notes.Folder{}, err
	}
	return result.(notes.Folder), nil
}

func (s *AppService) NotesFolderUpdate(id, name, color string, tags []string) (notes.Folder, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note-folder:"+opaqueID, map[string]any{
		"action":    "update_folder",
		"folder_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.UpdateFolder(id, name, color, tags)
	})
	if err != nil {
		return notes.Folder{}, err
	}
	return result.(notes.Folder), nil
}

func (s *AppService) NotesFolderDelete(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.notes", "notes:write", "note-folder:"+opaqueID, map[string]any{
		"action":    "delete_folder",
		"folder_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.Notes.DeleteFolder(id)
	})
	return err
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
