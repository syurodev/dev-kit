// internal/codegraph/plan_test.go
package codegraph

import "testing"

// resolvedFrom builds the planNodeID -> real Node map the way the Service
// does, but by hand so these tests stay pure (no store, no SQLite).
func resolvedFrom(pairs map[string]string) map[string]Node {
	out := make(map[string]Node, len(pairs))
	for planNodeID, realID := range pairs {
		out[planNodeID] = Node{ID: realID, Label: realID}
	}
	return out
}

func TestPlanNodeStatusNotImplementedWhenFQNUnresolved(t *testing.T) {
	got := planNodeStatus("pn-1", nil, map[string]Node{}, map[string][]string{})
	if got != PlanStatusNotImplemented {
		t.Fatalf("status = %q, want %q", got, PlanStatusNotImplemented)
	}
}

func TestPlanNodeStatusMatchingWhenNoOutEdgesPlanned(t *testing.T) {
	// A plan node referencing an existing method that the plan asks nothing
	// more of — matching immediately, per the doc's "Node tham chiếu method
	// cũ chỉ implemented_matching ngay lập tức nếu plan không yêu cầu thêm
	// cạnh nào từ nó".
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", nil, resolved, map[string][]string{})
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestPlanNodeStatusPendingEnhancementWhenPlannedEdgeMissing(t *testing.T) {
	// pn-1 exists in real code, pn-2 exists too, but the real call from
	// A.foo -> B.bar that the plan requires has not been written yet.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusPendingEnhancementWhenEdgeTargetNotImplemented(t *testing.T) {
	// The planned target itself does not exist yet, so the edge cannot be
	// satisfied — per the doc, edges need no status of their own, this is
	// inferred from the two endpoint nodes.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusMatchingWhenPlannedEdgeSatisfied(t *testing.T) {
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestPlanNodeStatusDivergedOnExtraRealEdge(t *testing.T) {
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar"})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar", "com.acme.Logger.debug"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusImplementedDiverged {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedDiverged)
	}
}

func TestPlanNodeStatusMissingEdgeWinsOverExtraEdge(t *testing.T) {
	// One planned edge satisfied, one not, plus an unplanned extra. The
	// unfinished work is the more important signal — pending_enhancement,
	// not diverged.
	edges := []PlanEdge{{From: "pn-1", To: "pn-2"}, {From: "pn-1", To: "pn-3"}}
	resolved := resolvedFrom(map[string]string{
		"pn-1": "com.acme.A.foo", "pn-2": "com.acme.B.bar", "pn-3": "com.acme.C.baz",
	})
	realOut := map[string][]string{"com.acme.A.foo": {"com.acme.B.bar", "com.acme.Logger.debug"}}
	got := planNodeStatus("pn-1", edges, resolved, realOut)
	if got != PlanStatusPendingEnhancement {
		t.Fatalf("status = %q, want %q", got, PlanStatusPendingEnhancement)
	}
}

func TestPlanNodeStatusIgnoresEdgesFromOtherNodes(t *testing.T) {
	// An unsatisfied edge out of pn-9 must not drag pn-1 down.
	edges := []PlanEdge{{From: "pn-9", To: "pn-2"}}
	resolved := resolvedFrom(map[string]string{"pn-1": "com.acme.A.foo"})
	got := planNodeStatus("pn-1", edges, resolved, map[string][]string{})
	if got != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want %q", got, PlanStatusImplementedMatching)
	}
}

func TestAggregateStatus(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		want     string
	}{
		{"all not implemented", []string{PlanStatusNotImplemented, PlanStatusNotImplemented}, PlanAggregateNotStarted},
		{"all matching", []string{PlanStatusImplementedMatching, PlanStatusImplementedMatching}, PlanAggregateComplete},
		{"matching and diverged", []string{PlanStatusImplementedMatching, PlanStatusImplementedDiverged}, PlanAggregateComplete},
		{"mixed done and not", []string{PlanStatusImplementedMatching, PlanStatusNotImplemented}, PlanAggregateInProgress},
		{"pending plus done", []string{PlanStatusPendingEnhancement, PlanStatusImplementedMatching}, PlanAggregateInProgress},
		// Spec gap, filled by making in_progress the fallback: every node is
		// pending_enhancement, so it is neither complete nor not_started.
		{"all pending enhancement", []string{PlanStatusPendingEnhancement, PlanStatusPendingEnhancement}, PlanAggregateInProgress},
		// Also a fallback case: nothing finished, but not everything is
		// untouched either.
		{"not implemented plus pending", []string{PlanStatusNotImplemented, PlanStatusPendingEnhancement}, PlanAggregateInProgress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateStatus(tc.statuses); got != tc.want {
				t.Fatalf("aggregateStatus(%v) = %q, want %q", tc.statuses, got, tc.want)
			}
		})
	}
}
