package notes

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

const (
	itemType  = "note"
	bodyKeyID = "note-body"
)

// Store is the shared vault-item substrate the notes service needs.
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Delete(id string) error
}

// metadata is the plaintext, local-only note metadata. Title stays plaintext so
// notes list without decrypting; encrypt it too if titles ever sync.
type metadata struct {
	Title    string   `json:"title"`
	FolderID string   `json:"folder_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Color    string   `json:"color,omitempty"`
}

// Summary is a note without its (encrypted) body, safe to list while unlocked.
type Summary struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Preview        string    `json:"preview"`
	FolderID       string    `json:"folder_id"`
	Tags           []string  `json:"tags"`
	EffectiveTags  []string  `json:"effective_tags"`
	Color          string    `json:"color"`
	EffectiveColor string    `json:"effective_color"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Note is the decrypted form returned to the caller.
type Note struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	FolderID       string    `json:"folder_id"`
	Tags           []string  `json:"tags"`
	EffectiveTags  []string  `json:"effective_tags"`
	Color          string    `json:"color"`
	EffectiveColor string    `json:"effective_color"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(title string, body string, folderID string, tags []string) (Summary, error) {
	if title == "" {
		return Summary{}, errors.New("title is required")
	}
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Summary{}, err
	}
	if folderID != "" {
		record, err := s.store.Get(folderID)
		if err != nil || record.Type != vaultitem.ItemTypeNoteFolder {
			return Summary{}, errors.New("folder not found")
		}
	}
	meta, err := json.Marshal(metadata{Title: title, FolderID: folderID, Tags: normalized})
	if err != nil {
		return Summary{}, err
	}
	record, err := s.store.Create(itemType, bodyKeyID, meta, []byte(body))
	if err != nil {
		return Summary{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Summary{}, err
	}
	return s.toSummary(record, folderMap)
}

func (s *Service) List() ([]Summary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(records))
	for _, record := range records {
		summary, err := s.toSummary(record, folderMap)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *Service) Get(id string) (Note, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return Note{}, err
	}
	if record.Type != itemType {
		return Note{}, errors.New("note not found")
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return Note{}, err
	}
	plain, err := s.store.OpenPayload(record, bodyKeyID)
	if err != nil {
		return Note{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Note{}, err
	}
	return noteFrom(record, meta, string(plain), folderMap), nil
}

func (s *Service) Update(id, title, body string) (Note, error) {
	if title == "" {
		return Note{}, errors.New("title is required")
	}
	existing, err := s.store.Get(id)
	if err != nil {
		return Note{}, err
	}
	if existing.Type != itemType {
		return Note{}, errors.New("note not found")
	}
	meta, err := decodeMetadata(existing)
	if err != nil {
		return Note{}, err
	}
	meta.Title = title
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return Note{}, err
	}
	record, err := s.store.Update(id, bodyKeyID, metaBytes, []byte(body))
	if err != nil {
		return Note{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Note{}, err
	}
	return noteFrom(record, meta, body, folderMap), nil
}

func (s *Service) SetOrganization(id, folderID string, tags []string, color string) (Summary, error) {
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Summary{}, err
	}
	if color != "" && !vaultitem.ValidNoteFolderColor(color) {
		return Summary{}, errors.New("invalid note color")
	}
	if folderID != "" {
		record, err := s.store.Get(folderID)
		if err != nil || record.Type != vaultitem.ItemTypeNoteFolder {
			return Summary{}, errors.New("folder not found")
		}
	}
	record, err := s.store.Get(id)
	if err != nil {
		return Summary{}, err
	}
	if record.Type != itemType {
		return Summary{}, errors.New("note not found")
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return Summary{}, err
	}
	meta.FolderID = folderID
	meta.Tags = normalized
	meta.Color = color
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return Summary{}, err
	}
	record, err = s.store.UpdateMetadata(id, metaBytes)
	if err != nil {
		return Summary{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Summary{}, err
	}
	return s.toSummary(record, folderMap)
}

func (s *Service) Delete(id string) error {
	record, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if record.Type != itemType {
		return errors.New("note not found")
	}
	return s.store.Delete(id)
}

// Search matches notes by query (case-insensitive). Titles, effective tags, and
// folder names are always searched from plaintext metadata. Bodies are encrypted,
// so they are only searched when the vault is unlocked (decrypt succeeds). An
// empty query returns all notes (newest first).
func (s *Service) Search(query string) ([]Summary, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return nil, err
	}

	matched := make([]Summary, 0, len(records))
	for _, record := range records {
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		if needle == "" || strings.Contains(strings.ToLower(meta.Title), needle) {
			matched = append(matched, s.summaryFrom(record, meta, folderMap))
			continue
		}
		tags := effectiveTags(meta, folderMap)
		tagMatched := false
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), needle) {
				matched = append(matched, s.summaryFrom(record, meta, folderMap))
				tagMatched = true
				break
			}
		}
		if tagMatched {
			continue
		}
		if meta.FolderID != "" {
			if folder, ok := folderMap[meta.FolderID]; ok {
				if strings.Contains(strings.ToLower(folder.Name), needle) {
					matched = append(matched, s.summaryFrom(record, meta, folderMap))
					continue
				}
			}
		}
		if plain, openErr := s.store.OpenPayload(record, bodyKeyID); openErr == nil {
			if strings.Contains(strings.ToLower(string(plain)), needle) {
				matched = append(matched, s.summaryFrom(record, meta, folderMap))
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
	})
	return matched, nil
}

func (s *Service) loadFolderMap() (map[string]Folder, error) {
	folders, err := s.ListFolders()
	if err != nil {
		return nil, err
	}
	m := make(map[string]Folder, len(folders))
	for _, folder := range folders {
		m[folder.ID] = folder
	}
	return m, nil
}

func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func effectiveTags(meta metadata, folderMap map[string]Folder) []string {
	own := nonNilTags(meta.Tags)
	if meta.FolderID == "" {
		return own
	}
	folder, ok := folderMap[meta.FolderID]
	if !ok {
		return own
	}
	return UnionTags(own, nonNilTags(folder.Tags))
}

func effectiveColor(meta metadata, folderMap map[string]Folder) string {
	if meta.Color != "" {
		return meta.Color
	}
	if meta.FolderID == "" {
		return ""
	}
	folder, ok := folderMap[meta.FolderID]
	if !ok {
		return ""
	}
	return folder.Color
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func previewFromBody(body string) string {
	collapsed := strings.Join(strings.Fields(body), " ")
	runes := []rune(collapsed)
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return collapsed
}

func (s *Service) summaryFrom(record vaultitem.Record, meta metadata, folderMap map[string]Folder) Summary {
	summary := Summary{
		ID:             record.ID,
		Title:          meta.Title,
		FolderID:       meta.FolderID,
		Tags:           nonNilTags(meta.Tags),
		EffectiveTags:  effectiveTags(meta, folderMap),
		Color:          meta.Color,
		EffectiveColor: effectiveColor(meta, folderMap),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
	if plain, err := s.store.OpenPayload(record, bodyKeyID); err == nil {
		summary.Preview = previewFromBody(string(plain))
	}
	return summary
}

func noteFrom(record vaultitem.Record, meta metadata, body string, folderMap map[string]Folder) Note {
	return Note{
		ID:             record.ID,
		Title:          meta.Title,
		Body:           body,
		FolderID:       meta.FolderID,
		Tags:           nonNilTags(meta.Tags),
		EffectiveTags:  effectiveTags(meta, folderMap),
		Color:          meta.Color,
		EffectiveColor: effectiveColor(meta, folderMap),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}

func (s *Service) toSummary(record vaultitem.Record, folderMap map[string]Folder) (Summary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return Summary{}, err
	}
	// FR-6 (mcp-agent): notes.list decrypts every body for the preview, so a
	// locked vault must surface a structured vault_locked error — never a
	// silently preview-less list that would read as empty notes. Search keeps
	// the tolerant form (title/tag matching never needs the body).
	if _, err := s.store.OpenPayload(record, bodyKeyID); err != nil {
		return Summary{}, err
	}
	return s.summaryFrom(record, meta, folderMap), nil
}
