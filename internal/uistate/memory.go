package uistate

import "sync"

type MemoryRepository struct {
	mu     sync.RWMutex
	values map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: make(map[string]string)}
}

func (r *MemoryRepository) Get(key string) (string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, found := r.values[key]
	return value, found, nil
}

func (r *MemoryRepository) Set(key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}
