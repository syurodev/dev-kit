package app

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/codegraph"
	"devkit/internal/tasks"
)

// A draft is a proposal under discussion. Turning one into a backlog would put
// work on the board that nobody agreed to.
func TestSeedsFromPlanRefusesADraft(t *testing.T) {
	draft := codegraph.Plan{
		State: codegraph.PlanStateDraft,
		Nodes: []codegraph.PlanNode{{PlanNodeID: "n1", Label: "RefundService.refund"}},
	}
	if _, err := seedsFromPlan(draft); !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("seedsFromPlan(draft) = %v, want ErrPlanNotApproved", err)
	}
}

func TestSeedsFromPlanCarriesLabelAndDescription(t *testing.T) {
	approved := codegraph.Plan{
		State: codegraph.PlanStateApproved,
		Nodes: []codegraph.PlanNode{
			{PlanNodeID: "n1", Label: "RefundService.refund", Description: "issue the refund"},
			{PlanNodeID: "n2", Label: "RefundController.post"},
		},
	}
	seeds, err := seedsFromPlan(approved)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 2 {
		t.Fatalf("seeds = %+v", seeds)
	}
	if seeds[0] != (tasks.PlanSeed{PlanNodeID: "n1", Label: "RefundService.refund", Description: "issue the refund"}) {
		t.Fatalf("seed = %+v", seeds[0])
	}
	// An approved plan with no nodes is an empty backlog, not an error.
	empty, err := seedsFromPlan(codegraph.Plan{State: codegraph.PlanStateApproved})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty approved plan = %+v, %v", empty, err)
	}
}

// Only the status crosses: Tasks never sees an FQN, a node kind or an edge.
func TestStatusesFromProgressNarrowsToTheOneFactTasksActsOn(t *testing.T) {
	progress := codegraph.PlanProgress{
		State: codegraph.PlanStateApproved,
		Nodes: []codegraph.PlanNodeProgress{
			{PlanNode: codegraph.PlanNode{PlanNodeID: "n1", FQN: "com.x.RefundService#refund"},
				Status: codegraph.PlanStatusImplementedMatching},
			{PlanNode: codegraph.PlanNode{PlanNodeID: "n2"},
				Status: codegraph.PlanStatusNotImplemented},
		},
	}
	statuses := statusesFromProgress(progress)
	if statuses["n1"] != tasks.PlanImplementedMatching {
		t.Fatalf("n1 = %q", statuses["n1"])
	}
	if statuses["n2"] != codegraph.PlanStatusNotImplemented {
		t.Fatalf("n2 = %q", statuses["n2"])
	}
}

// A nil *PlanBacklog inside an interface is not a nil interface, so a
// constructor returning one would sail past the caller's != nil check and
// panic on first use. The constructor never returns nil, and an unwired
// adapter reports instead.
func TestPlanBacklogIsNeverATypedNilInAnInterface(t *testing.T) {
	var backlog interface {
		CreateTasksFromPlan(ctx context.Context, boardID string) ([]tasks.Task, error)
	} = NewPlanBacklog(nil)

	if backlog == nil {
		t.Fatal("the interface is nil, so the caller would skip registration")
	}
	// The point: calling it must report, not panic.
	if _, err := backlog.CreateTasksFromPlan(context.Background(), "board"); !errors.Is(err, ErrPlanNotApproved) {
		t.Fatalf("an unwired adapter returned %v", err)
	}
}
