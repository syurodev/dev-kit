// internal/codegraph/plan.go
package codegraph

// Per-plan-node status values. See base-doc codegraph.mdx "4 trạng thái mỗi
// plan node" — these are computed on read, never stored (see this plan's
// header for why).
const (
	PlanStatusNotImplemented      = "not_implemented"
	PlanStatusPendingEnhancement  = "pending_enhancement"
	PlanStatusImplementedMatching = "implemented_matching"
	PlanStatusImplementedDiverged = "implemented_diverged"
)

// Whole-plan status values, derived from the set of per-node statuses.
const (
	PlanAggregateNotStarted = "not_started"
	PlanAggregateInProgress = "in_progress"
	PlanAggregateComplete   = "complete"
)

// Plan draft/approved lifecycle. A draft plan is inert — it exists to be
// reviewed on the canvas; approval is a human-only bridge action.
const (
	PlanStateDraft    = "draft"
	PlanStateApproved = "approved"
)

// PlanNode is one intended class or method in a proposed flow. FQN is the
// fully-qualified name the agent commits to creating; matching resolves it
// against the real index (see this plan's "FQN matching rule").
type PlanNode struct {
	PlanNodeID  string `json:"planNodeId"`
	Label       string `json:"label"`
	Description string `json:"description"`
	FQN         string `json:"fqn"`
	Kind        string `json:"kind"` // "class" | "method"
}

// PlanEdge is one intended call between two plan nodes. Condition is
// plan-time documentation written by the proposing agent for the human
// reviewing the canvas — it is never machine-verified against real code
// (see the doc's "Điều kiện trên cạnh" section).
type PlanEdge struct {
	From      string `json:"from"` // PlanNode.PlanNodeID
	To        string `json:"to"`   // PlanNode.PlanNodeID
	Condition string `json:"condition"`
}

// Plan is the single active plan for a project.
type Plan struct {
	Description string     `json:"description"`
	State       string     `json:"state"` // "draft" | "approved"
	Nodes       []PlanNode `json:"nodes"`
	Edges       []PlanEdge `json:"edges"`
}

// PlanNodeProgress is one plan node plus its freshly computed status.
type PlanNodeProgress struct {
	PlanNode
	Status string `json:"status"`
	// RealNodeID is the indexed node the FQN resolved to, empty when the
	// status is not_implemented. Lets a caller jump straight to the real
	// code (e.g. via GetCodeSnippet) without re-resolving the FQN itself.
	RealNodeID string `json:"realNodeId"`
}

// PlanProgress is GetPlanProgress's result: every node's status plus the
// derived whole-plan status.
type PlanProgress struct {
	Description string             `json:"description"`
	State       string             `json:"state"`
	Aggregate   string             `json:"aggregate"`
	Nodes       []PlanNodeProgress `json:"nodes"`
}

// planNodeStatus computes one plan node's status from fully resolved inputs.
// Pure by design — no store access — so every branch of the doc's 4-state
// table is unit-testable without a database.
//
//   - resolved maps a plan node ID to the real indexed Node its FQN resolved
//     to; a missing key means that FQN matched nothing in the index.
//   - realOutTargets maps a real node ID to the real node IDs it calls.
func planNodeStatus(planNodeID string, planEdges []PlanEdge, resolved map[string]Node, realOutTargets map[string][]string) string {
	real, ok := resolved[planNodeID]
	if !ok {
		return PlanStatusNotImplemented
	}

	realTargets := make(map[string]bool, len(realOutTargets[real.ID]))
	for _, t := range realOutTargets[real.ID] {
		realTargets[t] = true
	}

	plannedAndPresent := make(map[string]bool)
	for _, e := range planEdges {
		if e.From != planNodeID {
			continue
		}
		target, targetResolved := resolved[e.To]
		if !targetResolved || !realTargets[target.ID] {
			// A planned call that reality does not have yet. The method
			// itself exists (FQN resolved) but the plan asks more of it.
			return PlanStatusPendingEnhancement
		}
		plannedAndPresent[target.ID] = true
	}

	for target := range realTargets {
		if !plannedAndPresent[target] {
			return PlanStatusImplementedDiverged
		}
	}
	return PlanStatusImplementedMatching
}

// aggregateStatus derives the whole-plan status from its nodes' statuses.
// in_progress is the deliberate fallback for combinations the design doc's
// table does not name (e.g. every node pending_enhancement) — see this
// plan's header.
func aggregateStatus(statuses []string) string {
	allNotImplemented := true
	anyUnfinished := false
	for _, s := range statuses {
		if s != PlanStatusNotImplemented {
			allNotImplemented = false
		}
		if s == PlanStatusNotImplemented || s == PlanStatusPendingEnhancement {
			anyUnfinished = true
		}
	}
	switch {
	case !anyUnfinished:
		return PlanAggregateComplete
	case allNotImplemented:
		return PlanAggregateNotStarted
	default:
		return PlanAggregateInProgress
	}
}
