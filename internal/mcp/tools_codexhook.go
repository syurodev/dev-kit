package mcp

import (
	"context"

	"devkit/internal/codexhook"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerCodexHook exposes the one tool codexhook.MCPToolName names: what
// DevKit's own MCP-server install writes into an external Codex CLI's
// `~/.codex/config.toml` under [hooks], for its lifecycle hooks to call back
// into.
//
// This is the entire mechanism Layer 1 rests on: DevKit does not spawn Codex,
// does not sandbox it, and is not on its stdio. It only asks to be told.
func (s *Server) registerCodexHook(ms *sdk.Server) {
	if s.deps.CodexHook == nil {
		return
	}
	sdk.AddTool(ms, &sdk.Tool{
		Name: codexhook.MCPToolName,
		Description: "Called by Codex's own lifecycle hooks to report what a session did. " +
			"DevKit records it for later viewing and always answers to continue — " +
			"this tool never blocks a Codex turn or changes its outcome.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodexHookEvent)
}

// toolCodexHookEvent answers every well-formed call the same way: recorded,
// continue. A call that fails Validate — an event DevKit never registered, no
// session id — is refused with an error Codex logs and moves on from; see
// codexhook's package doc on why a failing hook costs Codex a warning and
// nothing else.
//
// The input type is codexhook.Call itself, unmarshaled by the SDK from the
// tool's JSON arguments using the schema it reflects from that struct — not
// json.RawMessage, which reflects to a bare string/array schema and AddTool
// rejects outright ("input schema must have type object").
//
// It does not go through the Capability Gateway's confirmation path: nothing
// here is a decision an environment or a guard could gate, only a fact being
// written down, and Record itself is nil-safe when nothing is wired.
func (s *Server) toolCodexHookEvent(ctx context.Context, _ *sdk.CallToolRequest, call codexhook.Call) (*sdk.CallToolResult, any, error) {
	if err := codexhook.Validate(call); err != nil {
		return nil, nil, err
	}
	_, err := s.deps.Caller.Call(ctx, "builtin.codexhook", "codexhook:report", "event", meta("hook_event", map[string]any{
		"codex_event": string(call.Event),
		"session_id":  call.SessionID,
	}), func(context.Context) (any, error) {
		return nil, s.deps.CodexHook.Record(call)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(codexhook.Observed()), nil
}
