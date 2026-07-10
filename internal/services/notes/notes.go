package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"changeme/internal/crypto"
)

const bodyKeyID = "note-body"

// Vault is the encryption capability the notes service needs. The DEK stays
// inside the vault; notes only get seal/open.
type Vault interface {
	Seal(keyID string, plaintext []byte) (crypto.Envelope, error)
	Open(envelope crypto.Envelope) ([]byte, error)
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
	envelope, err := s.vault.Seal(bodyKeyID, []byte(body))
	if err != nil {
		return Summary{}, err
	}
	id, err := newID()
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
	plain, err := s.vault.Open(record.Body)
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
