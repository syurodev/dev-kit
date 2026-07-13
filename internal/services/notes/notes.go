package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"changeme/internal/crypto"
)

const (
	bodyKeyID      = "note-body"
	noteRecordType = "note"
)

// Vault is the encryption capability the notes service needs. The DEK stays
// inside the vault; notes only get seal/open.
type Vault interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}

// Record is the at-rest form of a note. The body is encrypted; the title is
// treated as private, local-only metadata kept in plaintext so notes can be
// listed without decrypting every record.
// ponytail: title is plaintext metadata for now; encrypt it too if titles ever sync.
type Record struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Body      crypto.Envelope `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Repository interface {
	List() ([]Record, error)
	Get(id string) (Record, error)
	Put(record Record) error
	Delete(id string) error
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
	repo  Repository
	vault Vault
}

func NewService(repo Repository, vault Vault) *Service {
	return &Service{repo: repo, vault: vault}
}

// Create encrypts the body with the vault DEK and persists the note. Fails if
// the vault is locked.
func (s *Service) Create(title string, body string) (Summary, error) {
	if title == "" {
		return Summary{}, errors.New("title is required")
	}
	id, err := newID()
	if err != nil {
		return Summary{}, err
	}
	envelope, err := s.vault.SealV2(crypto.AssociatedData{RecordType: noteRecordType, RecordID: id, KeyID: bodyKeyID}, []byte(body))
	if err != nil {
		return Summary{}, err
	}
	now := time.Now().UTC()
	record := Record{ID: id, Title: title, Body: envelope, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Put(record); err != nil {
		return Summary{}, err
	}
	return toSummary(record), nil
}

// List returns note summaries (no body), newest first.
func (s *Service) List() ([]Summary, error) {
	records, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, toSummary(record))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

// Get returns the decrypted note. Fails if the vault is locked.
func (s *Service) Get(id string) (Note, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return Note{}, err
	}
	plain, err := s.vault.OpenWithAD(record.Body, noteAssociatedData(record))
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID:        record.ID,
		Title:     record.Title,
		Body:      string(plain),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// Search matches notes by query (case-insensitive). Titles are plaintext
// metadata and are always searched. Bodies are encrypted, so they are only
// searched when the vault is unlocked (decrypt succeeds); when locked, Search
// degrades to metadata-only, per architecture §8.3. An empty query returns all
// notes (newest first).
func (s *Service) Search(query string) ([]Summary, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	records, err := s.repo.List()
	if err != nil {
		return nil, err
	}

	matched := make([]Summary, 0, len(records))
	for _, record := range records {
		if needle == "" || strings.Contains(strings.ToLower(record.Title), needle) {
			matched = append(matched, toSummary(record))
			continue
		}
		// Body search requires an unlocked vault; skip silently when locked.
		if plain, openErr := s.vault.OpenWithAD(record.Body, noteAssociatedData(record)); openErr == nil {
			if strings.Contains(strings.ToLower(string(plain)), needle) {
				matched = append(matched, toSummary(record))
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
	})
	return matched, nil
}

func noteAssociatedData(record Record) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: noteRecordType, RecordID: record.ID, KeyID: bodyKeyID}
}

func toSummary(record Record) Summary {
	return Summary{
		ID:        record.ID,
		Title:     record.Title,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func newID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
