// internal/bridge/plan_test.go
package bridge

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/codegraph"
)

func newPlanTestService(t *testing.T, analysis codegraph.AnalysisResult) *AppService {
	t.Helper()
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: analysis})
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), t.TempDir()+"/index.db", "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	return s
}

func TestCodeGraphProposeApproveAndProgress(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})

	plan := codegraph.Plan{
		Description: "Refund flow",
		Nodes: []codegraph.PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.A.create", Kind: "method"},
		},
	}
	progress, err := s.CodeGraphProposeFlow(plan)
	if err != nil {
		t.Fatalf("CodeGraphProposeFlow: %v", err)
	}
	if progress.State != codegraph.PlanStateDraft || progress.Aggregate != codegraph.PlanAggregateNotStarted {
		t.Fatalf("progress = %+v", progress)
	}

	flow, err := s.CodeGraphGetPlannedFlow()
	if err != nil {
		t.Fatalf("CodeGraphGetPlannedFlow: %v", err)
	}
	if len(flow.Nodes) != 1 || flow.Nodes[0].FQN != "com.acme.A.create" {
		t.Fatalf("flow = %+v", flow)
	}

	if err := s.CodeGraphApprovePlan(); err != nil {
		t.Fatalf("CodeGraphApprovePlan: %v", err)
	}
	progress, err = s.CodeGraphGetPlanProgress()
	if err != nil {
		t.Fatalf("CodeGraphGetPlanProgress: %v", err)
	}
	if progress.State != codegraph.PlanStateApproved {
		t.Fatalf("state = %q, want approved", progress.State)
	}
}

func TestCodeGraphRelinkPlanNodeBridge(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})
	plan := codegraph.Plan{
		Description: "x",
		Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}},
	}
	if _, err := s.CodeGraphProposeFlow(plan); err != nil {
		t.Fatalf("CodeGraphProposeFlow: %v", err)
	}

	if err := s.CodeGraphRelinkPlanNode("pn-1", "com.acme.A.b"); err != nil {
		t.Fatalf("CodeGraphRelinkPlanNode: %v", err)
	}
	flow, err := s.CodeGraphGetPlannedFlow()
	if err != nil {
		t.Fatalf("CodeGraphGetPlannedFlow: %v", err)
	}
	if flow.Nodes[0].FQN != "com.acme.A.b" {
		t.Fatalf("fqn = %q", flow.Nodes[0].FQN)
	}
}

func TestCodeGraphGetPlanProgressWithoutPlanFails(t *testing.T) {
	s := newPlanTestService(t, codegraph.AnalysisResult{})
	if _, err := s.CodeGraphGetPlanProgress(); !errors.Is(err, codegraph.ErrNoPlan) {
		t.Fatalf("expected ErrNoPlan, got %v", err)
	}
}
