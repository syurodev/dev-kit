package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerEnv() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "env.listTags",
		Description: "List environment tags (dev/stg/prod and custom)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolEnvListTags)
}

func (s *Server) toolEnvListTags(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.environments", "env:read", "tag:list", meta("list_tags", nil), func(context.Context) (any, error) {
		return s.deps.Envs.ListTags()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
