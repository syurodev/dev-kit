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
