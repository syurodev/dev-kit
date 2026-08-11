// Package environments provides the env tag registry (dev/stg/prod + custom)
// that internal/ssh and internal/database reference for MCP confirmation
// policy. Tags are not secrets; the registry lives outside the vaultitem
// substrate.
package environments

import (
	"errors"

	"devkit/internal/vaultitem"
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
	// ErrTagInUse is returned when DeleteTag would leave ssh/database (or
	// future) profiles pointing at a removed env name.
	ErrTagInUse = errors.New("environment tag is in use")
)

// ProfileEnvUsage reports whether any connection profile still references an
// env tag name. Declared here (consumer) so environments never imports
// ssh/database — app wires a vaultitem-backed checker at the composition root.
type ProfileEnvUsage interface {
	EnvInUse(name string) (bool, error)
}

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
	repo  Repository
	usage ProfileEnvUsage
}

// NewService wires the registry. usage may be nil in unit tests that do not
// exercise delete-in-use; production must pass a ProfileEnvUsage covering every
// profile type that stores metadata.env.
func NewService(repo Repository, usage ProfileEnvUsage) *Service {
	return &Service{repo: repo, usage: usage}
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
// deletable, and locked tags (prod) are rejected the same way. Custom tags
// still referenced by any profile (via ProfileEnvUsage) return ErrTagInUse.
//
// MCP confirmation readers must still treat an unresolvable env name as
// RequiresConfirmation=true (fail closed) — this guard only blocks new
// dangling references created by delete.
func (s *Service) DeleteTag(id string) error {
	tag, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if tag.Locked || tag.IsBuiltin {
		return ErrTagLocked
	}
	if s.usage != nil {
		inUse, err := s.usage.EnvInUse(tag.Name)
		if err != nil {
			return err
		}
		if inUse {
			return ErrTagInUse
		}
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
