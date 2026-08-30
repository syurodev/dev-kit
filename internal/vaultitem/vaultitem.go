// Package vaultitem is the shared storage + crypto substrate for built-in
// item types (notes, database profiles, SSH profiles). A Record is the at-rest
// shape common to every type: plaintext Metadata for listing/search, plus an
// optional encrypted Payload envelope for the type's secret.
package vaultitem

import (
	"encoding/json"
	"errors"
	"time"

	"devkit/internal/crypto"
)

// ErrNotFound is returned by repositories when an id has no item.
var ErrNotFound = errors.New("vault item not found")

const (
	currentKeyVersion = 1
	syncStateLocal    = "local"
)

// Record is one stored item of any type.
type Record struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Metadata   json.RawMessage  `json:"metadata"`
	Payload    *crypto.Envelope `json:"payload,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	KeyVersion int              `json:"key_version"`
	SyncState  string           `json:"sync_state"`
}

// Repository persists Records. Payloads are already-encrypted envelopes, so a
// persisted row never contains plaintext secret content.
type Repository interface {
	List() ([]Record, error)
	ListByType(itemType string) ([]Record, error)
	// ListByTypeAndMetaField narrows by one plaintext metadata field. Note
	// version history needs the versions of one note, and scanning every
	// version in the vault to filter in Go does not scale with the number of
	// notes. Generic on purpose — any single-field metadata lookup can use it.
	ListByTypeAndMetaField(itemType, field, value string) ([]Record, error)
	Get(id string) (Record, error)
	Put(record Record) error
	Delete(id string) error
}

// Vault is the encryption capability the store needs. The DEK stays inside the
// vault; the store only gets seal/open.
type Vault interface {
	SealV2(associatedData crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
	OpenWithAD(envelope crypto.Envelope, associatedData crypto.AssociatedData) ([]byte, error)
}
