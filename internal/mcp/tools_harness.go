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
	ProfileWithBudgets(ctx context.Context, f harnessconfig.BudgetFilter) (harnessconfig.ProfileView, error)
	Request(ctx context.Context, req harnessconfig.GroupRequest) (harnessconfig.RequestResult, error)
}

// HarnessGetInput optionally adds a bounded page of the result limits.
type HarnessGetInput struct {
	Budgets bool   `json:"budgets,omitempty" jsonschema:"Also return a page of the workspace's result limits per tool (bytes of host text, not tokens)."`
	Group   string `json:"group,omitempty" jsonschema:"With budgets: only this tool group."`
	Cursor  string `json:"cursor,omitempty" jsonschema:"With budgets: the nextCursor of the previous page, with the same group."`
}

// HarnessBudgetInput is one partial edit of a result budget in YOUR workspace's
// saved profile. A field you leave out is unchanged; a value sets it; a reset
// removes it so the next level applies.
type HarnessBudgetInput struct {
	Kind       string `json:"kind" jsonschema:"\"tool\" or \"group\"."`
	Name       string `json:"name" jsonschema:"The tool (e.g. git.diff) or group (e.g. git)."`
	Bytes      *int   `json:"bytes,omitempty" jsonschema:"New size in bytes, 2048 to 65536."`
	ResetBytes bool   `json:"resetBytes,omitempty" jsonschema:"Remove the size so the group or the 24 KiB default applies."`
	Cut        string `json:"cut,omitempty" jsonschema:"\"head\" or \"headTail\" (keeps the start and the end of plain-text output)."`
	ResetCut   bool   `json:"resetCut,omitempty" jsonschema:"Remove the cut mode so the group or head applies."`
}

// HarnessProposeInput asks to turn tool groups on for the calling session, or to
// edit result budgets of its workspace. The two forms are exclusive.
type HarnessProposeInput struct {
	Groups    []string             `json:"groups,omitempty" jsonschema:"Tool groups to turn on for your own session, e.g. [\"git\"]. Omit to list the groups that are off and what they would cost. Names come from that list."`
	Budgets   []HarnessBudgetInput `json:"budgets,omitempty" jsonschema:"Instead of groups: edits of result budgets in your workspace's saved profile (never a session). Not combinable with groups."`
	Rationale string               `json:"rationale,omitempty" jsonschema:"Why you need them, in a sentence. The person sees it labelled as your own untrusted text; it decides nothing."`
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
			"Or pass `budgets` (not with groups) to edit result budgets: they change your workspace's SAVED profile only, never a session, and take effect when a session is next opened or reloaded. " +
			"It never runs a tool, never grants permission and cannot target another session. If the result says the change was saved but access is paused, do not ask again: the person reconciles it.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: false},
	}), s.toolHarnessPropose)
	s.registerHarnessGroups(ms)
}

func (s *Server) toolHarnessProfileGet(ctx context.Context, _ *sdk.CallToolRequest, in HarnessGetInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.harness", "harness:read", "profile:self", meta("harness_profile_get", map[string]any{"budgets": in.Budgets}), func(callCtx context.Context) (any, error) {
		if in.Budgets {
			return s.deps.Harness.ProfileWithBudgets(callCtx, harnessconfig.BudgetFilter{Group: in.Group, Cursor: in.Cursor})
		}
		return s.deps.Harness.Profile(callCtx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolHarnessPropose(ctx context.Context, _ *sdk.CallToolRequest, in HarnessProposeInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.harness", "harness:propose", "profile:self",
		meta("harness_profile_propose", map[string]any{"groups": len(in.Groups), "budgets": len(in.Budgets)}), func(callCtx context.Context) (any, error) {
			req := harnessconfig.GroupRequest{Groups: in.Groups, Rationale: in.Rationale}
			for _, b := range in.Budgets {
				req.Budgets = append(req.Budgets, harnessconfig.BudgetEdit{Kind: harnessconfig.BudgetKind(b.Kind), Name: b.Name,
					Bytes: b.Bytes, ResetBytes: b.ResetBytes, Cut: b.Cut, ResetCut: b.ResetCut})
			}
			return s.deps.Harness.Request(callCtx, req)
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
