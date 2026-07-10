package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"changeme/internal/crypto"
)

// MemoryKeyringStore keeps the keyring in memory. Useful for tests and
// ephemeral runs; it does not survive a restart.
type MemoryKeyringStore struct {
	mu      sync.RWMutex
	keyring *crypto.Keyring
}

func NewMemoryKeyringStore() *MemoryKeyringStore {
	return &MemoryKeyringStore{}
}

func (s *MemoryKeyringStore) Exists() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyring != nil, nil
}

func (s *MemoryKeyringStore) Load() (*crypto.Keyring, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.keyring == nil {
		return nil, errors.New("keyring not found")
	}
	return s.keyring, nil
}

func (s *MemoryKeyringStore) Save(keyring *crypto.Keyring) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyring = keyring
	return nil
}

// FileKeyringStore persists the keyring as JSON. The file only holds wrapped
// key material and KDF params, never the plaintext DEK or the Master Password.
type FileKeyringStore struct {
	path string
}

func NewFileKeyringStore(path string) *FileKeyringStore {
	return &FileKeyringStore{path: path}
}

func (s *FileKeyringStore) Exists() (bool, error) {
	_, err := os.Stat(s.path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *FileKeyringStore) Load() (*crypto.Keyring, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var keyring crypto.Keyring
	if err := json.Unmarshal(data, &keyring); err != nil {
		return nil, err
	}
	return &keyring, nil
}

func (s *FileKeyringStore) Save(keyring *crypto.Keyring) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keyring, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
