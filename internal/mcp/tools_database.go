package mcp

import (
	"context"
	"time"

	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerDatabase(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.listProfiles",
		Description: "List database connection profiles (metadata only)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBListProfiles)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.testConnection",
		Description: "Test connectivity for a database profile by id (asks first when the profile's env requires confirmation)",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolDBTestConnection)

	// Read-only. db.saveQuery and db.deleteSavedQuery stay off the surface:
	// deciding what is worth keeping is a human act, same rule that keeps
	// DBSetActiveProfile and DBUpdateProfileOptions off it.
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.history.list",
		Description: "List recent statements run against a database profile on this device",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBListQueryHistory)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.savedQuery.list",
		Description: "List saved queries for a database profile",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBListSavedQueries)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.listForeignKeys",
		Description: "Declared foreign keys for a schema — real constraints only, never inferred guesses",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBListForeignKeys)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.schema.get",
		Description: "Read the cached schema index so an agent writes correct table and column names",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBGetSchemaCache)

	// Plan only. db.explainAnalyze does not exist: ANALYZE executes the
	// statement, and DevKit cannot tell whether a statement mutates.
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.explain",
		Description: "Plan a statement without running it",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBExplain)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "db.activeProfile",
		Description: "Report the agent sticky active database profile",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolDBActiveProfile)

	sdk.AddTool(ms, s.gateOwnedTool(&sdk.Tool{
		Name:        "db.query",
		Description: "Run SQL on the active database profile (gated)",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}), s.toolDBQuery)
}

func (s *Server) toolDBListProfiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:list", meta("list_profiles", nil), func(context.Context) (any, error) {
		return s.deps.DB.ListProfiles()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// gateDBProfile runs GateProfileMCP for a database tool that names its own
// profile. Metadata only: on an env that does not require confirmation it
// never prompts.
func (s *Server) gateDBProfile(ctx context.Context, profileID, tool, summary string) error {
	target := s.databaseTarget()
	if err := GateProfileMCP(ctx, s.deps.Hub, s.deps.Envs, target, profileID, tool, summary, interaction.KindDatabase); err != nil {
		return mapInteractErr(err)
	}
	return nil
}

type dbTestConnectionInput struct {
	ProfileID string `json:"profileId"`
}

func (s *Server) toolDBTestConnection(ctx context.Context, _ *sdk.CallToolRequest, in dbTestConnectionInput) (*sdk.CallToolResult, any, error) {
	if err := s.gateDBProfile(ctx, in.ProfileID, "db.testConnection", "test connection"); err != nil {
		return nil, nil, err
	}
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("test_connection", map[string]any{
		"profile_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return nil, s.deps.DB.TestConnection(callCtx, in.ProfileID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolDBActiveProfile(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:active", meta("active_profile", nil), func(context.Context) (any, error) {
		return ActiveProfileResult{ProfileID: s.deps.DB.AgentActiveProfileID()}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// agentQueryTimeout bounds a statement an agent runs through db.query.
const agentQueryTimeout = 5 * time.Minute

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
		// Bounded here, not in the service (#46): the console's statements have
		// no deadline because Stop ends them, and an agent has no Stop.
		callCtx, cancel := context.WithTimeout(callCtx, agentQueryTimeout)
		defer cancel()
		// 0: the service default. An MCP client reads a result, it does not
		// page a grid, so there is no page size to honour here.
		return s.deps.DB.Query(callCtx, id, in.SQL, 0)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type dbProfileScopedInput struct {
	ProfileID string `json:"profileId"`
}

func (s *Server) toolDBListQueryHistory(ctx context.Context, _ *sdk.CallToolRequest, in dbProfileScopedInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("list_query_history", map[string]any{
		"profile_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.DB.ListQueryHistory(in.ProfileID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolDBListSavedQueries(ctx context.Context, _ *sdk.CallToolRequest, in dbProfileScopedInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("list_saved_queries", map[string]any{
		"profile_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.DB.ListSavedQueries(in.ProfileID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type dbSchemaScopedInput struct {
	ProfileID string `json:"profileId"`
	Schema    string `json:"schema"`
}

func (s *Server) toolDBListForeignKeys(ctx context.Context, _ *sdk.CallToolRequest, in dbSchemaScopedInput) (*sdk.CallToolResult, any, error) {
	if err := s.gateDBProfile(ctx, in.ProfileID, "db.listForeignKeys", "foreign keys of schema "+in.Schema); err != nil {
		return nil, nil, err
	}
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("list_foreign_keys", map[string]any{
		"profile_id": opaqueID, "schema": in.Schema,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.DB.ListForeignKeys(callCtx, in.ProfileID, in.Schema)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolDBGetSchemaCache(ctx context.Context, _ *sdk.CallToolRequest, in dbProfileScopedInput) (*sdk.CallToolResult, any, error) {
	// Not only a cache read: on a miss GetSchemaCache indexes the live
	// database, so it opens a connection like the other gated tools.
	if err := s.gateDBProfile(ctx, in.ProfileID, "db.schema.get", "read schema index"); err != nil {
		return nil, nil, err
	}
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("get_schema_cache", map[string]any{
		"profile_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.DB.GetSchemaCache(callCtx, in.ProfileID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type dbExplainInput struct {
	ProfileID string `json:"profileId"`
	SQL       string `json:"sql"`
}

func (s *Server) toolDBExplain(ctx context.Context, _ *sdk.CallToolRequest, in dbExplainInput) (*sdk.CallToolResult, any, error) {
	if err := s.gateDBProfile(ctx, in.ProfileID, "db.explain", in.SQL); err != nil {
		return nil, nil, err
	}
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.database", "db:query", "profile:"+opaqueID, meta("explain", map[string]any{
		"profile_id": opaqueID, "analyzed": false,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.DB.Explain(callCtx, in.ProfileID, in.SQL)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
