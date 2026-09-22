package workspace

import "sync"

// MemoryRepository is the in-memory Repository used by tests and the
// ephemeral container — same role as vaultitem.MemoryRepository.
type MemoryRepository struct {
	mu         sync.Mutex
	workspaces map[string]Workspace
	services   map[string][]WorkspaceService // keyed by workspace id
}

// NewMemoryRepository returns an empty in-memory Repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		workspaces: make(map[string]Workspace),
		services:   make(map[string][]WorkspaceService),
	}
}

func (r *MemoryRepository) ListWorkspaces() ([]Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Workspace, 0, len(r.workspaces))
	for _, ws := range r.workspaces {
		out = append(out, ws)
	}
	return out, nil
}

func (r *MemoryRepository) CreateWorkspace(ws Workspace, services []WorkspaceService) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaces[ws.ID] = ws
	r.services[ws.ID] = append([]WorkspaceService(nil), services...)
	return nil
}

func (r *MemoryRepository) DeleteWorkspace(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workspaces, id)
	delete(r.services, id)
	return nil
}

func (r *MemoryRepository) ListServices(workspaceID string) ([]WorkspaceService, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WorkspaceService(nil), r.services[workspaceID]...), nil
}
