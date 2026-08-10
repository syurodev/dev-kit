package cloudconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Store persists a config snapshot on disk for offline bootstrap.
type Store interface {
	Load() (Snapshot, bool, error)
	Save(Snapshot) error
}

// FileStore reads and writes desktop-config.json under the app config dir.
type FileStore struct {
	path string
}

// NewFileStore returns a store at configDir/DevKit/desktop-config.json.
func NewFileStore(configDir string) *FileStore {
	return &FileStore{path: filepath.Join(configDir, "DevKit", "desktop-config.json")}
}

func (s *FileStore) Load() (Snapshot, bool, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode desktop config cache: %w", err)
	}
	return snapshot, true, nil
}

func (s *FileStore) Save(snapshot Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
