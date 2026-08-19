// internal/mcp/tools_codegraph.go
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerCodeGraph() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.listEndpoints",
		Description: "List HTTP endpoints detected in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListEndpoints)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getApiFlow",
		Description: "Get the Controller -> Service -> Repository call chain for one endpoint",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetApiFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.searchSymbol",
		Description: "Search classes/methods by name in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphSearchSymbol)
}

func (s *Server) toolCodeGraphListEndpoints(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_endpoints", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
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
	return nil, v, nil
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
	return nil, v, nil
}
