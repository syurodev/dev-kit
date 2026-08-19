// internal/mcp/tools_codegraph_plan.go
package mcp

import (
	"context"

	"devkit/internal/codegraph"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// planFQNInstruction is returned with every getPlannedFlow response. The
// design doc requires the reminder to travel with the data on every call
// rather than relying on the agent to remember the rule from a previous
// turn — FQN matching is the entire basis of progress tracking, and a
// differently-named method silently reads as "not implemented".
const planFQNInstruction = "Implement each node using EXACTLY the package, class, and method name given in its fqn field. " +
	"Progress is tracked by matching these names against the real index — a different name is reported as not implemented. " +
	"If you must use a different name, call codegraph.relinkPlanNode to repoint the plan node first."

func (s *Server) registerCodeGraphPlan() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.proposeFlow",
		Description: "Propose a draft flow of intended classes/methods for a new feature, for a human to review and approve",
	}, s.toolCodeGraphProposeFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getPlannedFlow",
		Description: "Get the active plan's nodes (with the exact FQNs to create), edges, and conditions",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetPlannedFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getPlanProgress",
		Description: "Get per-node implementation status and the overall plan status, diffed against the current index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetPlanProgress)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.relinkPlanNode",
		Description: "Repoint one plan node at a different fully-qualified name, after a deliberate rename",
	}, s.toolCodeGraphRelinkPlanNode)
}

type codeGraphProposeFlowInput struct {
	Description string               `json:"description"`
	Nodes       []codegraph.PlanNode `json:"nodes"`
	Edges       []codegraph.PlanEdge `json:"edges"`
}

func (s *Server) toolCodeGraphProposeFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphProposeFlowInput) (*sdk.CallToolResult, any, error) {
	plan := codegraph.Plan{Description: in.Description, Nodes: in.Nodes, Edges: in.Edges}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("propose_flow", map[string]any{"nodes": len(in.Nodes)}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ProposeFlow(ctx, plan)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

// plannedFlowResult pairs the plan with the naming instruction, so the
// reminder cannot be separated from the data it applies to.
type plannedFlowResult struct {
	Plan        codegraph.Plan `json:"plan"`
	Instruction string         `json:"instruction"`
}

func (s *Server) toolCodeGraphGetPlannedFlow(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("get_planned_flow", nil), func(context.Context) (any, error) {
		plan, err := s.deps.CodeGraph.GetPlannedFlow(ctx)
		if err != nil {
			return nil, err
		}
		return plannedFlowResult{Plan: plan, Instruction: planFQNInstruction}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolCodeGraphGetPlanProgress(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("get_plan_progress", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetPlanProgress(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphRelinkPlanNodeInput struct {
	PlanNodeID string `json:"planNodeId"`
	FQN        string `json:"fqn"`
}

func (s *Server) toolCodeGraphRelinkPlanNode(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphRelinkPlanNodeInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:plan", "plan:active", meta("relink_plan_node", map[string]any{"plan_node_id": in.PlanNodeID, "fqn": in.FQN}), func(context.Context) (any, error) {
		return nil, s.deps.CodeGraph.RelinkPlanNode(ctx, in.PlanNodeID, in.FQN)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
