package notes

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

const folderKeyID = "note-folder"

// ErrInvalidColor is a color that is neither a preset nor a hex color. The wrapped
// message names the value given and the ones accepted, so a caller — an
// agent over MCP most of all — can correct itself instead of retrying blind.
var ErrInvalidColor = errors.New("invalid color")

// normalizeColor brings color to its stored form ("Blue " is blue, "#3B82F6"
// is #3b82f6, "#abc" is #aabbcc) and checks it: a preset name or a hex
// color. optional lets "" through as "no color of its own" (a note
// inheriting its folder's); a folder must have one.
func normalizeColor(color string, optional bool) (string, error) {
	color = vaultitem.NormalizeNoteFolderColor(color)
	if color == "" && optional {
		return "", nil
	}
	if !vaultitem.ValidNoteFolderColor(color) {
		shown := color
		if shown == "" {
			shown = "(empty)"
		}
		return "", fmt.Errorf("%w %q: use a hex color (#rrggbb or #rgb) or one of the presets %s",
			ErrInvalidColor, shown, strings.Join(vaultitem.NoteFolderColors, ", "))
	}
	return color, nil
}

type folderMetadata struct {
	Name  string   `json:"name"`
	Color string   `json:"color"`
	Tags  []string `json:"tags,omitempty"`
}

// Folder is a note-folder without any encrypted payload.
type Folder struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Service) CreateFolder(name, color string, tags []string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, errors.New("name is required")
	}
	color, err := normalizeColor(color, false)
	if err != nil {
		return Folder{}, err
	}
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Folder{}, err
	}
	meta, err := json.Marshal(folderMetadata{Name: name, Color: color, Tags: normalized})
	if err != nil {
		return Folder{}, err
	}
	record, err := s.store.Create(vaultitem.ItemTypeNoteFolder, folderKeyID, meta, []byte("{}"))
	if err != nil {
		return Folder{}, err
	}
	folder, err := folderFrom(record)
	if err != nil {
		return Folder{}, err
	}
	s.changed(record.ID)
	return folder, nil
}

func (s *Service) ListFolders() ([]Folder, error) {
	records, err := s.store.ListByType(vaultitem.ItemTypeNoteFolder)
	if err != nil {
		return nil, err
	}
	folders := make([]Folder, 0, len(records))
	for _, record := range records {
		folder, err := folderFrom(record)
		if err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].UpdatedAt.After(folders[j].UpdatedAt)
	})
	return folders, nil
}

func (s *Service) UpdateFolder(id, name, color string, tags []string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Folder{}, errors.New("name is required")
	}
	color, err := normalizeColor(color, false)
	if err != nil {
		return Folder{}, err
	}
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Folder{}, err
	}
	meta, err := json.Marshal(folderMetadata{Name: name, Color: color, Tags: normalized})
	if err != nil {
		return Folder{}, err
	}
	existing, err := s.store.Get(id)
	if err != nil {
		return Folder{}, err
	}
	if existing.Type != vaultitem.ItemTypeNoteFolder {
		return Folder{}, errors.New("folder not found")
	}
	record, err := s.store.UpdateMetadata(id, meta)
	if err != nil {
		return Folder{}, err
	}
	folder, err := folderFrom(record)
	if err != nil {
		return Folder{}, err
	}
	s.changed(id)
	return folder, nil
}

func decodeFolderMetadata(record vaultitem.Record) (folderMetadata, error) {
	var meta folderMetadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return folderMetadata{}, err
	}
	return meta, nil
}

func folderFrom(record vaultitem.Record) (Folder, error) {
	meta, err := decodeFolderMetadata(record)
	if err != nil {
		return Folder{}, err
	}
	return Folder{
		ID:        record.ID,
		Name:      meta.Name,
		Color:     meta.Color,
		Tags:      nonNilTags(meta.Tags),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Service) DeleteFolder(id string) error {
	record, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if record.Type != vaultitem.ItemTypeNoteFolder {
		return errors.New("folder not found")
	}
	folder, err := folderFrom(record)
	if err != nil {
		return err
	}
	notes, err := s.List()
	if err != nil {
		return err
	}
	type noteUpdate struct {
		id    string
		tags  []string
		color string
	}
	updates := make([]noteUpdate, 0, len(notes))
	for _, note := range notes {
		if note.FolderID != id {
			continue
		}
		color := note.Color
		if color == "" {
			color = folder.Color
		}
		union := UnionTags(note.Tags, folder.Tags)
		normalized, err := NormalizeTags(union)
		if err != nil {
			return err
		}
		updates = append(updates, noteUpdate{
			id:    note.ID,
			tags:  normalized,
			color: color,
		})
	}
	for _, u := range updates {
		if _, err := s.SetOrganization(u.id, "", u.tags, u.color); err != nil {
			return err
		}
	}
	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.changed(id)
	return nil
}
