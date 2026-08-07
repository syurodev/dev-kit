package environments

import (
	"fmt"
	"sync"
)

// MemoryRepository keeps tags in memory (tests / ephemeral runs). It seeds
// the three builtin tags at construction, mirroring what storage migration
// m12 seeds for the SQL-backed path (the two seeds are independent — an
// ephemeral container never runs storage.Migrate).
type MemoryRepository struct {
	mu   sync.RWMutex
	tags map[string]Tag
}

func NewMemoryRepository() *MemoryRepository {
	repo := &MemoryRepository{tags: map[string]Tag{}}
	for _, tag := range []Tag{
		{ID: fmt.Sprintf("%024x", 1), Name: "dev", IsBuiltin: true},
		{ID: fmt.Sprintf("%024x", 2), Name: "stg", IsBuiltin: true},
		{ID: fmt.Sprintf("%024x", 3), Name: "prod", RequiresConfirmation: true, IsBuiltin: true, Locked: true},
	} {
		repo.tags[tag.ID] = tag
	}
	return repo
}

func (r *MemoryRepository) List() ([]Tag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tag, 0, len(r.tags))
	for _, tag := range r.tags {
		out = append(out, tag)
	}
	return out, nil
}

func (r *MemoryRepository) Get(name string) (Tag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, tag := range r.tags {
		if tag.Name == name {
			return tag, nil
		}
	}
	return Tag{}, ErrTagNotFound
}

func (r *MemoryRepository) GetByID(id string) (Tag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tag, ok := r.tags[id]
	if !ok {
		return Tag{}, ErrTagNotFound
	}
	return tag, nil
}

func (r *MemoryRepository) Create(tag Tag) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.tags {
		if existing.Name == tag.Name {
			return ErrTagExists
		}
	}
	r.tags[tag.ID] = tag
	return nil
}

func (r *MemoryRepository) Update(tag Tag) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tags[tag.ID]; !ok {
		return ErrTagNotFound
	}
	r.tags[tag.ID] = tag
	return nil
}

func (r *MemoryRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tags, id)
	return nil
}
