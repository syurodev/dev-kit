// Package workspace is the multi-repo registry: a named (service name →
// path) list, machine-local and unsynced — see docs/dev-kit/workspace.mdx.
// This increment covers the registry itself (add / list / delete),
// auto-discovery (discover.go, springconfig.go) and resolving an outbound
// call's target to a service (target.go); the manifest tier, the join
// itself, and the Registry N-service full-index cache are not built yet.
package workspace

import (
	"errors"
	"path/filepath"

	"devkit/internal/vaultitem"
)

// ErrWorkspaceNotFound is returned when no workspace matches the given ID.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// Name sources for WorkspaceService.NameSource — see workspace.mdx's data
// model. NameSourceConfig comes from spring.application.name
// (springconfig.go), NameSourceFolder is the fallback; manual renames have
// no UI path yet.
const (
	NameSourceFolder = "folder"
	NameSourceConfig = "config"
	NameSourceManual = "manual"
)

// Workspace is a named root that groups one or more services.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

// WorkspaceService is one persisted (name, path) entry belonging to a
// Workspace — the row DiscoveredService becomes once AddWorkspace writes it.
type WorkspaceService struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	NameSource  string `json:"name_source"`
	// IndexState is the index outcome that outlives a restart: "failed" or
	// "unsupported", else "" (see RecordIndexOutcome). Whether a service is
	// queued, running or indexed is codegraph.Registry's to say.
	IndexState string `json:"index_state,omitempty"`
	// IndexError is why, for IndexState "failed" or "unsupported".
	IndexError string `json:"index_error,omitempty"`
	// IndexMissingToolchain: the failure was a missing analyzer toolchain,
	// which building or installing it fixes — not a failure of the code.
	IndexMissingToolchain bool `json:"index_missing_toolchain,omitempty"`
}

// Persisted index outcomes (WorkspaceService.IndexState).
const (
	IndexStateFailed      = "failed"
	IndexStateUnsupported = "unsupported"
)

// Repository persists workspaces and their services.
type Repository interface {
	ListWorkspaces() ([]Workspace, error)
	// CreateWorkspace writes ws and every entry in services as one unit —
	// the SQL implementation wraps it in a transaction so a workspace never
	// ends up with only some of its services on a partial failure.
	CreateWorkspace(ws Workspace, services []WorkspaceService) error
	// DeleteWorkspace removes the workspace and every service under it.
	DeleteWorkspace(id string) error
	ListServices(workspaceID string) ([]WorkspaceService, error)
	// SetIndexOutcome records the index outcome of every service at path
	// (one folder can be a service of two workspaces): state "" clears it.
	SetIndexOutcome(path, state, reason string, missingToolchain bool) error
}

// Service orchestrates the registry on top of a Repository.
type Service struct {
	repo Repository

	// OnServicesAdded fires after services are registered — today by
	// AddWorkspace — so the composition root can queue each for its one
	// index (workspace.mdx § "Thêm service"). workspace does not import
	// codegraph; the hook is how the two meet.
	OnServicesAdded func([]WorkspaceService)
}

// NewService wires the registry.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// ListWorkspaces returns every registered workspace.
func (s *Service) ListWorkspaces() ([]Workspace, error) {
	return s.repo.ListWorkspaces()
}

// ListServices returns every service under workspaceID.
func (s *Service) ListServices(workspaceID string) ([]WorkspaceService, error) {
	return s.repo.ListServices(workspaceID)
}

// AddWorkspace registers root as a new workspace, auto-discovering its
// services via DiscoverServices: one service per nested git checkout it
// finds at depth 1, or root itself when it finds none — see discover.go.
//
// name defaults to root's own folder name when empty, matching the "tên do
// người đặt, hoặc tên thư mục cha" fallback in workspace.mdx's data model.
func (s *Service) AddWorkspace(name, root string) (Workspace, []WorkspaceService, error) {
	root = filepath.Clean(root)
	if name == "" {
		name = filepath.Base(root)
	}

	discovered, err := DiscoverServices(root)
	if err != nil {
		return Workspace{}, nil, err
	}

	wsID, err := vaultitem.NewID()
	if err != nil {
		return Workspace{}, nil, err
	}
	ws := Workspace{ID: wsID, Name: name, Root: root}

	services := make([]WorkspaceService, 0, len(discovered))
	for _, d := range discovered {
		svcID, err := vaultitem.NewID()
		if err != nil {
			return Workspace{}, nil, err
		}
		name, source := serviceName(d)
		services = append(services, WorkspaceService{
			ID:          svcID,
			WorkspaceID: wsID,
			Name:        name,
			Path:        d.Path,
			NameSource:  source,
		})
	}

	if err := s.repo.CreateWorkspace(ws, services); err != nil {
		return Workspace{}, nil, err
	}
	if s.OnServicesAdded != nil && len(services) > 0 {
		s.OnServicesAdded(append([]WorkspaceService(nil), services...))
	}
	return ws, services, nil
}

// serviceName is auto-discovery step 2: the name in a service's URLs comes
// from its own spring.application.name when that folds to a literal, and
// only falls back to the folder name otherwise. A config that fails to
// parse falls back too — a folder name that might be wrong beats refusing
// to register the workspace over one file.
func serviceName(d DiscoveredService) (name, source string) {
	cfg, err := ReadSpringConfig(d.Path)
	if err == nil {
		if n := cfg.ApplicationName(); n != "" {
			return n, NameSourceConfig
		}
	}
	return d.Name, NameSourceFolder
}

// DeleteWorkspace removes a workspace and every service under it. Services'
// own directories are never touched — this only forgets the registry entry.
func (s *Service) DeleteWorkspace(id string) error {
	return s.repo.DeleteWorkspace(id)
}

// AllServices lists every service of every workspace.
func (s *Service) AllServices() ([]WorkspaceService, error) {
	workspaces, err := s.repo.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var out []WorkspaceService
	for _, ws := range workspaces {
		services, err := s.repo.ListServices(ws.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, services...)
	}
	return out, nil
}

// RecordIndexOutcome keeps what a service's last index run ended in, when it
// is worth keeping across a restart: "failed" and "unsupported" are stored,
// so a run known to fail is not repeated on every start; anything else
// clears what was stored, since the index itself says when it was built.
func (s *Service) RecordIndexOutcome(path, state, reason string, missingToolchain bool) error {
	switch state {
	case IndexStateFailed, IndexStateUnsupported:
	default:
		state, reason, missingToolchain = "", "", false
	}
	return s.repo.SetIndexOutcome(filepath.Clean(path), state, reason, missingToolchain)
}
