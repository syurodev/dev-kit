// internal/codegraph/service.go
package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// maxFlowDepth caps the BFS walk in GetApiFlow so a pathological call graph
// (very deep chain, or a cycle missed by the visited-set for some reason)
// can never turn into an unbounded response — same "bounded, no full-graph
// dump" contract the design doc requires everywhere else in this module.
const maxFlowDepth = 32

// Service is the CodeGraph domain service. It owns exactly one active
// project's index at a time (FR-1: no multi-project registry in v1).
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string
}

func NewService(store Store, analyzer Analyzer) *Service {
	return &Service{store: store, analyzer: analyzer}
}

// SetProjectPath points the service at a new project's on-disk index file,
// opening (creating if needed) a fresh sqliteStore for it and discarding the
// previous one. Call this once per OpenProject, before running the analyzer,
// so a fresh analyze always starts from a clean per-project store.
func (s *Service) SetProjectPath(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("codegraph: create project store dir: %w", err)
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("codegraph: open project store: %w", err)
	}
	s.mu.Lock()
	old := s.store
	s.store = store
	s.active = false
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// OpenProject runs the analyzer over projectPath and replaces the active
// index. A failed analyze leaves the service with no active project rather
// than a stale or partial one.
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	s.projectRoot = projectPath
	return nil
}

// activeProject returns the current store and project root under the read
// lock, so neither field is ever read concurrently with the writes
// SetProjectPath/OpenProject make to them under s.mu.Lock(). Both are used
// after the lock is released — *sql.DB (what sqliteStore wraps) and a plain
// string are both safe to use unsynchronized once captured.
func (s *Service) activeProject() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return nil, "", ErrNoActiveProject
	}
	return s.store, s.projectRoot, nil
}

func (s *Service) activeStore() (Store, error) {
	store, _, err := s.activeProject()
	return store, err
}

func (s *Service) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListEndpoints(ctx)
}

func (s *Service) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.SearchSymbol(ctx, query)
}

// GetApiFlow walks the call graph from the endpoint's entry node, in Go
// (not SQL), so the traversal — including cycle protection — is testable
// without a database. Cycles are broken by the visited-set; maxFlowDepth is
// a second, independent safety bound.
func (s *Service) GetApiFlow(ctx context.Context, endpointID string) (ApiFlow, error) {
	store, err := s.activeStore()
	if err != nil {
		return ApiFlow{}, err
	}
	ep, err := store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return ApiFlow{}, err
	}
	steps, err := walkFlow(ctx, store, ep.EntryNode)
	if err != nil {
		return ApiFlow{}, err
	}
	return ApiFlow{EndpointID: endpointID, Steps: steps}, nil
}

func walkFlow(ctx context.Context, store Store, rootNodeID string) ([]FlowStep, error) {
	var steps []FlowStep
	visited := make(map[string]bool)

	var walk func(nodeID string, depth int) error
	walk = func(nodeID string, depth int) error {
		if visited[nodeID] || depth > maxFlowDepth {
			return nil
		}
		visited[nodeID] = true

		node, err := store.GetNode(ctx, nodeID)
		if err != nil {
			return err
		}
		steps = append(steps, FlowStep{Label: node.Label, FilePath: node.FilePath, Line: node.Line})

		edges, err := store.GetOutEdges(ctx, nodeID)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if e.Kind != "call" {
				continue
			}
			if err := walk(e.To, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(rootNodeID, 0); err != nil {
		return nil, err
	}
	return steps, nil
}

// nilStore is the container's placeholder Store before any project has been
// opened. Every method is unreachable in practice because Service checks
// s.active first — but a safe zero-value beats a nil-pointer panic if that
// invariant is ever broken by a future change.
type nilStore struct{}

func (nilStore) ReplaceIndex(context.Context, AnalysisResult) error         { return ErrNoActiveProject }
func (nilStore) HasIndex(context.Context) (bool, error)                    { return false, nil }
func (nilStore) ListEndpoints(context.Context) ([]Endpoint, error)         { return nil, ErrNoActiveProject }
func (nilStore) GetEndpoint(context.Context, string) (Endpoint, error)     { return Endpoint{}, ErrNoActiveProject }
func (nilStore) GetNode(context.Context, string) (Node, error)             { return Node{}, ErrNoActiveProject }
func (nilStore) GetOutEdges(context.Context, string) ([]Edge, error)       { return nil, ErrNoActiveProject }
func (nilStore) SearchSymbol(context.Context, string) ([]SymbolMatch, error) { return nil, ErrNoActiveProject }
func (nilStore) FindNodeAtLine(context.Context, string, int) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
func (nilStore) Close() error { return nil }

// NewNilStore returns a placeholder Store that always reports no active
// project. internal/app/container.go uses this to construct Service before
// any CodeGraph project has been opened, since container.go is a different
// package and cannot construct the unexported nilStore type directly.
func NewNilStore() Store { return nilStore{} }
