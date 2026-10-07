package agenthub

import (
	"context"
	"encoding/json"
	"strings"

	"devkit/internal/codexapp"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

// ConfirmTool is the gate transport into the agent permission broker. The gate
// still owns policy and executes only after it validates this returned decision.
func (s *Service) ConfirmTool(ctx context.Context, req interaction.ConfirmRequest) (string, error) {
	caller, ok := identity.FromContext(ctx)
	if !ok || !caller.IsAgent() {
		return "", interaction.ErrConfirmationDenied
	}
	if sess, err := s.store.GetSession(ctx, caller.SessionID); err != nil || sess.Status == SessionStatusClosed {
		return "", interaction.ErrConfirmationDenied
	}
	profileName := req.ProfileID
	if s.GateTargetName != nil {
		profileName = s.GateTargetName(string(req.Kind), req.ProfileID)
	}
	options := []map[string]string{{"optionId": "allow", "name": "Allow once", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}}
	if !req.EnvBlocks {
		options = append(options, map[string]string{"optionId": "allow_always", "name": "Allow always", "kind": "allow_always"})
	}
	params, _ := json.Marshal(map[string]any{
		"sessionId": caller.SessionID, "origin": "devkit_gate", "env": req.Env, "locked": req.EnvBlocks,
		"profileId": req.ProfileID, "profileName": profileName, "kind": req.Kind, "tool": req.Tool, "summary": req.Summary,
		"toolCall": map[string]any{"title": req.Tool, "kind": "other", "rawInput": map[string]string{"command": req.Summary}},
		"options":  options,
	})
	outcome, err := s.mediatePermissionWith(ctx, caller.SessionID, params, s.ResolveGatePermission)
	if err != nil {
		return "", interaction.ErrConfirmationDenied
	}
	if ctx.Err() != nil {
		return "", interaction.ErrConfirmationDenied
	}
	if sess, err := s.store.GetSession(ctx, caller.SessionID); err != nil || sess.Status == SessionStatusClosed {
		return "", interaction.ErrConfirmationDenied
	}
	var response struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(outcome, &response) != nil || response.Outcome.Outcome != "selected" {
		return "", interaction.ErrConfirmationDenied
	}
	switch response.Outcome.OptionID {
	case "allow":
		return "allow", nil
	case "allow_always":
		if !req.EnvBlocks {
			return "allow_always", nil
		}
	}
	return "", interaction.ErrConfirmationDenied
}

func (r *codexRuntime) delegatesMCPGate(params json.RawMessage) bool {
	var request codexapp.McpElicitationParams
	if json.Unmarshal(params, &request) != nil {
		return false
	}
	var meta struct {
		Kind string `json:"codex_approval_kind"`
		Tool string `json:"tool_name"`
	}
	if json.Unmarshal(request.Meta, &meta) != nil || meta.Kind != codexapp.McpApprovalKindToolCall {
		return false
	}
	_, call, known := r.tr.openMcpCall(request.ServerName)
	if meta.Tool == "" {
		if !known {
			return false
		}
		meta.Tool = call.McpTool
	} else if known && call.McpTool != meta.Tool {
		return false
	}
	for _, tool := range r.gateOwnedMCP[request.ServerName] {
		if tool == meta.Tool {
			return true
		}
	}
	return false
}

func delegatedClaudeTools(spec OpenSpec) []string {
	var names []string
	for _, server := range spec.McpServers {
		for _, tool := range spec.GateOwnedMCP[server.Name] {
			names = append(names, "mcp__"+server.Name+"__"+strings.ReplaceAll(tool, ".", "_"))
		}
	}
	return names
}
