package modules

import (
	"errors"
	"fmt"
	"sync"
)

type Module struct {
	ID       string
	Title    string
	Commands []string
	Views    []string
}

type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{modules: map[string]Module{}}
}

func (r *Registry) Register(module Module) error {
	if module.ID == "" {
		return errors.New("module id is required")
	}
	if module.Title == "" {
		return errors.New("module title is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.modules[module.ID]; exists {
		return fmt.Errorf("module %q is already registered", module.ID)
	}
	r.modules[module.ID] = module
	return nil
}

func (r *Registry) List() []Module {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Module, 0, len(r.modules))
	for _, module := range r.modules {
		result = append(result, module)
	}
	return result
}
