// Package environments provides the env tag registry (dev/stg/prod + custom)
// that internal/ssh and internal/database reference for MCP confirmation
// policy. Tags are not secrets; the registry lives outside the vaultitem
// substrate.
package environments

import (
	"errors"

	"changeme/internal/vaultitem"
)

var (
	// ErrTagNotFound is returned by Repository.Get/GetByID when no matching
	// row exists.
	ErrTagNotFound = errors.New("environment tag not found")
	// ErrTagExists is returned when a Create call's name collides with an
	// existing tag (builtin or custom).
	ErrTagExists = errors.New("environment tag name already exists")
	// ErrTagLocked is returned when a locked (prod) or builtin tag is
	// updated/deleted through a path that must never succeed.
	ErrTagLocked = errors.New("environment tag is locked")
)

// Tag is one entry in the env tag registry.
type Tag struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	IsBuiltin            bool   `json:"is_builtin"`
	Locked               bool   `json:"locked"`
}

// Repository persists Tag records.
type Repository interface {
	List() ([]Tag, error)
	Get(name string) (Tag, error)
	GetByID(id string) (Tag, error)
	Create(tag Tag) error
	// Update persists only RequiresConfirmation for the tag at ID — Name,
	// IsBuiltin, and Locked are immutable after creation. Callers (only
	// Service.UpdateTag today) must not rely on Update changing any other
	// field.
	Update(tag Tag) error
	Delete(id string) error
}

// Service applies tag policy (locked/builtin rules) on top of a Repository.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// ListTags returns every tag, builtin and custom.
func (s *Service) ListTags() ([]Tag, error) {
	return s.repo.List()
}

// CreateTag adds a new, unlocked, non-builtin tag.
func (s *Service) CreateTag(name string, requiresConfirmation bool) (Tag, error) {
	if name == "" {
		return Tag{}, errors.New("name is required")
	}
	if _, err := s.repo.Get(name); err == nil {
		return Tag{}, ErrTagExists
	} else if !errors.Is(err, ErrTagNotFound) {
		return Tag{}, err
	}
	id, err := vaultitem.NewID()
	if err != nil {
		return Tag{}, err
	}
	tag := Tag{ID: id, Name: name, RequiresConfirmation: requiresConfirmation}
	if err := s.repo.Create(tag); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

// UpdateTag changes requiresConfirmation. Locked tags (prod) always reject —
// this is a hard exclusion at the service layer, not a policy toggle.
func (s *Service) UpdateTag(id string, requiresConfirmation bool) (Tag, error) {
	tag, err := s.repo.GetByID(id)
	if err != nil {
		return Tag{}, err
	}
	if tag.Locked {
		return Tag{}, ErrTagLocked
	}
	tag.RequiresConfirmation = requiresConfirmation
	if err := s.repo.Update(tag); err != nil {
		return Tag{}, err
	}
	return tag, nil
}

// DeleteTag removes a custom tag. Builtin tags (dev/stg/prod) are never
// deletable, and locked tags (prod) are rejected the same way.
//
// ponytail: no "tag in use by a profile" guard. internal/ssh and
// internal/database validate EnvLookup.Exists() only at profile creation
// time — deleting a tag afterward leaves any referencing profile with a
// dangling env name, and nothing here re-validates it. There is currently no
// reader of RequiresConfirmation anywhere in this codebase, so this is not
// yet a live policy gap, but it means whoever wires the future MCP
// confirmation gate MUST treat an unresolvable env name as
// RequiresConfirmation=true (fail closed) at read time — do not assume this
// package guarantees that. Add a cross-service in-use check on delete when
// that gate exists and the UX cost of a blocked delete matters.
func (s *Service) DeleteTag(id string) error {
	tag, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if tag.Locked || tag.IsBuiltin {
		return ErrTagLocked
	}
	return s.repo.Delete(id)
}

// Exists reports whether name is a known tag. It satisfies the EnvLookup
// interface declared by internal/ssh and internal/database (accept
// interfaces, return structs — README.md "Dependencies").
func (s *Service) Exists(name string) bool {
	_, err := s.repo.Get(name)
	return err == nil
}
