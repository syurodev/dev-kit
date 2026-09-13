package database

import "testing"

// Shape captured from MariaDB 10.11 ANALYZE FORMAT=JSON.
const mariaAnalyzeJSON = `{
  "query_block": {
    "select_id": 1,
    "r_total_time_ms": 0.108,
    "nested_loop": [
      {"table": {"table_name": "c", "access_type": "const", "key": "uq_email",
                 "rows": 1, "r_rows": null, "r_total_time_ms": 0.004}},
      {"table": {"table_name": "o", "access_type": "ref", "key": "fk_o_c",
                 "rows": 2, "r_rows": 2, "r_total_time_ms": 0.008}}
    ]
  }
}`

func TestParseMySQLPlanFlattensNestedLoopUnderTheQueryBlock(t *testing.T) {
	plan, err := parseMySQLPlan(mariaAnalyzeJSON, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !plan.Analyzed {
		t.Fatal("analyzed flag lost")
	}
	if len(plan.Nodes) != 1 || plan.Nodes[0].Operation != "Query block" {
		t.Fatalf("want one query block at the root, got %+v", plan.Nodes)
	}

	// nested_loop is a container, not a step: its tables belong at the depth of
	// the block holding them, otherwise every join adds a phantom level.
	children := plan.Nodes[0].Children
	if len(children) != 2 {
		t.Fatalf("want both joined tables, got %+v", children)
	}
	for _, child := range children {
		if child.Depth != 1 {
			t.Fatalf("table %q sits at depth %d, want 1", child.Detail, child.Depth)
		}
	}

	if children[1].ActualRows != 2 || children[1].EstimatedRows != 2 {
		t.Fatalf("row counts lost: %+v", children[1])
	}
	if children[1].Detail != "o using fk_o_c" {
		t.Fatalf("detail should name the table and its index, got %q", children[1].Detail)
	}
	if children[0].ActualRows != 0 {
		t.Fatalf("a null r_rows must read as 0, got %v", children[0].ActualRows)
	}
}

// MySQL reports estimates under a different key. Reading only MariaDB's spelling
// would leave every MySQL plan claiming zero rows, which looks like a real
// answer rather than a missing one.
func TestParseMySQLPlanReadsMySQLRowKey(t *testing.T) {
	plan, err := parseMySQLPlan(`{"query_block": {"table": {
		"table_name": "orders", "access_type": "ALL",
		"rows_examined_per_scan": 512}}}`, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	node := plan.Nodes[0].Children[0]
	if node.EstimatedRows != 512 {
		t.Fatalf("want 512 estimated rows, got %v", node.EstimatedRows)
	}
	if node.Operation != "ALL" {
		t.Fatalf("want the access type as the operation, got %q", node.Operation)
	}
}

func TestParseMySQLPlanRejectsNonJSON(t *testing.T) {
	if _, err := parseMySQLPlan("-> Table scan on orders  (cost=1.2 rows=3)", false); err == nil {
		t.Fatal("MySQL's text tree must not parse as a plan")
	}
}

/*
Which engine a server is, from what it says about itself.

The profile's engine is a dropdown, and "MySQL" is the natural pick for a
MariaDB server — same port, same driver, same answers to everything until
ANALYZE. A MariaDB server saved as MySQL was sent EXPLAIN ANALYZE and came
back with error 1064. VERSION() names MariaDB in its own text; MySQL's never
does.
*/
func TestMariaDBIsReadFromTheServersOwnVersion(t *testing.T) {
	for _, version := range []string{
		"10.11.6-MariaDB",
		"11.4.2-MariaDB-ubu2404",
		"5.5.5-10.6.12-MariaDB-log",
	} {
		if !isMariaDBVersion(version) {
			t.Errorf("%q is MariaDB", version)
		}
	}
	for _, version := range []string{"8.0.36", "8.4.0-commercial", "5.7.44-log", ""} {
		if isMariaDBVersion(version) {
			t.Errorf("%q is not MariaDB", version)
		}
	}
}
