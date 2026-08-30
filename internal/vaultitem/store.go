package vaultitem

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"devkit/internal/crypto"
)

// Store owns id generation, seal/open with per-type AAD binding, and plaintext
// metadata search. Services provide their type name and key id; the Store never
// interprets a type's metadata beyond string search.
type Store struct {
	repo  Repository
	vault Vault
}

func NewStore(repo Repository, vault Vault) *Store {
	return &Store{repo: repo, vault: vault}
}

// NewID returns a 24-char lowercase hex id (12 random bytes). The length is
// load-bearing: the bridge recordID() opaque-id check requires exactly 24 hex.
func NewID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func associatedData(itemType, id, keyID string) crypto.AssociatedData {
	return crypto.AssociatedData{RecordType: itemType, RecordID: id, KeyID: keyID}
}

// Reseal returns a copy of record whose payload is re-encrypted from plaintext,
// bound to the record's own identity and the given inner keyID. It is used by
// conflict resolution's edit-merge path to produce a merged note body; the
// caller re-materialises the returned record as a new local mutation.
func (s *Store) Reseal(record Record, innerKeyID string, plaintext []byte) (Record, error) {
	envelope, err := s.vault.SealV2(associatedData(record.Type, record.ID, innerKeyID), plaintext)
	if err != nil {
		return Record{}, err
	}
	record.Payload = &envelope
	record.UpdatedAt = time.Now().UTC()
	return record, nil
}

// Create seals secret (when non-nil) under AAD {itemType, newID, keyID} and
// stores the record. When secret is nil, the item has no payload. Sealing
// goes through the vault DEK, so this fails if the vault is locked.
func (s *Store) Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (Record, error) {
	id, err := NewID()
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	record := Record{
		ID:         id,
		Type:       itemType,
		Metadata:   metadata,
		CreatedAt:  now,
		UpdatedAt:  now,
		KeyVersion: currentKeyVersion,
		SyncState:  syncStateLocal,
	}
	if secret != nil {
		envelope, err := s.vault.SealV2(associatedData(itemType, id, keyID), secret)
		if err != nil {
			return Record{}, err
		}
		record.Payload = &envelope
	}
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Get returns the stored record (payload still encrypted).
func (s *Store) Get(id string) (Record, error) {
	return s.repo.Get(id)
}

// ListByType returns all records of a type (payloads still encrypted).
func (s *Store) ListByType(itemType string) ([]Record, error) {
	return s.repo.ListByType(itemType)
}

// ListByTypeAndMetaField narrows by one plaintext metadata field.
func (s *Store) ListByTypeAndMetaField(itemType, field, value string) ([]Record, error) {
	return s.repo.ListByTypeAndMetaField(itemType, field, value)
}

// OpenPayload decrypts a record's payload. Returns an error when the item has
// no payload or when the vault is locked / AAD does not match.
func (s *Store) OpenPayload(record Record, keyID string) ([]byte, error) {
	if record.Payload == nil {
		return nil, errors.New("item has no payload")
	}
	return s.vault.OpenWithAD(*record.Payload, associatedData(record.Type, record.ID, keyID))
}

// UpdateMetadata rewrites plaintext metadata, bumping UpdatedAt. Payload and
// timestamps other than UpdatedAt are preserved.
func (s *Store) UpdateMetadata(id string, metadata json.RawMessage) (Record, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return Record{}, err
	}
	record.Metadata = metadata
	record.UpdatedAt = time.Now().UTC()
	// A local edit must become dirty even when the record originally came from
	// sync. The production mutation repository captures the corresponding sync
	// intent in the same transaction, preventing remote apply from silently
	// overwriting newer local work.
	record.SyncState = syncStateLocal
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Update rewrites both metadata and payload in one write, bumping
// UpdatedAt and marking the record dirty for sync. The general-purpose
// sibling of UpdateMetadata (metadata-only) and Reseal (payload-only,
// caller persists) — this is the method that lets a caller replace both
// plaintext metadata and the encrypted payload atomically in one Put.
func (s *Store) Update(id, keyID string, metadata json.RawMessage, secret []byte) (Record, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return Record{}, err
	}
	// A nil secret keeps the record payload-less, matching Create's semantics;
	// an empty-but-non-nil secret still seals an empty-plaintext envelope.
	if secret != nil {
		envelope, err := s.vault.SealV2(associatedData(record.Type, id, keyID), secret)
		if err != nil {
			return Record{}, err
		}
		record.Payload = &envelope
	}
	record.Metadata = metadata
	record.UpdatedAt = time.Now().UTC()
	record.SyncState = syncStateLocal
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Delete removes an item by id.
func (s *Store) Delete(id string) error {
	return s.repo.Delete(id)
}

// Search matches query (case-insensitive) against plaintext metadata across the
// given types (all types when none are given). It reads plaintext metadata only
// and never decrypts payloads, so it works while the vault is locked. Matching
// is a substring test over the raw metadata JSON; that can match a JSON key
// name, which is acceptable for the local MVP scale.
func (s *Store) Search(query string, types ...string) ([]Record, error) {
	needle := strings.ToLower(strings.TrimSpace(query))

	var candidates []Record
	if len(types) == 0 {
		all, err := s.repo.List()
		if err != nil {
			return nil, err
		}
		candidates = all
	} else {
		for _, t := range types {
			recs, err := s.repo.ListByType(t)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, recs...)
		}
	}

	if needle == "" {
		return candidates, nil
	}
	matched := make([]Record, 0, len(candidates))
	for _, record := range candidates {
		if strings.Contains(strings.ToLower(string(record.Metadata)), needle) {
			matched = append(matched, record)
		}
	}
	return matched, nil
}
