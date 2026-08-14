package notes

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

const folderKeyID = "note-folder"

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
	if !vaultitem.ValidNoteFolderColor(color) {
		return Folder{}, errors.New("invalid folder color")
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
	return folderFrom(record)
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
	if !vaultitem.ValidNoteFolderColor(color) {
		return Folder{}, errors.New("invalid folder color")
	}
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Folder{}, err
	}
	meta, err := json.Marshal(folderMetadata{Name: name, Color: color, Tags: normalized})
	if err != nil {
		return Folder{}, err
	}
	record, err := s.store.UpdateMetadata(id, meta)
	if err != nil {
		return Folder{}, err
	}
	return folderFrom(record)
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
		Tags:      meta.Tags,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}
