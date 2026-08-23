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
	// ErrTerminalNotConfigured is returned by StartTerminal when no
	// *terminal.Registry has been wired in via SetTerminalRegistry — the
	// container always wires a real one (see main.go), this only fires for
	// an ephemeral/test Service that never called SetTerminalRegistry.
	ErrTerminalNotConfigured = errors.New("codegraph: terminal registry not configured")
	// ErrNoPlan is returned when a plan operation runs against a project
	// that has no active plan (none proposed yet, per "1 plan active tại 1
	// thời điểm").
	ErrNoPlan = errors.New("codegraph: no active plan")
	// ErrPlanNodeNotFound is returned by RelinkPlanNode for a plan node ID
	// that is not part of the active plan.
	ErrPlanNodeNotFound = errors.New("codegraph: plan node not found")
	// ErrInvalidPlan is returned by ProposeFlow when the submitted plan is
	// internally inconsistent (no nodes, duplicate node IDs, or an edge
	// referencing an unknown node).
	ErrInvalidPlan = errors.New("codegraph: invalid plan")
	// ErrBinaryFile is returned by ReadFile when the file's content is not
	// valid UTF-8 — e.g. a compiled .class file, an image, a jar. The
	// bridge/frontend can only carry a string across the Wails JSON bridge;
	// silently returning invalid-UTF-8 bytes as a string would have the
	// JSON encoder replace the offending bytes with U+FFFD, corrupting the
	// content in memory — and a subsequent WriteFile would then write that
	// corrupted content back over the real file on disk. Reject up front
	// instead.
	ErrBinaryFile = errors.New("codegraph: file is not text")
)

// Param is one method parameter's name and type, e.g. {Name: "request", Type:
// "RefundRequest"}. Populated by the analyzer for method nodes; always empty
// for class nodes and for any node the analyzer hasn't resolved a signature
// for.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Node is a class or method discovered during indexing.
type Node struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"` // fully-qualified, e.g. "com.acme.OrderService.create"
	Kind     string  `json:"kind"`  // "class" | "method"
	Layer    string  `json:"layer"` // "controller" | "service" | "repository" | "unknown"
	FilePath string  `json:"filePath"`
	Line     int     `json:"line"`
	EndLine  int     `json:"endLine"`
	Summary  string  `json:"summary"` // Javadoc summary text, empty if none/not a doc comment
	Params   []Param `json:"params"`  // full parameter signature, empty for class nodes
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
	Label    string  `json:"label"` // matches the Node.Label it came from
	FilePath string  `json:"filePath"`
	Line     int     `json:"line"`
	Summary  string  `json:"summary"`
	Params   []Param `json:"params"`
}

// ApiFlow is the full call chain discovered for one endpoint.
type ApiFlow struct {
	EndpointID string     `json:"endpointId"`
	Steps      []FlowStep `json:"steps"`
}

// NeighborFilter narrows a GetNeighbors query. Layers, when non-empty,
// restricts results to nodes in one of the listed layers — the doc's own
// "màu theo layer" framing is the only filter dimension asked for in v1;
// add Kind/edge-kind filters later only if a real UI need shows up (YAGNI).
//
// Continuation carries pagination state: empty to start a fresh query from
// rootID, or the exact string a previous truncated Subgraph.Continuation
// returned to resume. It is an opaque decimal offset into a deterministic
// (sorted-by-ID) ordering of the full depth-bounded neighborhood — see
// GetNeighbors's doc comment for why an offset is enough (no server-side
// cursor/session state needed).
type NeighborFilter struct {
	Layers       []string `json:"layers,omitempty"`
	Continuation string   `json:"continuation,omitempty"`
}

// Subgraph is one bounded page of a GetNeighbors result.
type Subgraph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	// Truncated reports the page hit a ceiling. Node-side: more node pages
	// exist and Continuation fetches them. Edge-side: this page's edge list
	// was cut at the per-page edge cap (node-complete but edge-partial) and
	// Continuation is empty — there is no edge continuation.
	Truncated    bool   `json:"truncated"`
	Continuation string `json:"continuation,omitempty"` // next page's offset; empty when no further node page exists
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

// FileDependency is one file-level "depends on" edge — filePath imports or
// otherwise references a symbol declared in dependsOnFilePath. Produced as a
// byproduct of the analyzer's symbol resolution (it already knows this while
// resolving Node/Edge — see AnalysisResult's doc comment); kept for
// GetBlastRadius rather than recomputed later.
type FileDependency struct {
	FilePath          string `json:"filePath"`
	DependsOnFilePath string `json:"dependsOnFilePath"`
}

// FieldAccess is one "method X reads/writes field Y" fact, scoped to the
// class that declares the field (classID is that class's Node.ID) — Java
// field access is normally limited to the declaring class (or a subclass),
// so accesses are always looked up together with the class they belong to,
// never by field name alone across the whole project.
type FieldAccess struct {
	ClassID   string `json:"classId"`
	NodeID    string `json:"nodeId"` // the method Node.ID that performs the access
	FieldName string `json:"fieldName"`
	Kind      string `json:"kind"` // "read" | "write"
	Line      int    `json:"line"`
}

// FieldRef is one field a node touches, with how often — enough for a picker
// to list the fields worth tracing without the caller guessing a name. Reads
// and Writes are counted over whatever scope was asked for: every method on
// the class when ListFields was given a class, or the one method when it was
// given a method.
type FieldRef struct {
	// ClassID is the class that declares the field. Pass it straight back to
	// TraceDataFlow — it is what that method means by symbolID.
	ClassID   string `json:"classId"`
	FieldName string `json:"fieldName"`
	Reads     int    `json:"reads"`
	Writes    int    `json:"writes"`
}

// CallSite is one call expression, un-deduped and with its source line —
// everything Edge throws away for the sake of a clean graph. To is empty when
// the callee could not be turned into a node, and Unresolved says which wall
// the analyzer hit: "library" (bytecode, no body to read), "interface" (the
// implementation is not knowable statically) or "unresolved" (the solver could
// not resolve the call at all).
type CallSite struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Line       int    `json:"line"`
	Unresolved string `json:"unresolved"`
}

// DataFlowStep is one event in the trace: a read, a write, or a call the flow
// continues into. Depth is how many calls were followed to reach it, so a flat
// slice renders as an indented tree without the JSON having to nest.
type DataFlowStep struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // "read" | "write" | "call"
	Depth    int    `json:"depth"`
	// TargetID is the callee for kind=="call", empty otherwise.
	TargetID string `json:"targetId"`
	// Unresolved is empty when nothing was lost. Otherwise it names the wall the
	// trace hit: "library" (bytecode — there is no body to read, ever),
	// "interface" (the implementation is not knowable statically), "unresolved"
	// (the solver could not resolve the call), "depth" (the ceiling below) or
	// "cycle" (this method is already on the current path).
	Unresolved string `json:"unresolved"`
}

// DataFlowTrace is TraceDataFlow's result: every already-indexed method that
// touches one field on one class, in call-graph order, with the calls between
// them shown.
type DataFlowTrace struct {
	SymbolID  string `json:"symbolId"`
	FieldName string `json:"fieldName"`
	// OrderedBy is always "source" in v1 and is in the payload so no consumer
	// mistakes it for execution order. Events within a method are sorted by
	// line; branches, loops and early returns are not modelled, so a write in
	// an else-branch still sorts before a read on a later line. Modelling that
	// needs a control-flow graph, which JavaParser does not build.
	OrderedBy string         `json:"orderedBy"`
	Steps     []DataFlowStep `json:"steps"`
}

// AnalysisResult is everything an Analyzer produces for one project.
type AnalysisResult struct {
	Nodes            []Node           `json:"nodes"`
	Edges            []Edge           `json:"edges"`
	Endpoints        []Endpoint       `json:"endpoints"`
	FileDependencies []FileDependency `json:"fileDependencies"`
	FieldAccesses    []FieldAccess    `json:"fieldAccesses"`
	CallSites        []CallSite       `json:"callSites"`
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
	for _, c := range r.CallSites {
		if !ids[c.From] {
			return fmt.Errorf("codegraph: call site references unknown node %q", c.From)
		}
		// An empty To is the recorded shape of an unresolved call, not a defect.
		if c.To != "" && !ids[c.To] {
			return fmt.Errorf("codegraph: call site references unknown node %q", c.To)
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
	GetInEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error)
	GetDependents(ctx context.Context, filePath string) ([]string, error)
	GetFieldAccesses(ctx context.Context, classID, fieldName string) ([]FieldAccess, error)
	ListFields(ctx context.Context, nodeID string) ([]FieldRef, error)
	GetCallSites(ctx context.Context, fromID string) ([]CallSite, error)
	SavePlan(ctx context.Context, plan Plan) error
	GetPlan(ctx context.Context) (Plan, error)
	SetPlanState(ctx context.Context, state string) error
	SetPlanNodeFQN(ctx context.Context, planNodeID, fqn string) error
	ResolveFQN(ctx context.Context, fqn string) (Node, bool, error)
	Close() error
}
