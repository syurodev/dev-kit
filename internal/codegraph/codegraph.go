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
	ErrNodeNotFound  = errors.New("codegraph: node not found")
	ErrIndexNotReady = errors.New("codegraph: index not ready")
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
	// ErrFileTooLarge is returned by ReadFile (and GetCodeSnippet) for a file
	// past maxReadFileBytes — the editor is for source, and a huge file would
	// be read whole into memory and pushed across the bridge as one string.
	ErrFileTooLarge = errors.New("codegraph: file is too large to open")
	// ErrProjectChanged is returned when the active project is no longer the
	// one a call was made for: WriteFile given an expectedRoot that is not the
	// open project, an open that a newer open (or Close) replaced before it
	// finished, a query whose store was retired by a switch mid-call. The
	// caller's view of which folder is open is stale; the fix is to re-read
	// it, never to retry the same call blindly.
	ErrProjectChanged = errors.New("codegraph: the active project changed")
)

// Where a handler parameter's value comes from on the wire. An analyzer that
// cannot tell leaves it empty, which is not the same as ParamSourceOther: one
// means "this language does not say", the other means "this is not part of the
// request".
const (
	ParamSourceBody   = "body"
	ParamSourcePath   = "path"
	ParamSourceQuery  = "query"
	ParamSourceHeader = "header"
	ParamSourceOther  = "other"
)

// Field is one field of a body type, e.g. {Name: "amount", Type: "BigDecimal"}.
// One level deep only: a nested DTO is a field with a type and no fields of its
// own, because a skeleton that guessed two levels would be guessing about
// serialization it cannot see.
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Param is one method parameter's name and type, e.g. {Name: "request", Type:
// "RefundRequest"}. Populated by the analyzer for method nodes; always empty
// for class nodes and for any node the analyzer hasn't resolved a signature
// for.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Source is one of the ParamSource constants, or empty when the analyzer
	// cannot say. Lua leaves it empty for every parameter and that is the
	// honest answer: a Kong phase or a Lapis route declares no types and no
	// body, so there is nothing to read rather than nothing there.
	Source string `json:"source,omitempty"`
	// WireName is the name on the wire when the declaration renames it —
	// @RequestParam("page_size") int pageSize. Empty means it is Name.
	WireName string `json:"wireName,omitempty"`
	// Fields are the body type's own fields, for a Source of body. Empty for
	// a type the analyzer could not resolve to project source, which is why
	// the draft says "could not read this type" rather than showing {}.
	Fields []Field `json:"fields,omitempty"`
}

// Wire is the name this parameter travels under.
func (p Param) Wire() string {
	if p.WireName != "" {
		return p.WireName
	}
	return p.Name
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
	Summary  string  `json:"summary"` // doc comment summary (Javadoc, or the LDoc block above a Lua function); empty if none
	Params   []Param `json:"params"`  // full parameter signature, empty for class nodes
}

// Edge is a directed call relationship between two Nodes, by Node.ID.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // "call" | "di"
	// Guard is the condition the call runs under, as the source writes it
	// ("!moduleBasedEnabled", "mode == A && flag"); empty when it runs whenever
	// the caller does. Read from the code around the call and never evaluated:
	// a claim about the source, not about any value. Only the Java analyzer
	// sets it so far.
	Guard string `json:"guard,omitempty"`
}

// EntryPoint is where a flow starts because something outside the code calls
// in. An HTTP route is the common case, not the only one: a Kong plugin phase,
// a message or event pattern, a gRPC method, a GraphQL field, a WebSocket
// message and a scheduled job are entry points too, and Kind says which.
type EntryPoint struct {
	ID string `json:"id"`
	// Verb is what the chip says: GET, ACCESS, EVENT, GRPC, CRON…
	Verb string `json:"verb"`
	// Target is what the entry point answers to: a URL, a Kong plugin's name,
	// a message pattern, a gRPC "Service/Method", a schedule.
	Target    string `json:"target"`
	EntryNode string `json:"entryNode"` // Node.ID of the handler
	// Kind is one of the EntryKind* constants.
	Kind string `json:"kind"`
	// Service is the top-level bucket in a folder that holds several — the
	// Java service package. Empty when there is only one.
	Service string `json:"service"`
	// Owner identifies what declares the entry point (a class FQN, a module
	// path) and Group is how it reads as a heading: a class's simple name, a
	// Kong plugin's name, a Next route. Owner is the key — two classes may
	// share a simple name.
	Owner string `json:"owner"`
	Group string `json:"group"`
	// Order sorts entry points within a group where order means something:
	// a Kong plugin's phases, in the order Kong runs them. Zero means none,
	// and the reader sorts by Target.
	Order int `json:"order"`
}

// FlowStep is one hop in an API flow.
type FlowStep struct {
	// Depth is how many calls below the entry point this step is. Steps are
	// listed depth-first, so Depth is what turns the flat list back into a
	// tree: a step's caller is the nearest step above it that is one less deep.
	Depth int `json:"depth"`
	// Guard is the condition under which the caller reaches this step (Edge.Guard);
	// empty when it always does. The entry point itself has none.
	Guard    string  `json:"guard,omitempty"`
	Label    string  `json:"label"` // matches the Node.Label it came from
	FilePath string  `json:"filePath"`
	Line     int     `json:"line"`
	Summary  string  `json:"summary"`
	Params   []Param `json:"params"`
}

// ApiFlow is the full call chain discovered for one entry point.
type ApiFlow struct {
	EntryPointID string     `json:"entryPointId"`
	Steps        []FlowStep `json:"steps"`
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
	NodeID string `json:"nodeId"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	// Layer is the node's layer (controller, service, repository, unknown),
	// for the dot a search result wears.
	Layer    string `json:"layer"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

// Definition is one place the symbol under the cursor may be defined. A
// single FindDefinitions call can return more than one — an unresolved
// interface call, an overload picked by name alone, or two candidates the
// fallback-by-name tier could not tell apart — so the editor always shows a
// list, even when it happens to have one entry.
type Definition struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
}

// Reference is one place a symbol is used — FindReferences' result. Unlike
// Definition, NodeID/Label/FilePath/Line describe the USE site (the caller,
// the reading/writing method, the injecting node), not the symbol itself:
// "go to definition" jumps to one place, but "find references" is inherently
// a list of many different places, each in a different enclosing node.
type Reference struct {
	NodeID   string `json:"nodeId"` // the node the use sits in (the caller / accessor / injector)
	Label    string `json:"label"`  // that node's label
	Kind     string `json:"kind"`   // "call" | "read" | "write" | "inject"
	FilePath string `json:"filePath"`
	Line     int    `json:"line"` // the line of the USE, not of the enclosing node
	Code     string `json:"code"` // that source line, trimmed and clipped
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
	Language string `json:"language"` // "java" | "lua" | "typescript" | "javascript" | "json" | "yaml" | "markdown" | "" (unknown extension)
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
	// Unresolved marks "depth" (the traversal ceiling) or "cycle" (a method
	// already on the current path). Unresolved library/interface call sites
	// remain in the index, but lack field evidence and are omitted here.
	Unresolved string `json:"unresolved"`
	// Code is the source line itself, trimmed — what "read at line 42" means
	// to someone reading it. Empty when the file could not be read; the trace
	// does not fail over a missing line.
	Code string `json:"code"`
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
	EntryPoints      []EntryPoint     `json:"entryPoints"`
	FileDependencies []FileDependency `json:"fileDependencies"`
	FieldAccesses    []FieldAccess    `json:"fieldAccesses"`
	CallSites        []CallSite       `json:"callSites"`
}

// Validate checks internal consistency: every Edge and EntryPoint must
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
	for _, ep := range r.EntryPoints {
		if !ids[ep.EntryNode] {
			return fmt.Errorf("codegraph: entry point %q references unknown entry node %q", ep.ID, ep.EntryNode)
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
	IndexVersion(ctx context.Context) (int, error)
	// IndexedAt is when the index was last replaced, Unix milliseconds; 0 if unknown.
	IndexedAt(ctx context.Context) (int64, error)
	ReplaceIndex(ctx context.Context, result AnalysisResult) error
	// IndexedProject / SetIndexedProject say which project the stored index
	// describes, so an open can tell "this project's index" from "an index".
	IndexedProject(ctx context.Context) (string, error)
	SetIndexedProject(ctx context.Context, root string) error
	// IndexedAnalyzer / SetIndexedAnalyzer say which analyzer wrote the stored
	// index (see analyzerIdentity), so a reopen can tell an index it may serve
	// while it refreshes from one written by something else. "" is unknown.
	IndexedAnalyzer(ctx context.Context) (string, error)
	SetIndexedAnalyzer(ctx context.Context, identity string) error
	HasIndex(ctx context.Context) (bool, error)
	ListEntryPoints(ctx context.Context) ([]EntryPoint, error)
	GetEntryPoint(ctx context.Context, endpointID string) (EntryPoint, error)
	GetNode(ctx context.Context, nodeID string) (Node, error)
	// GetNodes is GetNode for many IDs at once; missing IDs are absent from
	// the map rather than an error.
	GetNodes(ctx context.Context, ids []string) (map[string]Node, error)
	// GetAdjacentEdges returns every edge into or out of any of ids.
	GetAdjacentEdges(ctx context.Context, ids []string) ([]Edge, error)
	// ListClassMethods returns the method nodes a class declares directly.
	ListClassMethods(ctx context.Context, classID string) ([]Node, error)
	GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error)
	GetInEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	// SearchSymbols is SearchSymbol with filters and paging (see SymbolQuery).
	SearchSymbols(ctx context.Context, q SymbolQuery) (SymbolPage, error)
	FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error)
	// FindNodesByName returns every node whose label names identifier (see
	// matchesIdentifier) — FindDefinitions' fallback-by-name tier, e.g.
	// resolving a type reference like "DeviceListResponse" to its class.
	FindNodesByName(ctx context.Context, identifier string) ([]Node, error)
	GetDependents(ctx context.Context, filePath string) ([]string, error)
	// GetDependencies is GetDependents in the other direction: every file
	// filePath itself depends on. Used to rank FindDefinitions' fallback
	// candidates by proximity when several files declare the same name.
	GetDependencies(ctx context.Context, filePath string) ([]string, error)
	GetFieldAccesses(ctx context.Context, classID, fieldName string) ([]FieldAccess, error)
	// GetFieldAccessesAt returns the field accesses recorded at one exact
	// source line in filePath — FindDefinitions' field-access tier, scoped to
	// a single field name so a coincidental line/name match in another file
	// can never leak in.
	GetFieldAccessesAt(ctx context.Context, filePath string, line int, fieldName string) ([]FieldAccess, error)
	ListFields(ctx context.Context, nodeID string) ([]FieldRef, error)
	GetCallSites(ctx context.Context, fromID string) ([]CallSite, error)
	// GetCallSitesAt returns the call sites recorded at one exact source line
	// in filePath — FindDefinitions' call-site tier. A line can hold more than
	// one call expression ("a.foo(); b.foo();" on one line), so this returns
	// all of them, not just the first.
	GetCallSitesAt(ctx context.Context, filePath string, line int) ([]CallSite, error)
	// GetCallSitesTo returns every call site whose to_id is toID — the
	// reverse lookup FindReferences' method tier needs ("who calls this"),
	// which neither GetCallSites (by caller) nor GetCallSitesAt (by exact
	// location) answers.
	GetCallSitesTo(ctx context.Context, toID string) ([]CallSite, error)
	SavePlan(ctx context.Context, plan Plan) error
	GetPlan(ctx context.Context) (Plan, error)
	SetPlanState(ctx context.Context, state string) error
	// SetPlanNodeFQN reports whether the change took an approved plan back
	// to draft.
	SetPlanNodeFQN(ctx context.Context, planNodeID, fqn string) (bool, error)
	AddComment(ctx context.Context, c PlanComment) error
	ListComments(ctx context.Context, planNodeID string) ([]PlanComment, error)
	CountComments(ctx context.Context) (map[string]int, error)
	ResolveFQN(ctx context.Context, fqn string) (Node, bool, error)
	Close() error
}
