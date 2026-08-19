package mcp

import (
	"context"

	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerDatabase() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "db.listProfiles",
		Description: "List database connection profiles (metadata only)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBListProfiles)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "db.testConnection",
		Description: "Test connectivity for a database profile by id",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolDBTestConnection)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "db.activeProfile",
		Description: "Report the agent sticky active database profile",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBActiveProfile)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "db.query",
		Description: "Run SQL on the active database profile (gated)",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolDBQuery)
}

func (s *Server) toolDBListProfiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:list", meta("list_profiles", nil), func(context.Context) (any, error) {
		return s.deps.DB.ListProfiles()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type dbTestConnectionInput struct {
	ProfileID string `json:"profileId"`
}

func (s *Server) toolDBTestConnection(ctx context.Context, _ *sdk.CallToolRequest, in dbTestConnectionInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("test_connection", map[string]any{
		"profile_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return nil, s.deps.DB.TestConnection(callCtx, in.ProfileID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolDBActiveProfile(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:active", meta("active_profile", nil), func(context.Context) (any, error) {
		return ActiveProfileResult{ProfileID: s.deps.DB.AgentActiveProfileID()}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type dbQueryInput struct {
	SQL string `json:"sql"`
}

func (s *Server) toolDBQuery(ctx context.Context, _ *sdk.CallToolRequest, in dbQueryInput) (*sdk.CallToolResult, any, error) {
	target := s.databaseTarget()
	// Execute against the exact profile the confirmation covered — re-reading
	// the sticky active target here would race a concurrent SetActiveProfile.
	id, err := GateActiveMCP(ctx, s.deps.Hub, s.deps.Envs, target, target, "db.query", in.SQL, interaction.KindDatabase)
	if err != nil {
		return nil, nil, mapInteractErr(err)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:active", meta("query", map[string]any{
		"profile_id": recordID(id),
	}), func(callCtx context.Context) (any, error) {
		return s.deps.DB.Query(callCtx, id, in.SQL)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
