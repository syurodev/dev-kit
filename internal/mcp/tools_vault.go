package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerVault(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "vault.status",
		Description: "Report whether the vault is not configured, locked, or unlocked",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolVaultStatus)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "vault.search",
		Description: "Search vault items by plaintext metadata (works while locked; no secret payloads)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolVaultSearch)
}

func (s *Server) toolVaultStatus(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.vault", "vault:manage", "vault", meta("status", nil), func(context.Context) (any, error) {
		state, err := s.deps.Vault.Status()
		if err != nil {
			return nil, err
		}
		return VaultStatusResult{State: string(state)}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type vaultSearchInput struct {
	Query string `json:"query"`
}

func (s *Server) toolVaultSearch(ctx context.Context, _ *sdk.CallToolRequest, in vaultSearchInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.search", "vault:search", "item:search", meta("search", nil), func(context.Context) (any, error) {
		return s.deps.Search.Search(in.Query)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
