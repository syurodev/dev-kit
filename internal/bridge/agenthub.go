package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"devkit/internal/agenthub"
)

var errAgentHubUnavailable = errors.New("agenthub is not available")

func (s *AppService) agentHubService() (*agenthub.Service, error) {
	if s.container.AgentHub == nil {
		return nil, errAgentHubUnavailable
	}
	return s.container.AgentHub, nil
}

func (s *AppService) agentHubPermissions() (*agenthub.PermissionWaiter, error) {
	if s.container.AgentHubPermissions == nil {
		return nil, errAgentHubUnavailable
	}
	return s.container.AgentHubPermissions, nil
}

// hubSessionID validates a hub session UUID for capability scopes / audit metadata.
// Agent hub sessions use google/uuid strings, not vaultitem 24-char hex ids.
func hubSessionID(value string) string {
	if len(value) != 36 {
		return invalidOpaqueID
	}
	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return invalidOpaqueID
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", char) {
				return invalidOpaqueID
			}
		}
	}
	return value
}

func (s *AppService) AgentHubListAgents() ([]agenthub.AgentSession, error) {
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:read", "session:list", map[string]any{
		"action": "list_agents",
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListSessions(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.AgentSession), nil
}

func (s *AppService) AgentHubCreateSession(input agenthub.CreateInput) (agenthub.AgentSession, error) {
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:new", map[string]any{
		"action":     "create_session",
		"agent_type": string(input.AgentType),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.CreateSession(ctx, input)
	})
	if err != nil {
		return agenthub.AgentSession{}, err
	}
	return result.(agenthub.AgentSession), nil
}

func (s *AppService) AgentHubGetStatus(id string) (agenthub.AgentSession, error) {
	opaqueID := hubSessionID(id)
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action":     "get_status",
		"session_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.GetStatus(ctx, id)
	})
	if err != nil {
		return agenthub.AgentSession{}, err
	}
	return result.(agenthub.AgentSession), nil
}

func (s *AppService) AgentHubInvoke(ctx context.Context, input agenthub.InvokeInput) (agenthub.InvokeResult, error) {
	opaqueID := hubSessionID(input.SessionID)
	result, err := s.Call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "invoke",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.Invoke(callCtx, input)
	})
	if err != nil {
		return agenthub.InvokeResult{}, err
	}
	return result.(agenthub.InvokeResult), nil
}

func (s *AppService) AgentHubCancel(ctx context.Context, id string) error {
	opaqueID := hubSessionID(id)
	_, err := s.Call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "cancel",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.Cancel(callCtx, id)
	})
	return err
}

func (s *AppService) AgentHubListVendorTypes() ([]agenthub.AgentType, error) {
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:read", "vendor:list", map[string]any{
		"action": "list_vendor_types",
	}, func(context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListVendorTypes(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.AgentType), nil
}

// AgentHubResolvePermission completes a pending ACP permission prompt from the UI.
func (s *AppService) AgentHubResolvePermission(id string, outcome json.RawMessage) error {
	opaqueID := recordID(id)
	_, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:execute", "permission:complete", map[string]any{
		"action": "resolve_permission",
		"id":     opaqueID,
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		return nil, waiter.Complete(id, outcome)
	})
	return err
}

// AgentHubPendingPermissions lists every ACP permission prompt awaiting the
// user, oldest first. The frontend uses it to rebuild its queue on mount;
// live arrivals still come through the agenthub:permission event.
func (s *AppService) AgentHubPendingPermissions() ([]agenthub.PendingPermission, error) {
	result, err := s.Call(context.Background(), "builtin.agenthub", "agenthub:read", "permission:list", map[string]any{
		"action": "pending_permissions",
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		return waiter.PendingList(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.PendingPermission), nil
}
