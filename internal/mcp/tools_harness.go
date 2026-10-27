package mcp

import (
	"context"
	"encoding/json"

	"devkit/internal/apperrors"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// HarnessAPI is the public tool-group controls as the harness.profile.* tools use
// them. The implementation derives the session and workspace from the caller's
// authenticated identity; nothing here (and nothing in a tool argument) names a
// target.
type HarnessAPI interface {
	Profile(ctx context.Context) (harnessconfig.ProfileView, error)
	Request(ctx context.Context, req harnessconfig.GroupRequest) (harnessconfig.RequestResult, error)
}

// HarnessProposeInput asks to turn tool groups on for the calling session.
type HarnessProposeInput struct {
	Groups    []string `json:"groups,omitempty" jsonschema:"Tool groups to turn on for your own session, e.g. [\"git\"]. Omit to list the groups that are off and what they would cost. Names come from that list."`
	Rationale string   `json:"rationale,omitempty" jsonschema:"Why you need them, in a sentence. The person sees it labelled as your own untrusted text; it decides nothing."`
}

// registerHarness registers the two tools. They exist only when the controls are
// wired (Deps.Harness), so an agent is never offered a tool that cannot answer.
func (s *Server) registerHarness(ms *sdk.Server) {
	if s.deps.Harness == nil {
		return
	}
	sdk.AddTool(ms, &sdk.Tool{
		Name: "harness.profile.get",
		Description: "Show which DevKit tool groups your session has, each with its tool count and definition size, " +
			"what the saved configuration gives a launch, and what is running now. " +
			"Sizes are bytes of tool definitions, not tokens or cost.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolHarnessProfileGet)
	sdk.AddTool(ms, s.gateOwnedTool(&sdk.Tool{
		Name: harness.ProposeTool,
		Description: "Ask the person to turn DevKit tool groups on for your own session. Without `groups` it only lists the groups that are off. " +
			"With `groups` the person chooses this session only or the workspace, reviews the exact change and approves or declines; nothing changes without that. " +
			"It never runs a tool, never grants permission and cannot target another session. If the result says the change was saved but access is paused, do not ask again: the person reconciles it.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: false},
	}), s.toolHarnessPropose)
}

func (s *Server) toolHarnessProfileGet(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.harness", "harness:read", "profile:self", meta("harness_profile_get", nil), func(callCtx context.Context) (any, error) {
		return s.deps.Harness.Profile(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolHarnessPropose(ctx context.Context, _ *sdk.CallToolRequest, in HarnessProposeInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.harness", "harness:propose", "profile:self",
		meta("harness_profile_propose", map[string]any{"groups": len(in.Groups)}), func(callCtx context.Context) (any, error) {
			return s.deps.Harness.Request(callCtx, harnessconfig.GroupRequest{Groups: in.Groups, Rationale: in.Rationale})
		})
	if err != nil {
		return nil, nil, err
	}
	res, ok := v.(harnessconfig.RequestResult)
	if !ok {
		return nil, nil, apperrors.New("internal_error", "The harness controls returned an unexpected answer.", false)
	}
	// A change that was saved but could not be published, or whose commit is not
	// known, is a failure response that still carries its facts: the structured
	// outcome is the same object a success returns, so a client cannot mistake it
	// for "nothing happened" and propose again.
	if res.Outcome != nil && (res.Outcome.Status == harnessconfig.OutcomePaused || res.Outcome.Status == harnessconfig.OutcomeUnknown) {
		raw, _ := json.Marshal(res)
		return &sdk.CallToolResult{
			IsError:           true,
			Content:           []sdk.Content{&sdk.TextContent{Text: res.Outcome.Message + " " + res.Outcome.Recovery}},
			StructuredContent: json.RawMessage(raw),
		}, nil, nil
	}
	return nil, structured(res), nil
}
