package bridge

import "changeme/internal/services/notes"

func (s *AppService) NotesList() ([]notes.Summary, error) {
	return s.container.Notes.List()
}

func (s *AppService) NotesSearch(query string) ([]notes.Summary, error) {
	return s.container.Notes.Search(query)
}

func (s *AppService) NotesCreate(title string, body string) (notes.Summary, error) {
	return s.container.Notes.Create(title, body)
}

func (s *AppService) NotesGet(id string) (notes.Note, error) {
	return s.container.Notes.Get(id)
}

func (s *AppService) NotesDelete(id string) error {
	return s.container.Notes.Delete(id)
}
