package bridge

import (
	"context"

	"devkit/internal/notes"
	"devkit/internal/ops"
)

func (s *AppService) NotesList(ctx context.Context) ([]notes.Summary, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesList, ops.NotesEmptyIn{})
}

func (s *AppService) NotesSearch(ctx context.Context, query string) ([]notes.Summary, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesSearch, ops.NotesQueryIn{Query: query})
}

func (s *AppService) NotesCreate(ctx context.Context, title string, body string, folderID string, tags []string) (notes.Summary, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesCreate, ops.NotesCreateIn{Title: title, Body: body, FolderID: folderID, Tags: tags})
}

func (s *AppService) NotesSetOrganization(ctx context.Context, id, folderID string, tags []string, color string) (notes.Summary, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesSetOrganization, ops.NotesSetOrganizationIn{ID: id, FolderID: folderID, Tags: tags, Color: color})
}

func (s *AppService) NotesFolderList(ctx context.Context) ([]notes.Folder, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesFolderList, ops.NotesEmptyIn{})
}

func (s *AppService) NotesFolderCreate(ctx context.Context, name, color string, tags []string) (notes.Folder, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesFolderCreate, ops.NotesFolderCreateIn{Name: name, Color: color, Tags: tags})
}

func (s *AppService) NotesFolderUpdate(ctx context.Context, id, name, color string, tags []string) (notes.Folder, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesFolderUpdate, ops.NotesFolderUpdateIn{ID: id, Name: name, Color: color, Tags: tags})
}

func (s *AppService) NotesFolderDelete(ctx context.Context, id string) error {
	_, err := ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesFolderDelete, ops.NotesIDIn{ID: id})
	return err
}

func (s *AppService) NotesUpdate(ctx context.Context, id string, title string, body string) (notes.Note, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesUpdate, ops.NotesUpdateIn{ID: id, Title: title, Body: body})
}

func (s *AppService) NotesGet(ctx context.Context, id string) (notes.Note, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesGet, ops.NotesIDIn{ID: id})
}

func (s *AppService) NotesListBacklinks(ctx context.Context, id string) ([]notes.Summary, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesListBacklinks, ops.NotesIDIn{ID: id})
}

// NotesResolveLinks turns the ids in a body into the labels to draw. Titles are
// resolved at render time, so renaming a note updates every link to it without
// rewriting a single body.
func (s *AppService) NotesResolveLinks(ctx context.Context, ids []string) ([]notes.LinkTarget, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesResolveLinks, ops.NotesIDsIn{IDs: ids})
}

func (s *AppService) NotesCheckpoint(ctx context.Context, id string) (notes.Version, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesCheckpoint, ops.NotesIDIn{ID: id})
}

func (s *AppService) NotesListVersions(ctx context.Context, id string) ([]notes.Version, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesListVersions, ops.NotesIDIn{ID: id})
}

// NotesGetVersion returns the snapshot body so the diff view can compare it
// with what is on screen.
func (s *AppService) NotesGetVersion(ctx context.Context, versionID string) (map[string]string, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesGetVersion, ops.NotesVersionIn{VersionID: versionID})
}

func (s *AppService) NotesRestoreVersion(ctx context.Context, id, versionID string) (notes.Note, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesRestoreVersion, ops.NotesRestoreVersionIn{ID: id, VersionID: versionID})
}

func (s *AppService) NotesListTrash(ctx context.Context) ([]notes.TrashedNote, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesListTrash, ops.NotesEmptyIn{})
}

func (s *AppService) NotesRestore(ctx context.Context, id string) error {
	_, err := ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesRestore, ops.NotesIDIn{ID: id})
	return err
}

func (s *AppService) NotesPurge(ctx context.Context, id string) error {
	_, err := ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesPurge, ops.NotesIDIn{ID: id})
	return err
}

func (s *AppService) NotesEmptyTrash(ctx context.Context) (int, error) {
	return ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesEmptyTrash, ops.NotesEmptyIn{})
}

func (s *AppService) NotesDelete(ctx context.Context, id string) error {
	_, err := ops.Invoke(callerContext(ctx), s.notesRuntime(), ops.NotesDelete, ops.NotesIDIn{ID: id})
	return err
}

func (s *AppService) notesRuntime() ops.Runtime {
	return ops.Runtime{Deps: ops.Deps{Notes: s.container.Notes, NotesUI: s.container.Notes}, Caller: gatewayCaller{app: s}, ServerMode: s.inServerMode()}
}
