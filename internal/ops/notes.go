package ops

import (
	"context"

	"devkit/internal/notes"
)

type NotesQueryIn struct {
	Query string `json:"query"`
}

type NotesIDIn struct {
	ID string `json:"id"`
}

type NotesCreateIn struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	FolderID string   `json:"folder_id"`
	Tags     []string `json:"tags"`
}

type NotesUpdateIn struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type NotesSetOrganizationIn struct {
	ID       string   `json:"id"`
	FolderID string   `json:"folder_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Color    string   `json:"color,omitempty" jsonschema:"optional; a hex color (#rrggbb or #rgb) or a preset: red, orange, yellow, green, teal, blue, violet, pink, gray; empty inherits the folder color"`
}

type NotesFolderCreateIn struct {
	Name  string   `json:"name"`
	Color string   `json:"color" jsonschema:"a hex color (#rrggbb or #rgb) or a preset: red, orange, yellow, green, teal, blue, violet, pink, gray"`
	Tags  []string `json:"tags"`
}

type NotesFolderUpdateIn struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Color string   `json:"color" jsonschema:"a hex color (#rrggbb or #rgb) or a preset: red, orange, yellow, green, teal, blue, violet, pink, gray"`
	Tags  []string `json:"tags"`
}

type NotesEmptyIn struct{}
type NotesIDsIn struct {
	IDs []string `json:"ids"`
}
type NotesVersionIn struct {
	VersionID string `json:"version_id"`
}
type NotesRestoreVersionIn struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
}

var NotesList = Op[NotesEmptyIn, []notes.Summary]{
	Name: "notes.list", Bridge: "NotesList", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:list",
	Action: "list", Risk: Read, Exposure: Both,
	Scope: func(_ context.Context, in NotesEmptyIn) (Scope, error) {
		return FixedScope("note:list"), nil
	},
	Agent:  AgentSurface{Description: "List encrypted notes (metadata only)"},
	Handle: func(_ context.Context, d Deps, in NotesEmptyIn) ([]notes.Summary, error) { return d.Notes.List() },
}

var NotesSearch = Op[NotesQueryIn, []notes.Summary]{
	Name: "notes.search", Bridge: "NotesSearch", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:search",
	Action: "search", Risk: Read, Exposure: Both,
	Scope: func(_ context.Context, in NotesQueryIn) (Scope, error) {
		return FixedScope("note:search"), nil
	},
	Agent: AgentSurface{Description: "Search notes by title (and body when unlocked)"},
	Handle: func(_ context.Context, d Deps, in NotesQueryIn) ([]notes.Summary, error) {
		return d.Notes.Search(in.Query)
	},
}

var NotesCreate = Op[NotesCreateIn, notes.Summary]{
	Name: "notes.create", Bridge: "NotesCreate", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:new",
	Action: "create", Risk: Write, Exposure: Both,
	Scope: func(_ context.Context, in NotesCreateIn) (Scope, error) {
		return FixedScope("note:new"), nil
	},
	Agent: AgentSurface{Description: "Create a new encrypted note"},
	Handle: func(_ context.Context, d Deps, in NotesCreateIn) (notes.Summary, error) {
		return d.Notes.Create(in.Title, in.Body, in.FolderID, in.Tags)
	},
}

var NotesSetOrganization = Op[NotesSetOrganizationIn, notes.Summary]{
	Name: "notes.set_organization", Bridge: "NotesSetOrganization", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "set_organization", Risk: Write, Exposure: Both,
	Scope: func(_ context.Context, in NotesSetOrganizationIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Agent: AgentSurface{Description: "Set a note's folder and tags"},
	Handle: func(_ context.Context, d Deps, in NotesSetOrganizationIn) (notes.Summary, error) {
		return d.Notes.SetOrganization(in.ID, in.FolderID, in.Tags, in.Color)
	},
}

var NotesFolderList = Op[NotesEmptyIn, []notes.Folder]{
	Name: "notes.folder.list", Bridge: "NotesFolderList", Subject: "builtin.notes", Capability: "notes:read", Grant: "note-folder:list",
	Action: "list_folders", Risk: Read, Exposure: Both,
	Scope: func(_ context.Context, in NotesEmptyIn) (Scope, error) {
		return FixedScope("note-folder:list"), nil
	},
	Agent:  AgentSurface{Description: "List note folders"},
	Handle: func(_ context.Context, d Deps, in NotesEmptyIn) ([]notes.Folder, error) { return d.Notes.ListFolders() },
}

var NotesFolderCreate = Op[NotesFolderCreateIn, notes.Folder]{
	Name: "notes.folder.create", Bridge: "NotesFolderCreate", Subject: "builtin.notes", Capability: "notes:write", Grant: "note-folder:new",
	Action: "create_folder", Risk: Write, Exposure: Both,
	Scope: func(_ context.Context, in NotesFolderCreateIn) (Scope, error) {
		return FixedScope("note-folder:new"), nil
	},
	Agent: AgentSurface{Description: "Create a note folder"},
	Handle: func(_ context.Context, d Deps, in NotesFolderCreateIn) (notes.Folder, error) {
		return d.Notes.CreateFolder(in.Name, in.Color, in.Tags)
	},
}

var NotesFolderUpdate = Op[NotesFolderUpdateIn, notes.Folder]{
	Name: "notes.folder.update", Bridge: "NotesFolderUpdate", Subject: "builtin.notes", Capability: "notes:write", Grant: "note-folder:*",
	Action: "update_folder", Risk: Write, Exposure: Both,
	Scope: func(_ context.Context, in NotesFolderUpdateIn) (Scope, error) {
		return RecordScope("note-folder", "folder_id", in.ID), nil
	},
	Agent: AgentSurface{Description: "Update a note folder"},
	Handle: func(_ context.Context, d Deps, in NotesFolderUpdateIn) (notes.Folder, error) {
		return d.Notes.UpdateFolder(in.ID, in.Name, in.Color, in.Tags)
	},
}

var NotesFolderDelete = Op[NotesIDIn, Unit]{
	Name: "notes.folder.delete", Bridge: "NotesFolderDelete", Subject: "builtin.notes", Capability: "notes:write", Grant: "note-folder:*",
	Action: "delete_folder", Risk: Destructive, Exposure: Both, Safe: "deletes folder metadata; notes remain available",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note-folder", "folder_id", in.ID), nil
	},
	Agent: AgentSurface{Description: "Delete a note folder"},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (Unit, error) {
		return Unit{}, d.Notes.DeleteFolder(in.ID)
	},
}

var NotesUpdate = Op[NotesUpdateIn, notes.Note]{
	Name: "notes.update", Bridge: "NotesUpdate", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "update", Risk: Write, Exposure: Both,
	Scope: func(_ context.Context, in NotesUpdateIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Agent: AgentSurface{Description: "Update an existing note's title and body"},
	Handle: func(_ context.Context, d Deps, in NotesUpdateIn) (notes.Note, error) {
		return d.Notes.Update(in.ID, in.Title, in.Body)
	},
}

var NotesGet = Op[NotesIDIn, notes.Note]{
	Name: "notes.get", Bridge: "NotesGet", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Action: "get", Risk: Read, Exposure: Both,
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Agent:  AgentSurface{Description: "Get a note by id (decrypts body; requires unlocked vault)"},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (notes.Note, error) { return d.Notes.Get(in.ID) },
}

var NotesListBacklinks = Op[NotesIDIn, []notes.Summary]{
	Name: "notes.listBacklinks", Bridge: "NotesListBacklinks", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Action: "list_backlinks", Risk: Read, Exposure: UI, Reason: "render helpers, not requested for agents",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) ([]notes.Summary, error) {
		return d.NotesUI.ListBacklinks(in.ID)
	},
}

var NotesResolveLinks = Op[NotesIDsIn, []notes.LinkTarget]{
	Name: "notes.resolveLinks", Bridge: "NotesResolveLinks", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Action: "resolve_links", Risk: Read, Exposure: UI, Reason: "render helpers, not requested for agents",
	Scope: func(_ context.Context, in NotesIDsIn) (Scope, error) {
		scope := FixedScope("note:links")
		scope.Meta = map[string]any{"count": len(in.IDs)}
		return scope, nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDsIn) ([]notes.LinkTarget, error) {
		return d.NotesUI.ResolveLinks(in.IDs)
	},
}

var NotesCheckpoint = Op[NotesIDIn, notes.Version]{
	Name: "notes.checkpoint", Bridge: "NotesCheckpoint", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "checkpoint", Risk: Write, Exposure: UI, Reason: "restoring overwrites the note; an agent should not roll one back",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (notes.Version, error) {
		return d.NotesUI.Checkpoint(in.ID, notes.TriggerCheckpoint)
	},
}

var NotesListVersions = Op[NotesIDIn, []notes.Version]{
	Name: "notes.listVersions", Bridge: "NotesListVersions", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Action: "list_versions", Risk: Read, Exposure: UI, Reason: "restoring overwrites the note; an agent should not roll one back",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) ([]notes.Version, error) {
		return d.NotesUI.ListVersions(in.ID)
	},
}

var NotesGetVersion = Op[NotesVersionIn, map[string]string]{
	Name: "notes.getVersion", Bridge: "NotesGetVersion", Subject: "builtin.notes", Capability: "notes:read", Grant: "note-version:*",
	Action: "get_version", Risk: Read, Exposure: UI, Reason: "restoring overwrites the note; an agent should not roll one back",
	Scope: func(_ context.Context, in NotesVersionIn) (Scope, error) {
		return RecordScope("note-version", "version_id", in.VersionID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesVersionIn) (map[string]string, error) {
		title, body, err := d.NotesUI.GetVersion(in.VersionID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"title": title, "body": body}, nil
	},
}

var NotesRestoreVersion = Op[NotesRestoreVersionIn, notes.Note]{
	Name: "notes.restoreVersion", Bridge: "NotesRestoreVersion", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "restore_version", Risk: Write, Exposure: UI, Reason: "restoring overwrites the note; an agent should not roll one back",
	Scope: func(_ context.Context, in NotesRestoreVersionIn) (Scope, error) {
		scope := RecordScope("note", "note_id", in.ID)
		scope.Meta["version_id"] = RecordID(in.VersionID)
		return scope, nil
	},
	Handle: func(_ context.Context, d Deps, in NotesRestoreVersionIn) (notes.Note, error) {
		return d.NotesUI.RestoreVersion(in.ID, in.VersionID)
	},
}

var NotesListTrash = Op[NotesEmptyIn, []notes.TrashedNote]{
	Name: "notes.listTrash", Bridge: "NotesListTrash", Subject: "builtin.notes", Capability: "notes:read", Grant: "note:*",
	Action: "list_trash", Risk: Read, Exposure: UI, Reason: "irreversible, reachable only from the Trash screen",
	Scope: func(_ context.Context, in NotesEmptyIn) (Scope, error) {
		return FixedScope("note:trash"), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesEmptyIn) ([]notes.TrashedNote, error) {
		return d.NotesUI.ListTrash()
	},
}

var NotesRestore = Op[NotesIDIn, Unit]{
	Name: "notes.restore", Bridge: "NotesRestore", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "restore", Risk: Write, Exposure: UI, Reason: "irreversible, reachable only from the Trash screen",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (Unit, error) { return Unit{}, d.NotesUI.Restore(in.ID) },
}

var NotesPurge = Op[NotesIDIn, Unit]{
	Name: "notes.purge", Bridge: "NotesPurge", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "purge", Risk: Destructive, Exposure: UI, Reason: "irreversible, reachable only from the Trash screen", Safe: "explicit action from the UI Trash screen",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (Unit, error) { return Unit{}, d.NotesUI.Purge(in.ID) },
}

var NotesEmptyTrash = Op[NotesEmptyIn, int]{
	Name: "notes.emptyTrash", Bridge: "NotesEmptyTrash", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "empty_trash", Risk: Destructive, Exposure: UI, Reason: "irreversible, reachable only from the Trash screen", Safe: "explicit action from the UI Trash screen",
	Scope: func(_ context.Context, in NotesEmptyIn) (Scope, error) {
		return FixedScope("note:trash"), nil
	},
	Handle: func(_ context.Context, d Deps, in NotesEmptyIn) (int, error) { return d.NotesUI.EmptyTrash() },
}

var NotesDelete = Op[NotesIDIn, Unit]{
	Name: "notes.delete", Bridge: "NotesDelete", Subject: "builtin.notes", Capability: "notes:write", Grant: "note:*",
	Action: "delete", Risk: Destructive, Exposure: Both, Safe: "reversible for 30 days from the trash",
	Scope: func(_ context.Context, in NotesIDIn) (Scope, error) {
		return RecordScope("note", "note_id", in.ID), nil
	},
	Agent:  AgentSurface{Description: "Delete a note by id"},
	Handle: func(_ context.Context, d Deps, in NotesIDIn) (Unit, error) { return Unit{}, d.Notes.Delete(in.ID) },
}

func Notes() []Operation {
	return []Operation{NotesList, NotesSearch, NotesCreate, NotesSetOrganization, NotesFolderList, NotesFolderCreate, NotesFolderUpdate, NotesFolderDelete, NotesUpdate, NotesGet, NotesListBacklinks, NotesResolveLinks, NotesCheckpoint, NotesListVersions, NotesGetVersion, NotesRestoreVersion, NotesListTrash, NotesRestore, NotesPurge, NotesEmptyTrash, NotesDelete}
}
