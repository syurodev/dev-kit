// internal/bridge/codegraph.go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

func (s *AppService) CodeGraphOpenProject(path string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:index", "project:new", map[string]any{
		"action": "open_project",
	}, func(ctx context.Context) (any, error) {
		dbPath := codegraph.ProjectDBPath(s.container.ConfigDir, path)
		if err := s.container.CodeGraph.SetProjectPath(dbPath); err != nil {
			return nil, err
		}
		return nil, s.container.CodeGraph.OpenProject(ctx, path)
	})
	return err
}

func (s *AppService) CodeGraphListEndpoints() ([]codegraph.Endpoint, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "list_endpoints",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.Endpoint), nil
}

func (s *AppService) CodeGraphGetApiFlow(endpointID string) (codegraph.ApiFlow, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":      "get_api_flow",
		"endpoint_id": endpointID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetApiFlow(ctx, endpointID)
	})
	if err != nil {
		return codegraph.ApiFlow{}, err
	}
	return result.(codegraph.ApiFlow), nil
}

func (s *AppService) CodeGraphSearchSymbol(query string) ([]codegraph.SymbolMatch, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "search_symbol",
		"query":  query,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.SearchSymbol(ctx, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.SymbolMatch), nil
}

func (s *AppService) CodeGraphGetBlastRadius(filePath string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":    "get_blast_radius",
		"file_path": filePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetBlastRadius(ctx, filePath)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (s *AppService) CodeGraphWaitForIndexReady() (int, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "wait_for_index_ready",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.WaitForIndexReady(ctx)
	})
	if err != nil {
		return 0, err
	}
	return result.(int), nil
}

func (s *AppService) CodeGraphGetNeighbors(rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "get_neighbors",
		"root_id": rootID,
		"depth":   depth,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetNeighbors(ctx, rootID, depth, filter)
	})
	if err != nil {
		return codegraph.Subgraph{}, err
	}
	return result.(codegraph.Subgraph), nil
}

func (s *AppService) CodeGraphListFiles() ([]codegraph.FileEntry, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action": "list_files",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFiles(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FileEntry), nil
}

func (s *AppService) CodeGraphReadFile(relativePath string) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "read_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ReadFile(ctx, relativePath)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) CodeGraphWriteFile(relativePath, content string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "write_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.WriteFile(ctx, relativePath, content)
	})
	return err
}

func (s *AppService) CodeGraphGetCodeSnippet(filePath string, line, endLine int) (codegraph.CodeSnippet, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+filePath, map[string]any{
		"action":    "get_code_snippet",
		"file_path": filePath,
		"line":      line,
		"end_line":  endLine,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetCodeSnippet(ctx, filePath, line, endLine)
	})
	if err != nil {
		return codegraph.CodeSnippet{}, err
	}
	return result.(codegraph.CodeSnippet), nil
}

func (s *AppService) CodeGraphTraceDataFlow(symbolID, fieldName string) (codegraph.DataFlowTrace, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":     "trace_data_flow",
		"symbol_id":  symbolID,
		"field_name": fieldName,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.TraceDataFlow(ctx, symbolID, fieldName)
	})
	if err != nil {
		return codegraph.DataFlowTrace{}, err
	}
	return result.(codegraph.DataFlowTrace), nil
}

func (s *AppService) CodeGraphListFields(nodeID string) ([]codegraph.FieldRef, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "list_fields",
		"node_id": nodeID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFields(ctx, nodeID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FieldRef), nil
}

func (s *AppService) CodeGraphStartTerminal(rows, cols int) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:terminal", "project:active", map[string]any{
		"action": "start_terminal",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.StartTerminal(rows, cols)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
