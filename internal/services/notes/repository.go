package notes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotFound = errors.New("note not found")

// MemoryRepository keeps notes in memory (tests / ephemeral runs).
type MemoryRepository struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{records: map[string]Record{}}
}

func (r *MemoryRepository) List() ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record)
	}
	return out, nil
}

func (r *MemoryRepository) Get(id string) (Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (r *MemoryRepository) Put(record Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[record.ID] = record
	return nil
}

func (r *MemoryRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, id)
	return nil
}

// FileRepository persists notes as a JSON array. Bodies are already encrypted
// envelopes, so the file never contains plaintext note content.
type FileRepository struct {
	mu   sync.Mutex
	path string
}

func NewFileRepository(path string) *FileRepository {
	return &FileRepository{path: path}
}

func (r *FileRepository) load() (map[string]Record, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Record{}, nil
		}
		return nil, err
	}
	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	out := make(map[string]Record, len(records))
	for _, record := range records {
		out[record.ID] = record
	}
	return out, nil
}

func (r *FileRepository) save(records map[string]Record) error {
	list := make([]Record, 0, len(records))
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

func (r *FileRepository) List() ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		out = append(out, record)
	}
	return out, nil
}

func (r *FileRepository) Get(id string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records, err := r.load()
	if err != nil {
		return Record{}, err
	}
	record, ok := records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (r *FileRepository) Put(record Record) error {
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
