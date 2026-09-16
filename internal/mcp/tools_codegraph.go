// internal/mcp/tools_codegraph.go
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		Description: "List HTTP endpoints detected in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListEndpoints)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.getApiFlow",
		Description: "Get the Controller -> Service -> Repository call chain for one endpoint",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetApiFlow)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.searchSymbol",
		Description: "Search classes/methods by name in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphSearchSymbol)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "codegraph.listFiles",
		Description: "List every file and directory in the active CodeGraph project (not just .java)",
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
// bridge.AppService.CodeGraphOpenProject — same atomic OpenProjectAt call
// under the same codegraph:index/project:new capability,
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
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("codegraph: resolve config dir: %w", err)
		}
		dbPath := codegraph.ProjectDBPath(configDir, in.Path)
		if err := s.deps.CodeGraph.OpenProjectAt(ctx, dbPath, in.Path); err != nil {
			return nil, err
		}
		return s.deps.CodeGraph.ActiveDetection(), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolCodeGraphListEndpoints(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_endpoints", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetApiFlowInput struct {
	EndpointID string `json:"endpointId"`
}

func (s *Server) toolCodeGraphGetApiFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetApiFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_api_flow", map[string]any{"endpoint_id": in.EndpointID}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetApiFlow(ctx, in.EndpointID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphSearchSymbolInput struct {
	Query string `json:"query"`
}

func (s *Server) toolCodeGraphSearchSymbol(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphSearchSymbolInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("search_symbol", map[string]any{"query": in.Query}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.SearchSymbol(ctx, in.Query)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolCodeGraphListFiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", meta("list_files", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListFiles(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphReadFileInput struct {
	RelativePath string `json:"relativePath"`
}

func (s *Server) toolCodeGraphReadFile(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphReadFileInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.RelativePath, meta("read_file", map[string]any{"relative_path": in.RelativePath}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ReadFile(ctx, in.RelativePath)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetCodeSnippetInput struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
}

func (s *Server) toolCodeGraphGetCodeSnippet(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetCodeSnippetInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:fs", "file:"+in.FilePath, meta("get_code_snippet", map[string]any{"file_path": in.FilePath, "line": in.Line, "end_line": in.EndLine}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetCodeSnippet(ctx, in.FilePath, in.Line, in.EndLine)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphGetBlastRadiusInput struct {
	FilePath string `json:"filePath"`
}

func (s *Server) toolCodeGraphGetBlastRadius(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetBlastRadiusInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_blast_radius", map[string]any{"file_path": in.FilePath}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetBlastRadius(ctx, in.FilePath)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolCodeGraphWaitForIndexReady(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("wait_for_index_ready", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.WaitForIndexReady(ctx)
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
}

func (s *Server) toolCodeGraphGetNeighbors(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetNeighborsInput) (*sdk.CallToolResult, any, error) {
	filter := codegraph.NeighborFilter{Layers: in.Layers, Continuation: in.Continuation}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_neighbors", map[string]any{"root_id": in.RootID, "depth": in.Depth}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetNeighbors(ctx, in.RootID, in.Depth, filter)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphTraceDataFlowInput struct {
	SymbolID  string `json:"symbolId"`
	FieldName string `json:"fieldName"`
}

func (s *Server) toolCodeGraphTraceDataFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphTraceDataFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("trace_data_flow", map[string]any{"symbol_id": in.SymbolID, "field_name": in.FieldName}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.TraceDataFlow(ctx, in.SymbolID, in.FieldName)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphListFieldsInput struct {
	NodeID string `json:"nodeId"`
}

func (s *Server) toolCodeGraphListFields(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphListFieldsInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_fields", map[string]any{"node_id": in.NodeID}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListFields(ctx, in.NodeID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
