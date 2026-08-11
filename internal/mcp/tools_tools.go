package mcp

import (
	"context"

	"devkit/internal/tools"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerTools() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "tools.hash",
		Description: "Hash input with sha256, sha512, or md5",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false},
	}, s.toolToolsHash)
}

type toolsHashInput struct {
	Algorithm string `json:"algorithm"`
	Input     string `json:"input"`
}

func (s *Server) toolToolsHash(ctx context.Context, _ *sdk.CallToolRequest, in toolsHashInput) (*sdk.CallToolResult, any, error) {
	const commandID = "tools.hash"
	v, err := s.deps.Caller.Call(ctx, "builtin.tools", "tools:hash", "command:"+commandID, meta("execute", map[string]any{
		"command_id": commandID,
	}), func(context.Context) (any, error) {
		return s.deps.Tools.Hash(tools.HashRequest{
			Algorithm: in.Algorithm,
			Input:     in.Input,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
