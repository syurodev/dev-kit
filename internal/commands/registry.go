package commands

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Handler func(ctx context.Context, input any) (any, error)

type Command struct {
	ID          string
	Title       string
	Permissions []string
	Handler     Handler
}

type Registry struct {
	mu       sync.RWMutex
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: map[string]Command{}}
}

func (r *Registry) Register(command Command) error {
	if command.ID == "" {
		return errors.New("command id is required")
	}
	if command.Title == "" {
		return errors.New("command title is required")
	}
	if command.Handler == nil {
		return errors.New("command handler is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.commands[command.ID]; exists {
		return fmt.Errorf("command %q is already registered", command.ID)
	}
	r.commands[command.ID] = command
	return nil
}

func (r *Registry) Execute(ctx context.Context, id string, input any) (any, error) {
	r.mu.RLock()
	command, exists := r.commands[id]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("command %q is not registered", id)
	}
	return command.Handler(ctx, input)
}

func (r *Registry) List() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Command, 0, len(r.commands))
	for _, command := range r.commands {
		result = append(result, command)
	}
	return result
}
