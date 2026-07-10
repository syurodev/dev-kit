package ssh

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotFound = errors.New("ssh profile not found")

// MemoryRepository keeps SSH profiles in memory (tests / ephemeral runs).
type MemoryRepository struct {
	mu       sync.RWMutex
	profiles map[string]ProfileRecord
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{profiles: map[string]ProfileRecord{}}
}

func (r *MemoryRepository) List() ([]ProfileRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProfileRecord, 0, len(r.profiles))
	for _, record := range r.profiles {
		out = append(out, record)
	}
	return out, nil
}

func (r *MemoryRepository) Get(id string) (ProfileRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.profiles[id]
	if !ok {
		return ProfileRecord{}, ErrNotFound
	}
	return record, nil
}

func (r *MemoryRepository) Put(record ProfileRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[record.ID] = record
	return nil
}

func (r *MemoryRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.profiles, id)
	return nil
}

// FileRepository persists SSH profiles as a JSON array. Secrets are encrypted
// envelopes, so the file never contains plaintext passwords or private keys.
type FileRepository struct {
	mu   sync.Mutex
	path string
}

func NewFileRepository(path string) *FileRepository {
	return &FileRepository{path: path}
}

func (r *FileRepository) load() (map[string]ProfileRecord, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ProfileRecord{}, nil
		}
		return nil, err
	}
	var records []ProfileRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	out := make(map[string]ProfileRecord, len(records))
	for _, record := range records {
		out[record.ID] = record
	}
	return out, nil
}

func (r *FileRepository) save(records map[string]ProfileRecord) error {
	list := make([]ProfileRecord, 0, len(records))
	for _, record := range records {
		list = append(list, record)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0o600)
}

func (r *FileRepository) List() ([]ProfileRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]ProfileRecord, 0, len(records))
	for _, record := range records {
		out = append(out, record)
	}
	return out, nil
}

func (r *FileRepository) Get(id string) (ProfileRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return ProfileRecord{}, err
	}
	record, ok := records[id]
	if !ok {
		return ProfileRecord{}, ErrNotFound
	}
	return record, nil
}

func (r *FileRepository) Put(record ProfileRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return err
	}
	records[record.ID] = record
	return r.save(records)
}

func (r *FileRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return err
	}
	delete(records, id)
	return r.save(records)
}
