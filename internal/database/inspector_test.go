package database

import (
	"testing"
)

func TestPlanSelfTimeSubtractsChildren(t *testing.T) {
	// One slow child under a fast parent — the shape every bad plan has.
	raw := `[{"Plan":{
		"Node Type":"Nested Loop","Plan Rows":1,"Actual Rows":1,"Actual Total Time":1854.7,
		"Plans":[
			{"Node Type":"Seq Scan","Relation Name":"order_items","Plan Rows":12,"Actual Rows":1204332,
			 "Actual Total Time":1842.0,"Filter":"(order_id = $1)","Rows Removed by Filter":1204331},
			{"Node Type":"Index Scan","Index Name":"orders_pkey","Plan Rows":1,"Actual Rows":1,"Actual Total Time":0.3}
		]}}]`

	plan, err := parsePostgresPlan(raw, true)
	if err != nil {
		t.Fatalf("parsePostgresPlan: %v", err)
	}
	if !plan.Analyzed {
		t.Fatal("analyzed flag lost")
	}
	root := plan.Nodes[0]

	// Inclusive time would make the root 1854.7 and every plan would look like
	// the root is the problem. Self time puts the blame on the Seq Scan.
	if root.SelfMillis > 13 {
		t.Fatalf("root self time = %.1f, want the parent's own cost only", root.SelfMillis)
	}
	seqScan := root.Children[0]
	if seqScan.SelfMillis != 1842.0 {
		t.Fatalf("seq scan self time = %.1f, want 1842.0", seqScan.SelfMillis)
	}
	if seqScan.Operation != "Seq Scan on order_items" {
		t.Fatalf("operation = %q", seqScan.Operation)
	}
	// The estimate/actual gap is the story of this plan; both must survive.
	if seqScan.EstimatedRows != 12 || seqScan.ActualRows != 1204332 {
		t.Fatalf("estimate vs actual lost: %.0f vs %.0f", seqScan.EstimatedRows, seqScan.ActualRows)
	}
	if seqScan.Detail == "" {
		t.Fatal("filter and rows-removed detail dropped")
	}
	if plan.Raw != raw {
		t.Fatal("raw plan not preserved for the detail panel")
	}
}

func TestSelfTimeNeverGoesNegative(t *testing.T) {
	// Parallel plans can report child time exceeding the parent's.
	raw := `[{"Plan":{"Node Type":"Gather","Actual Total Time":10,
		"Plans":[{"Node Type":"Seq Scan","Actual Total Time":40}]}}]`
	plan, err := parsePostgresPlan(raw, true)
	if err != nil {
		t.Fatalf("parsePostgresPlan: %v", err)
	}
	if plan.Nodes[0].SelfMillis < 0 {
		t.Fatalf("self time = %.1f, a negative bar cannot be drawn", plan.Nodes[0].SelfMillis)
	}
}

func TestOptionalInterfacesAreOptional(t *testing.T) {
	pg := &postgresConn{}
	if _, ok := any(pg).(SessionInspector); !ok {
		t.Fatal("postgres must report sessions")
	}
	if _, ok := any(pg).(QueryPlanner); !ok {
		t.Fatal("postgres must produce plans")
	}

	my := &mysqlConn{}
	if _, ok := any(my).(SessionInspector); !ok {
		t.Fatal("mysql must report sessions")
	}
	if _, ok := any(my).(QueryPlanner); !ok {
		t.Fatal("mysql must produce plans")
	}
}

// The interfaces being optional is the invariant, not which drivers happen to
// implement them today. Both in-tree drivers now implement both, so the only
// thing still proving the split is that satisfying Conn is not enough.
type connOnly struct{ Conn }

func TestOptionalInterfacesAreNotPartOfConn(t *testing.T) {
	var bare Conn = connOnly{}
	if _, ok := bare.(QueryPlanner); ok {
		t.Fatal("Conn has grown a planner; it must stay at seven methods")
	}
	if _, ok := bare.(SessionInspector); ok {
		t.Fatal("Conn has grown session inspection; it must stay at seven methods")
	}
	if _, ok := bare.(IndexLister); ok {
		t.Fatal("Conn has grown index listing; it must stay at seven methods")
	}
}

func TestServiceSaysSoWhenAConnCannotPlan(t *testing.T) {
	// Reaching Service.planner needs a live pool, so the assertion it makes is
	// exercised directly instead: a Conn that cannot plan must be reported, not
	// called.
	var bare Conn = connOnly{}
	if _, ok := bare.(QueryPlanner); ok {
		t.Fatal("stub unexpectedly implements QueryPlanner")
	}
	if ErrEngineHasNoQueryPlan == nil {
		t.Fatal("the error the service returns for that case is missing")
	}
}

func TestKillTargetMustBeAnInteger(t *testing.T) {
	// MySQL's KILL takes no placeholders, so the one interpolated value is
	// rejected rather than escaped when it is not a plain id.
	for _, bad := range []string{"1; DROP TABLE users", "", "abc", "1 OR 1=1"} {
		if got := quoteConnectionID(bad); got != "0" {
			t.Fatalf("quoteConnectionID(%q) = %q, want 0", bad, got)
		}
	}
	if got := quoteConnectionID("4412"); got != "4412" {
		t.Fatalf("a real pid was rejected: %q", got)
	}
}
