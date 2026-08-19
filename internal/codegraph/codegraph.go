// internal/codegraph/codegraph.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrAnalyzerNotConfigured is returned by an Analyzer that has no real
	// backing implementation available yet (e.g. the Java CLI jar isn't
	// bundled/found). Service.OpenProject surfaces this unchanged.
	ErrAnalyzerNotConfigured = errors.New("codegraph: analyzer not configured")
	// ErrNoActiveProject is returned by any query method when no project has
	// been opened yet (or the last OpenProject failed).
	ErrNoActiveProject = errors.New("codegraph: no active project")
	// ErrNodeNotFound is returned when a node/endpoint ID does not exist in
	// the active project's index.
	ErrNodeNotFound = errors.New("codegraph: node not found")
	// ErrPathOutsideProject is returned by ReadFile/WriteFile/GetCodeSnippet
	// when the resolved path (symlinks included) falls outside the active
	// project's root.
	ErrPathOutsideProject = errors.New("codegraph: path outside project root")
)

// Node is a class or method discovered during indexing.
type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"` // fully-qualified, e.g. "com.acme.OrderService.create"
	Kind     string `json:"kind"`  // "class" | "method"
	Layer    string `json:"layer"` // "controller" | "service" | "repository" | "unknown"
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
}

// Edge is a directed call relationship between two Nodes, by Node.ID.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // "call" | "di"
}

// Endpoint is one detected HTTP entry point (e.g. a @GetMapping method).
type Endpoint struct {
	ID         string `json:"id"`
	HTTPMethod string `json:"httpMethod"`
	Path       string `json:"path"`
	EntryNode  string `json:"entryNode"` // Node.ID of the controller method
}

// FlowStep is one hop in an API flow.
type FlowStep struct {
	Label    string `json:"label"` // matches the Node.Label it came from
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

// ApiFlow is the full call chain discovered for one endpoint.
type ApiFlow struct {
	EndpointID string     `json:"endpointId"`
	Steps      []FlowStep `json:"steps"`
}

// SymbolMatch is one SearchSymbol result.
type SymbolMatch struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

// FileEntry is one file or directory in the active project, relative to its
// root. Deliberately flat (not a nested tree) — simpler to produce here and
// just as easy for a frontend to build a tree UI from a sorted flat list.
type FileEntry struct {
	Path  string `json:"path"` // relative to project root, forward-slash separated
	IsDir bool   `json:"isDir"`
}

// CodeSnippet is the result of GetCodeSnippet: the real source text for a
// method/class, plus the boundaries actually used (which may differ from
// the requested line/endLine — see GetCodeSnippet's doc comment).
type CodeSnippet struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
	Source   string `json:"source"`
	Language string `json:"language"` // "java" | "json" | "yaml" | "markdown" | "" (unknown extension)
	FQN      string `json:"fqn"`      // empty if the snippet didn't match exactly one indexed symbol
}

// AnalysisResult is everything an Analyzer produces for one project.
type AnalysisResult struct {
	Nodes     []Node     `json:"nodes"`
	Edges     []Edge     `json:"edges"`
	Endpoints []Endpoint `json:"endpoints"`
}

// Validate checks internal consistency: every Edge and Endpoint must
// reference a Node.ID that is actually present in Nodes. Catches analyzer
// bugs at the boundary instead of as a confusing failure three layers later.
func (r AnalysisResult) Validate() error {
	ids := make(map[string]bool, len(r.Nodes))
	for _, n := range r.Nodes {
		ids[n.ID] = true
	}
	for _, e := range r.Edges {
		if !ids[e.From] {
			return fmt.Errorf("codegraph: edge references unknown node %q", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("codegraph: edge references unknown node %q", e.To)
		}
	}
	for _, ep := range r.Endpoints {
		if !ids[ep.EntryNode] {
			return fmt.Errorf("codegraph: endpoint %q references unknown entry node %q", ep.ID, ep.EntryNode)
		}
	}
	return nil
}

// Analyzer builds an AnalysisResult from a project's source on disk. The
// real implementation shells out to a bundled Java CLI (JavaParser +
// JavaSymbolSolver) — see analyzer_subprocess.go; this plan ships a stub
// that returns ErrAnalyzerNotConfigured, plus the fakeAnalyzer test double
// used throughout this package's tests.
type Analyzer interface {
	Analyze(ctx context.Context, projectPath string) (AnalysisResult, error)
}

// Store persists one project's index. Each project gets its own SQLite file;
// ReplaceIndex overwrites it wholesale on every (re-)index.
type Store interface {
	ReplaceIndex(ctx context.Context, result AnalysisResult) error
	HasIndex(ctx context.Context) (bool, error)
	ListEndpoints(ctx context.Context) ([]Endpoint, error)
	GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error)
	GetNode(ctx context.Context, nodeID string) (Node, error)
	GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error)
	Close() error
}
