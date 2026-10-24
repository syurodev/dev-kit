package ops

import "devkit/internal/notes"

type NotesAPI interface {
	List() ([]notes.Summary, error)
	Search(query string) ([]notes.Summary, error)
	Get(id string) (notes.Note, error)
	Create(title, body, folderID string, tags []string) (notes.Summary, error)
	Update(id, title, body string) (notes.Note, error)
	Delete(id string) error
	SetOrganization(id, folderID string, tags []string, color string) (notes.Summary, error)
	ListFolders() ([]notes.Folder, error)
	CreateFolder(name, color string, tags []string) (notes.Folder, error)
	UpdateFolder(id, name, color string, tags []string) (notes.Folder, error)
	DeleteFolder(id string) error
}

// NotesUIAPI keeps render/history/trash dependencies out of the agent surface.
// The window supplies both interfaces from the same notes service.
type NotesUIAPI interface {
	ListBacklinks(string) ([]notes.Summary, error)
	ResolveLinks([]string) ([]notes.LinkTarget, error)
	Checkpoint(string, string) (notes.Version, error)
	ListVersions(string) ([]notes.Version, error)
	GetVersion(string) (string, string, error)
	RestoreVersion(string, string) (notes.Note, error)
	ListTrash() ([]notes.TrashedNote, error)
	Restore(string) error
	Purge(string) error
	EmptyTrash() (int, error)
}

type Deps struct {
	Notes   NotesAPI
	NotesUI NotesUIAPI
}
