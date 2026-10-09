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
	"strings"
	"unicode/utf8"

	"devkit/internal/vaultitem"
)

// ErrWorkspaceNotFound is returned when no workspace matches the given ID.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// ErrServiceNotFound is a folder id that is not one of the workspace's.
var ErrServiceNotFound = errors.New("workspace folder not found")

// ErrNameRequired is CreateWorkspace or UpdateWorkspace given a blank name.
var ErrNameRequired = errors.New("workspace name is required")

// MaxInstructionsBytes bounds the workspace context passed to every agent turn.
const MaxInstructionsBytes = 32 * 1024

// ErrInvalidInstructions rejects oversized text and invalid UTF-8.
var ErrInvalidInstructions = errors.New("workspace instructions must be valid UTF-8 and at most 32768 bytes")

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
	// Instructions are machine-local text shared by all agents of this workspace.
	Instructions string `json:"instructions"`
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
	// UpdateWorkspace writes ws's name and root, adds the services in add
	// and removes those with an id in removeIDs, as one unit.
	UpdateWorkspace(ws Workspace, add []WorkspaceService, removeIDs []string) error
	// UpdateWorkspaceWithInstructions also replaces instructions in the same write.
	UpdateWorkspaceWithInstructions(ws Workspace, add []WorkspaceService, removeIDs []string) error
	// SetInstructions updates only instructions, preserving concurrent folder edits.
	SetInstructions(id, instructions string) error
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

// GetInstructions reads the current workspace context by stable identity.
func (s *Service) GetInstructions(id string) (string, error) {
	ws, err := s.workspace(id)
	return ws.Instructions, err
}

// SetInstructions replaces the workspace instructions verbatim. Empty text clears them.
func (s *Service) SetInstructions(id, instructions string) error {
	if err := validateInstructions(instructions); err != nil {
		return err
	}
	return s.repo.SetInstructions(id, instructions)
}

func validateInstructions(instructions string) error {
	if !utf8.ValidString(instructions) || len(instructions) > MaxInstructionsBytes {
		return ErrInvalidInstructions
	}
	return nil
}

// MatchingWorkspaceIDs lists every workspace containing path. A folder may
// intentionally belong to several workspaces, so callers must not pick one
// from this list without an explicit user choice.
func (s *Service) MatchingWorkspaceIDs(path string) ([]string, error) {
	all, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, ws := range all {
		services, err := s.ListServices(ws.ID)
		if err != nil {
			return nil, err
		}
		for _, svc := range services {
			if Within(path, svc.Path) {
				ids = append(ids, ws.ID)
				break
			}
		}
	}
	return ids, nil
}

// ListServices returns every service under workspaceID.
//
// The primary (the workspace's Root) comes first, whatever the repository's
// own order: the SQL one sorts by name, and every caller that wants the
// primary takes the first service — Agent Hub opened a "whole workspace"
// session in whichever service sorted first rather than in the primary.
func (s *Service) ListServices(workspaceID string) ([]WorkspaceService, error) {
	services, err := s.repo.ListServices(workspaceID)
	if err != nil {
		return nil, err
	}
	ws, err := s.workspace(workspaceID)
	if err != nil {
		// An unknown id lists what the repository has for it (nothing):
		// ordering is not a reason to turn a listing into an error.
		return services, nil
	}
	return primaryFirst(services, ws.Root), nil
}

// primaryFirst moves the service at root to the front, keeping the rest in
// their order.
func primaryFirst(services []WorkspaceService, root string) []WorkspaceService {
	for i, svc := range services {
		if svc.Path == root {
			if i > 0 {
				out := make([]WorkspaceService, 0, len(services))
				out = append(out, svc)
				out = append(out, services[:i]...)
				out = append(out, services[i+1:]...)
				return out
			}
			break
		}
	}
	return services
}

// AddWorkspace registers root as a new workspace — CreateWorkspace with one
// folder, named after that folder when name is empty ("tên do người đặt, hoặc
// tên thư mục cha", workspace.mdx's data model).
func (s *Service) AddWorkspace(name, root string) (Workspace, []WorkspaceService, error) {
	root = filepath.Clean(root)
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(root)
	}
	return s.CreateWorkspace(name, []string{root})
}

// CreateWorkspace registers a named workspace over folders. Each folder goes
// through auto-discovery (DiscoverServices): a folder holding several git
// checkouts becomes one service per checkout, any other folder is one
// service. The first folder is the workspace's primary — its Root.
//
// No folders is allowed: a workspace is created by name first and given its
// folders afterwards (UpdateWorkspace), the order the Create project dialog
// asks for them in.
func (s *Service) CreateWorkspace(name string, folders []string) (Workspace, []WorkspaceService, error) {
	return s.CreateWorkspaceWithInstructions(name, folders, "")
}

// CreateWorkspaceWithInstructions creates the complete workspace in one transaction.
func (s *Service) CreateWorkspaceWithInstructions(name string, folders []string, instructions string) (Workspace, []WorkspaceService, error) {
	if err := validateInstructions(instructions); err != nil {
		return Workspace{}, nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Workspace{}, nil, ErrNameRequired
	}
	wsID, err := vaultitem.NewID()
	if err != nil {
		return Workspace{}, nil, err
	}
	services, err := discoverAll(wsID, folders, nil)
	if err != nil {
		return Workspace{}, nil, err
	}
	ws := Workspace{ID: wsID, Name: name, Instructions: instructions}
	if len(services) > 0 {
		ws.Root = services[0].Path
	}
	if err := s.repo.CreateWorkspace(ws, services); err != nil {
		return Workspace{}, nil, err
	}
	s.servicesAdded(services)
	return ws, services, nil
}

// UpdateWorkspace renames a workspace and makes its services exactly paths,
// in order — the Edit project dialog's list. A path already registered keeps
// its row (its name, a manual rename, its index outcome); a new one is a
// folder to discover, which may bring several services; a registered path
// not in the list is dropped. The first service is the new primary.
func (s *Service) UpdateWorkspace(id, name string, paths []string) (Workspace, []WorkspaceService, error) {
	return s.updateWorkspace(id, name, paths, nil)
}

// UpdateWorkspaceWithInstructions saves the complete workspace in one transaction.
func (s *Service) UpdateWorkspaceWithInstructions(id, name string, paths []string, instructions string) (Workspace, []WorkspaceService, error) {
	if err := validateInstructions(instructions); err != nil {
		return Workspace{}, nil, err
	}
	return s.updateWorkspace(id, name, paths, &instructions)
}

func (s *Service) updateWorkspace(id, name string, paths []string, instructions *string) (Workspace, []WorkspaceService, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Workspace{}, nil, ErrNameRequired
	}
	ws, err := s.workspace(id)
	if err != nil {
		return Workspace{}, nil, err
	}
	existing, err := s.repo.ListServices(id)
	if err != nil {
		return Workspace{}, nil, err
	}
	byPath := make(map[string]WorkspaceService, len(existing))
	for _, svc := range existing {
		byPath[svc.Path] = svc
	}

	var ordered, added []WorkspaceService
	seen := map[string]bool{}
	for _, raw := range paths {
		path := filepath.Clean(raw)
		if seen[path] {
			continue
		}
		if svc, ok := byPath[path]; ok {
			seen[path] = true
			ordered = append(ordered, svc)
			continue
		}
		found, err := discoverAll(id, []string{path}, seen)
		if err != nil {
			return Workspace{}, nil, err
		}
		for _, svc := range found {
			if old, ok := byPath[svc.Path]; ok {
				// Discovery landed on a service already registered.
				ordered = append(ordered, old)
				continue
			}
			ordered = append(ordered, svc)
			added = append(added, svc)
		}
	}
	var removeIDs []string
	for _, svc := range existing {
		if !seen[svc.Path] {
			removeIDs = append(removeIDs, svc.ID)
		}
	}

	ws.Name = name
	ws.Root = ""
	if len(ordered) > 0 {
		ws.Root = ordered[0].Path
	}
	if instructions == nil {
		err = s.repo.UpdateWorkspace(ws, added, removeIDs)
	} else {
		ws.Instructions = *instructions
		err = s.repo.UpdateWorkspaceWithInstructions(ws, added, removeIDs)
	}
	if err != nil {
		return Workspace{}, nil, err
	}
	s.servicesAdded(added)
	return ws, ordered, nil
}

// SetPrimary makes one of the workspace's folders its primary — the folder an
// agent session is spawned in by default. It writes only workspaces.root:
// nothing is re-discovered, re-indexed or removed, the folder keeps its ID, and
// every document, scope and search that names the folder by ID is unaffected.
// That is the point of a separate call: reordering through UpdateWorkspace
// rewrites the whole folder list.
func (s *Service) SetPrimary(workspaceID, serviceID string) (Workspace, error) {
	ws, err := s.workspace(workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	services, err := s.repo.ListServices(workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	for _, svc := range services {
		if svc.ID != serviceID {
			continue
		}
		if ws.Root == svc.Path {
			return ws, nil
		}
		ws.Root = svc.Path
		if err := s.repo.UpdateWorkspace(ws, nil, nil); err != nil {
			return Workspace{}, err
		}
		return ws, nil
	}
	return Workspace{}, ErrServiceNotFound
}

func (s *Service) workspace(id string) (Workspace, error) {
	all, err := s.repo.ListWorkspaces()
	if err != nil {
		return Workspace{}, err
	}
	for _, ws := range all {
		if ws.ID == id {
			return ws, nil
		}
	}
	return Workspace{}, ErrWorkspaceNotFound
}

func (s *Service) servicesAdded(services []WorkspaceService) {
	if s.OnServicesAdded != nil && len(services) > 0 {
		s.OnServicesAdded(append([]WorkspaceService(nil), services...))
	}
}

// discoverAll runs auto-discovery over each folder and turns what it finds
// into rows for workspace wsID, skipping paths in seen (and recording the
// ones it returns there). Order is the folders' order, then discovery's.
func discoverAll(wsID string, folders []string, seen map[string]bool) ([]WorkspaceService, error) {
	if seen == nil {
		seen = map[string]bool{}
	}
	var out []WorkspaceService
	for _, folder := range folders {
		discovered, err := DiscoverServices(filepath.Clean(folder))
		if err != nil {
			return nil, err
		}
		for _, d := range discovered {
			if seen[d.Path] {
				continue
			}
			seen[d.Path] = true
			id, err := vaultitem.NewID()
			if err != nil {
				return nil, err
			}
			name, source := serviceName(d)
			out = append(out, WorkspaceService{
				ID:          id,
				WorkspaceID: wsID,
				Name:        name,
				Path:        d.Path,
				NameSource:  source,
			})
		}
	}
	return out, nil
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
