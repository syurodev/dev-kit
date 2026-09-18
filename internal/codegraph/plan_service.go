// internal/codegraph/plan_service.go
package codegraph

import (
	"context"
	"fmt"
)

// validatePlan rejects internally inconsistent plans at the boundary, the
// same way AnalysisResult.Validate rejects bad analyzer output — a bad plan
// caught here beats a confusing failure three layers later.
func validatePlan(plan Plan) error {
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("%w: plan has no nodes", ErrInvalidPlan)
	}
	ids := make(map[string]bool, len(plan.Nodes))
	for _, n := range plan.Nodes {
		if n.PlanNodeID == "" {
			return fmt.Errorf("%w: plan node with empty id", ErrInvalidPlan)
		}
		if ids[n.PlanNodeID] {
			return fmt.Errorf("%w: duplicate plan node id %q", ErrInvalidPlan, n.PlanNodeID)
		}
		if n.FQN == "" {
			return fmt.Errorf("%w: plan node %q has no fqn", ErrInvalidPlan, n.PlanNodeID)
		}
		ids[n.PlanNodeID] = true
	}
	for _, e := range plan.Edges {
		if !ids[e.From] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.To)
		}
	}
	return nil
}

// ProposeFlow replaces the project's active plan with a new draft and
// returns its progress immediately. The returned progress can already be
// `complete` when every planned method exists and the flow already matches
// real code — the doc's explicit special case, where no approve/implement
// cycle is needed at all.
func (s *Service) ProposeFlow(ctx context.Context, plan Plan) (PlanProgress, error) {
	store, release, err := s.projectStore()
	if err != nil {
		return PlanProgress{}, err
	}
	defer release()
	if err := validatePlan(plan); err != nil {
		return PlanProgress{}, err
	}

	plan.State = PlanStateDraft
	if err := store.SavePlan(ctx, plan); err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// GetPlannedFlow returns the active plan as proposed, without computing
// statuses — the shape an implementing agent needs (intended FQNs, edges,
// conditions), not a progress report.
func (s *Service) GetPlannedFlow(ctx context.Context) (Plan, error) {
	store, release, err := s.projectStore()
	if err != nil {
		return Plan{}, err
	}
	defer release()
	return store.GetPlan(ctx)
}

// computeProgress resolves every plan node's FQN against the current index,
// collects each resolved node's real out-edges, and runs the pure status
// functions over the result. Statuses are always computed here, never
// stored — so any re-index (including the background one) is reflected on
// the very next call with no recompute hook anywhere.
func computeProgress(ctx context.Context, store Store, plan Plan) (PlanProgress, error) {
	resolved := make(map[string]Node, len(plan.Nodes))
	realOutTargets := make(map[string][]string, len(plan.Nodes))
	for _, n := range plan.Nodes {
		real, ok, err := store.ResolveFQN(ctx, n.FQN)
		if err != nil {
			return PlanProgress{}, err
		}
		if !ok {
			continue
		}
		resolved[n.PlanNodeID] = real
		if _, seen := realOutTargets[real.ID]; seen {
			continue // two plan nodes resolved to the same real node
		}
		edges, err := store.GetOutEdges(ctx, real.ID)
		if err != nil {
			return PlanProgress{}, err
		}
		targets := make([]string, 0, len(edges))
		for _, e := range edges {
			if e.Kind != "call" {
				continue
			}
			targets = append(targets, e.To)
		}
		realOutTargets[real.ID] = targets
	}

	progress := PlanProgress{
		Description: plan.Description,
		State:       plan.State,
		Nodes:       make([]PlanNodeProgress, 0, len(plan.Nodes)),
	}
	statuses := make([]string, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		status := planNodeStatus(n.PlanNodeID, plan.Edges, resolved, realOutTargets)
		statuses = append(statuses, status)
		progress.Nodes = append(progress.Nodes, PlanNodeProgress{
			PlanNode:   n,
			Status:     status,
			RealNodeID: resolved[n.PlanNodeID].ID,
		})
	}
	progress.Aggregate = aggregateStatus(statuses)
	return progress, nil
}

// GetPlanProgress returns per-node status plus the derived whole-plan
// status. Statuses are computed fresh against the current index on every
// call, so progress reflects the latest re-index automatically.
func (s *Service) GetPlanProgress(ctx context.Context) (PlanProgress, error) {
	store, release, err := s.projectStore()
	if err != nil {
		return PlanProgress{}, err
	}
	defer release()
	plan, err := store.GetPlan(ctx)
	if err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// ApprovePlan moves the active plan from draft to approved. Human-only by
// design — the bridge exposes it, MCP deliberately does not (see the doc's
// Bridge & MCP table: approving a plan is always a human decision).
func (s *Service) ApprovePlan(ctx context.Context) error {
	store, release, err := s.projectStore()
	if err != nil {
		return err
	}
	defer release()
	return store.SetPlanState(ctx, PlanStateApproved)
}

// RelinkPlanNode repoints one plan node at a different FQN — the escape
// hatch for the deliberate "matching by FQN breaks on rename" tradeoff.
// Explicit by design: DevKit never guesses that a new name is a renamed old
// one (see the doc's non-goals).
//
// Relinking an APPROVED plan takes it back to draft. Approval is the human's
// decision about what gets built (codegraph-plan.mdx: approvePlan is
// human-only, and it is what gives a plan effect), and relink is an MCP
// write an agent can make on its own — so it may change the plan, but not
// the approval of what it changed into. The person re-approves on the canvas,
// where the relinked node is visible. Relinking to the FQN a node already has
// changes nothing.
func (s *Service) RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error {
	if fqn == "" {
		return fmt.Errorf("%w: relink requires a non-empty fqn", ErrInvalidPlan)
	}
	store, release, err := s.projectStore()
	if err != nil {
		return err
	}
	defer release()
	_, err = store.SetPlanNodeFQN(ctx, planNodeID, fqn)
	return err
}
