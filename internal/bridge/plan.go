// internal/bridge/plan.go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

func (s *AppService) CodeGraphProposeFlow(plan codegraph.Plan) (codegraph.PlanProgress, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "propose_flow",
		"nodes":  len(plan.Nodes),
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ProposeFlow(ctx, plan)
	})
	if err != nil {
		return codegraph.PlanProgress{}, err
	}
	return result.(codegraph.PlanProgress), nil
}

// CodeGraphApprovePlan is human-only — exposed on the bridge for the UI,
// deliberately absent from the MCP tool surface.
func (s *AppService) CodeGraphApprovePlan() error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "approve_plan",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.ApprovePlan(ctx)
	})
	return err
}

func (s *AppService) CodeGraphGetPlannedFlow() (codegraph.Plan, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "get_planned_flow",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetPlannedFlow(ctx)
	})
	if err != nil {
		return codegraph.Plan{}, err
	}
	return result.(codegraph.Plan), nil
}

func (s *AppService) CodeGraphGetPlanProgress() (codegraph.PlanProgress, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "get_plan_progress",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetPlanProgress(ctx)
	})
	if err != nil {
		return codegraph.PlanProgress{}, err
	}
	return result.(codegraph.PlanProgress), nil
}

func (s *AppService) CodeGraphRelinkPlanNode(planNodeID, fqn string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action":       "relink_plan_node",
		"plan_node_id": planNodeID,
		"fqn":          fqn,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.RelinkPlanNode(ctx, planNodeID, fqn)
	})
	return err
}

// CodeGraphCommentPlanNode is the human write. The body is deliberately absent
// from the audit payload — comment text is content, not a parameter worth
// recording on every call, the same reason CodeGraphWriteFile omits content.
func (s *AppService) CodeGraphCommentPlanNode(planNodeID, body string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action":       "comment_plan_node",
		"plan_node_id": planNodeID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.CommentPlanNode(ctx, planNodeID, body)
	})
	return err
}

func (s *AppService) CodeGraphListPlanNodeComments(planNodeID string) ([]codegraph.PlanComment, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action":       "list_plan_node_comments",
		"plan_node_id": planNodeID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListPlanNodeComments(ctx, planNodeID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.PlanComment), nil
}

func (s *AppService) CodeGraphCountPlanNodeComments() (map[string]int, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:plan", "plan:active", map[string]any{
		"action": "count_plan_node_comments",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.CountPlanNodeComments(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]int), nil
}
