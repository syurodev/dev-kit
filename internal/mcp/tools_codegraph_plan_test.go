// internal/mcp/tools_codegraph_plan_test.go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/codegraph"
)

func TestToolCodeGraphProposeFlow(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{planProgress: codegraph.PlanProgress{
			State:     codegraph.PlanStateDraft,
			Aggregate: codegraph.PlanAggregateNotStarted,
			Nodes:     []codegraph.PlanNodeProgress{{Status: codegraph.PlanStatusNotImplemented}},
		}},
	}
	s := NewServer(deps)
	in := codeGraphProposeFlowInput{
		Description: "Refund flow",
		Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.A.create", Kind: "method"}},
	}
	_, v, err := s.toolCodeGraphProposeFlow(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("toolCodeGraphProposeFlow: %v", err)
	}
	progress, ok := unwrap(v).(codegraph.PlanProgress)
	if !ok || progress.Aggregate != codegraph.PlanAggregateNotStarted {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphGetPlannedFlowRemindsAgentOfFQNs(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{plan: codegraph.Plan{
			Description: "Refund flow",
			Nodes:       []codegraph.PlanNode{{PlanNodeID: "pn-1", FQN: "com.acme.A.create", Kind: "method"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetPlannedFlow(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphGetPlannedFlow: %v", err)
	}
	out, ok := unwrap(v).(plannedFlowResult)
	if !ok {
		t.Fatalf("unexpected result type: %#v", v)
	}
	if out.Instruction == "" {
		t.Fatal("expected a non-empty instruction reminding the agent to use the planned FQNs exactly")
	}
	if len(out.Plan.Nodes) != 1 || out.Plan.Nodes[0].FQN != "com.acme.A.create" {
		t.Fatalf("plan = %+v", out.Plan)
	}
}

func TestToolCodeGraphGetPlanProgress(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{planProgress: codegraph.PlanProgress{Aggregate: codegraph.PlanAggregateComplete}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetPlanProgress(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphGetPlanProgress: %v", err)
	}
	progress, ok := unwrap(v).(codegraph.PlanProgress)
	if !ok || progress.Aggregate != codegraph.PlanAggregateComplete {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphRelinkPlanNode(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	in := codeGraphRelinkPlanNodeInput{PlanNodeID: "pn-1", FQN: "com.acme.A.b"}
	if _, _, err := s.toolCodeGraphRelinkPlanNode(context.Background(), nil, in); err != nil {
		t.Fatalf("toolCodeGraphRelinkPlanNode: %v", err)
	}
}
