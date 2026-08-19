package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"devkit/internal/crypto"
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

// Save writes the keyring durably: a temp file in the same directory is fully
// written and fsynced, then atomically renamed over the target and the
// directory entry is fsynced. A crash mid-write can never leave a truncated
// keyring — the only copy of the wrapped DEKs — behind (os.WriteFile alone
// would), which would make every encrypted payload undecryptable.
func (s *FileKeyringStore) Save(keyring *crypto.Keyring) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keyring, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".keyring-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		// Best effort: the rename below removes it on success.
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, s.path); err != nil {
		return err
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()
	return dirFile.Sync()
}
