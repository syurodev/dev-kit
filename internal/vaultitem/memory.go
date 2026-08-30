package vaultitem

import (
	"encoding/json"
	"sync"
)

// MemoryRepository keeps items in memory (tests / ephemeral runs).
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

// ListByTypeAndMetaField decodes each record's metadata rather than indexing
// it. The SQL repositories push this into json_extract; in memory the record
// count is a test's worth, so the simple pass is the honest one.
func (r *MemoryRepository) ListByTypeAndMetaField(itemType, field, value string) ([]Record, error) {
	records, err := r.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		var fields map[string]any
		if json.Unmarshal(record.Metadata, &fields) != nil {
			continue
		}
		if text, ok := fields[field].(string); ok && text == value {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *MemoryRepository) ListByType(itemType string) ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		if record.Type == itemType {
			out = append(out, record)
		}
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
