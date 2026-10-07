// internal/mcp/tools_codegraph.go
package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"devkit/internal/codegraph"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerCodeGraph(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.openProject",
		Description: "Point CodeGraph at a project directory and index it — pass the absolute path of the directory you are working in. Refreshes a matching on-disk index in the background; otherwise indexes synchronously. Call waitForIndexReady before graph queries.",
	}, s.toolCodeGraphOpenProject)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.listEndpoints",
		Description: "List the entry points detected in the active CodeGraph project — where a flow starts because something outside the code calls in. kind says which: http (verb GET/POST…, target a URL), plugin (a Kong plugin phase: verb ACCESS/LOG…, target the plugin), message, rpc, graphql, socket, schedule. owner/group say what declares each one; order is the run order within a group where it means something (Kong phases). (The tool keeps its old name.) Paged: the answer is {result, total, nextCursor} — total counts every match, and while nextCursor is set, pass it back (with the same filters) for the next page. Narrow with kind, service, verb, pathPrefix (the target starts with it) or contains; a large project does not fit one page.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListEndpoints)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.getApiFlow",
		Description: "Get the call chain below one entry point (Controller -> Service -> Repository for an HTTP route). endpointId is an id from codegraph.listEndpoints.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetApiFlow)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.searchSymbol",
		Description: "Search classes/methods by name in the active CodeGraph project. Paged: the answer is {result, total, nextCursor} — total counts every match, and while nextCursor is set, pass it back (with the same query and filters) for the next page. match: contains (default) | prefix (a dot-separated part of the name starts with query) | exact (the name or its last part equals query). Narrow with kind (class|method), layer (controller|service|repository|unknown) and pathPrefix (the file path starts with it).",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphSearchSymbol)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.listFiles",
		Description: "List every file and directory in the active CodeGraph project (not just its source files)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListFiles)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.readFile",
		Description: "Read the full contents of one file in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphReadFile)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.getCodeSnippet",
		Description: "Get the real source for a method/class, snapped to its actual boundaries from the index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetCodeSnippet)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.getBlastRadius",
		Description: "List files that depend (import/reference) on a given file, transitively",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetBlastRadius)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.waitForIndexReady",
		Description: "Wait for cold-start indexing, startup refresh, and queued/running re-indexes. Returns the persistent per-project revision only after success; failed indexing returns index not ready.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphWaitForIndexReady)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.getNeighbors",
		Description: "Get a bounded subgraph (nodes + edges) around one symbol, both call directions, paginated",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetNeighbors)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.traceDataFlow",
		Description: "List methods that read or write a field on a class, using the already-resolved index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphTraceDataFlow)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.listFields",
		Description: "List the fields a class or method touches, with read/write counts — use it to find a field name for codegraph.traceDataFlow",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListFields)
}

type codeGraphOpenProjectInput struct {
	Path string `json:"path"`
}

// toolCodeGraphOpenProject is the MCP counterpart of
// bridge.AppService.CodeGraphOpenProject — the same open, with the index under
// the same config directory the desktop app uses (OpenProjectDir), under the
// same codegraph:index/project:new capability,
// documented in docs/dev-kit/codegraph.mdx's MCP tool surface table.
//
// path must be absolute: the MCP host runs inside the DevKit desktop
// process, so a relative path would resolve against that process's working
// directory, not the caller agent's — there is no cwd to make it meaningful.
func (s *Server) toolCodeGraphOpenProject(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphOpenProjectInput) (*sdk.CallToolResult, any, error) {
	if in.Path == "" {
		return nil, nil, fmt.Errorf("codegraph: path is required")
	}
	if !filepath.IsAbs(in.Path) {
		return nil, nil, fmt.Errorf("codegraph: path must be absolute (got %q) — pass the directory you are working in", in.Path)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:index", "project:new", meta("open_project", map[string]any{"path": in.Path}), func(ctx context.Context) (any, error) {
		if services := s.deps.CodeGraphServices; services != nil {
			focus, err := services.Focus(ctx, in.Path)
			if err != nil {
				return nil, err
			}
			return focus.ActiveDetection(), nil
		}
		if err := s.deps.CodeGraph.OpenProjectDir(ctx, in.Path); err != nil {
			return nil, err
		}
		return s.deps.CodeGraph.ActiveDetection(), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// codeGraphServiceInput is the input of a tool that takes nothing but which
// service to ask.
type codeGraphServiceInput struct {
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

// codeGraph is the service a tool call names: the focused project for "",
// else the workspace service at that path, from the Registry
// (docs/dev-kit/workspace.mdx § "Bề mặt API"). Without a Registry wired only
// the focused project exists, and naming another is an error rather than a
// silent answer about the wrong folder.
func (s *Server) codeGraph(ctx context.Context, servicePath string) (CodeGraphAPI, error) {
	if servicePath == "" {
		return s.deps.CodeGraph, nil
	}
	if !filepath.IsAbs(servicePath) {
		return nil, fmt.Errorf("codegraph: servicePath must be absolute (got %q)", servicePath)
	}
	if s.deps.CodeGraphServices == nil {
		return nil, fmt.Errorf("codegraph: servicePath is not supported here")
	}
	return s.deps.CodeGraphServices.Service(ctx, servicePath)
}

// indexWait is how long a graph query waits for an index that is still being
// built before it gives up. The first query after codegraph.openProject
// usually lands inside that window; answering "not ready" made the agent
// retry by hand.
const indexWait = 10 * time.Second

// whenIndexReady runs query, and if the index is still being built waits for
// it (up to indexWait) and runs it once more. A failed index and one that is
// merely slow are told apart, because only the second is worth retrying.
func whenIndexReady[T any](ctx context.Context, cg CodeGraphAPI, query func() (T, error)) (T, error) {
	v, err := query()
	if !errors.Is(err, codegraph.ErrIndexNotReady) {
		return v, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, indexWait)
	defer cancel()
	if _, werr := cg.WaitForIndexReady(waitCtx); werr != nil {
		switch {
		case ctx.Err() != nil:
			return v, ctx.Err()
		case errors.Is(werr, context.DeadlineExceeded):
			return v, fmt.Errorf("codegraph: still indexing after %s — call codegraph.waitForIndexReady, then repeat this query: %w", indexWait, codegraph.ErrIndexNotReady)
		case errors.Is(werr, codegraph.ErrIndexNotReady):
			return v, fmt.Errorf("%w — indexing failed; call codegraph.openProject again to retry", werr)
		default:
			return v, werr
		}
	}
	return query()
}

type codeGraphListEndpointsInput struct {
	Kind        string `json:"kind,omitempty" jsonschema:"optional; only this kind: http, message, rpc, graphql, socket, schedule or plugin"`
	Service     string `json:"service,omitempty" jsonschema:"optional; only entry points of this service (as listed in each row's service)"`
	Verb        string `json:"verb,omitempty" jsonschema:"optional; only this verb, e.g. GET, POST, ACCESS (case-insensitive)"`
	PathPrefix  string `json:"pathPrefix,omitempty" jsonschema:"optional; only entry points whose target starts with this, e.g. /v1/internal"`
	Contains    string `json:"contains,omitempty" jsonschema:"optional; only entry points whose target or owner contains this text (case-insensitive)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional; rows per page (default 100, at most 300)"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional; nextCursor of the previous page, with the same filters"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphListEndpoints(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphListEndpointsInput) (*sdk.CallToolResult, any, error) {
	// Filters are audited, the cursor is not: it is an opaque position.
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_endpoints", map[string]any{"kind": in.Kind, "service": in.Service, "verb": in.Verb, "path_prefix": in.PathPrefix, "contains": in.Contains, "limit": in.Limit}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		q := codegraph.EntryPointQuery{Kind: in.Kind, Service: in.Service, Verb: in.Verb, PathPrefix: in.PathPrefix, Contains: in.Contains, Limit: in.Limit, Cursor: in.Cursor}
		return whenIndexReady(ctx, cg, func() (codegraph.EntryPointPage, error) { return cg.ListEntryPointsPage(ctx, q) })
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetApiFlowInput struct {
	EndpointID  string `json:"endpointId"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphGetApiFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetApiFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_api_flow", map[string]any{"endpoint_id": in.EndpointID}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() (codegraph.ApiFlow, error) { return cg.GetApiFlow(ctx, in.EndpointID) })
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphSearchSymbolInput struct {
	Query       string `json:"query" jsonschema:"the name to look for; may be empty when a filter (kind, layer, pathPrefix) narrows the search"`
	Match       string `json:"match,omitempty" jsonschema:"optional; contains (default), prefix or exact"`
	Kind        string `json:"kind,omitempty" jsonschema:"optional; class or method"`
	Layer       string `json:"layer,omitempty" jsonschema:"optional; controller, service, repository or unknown"`
	PathPrefix  string `json:"pathPrefix,omitempty" jsonschema:"optional; only symbols whose file path starts with this"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional; matches per page (default 50, at most 200)"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional; nextCursor of the previous page, with the same query and filters"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphSearchSymbol(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphSearchSymbolInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("search_symbol", map[string]any{"query": in.Query, "match": in.Match, "kind": in.Kind, "layer": in.Layer, "path_prefix": in.PathPrefix, "limit": in.Limit}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		q := codegraph.SymbolQuery{Query: in.Query, Match: in.Match, Kind: in.Kind, Layer: in.Layer, PathPrefix: in.PathPrefix, Limit: in.Limit, Cursor: in.Cursor}
		return whenIndexReady(ctx, cg, func() (codegraph.SymbolPage, error) { return cg.SearchSymbolsPage(ctx, q) })
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolCodeGraphListFiles(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphServiceInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", meta("list_files", nil), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return cg.ListFiles(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphReadFileInput struct {
	RelativePath string `json:"relativePath"`
	ServicePath  string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphReadFile(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphReadFileInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.RelativePath, meta("read_file", map[string]any{"relative_path": in.RelativePath}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return cg.ReadFile(ctx, in.RelativePath)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetCodeSnippetInput struct {
	FilePath    string `json:"filePath"`
	Line        int    `json:"line"`
	EndLine     int    `json:"endLine"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphGetCodeSnippet(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetCodeSnippetInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.FilePath, meta("get_code_snippet", map[string]any{"file_path": in.FilePath, "line": in.Line, "end_line": in.EndLine}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() (codegraph.CodeSnippet, error) {
			return cg.GetCodeSnippet(ctx, in.FilePath, in.Line, in.EndLine)
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetBlastRadiusInput struct {
	FilePath    string `json:"filePath"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphGetBlastRadius(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetBlastRadiusInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_blast_radius", map[string]any{"file_path": in.FilePath}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() ([]string, error) { return cg.GetBlastRadius(ctx, in.FilePath) })
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolCodeGraphWaitForIndexReady(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphServiceInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("wait_for_index_ready", nil), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return cg.WaitForIndexReady(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetNeighborsInput struct {
	RootID       string   `json:"rootId"`
	Depth        int      `json:"depth"`
	Layers       []string `json:"layers,omitempty"`
	Continuation string   `json:"continuation,omitempty"`
	ServicePath  string   `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphGetNeighbors(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetNeighborsInput) (*sdk.CallToolResult, any, error) {
	filter := codegraph.NeighborFilter{Layers: in.Layers, Continuation: in.Continuation}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_neighbors", map[string]any{"root_id": in.RootID, "depth": in.Depth}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() (codegraph.Subgraph, error) {
			return cg.GetNeighbors(ctx, in.RootID, in.Depth, filter)
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphTraceDataFlowInput struct {
	SymbolID    string `json:"symbolId"`
	FieldName   string `json:"fieldName"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphTraceDataFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphTraceDataFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("trace_data_flow", map[string]any{"symbol_id": in.SymbolID, "field_name": in.FieldName}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() (codegraph.DataFlowTrace, error) {
			return cg.TraceDataFlow(ctx, in.SymbolID, in.FieldName)
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphListFieldsInput struct {
	NodeID      string `json:"nodeId"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of a workspace service to read instead of the focused project — opened on first use, and indexed first if it never was"`
}

func (s *Server) toolCodeGraphListFields(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphListFieldsInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_fields", map[string]any{"node_id": in.NodeID}), func(ctx context.Context) (any, error) {
		cg, err := s.codeGraph(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return whenIndexReady(ctx, cg, func() ([]codegraph.FieldRef, error) { return cg.ListFields(ctx, in.NodeID) })
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
