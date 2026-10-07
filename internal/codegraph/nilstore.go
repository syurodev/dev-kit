// internal/codegraph/nilstore.go
package codegraph

import (
	"context"
)

// nilStore is the container's placeholder Store before any project has been
// opened. Every method is unreachable in practice because Service checks
// s.active first — but a safe zero-value beats a nil-pointer panic if that
// invariant is ever broken by a future change.
type nilStore struct{}

func (nilStore) IndexVersion(context.Context) (int, error) { return 0, nil }
func (nilStore) IndexedAt(context.Context) (int64, error)  { return 0, nil }

func (nilStore) ReplaceIndex(context.Context, AnalysisResult) error { return ErrNoActiveProject }
func (nilStore) IndexedProject(context.Context) (string, error)     { return "", nil }
func (nilStore) SetIndexedProject(context.Context, string) error    { return nil }
func (nilStore) IndexedAnalyzer(context.Context) (string, error)    { return "", nil }
func (nilStore) SetIndexedAnalyzer(context.Context, string) error   { return nil }
func (nilStore) HasIndex(context.Context) (bool, error)             { return false, nil }
func (nilStore) ListEntryPoints(context.Context) ([]EntryPoint, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetEntryPoint(context.Context, string) (EntryPoint, error) {
	return EntryPoint{}, ErrNoActiveProject
}
func (nilStore) GetNode(context.Context, string) (Node, error) { return Node{}, ErrNoActiveProject }
func (nilStore) GetNodes(context.Context, []string) (map[string]Node, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetAdjacentEdges(context.Context, []string) ([]Edge, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) ListClassMethods(context.Context, string) ([]Node, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetOutEdges(context.Context, string) ([]Edge, error) { return nil, ErrNoActiveProject }
func (nilStore) GetInEdges(context.Context, string) ([]Edge, error)  { return nil, ErrNoActiveProject }
func (nilStore) SearchSymbol(context.Context, string) ([]SymbolMatch, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) SearchSymbols(context.Context, SymbolQuery) (SymbolPage, error) {
	return SymbolPage{}, ErrNoActiveProject
}
func (nilStore) FindConfigKeys(context.Context, string, bool) ([]ConfigKey, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) FindConfigUsages(context.Context, []string, bool, []string) ([]ConfigUsage, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) FindEdgesByGuard(context.Context, []string, string) ([]Edge, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) FindNodeAtLine(context.Context, string, int) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
func (nilStore) FindNodesByName(context.Context, string) ([]Node, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetDependents(context.Context, string) ([]string, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetDependencies(context.Context, string) ([]string, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetFieldAccesses(context.Context, string, string) ([]FieldAccess, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetFieldAccessesAt(context.Context, string, int, string) ([]FieldAccess, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetCallSites(context.Context, string) ([]CallSite, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetCallSitesAt(context.Context, string, int) ([]CallSite, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetCallSitesTo(context.Context, string) ([]CallSite, error) {
	return nil, ErrNoActiveProject
}

func (nilStore) AddComment(context.Context, PlanComment) error { return ErrNoActiveProject }
func (nilStore) ListComments(context.Context, string) ([]PlanComment, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) CountComments(context.Context) (map[string]int, error) {
	return nil, ErrNoActiveProject
}

func (nilStore) ListFields(context.Context, string) ([]FieldRef, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) SavePlan(context.Context, Plan) error       { return ErrNoActiveProject }
func (nilStore) GetPlan(context.Context) (Plan, error)      { return Plan{}, ErrNoActiveProject }
func (nilStore) SetPlanState(context.Context, string) error { return ErrNoActiveProject }
func (nilStore) SetPlanNodeFQN(context.Context, string, string) (bool, error) {
	return false, ErrNoActiveProject
}
func (nilStore) ResolveFQN(context.Context, string) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
func (nilStore) Close() error { return nil }

// NewNilStore returns a placeholder Store that always reports no active
// project. internal/app/container.go uses this to construct Service before
// any CodeGraph project has been opened, since container.go is a different
// package and cannot construct the unexported nilStore type directly.
func NewNilStore() Store { return nilStore{} }
