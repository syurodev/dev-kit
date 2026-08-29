package database

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"devkit/internal/vaultitem"
)

const savedQueryType = vaultitem.ItemTypeDatabaseQuery

// ErrQueryLogUnavailable means the service was built without a history log.
var ErrQueryLogUnavailable = errors.New("query history is not available")

// HistoryEntry is one past statement, decrypted for display.
type HistoryEntry struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profile_id"`
	SQL       string    `json:"sql"`
	CreatedAt time.Time `json:"created_at"`
}

// SavedQuery is a statement the user deliberately kept.
type SavedQuery struct {
	ID        string `json:"id"`
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
	SQL       string `json:"sql"`
}

type savedQueryMetadata struct {
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
}

// SetQueryLog wires the local history table and the vault used to seal it.
// Without it, history is simply not recorded — queries still run.
func (s *Service) SetQueryLog(repo QueryHistoryRepository, sealer Sealer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = repo
	s.sealer = sealer
}

func (s *Service) queryLog() (QueryHistoryRepository, Sealer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.history, s.sealer
}

// recordHistory is best effort by design. The statement already ran and the
// caller already holds rows; turning a successful query into an error because
// bookkeeping failed would be the wrong trade.
func (s *Service) recordHistory(profileID, sql string) {
	repo, sealer := s.queryLog()
	if repo == nil || sealer == nil {
		return
	}
	id, err := vaultitem.NewID()
	if err != nil {
		log.Printf("db history: new id: %v", err)
		return
	}
	envelope, err := sealer.SealV2(historyAD(id), []byte(sql))
	if err != nil {
		log.Printf("db history: seal: %v", err)
		return
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("db history: encode envelope: %v", err)
		return
	}
	if err := repo.Insert(HistoryRow{ID: id, ProfileID: profileID, Envelope: raw, CreatedAt: time.Now().UTC()}); err != nil {
		log.Printf("db history: insert: %v", err)
		return
	}
	if err := repo.PruneProfile(profileID, HistoryLimit); err != nil {
		log.Printf("db history: prune: %v", err)
	}
}

// ListQueryHistory returns the newest statements for a profile. A locked vault
// surfaces as an error, never as an empty list — empty reads as "you have never
// run a query here", which would be false.
func (s *Service) ListQueryHistory(profileID string) ([]HistoryEntry, error) {
	repo, sealer := s.queryLog()
	if repo == nil || sealer == nil {
		return nil, ErrQueryLogUnavailable
	}
	rows, err := repo.ListByProfile(profileID, HistoryLimit)
	if err != nil {
		return nil, err
	}
	entries := make([]HistoryEntry, 0, len(rows))
	for _, row := range rows {
		envelope, err := decodeEnvelope(row.Envelope)
		if err != nil {
			return nil, err
		}
		plaintext, err := sealer.OpenWithAD(envelope, historyAD(row.ID))
		if err != nil {
			return nil, err
		}
		entries = append(entries, HistoryEntry{
			ID: row.ID, ProfileID: row.ProfileID, SQL: string(plaintext), CreatedAt: row.CreatedAt,
		})
	}
	return entries, nil
}

// SaveQuery stores a named statement as a vault item, so it syncs. The name is
// plaintext metadata; the statement is sealed because it can carry literals.
func (s *Service) SaveQuery(profileID, name, sql string) (SavedQuery, error) {
	if profileID == "" || name == "" {
		return SavedQuery{}, errors.New("profile id and name are required")
	}
	metadata, err := json.Marshal(savedQueryMetadata{ProfileID: profileID, Name: name})
	if err != nil {
		return SavedQuery{}, err
	}
	record, err := s.store.Create(savedQueryType, HistoryKeyID, metadata, []byte(sql))
	if err != nil {
		return SavedQuery{}, err
	}
	return SavedQuery{ID: record.ID, ProfileID: profileID, Name: name, SQL: sql}, nil
}

// ListSavedQueries filters in Go rather than through an index: saved queries
// are tens per profile, not thousands.
func (s *Service) ListSavedQueries(profileID string) ([]SavedQuery, error) {
	records, err := s.store.ListByType(savedQueryType)
	if err != nil {
		return nil, err
	}
	out := make([]SavedQuery, 0)
	for _, record := range records {
		var meta savedQueryMetadata
		if json.Unmarshal(record.Metadata, &meta) != nil || meta.ProfileID != profileID {
			continue
		}
		plaintext, err := s.store.OpenPayload(record, HistoryKeyID)
		if err != nil {
			return nil, err
		}
		out = append(out, SavedQuery{ID: record.ID, ProfileID: meta.ProfileID, Name: meta.Name, SQL: string(plaintext)})
	}
	return out, nil
}

// DeleteSavedQuery is a hard delete. Queries have no trash, unlike notes.
func (s *Service) DeleteSavedQuery(id string) error {
	return s.store.Delete(id)
}
