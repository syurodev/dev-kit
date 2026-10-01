package mcp

import (
	"context"
	"strings"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AgentHubAPI is the Agent Hub domain surface MCP tools invoke (duck-typed).
type AgentHubAPI interface {
	ListSessions(ctx context.Context) ([]agenthub.AgentSession, error)
	CreateSession(ctx context.Context, in agenthub.CreateInput) (agenthub.AgentSession, error)
	GetStatus(ctx context.Context, id string) (agenthub.AgentSession, error)
	Invoke(ctx context.Context, in agenthub.InvokeInput) (agenthub.InvokeResult, error)
	Cancel(ctx context.Context, id string) error
}

func (s *Server) requireAgentHub() error {
	if s.deps.AgentHub == nil {
		return apperrors.New("internal_error", "Agent Hub is not available.", true)
	}
	return nil
}

func (s *Server) registerAgentHub(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name:        "agenthub.listAgents",
		Description: "List Agent Hub sessions",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAgentHubListAgents)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "agenthub.createAgentSession",
		Description: "Create a new Agent Hub session",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolAgentHubCreateAgentSession)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "agenthub.invokeAgent",
		Description: "Invoke a blocking prompt on an Agent Hub session",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolAgentHubInvokeAgent)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "agenthub.getSessionStatus",
		Description: "Get Agent Hub session status by id",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAgentHubGetSessionStatus)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "agenthub.cancelAgent",
		Description: "Cancel an in-flight Agent Hub prompt",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolAgentHubCancelAgent)
}

func (s *Server) toolAgentHubListAgents(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	if err := s.requireAgentHub(); err != nil {
		return nil, nil, err
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.agenthub", "agenthub:read", "session:list", meta("list_agents", nil), func(callCtx context.Context) (any, error) {
		return s.deps.AgentHub.ListSessions(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type agentHubCreateInput struct {
	AgentType       string  `json:"agentType"`
	Cwd             string  `json:"cwd"`
	WorkspaceID     string  `json:"workspaceId,omitempty"`
	CallerSessionID string  `json:"callerSessionId"`
	Title           *string `json:"title"`
}

func (s *Server) toolAgentHubCreateAgentSession(ctx context.Context, _ *sdk.CallToolRequest, in agentHubCreateInput) (*sdk.CallToolResult, any, error) {
	if err := s.requireAgentHub(); err != nil {
		return nil, nil, err
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.agenthub", "agenthub:execute", "session:new", meta("create_session", map[string]any{
		"agent_type": in.AgentType,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.AgentHub.CreateSession(callCtx, agenthub.CreateInput{
			AgentType:       agenthub.AgentType(in.AgentType),
			Cwd:             in.Cwd,
			WorkspaceID:     in.WorkspaceID,
			ParentSessionID: in.CallerSessionID,
			Title:           in.Title,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type agentHubInvokeInput struct {
	SessionID       string `json:"sessionId"`
	Prompt          string `json:"prompt"`
	CallerSessionID string `json:"callerSessionId"`
}

func (s *Server) toolAgentHubInvokeAgent(ctx context.Context, _ *sdk.CallToolRequest, in agentHubInvokeInput) (*sdk.CallToolResult, any, error) {
	if err := s.requireAgentHub(); err != nil {
		return nil, nil, err
	}
	opaqueID := hubSessionID(in.SessionID)
	v, err := s.deps.Caller.Call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, meta("invoke", map[string]any{
		"session_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.AgentHub.Invoke(callCtx, agenthub.InvokeInput{
			SessionID:       in.SessionID,
			Prompt:          in.Prompt,
			CallerSessionID: in.CallerSessionID,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type agentHubSessionIDInput struct {
	SessionID string `json:"sessionId"`
}

func (s *Server) toolAgentHubGetSessionStatus(ctx context.Context, _ *sdk.CallToolRequest, in agentHubSessionIDInput) (*sdk.CallToolResult, any, error) {
	if err := s.requireAgentHub(); err != nil {
		return nil, nil, err
	}
	opaqueID := hubSessionID(in.SessionID)
	v, err := s.deps.Caller.Call(ctx, "builtin.agenthub", "agenthub:read", "session:"+opaqueID, meta("get_status", map[string]any{
		"session_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.AgentHub.GetStatus(callCtx, in.SessionID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolAgentHubCancelAgent(ctx context.Context, _ *sdk.CallToolRequest, in agentHubSessionIDInput) (*sdk.CallToolResult, any, error) {
	if err := s.requireAgentHub(); err != nil {
		return nil, nil, err
	}
	opaqueID := hubSessionID(in.SessionID)
	v, err := s.deps.Caller.Call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, meta("cancel", map[string]any{
		"session_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return nil, s.deps.AgentHub.Cancel(callCtx, in.SessionID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// hubSessionID validates a hub session UUID for capability scopes / audit metadata.
// Agent hub sessions use google/uuid strings, not vaultitem 24-char hex ids.
// Invalid values become "invalid" so the capability scope never carries raw input.
func hubSessionID(value string) string {
	if len(value) != 36 {
		return "invalid"
	}
	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return "invalid"
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", char) {
				return "invalid"
			}
		}
	}
	return value
}
