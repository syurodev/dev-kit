package notes

import (
	"encoding/json"
	"errors"
	"regexp"
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
	ListByTypeAndMetaField(itemType, field, value string) ([]vaultitem.Record, error)
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
	// DeletedAt is RFC3339 and empty for a live note. Trash is a field on the
	// note, not a record type or a table: restore has to put the note back
	// exactly where it was, and moving it anywhere else would lose that.
	DeletedAt string `json:"deleted_at,omitempty"`
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
	// DeletedAt is empty for a live note. It is carried so an editor already
	// open on a note trashed somewhere else can say so instead of silently
	// saving into the trash.
	DeletedAt string `json:"deleted_at"`
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
	DeletedAt      string    `json:"deleted_at"`
}

type Service struct {
	store Store
	links LinkIndex

	// OnChanged fires after every successful write a client needs to learn
	// about without polling — the notes list, the sidebar rail, the folder
	// tree. The payload is the id of whatever changed (a note or a folder),
	// never a title or body. nil is a no-op, so a test that does not care
	// never has to wire one. main.go is the only caller that sets it,
	// straight to an app-event emit — never call this while holding a lock,
	// and never block on it. Without it, a note an MCP tool created or
	// updated was invisible until something else (e.g. a search) forced a
	// refetch of the stale react-query cache.
	OnChanged func(id string)
}

// changed is the one call site for OnChanged, so every write notifies the
// same way rather than each one checking nil for itself.
func (s *Service) changed(id string) {
	if s.OnChanged != nil {
		s.OnChanged(id)
	}
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
	// The body is still plaintext here — indexing after sealing would mean
	// decrypting it again to read what it references.
	if err := s.indexLinks(record.ID, body); err != nil {
		return Summary{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Summary{}, err
	}
	summary, err := s.toSummary(record, folderMap)
	if err != nil {
		return Summary{}, err
	}
	s.changed(record.ID)
	return summary, nil
}

func (s *Service) List() ([]Summary, error) {
	// Opening the dashboard is what ages the trash out; see sweepExpired.
	if err := s.sweepExpired(); err != nil {
		return nil, err
	}
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
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		if meta.DeletedAt != "" {
			continue
		}
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
	if err == nil {
		err = s.indexLinks(id, body)
	}
	if err != nil {
		return Note{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Note{}, err
	}
	note := noteFrom(record, meta, body, folderMap)
	s.changed(id)
	return note, nil
}

func (s *Service) SetOrganization(id, folderID string, tags []string, color string) (Summary, error) {
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Summary{}, err
	}
	color, err = normalizeColor(color, true)
	if err != nil {
		return Summary{}, err
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
	summary, err := s.toSummary(record, folderMap)
	if err != nil {
		return Summary{}, err
	}
	s.changed(id)
	return summary, nil
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
		// A trashed note is off every normal path, search included. It is
		// reachable only through ListTrash.
		if meta.DeletedAt != "" {
			continue
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

// previewFromBody is the card's two-line summary of a note: its text, not
// its markdown. The raw body put "# Heading", "**", "[label](https://…)" and
// code fences on every card and spent the 120-rune budget on syntax.
func previewFromBody(body string) string {
	collapsed := strings.Join(strings.Fields(markdownToPlain(body)), " ")
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
		DeletedAt:      meta.DeletedAt,
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
		DeletedAt:      meta.DeletedAt,
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

var (
	mdImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdEmphasis   = regexp.MustCompile(`(\*\*|__|~~|` + "`" + `)`)
	mdBlockStart = regexp.MustCompile(`^(#{1,6}\s+|>\s?|[-*+]\s+(\[[ xX]\]\s+)?|\d+[.)]\s+(\[[ xX]\]\s+)?)`)
	mdRule       = regexp.MustCompile(`^([-*_]\s*){3,}$`)
)

// markdownToPlain drops the markdown a reader does not see: heading, quote,
// list and task markers at the start of a line, fences and rules as whole
// lines, bold/strike/code markers, and links and images down to their text.
// Single * and _ are left alone — they are as often part of a word
// (snake_case, 2*3) as emphasis, and a preview that mangles the text is
// worse than one showing a stray asterisk.
func markdownToPlain(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") || mdRule.MatchString(t) {
			continue
		}
		for {
			stripped := mdBlockStart.ReplaceAllString(t, "")
			if stripped == t {
				break
			}
			t = stripped // "> - item" is two markers deep
		}
		t = mdImage.ReplaceAllString(t, "$1")
		t = mdLink.ReplaceAllString(t, "$1")
		t = mdEmphasis.ReplaceAllString(t, "")
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}
