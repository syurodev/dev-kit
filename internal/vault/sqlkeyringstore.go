package vault

import (
	"context"
	"encoding/json"
	"errors"

	"changeme/internal/crypto"
	"changeme/internal/storage"
)

const keyringRowID = "keyring"

// SQLKeyringStore persists the single vault keyring row in SQLite. The stored
// blob only contains wrapped key material and KDF params (never the plaintext
// DEK or Master Password).
type SQLKeyringStore struct {
	kv *storage.KV
}

func NewSQLKeyringStore(kv *storage.KV) *SQLKeyringStore {
	return &SQLKeyringStore{kv: kv}
}

func (s *SQLKeyringStore) Exists() (bool, error) {
	_, ok, err := s.kv.Get(context.Background(), keyringRowID)
	return ok, err
}

func (s *SQLKeyringStore) Load() (*crypto.Keyring, error) {
	data, ok, err := s.kv.Get(context.Background(), keyringRowID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("keyring not found")
	}
	var keyring crypto.Keyring
	if err := json.Unmarshal(data, &keyring); err != nil {
		return nil, err
	}
	return &keyring, nil
}

func (s *SQLKeyringStore) Save(keyring *crypto.Keyring) error {
	data, err := json.Marshal(keyring)
	if err != nil {
		return err
	}
	return s.kv.Put(context.Background(), keyringRowID, data)
}
