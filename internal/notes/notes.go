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
	Delete(id string) error
}

// metadata is the plaintext, local-only note metadata. Title stays plaintext so
// notes list without decrypting; encrypt it too if titles ever sync.
type metadata struct {
	Title string `json:"title"`
}

// Summary is a note without its (encrypted) body, safe to list while unlocked.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Note is the decrypted form returned to the caller.
type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(title string, body string) (Summary, error) {
	if title == "" {
		return Summary{}, errors.New("title is required")
	}
	meta, err := json.Marshal(metadata{Title: title})
	if err != nil {
		return Summary{}, err
	}
	record, err := s.store.Create(itemType, bodyKeyID, meta, []byte(body))
	if err != nil {
		return Summary{}, err
	}
	return toSummary(record)
}

func (s *Service) List() ([]Summary, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(records))
	for _, record := range records {
		summary, err := toSummary(record)
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
	meta, err := decodeMetadata(record)
	if err != nil {
		return Note{}, err
	}
	plain, err := s.store.OpenPayload(record, bodyKeyID)
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID:        record.ID,
		Title:     meta.Title,
		Body:      string(plain),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Service) Delete(id string) error {
	return s.store.Delete(id)
}

// Search matches notes by query (case-insensitive). Titles are plaintext and are
// always searched. Bodies are encrypted, so they are only searched when the
// vault is unlocked (decrypt succeeds); when locked, Search degrades to
// title-only. An empty query returns all notes (newest first).
func (s *Service) Search(query string) ([]Summary, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	records, err := s.store.ListByType(itemType)
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
			matched = append(matched, summaryFrom(record, meta))
			continue
		}
		if plain, openErr := s.store.OpenPayload(record, bodyKeyID); openErr == nil {
			if strings.Contains(strings.ToLower(string(plain)), needle) {
				matched = append(matched, summaryFrom(record, meta))
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
	})
	return matched, nil
}

func decodeMetadata(record vaultitem.Record) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func summaryFrom(record vaultitem.Record, meta metadata) Summary {
	return Summary{
		ID:        record.ID,
		Title:     meta.Title,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func toSummary(record vaultitem.Record) (Summary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return Summary{}, err
	}
	return summaryFrom(record, meta), nil
}
