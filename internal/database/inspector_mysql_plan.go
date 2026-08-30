package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

var _ QueryPlanner = (*mysqlConn)(nil)

// Explain asks for the plan without running the statement.
//
// Both engines answer EXPLAIN FORMAT=JSON with a query_block tree, so the
// normalised spine is built the same way for each. What they do not share is
// ANALYZE — see ExplainAnalyze.
func (c *mysqlConn) Explain(ctx context.Context, sql string) (QueryPlan, error) {
	raw, err := c.planJSON(ctx, "EXPLAIN FORMAT=JSON "+sql)
	if err != nil {
		return QueryPlan{}, err
	}
	return parseMySQLPlan(raw, false)
}

// ExplainAnalyze runs the statement inside a transaction that is rolled back,
// for the reason the interface states: DevKit does not parse SQL and cannot
// know whether a statement mutates. Rollback covers what InnoDB covers —
// AUTO_INCREMENT does not rewind, and a trigger reaching outside the database
// is gone for good.
//
// The two engines diverge here, and only one of them can fill the spine.
// MariaDB's ANALYZE FORMAT=JSON carries r_rows and r_total_time_ms beside the
// estimates. MySQL's EXPLAIN ANALYZE emits a text tree and nothing else, so
// its plan comes back with an empty spine and the tree in Raw — which is what
// Raw is for. Half-parsing that text into fake nodes would claim a precision
// the output does not have.
func (c *mysqlConn) ExplainAnalyze(ctx context.Context, sql string) (QueryPlan, error) {
	statement := "EXPLAIN ANALYZE " + sql
	if c.engine == EngineMariaDB {
		statement = "ANALYZE FORMAT=JSON " + sql
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return QueryPlan{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var raw string
	if err := tx.QueryRowContext(ctx, statement).Scan(&raw); err != nil {
		return QueryPlan{}, err
	}

	if c.engine != EngineMariaDB {
		return QueryPlan{Raw: raw, Analyzed: true}, nil
	}
	return parseMySQLPlan(raw, true)
}

func (c *mysqlConn) planJSON(ctx context.Context, statement string) (string, error) {
	var raw string
	if err := c.db.QueryRowContext(ctx, statement).Scan(&raw); err != nil {
		return "", err
	}
	return raw, nil
}

// Wrapper keys that describe a step rather than a table. Anything else that is
// an object gets walked without becoming a node of its own, so an engine adding
// a new section does not silently drop the tables underneath it.
var mysqlPlanSteps = map[string]string{
	"query_block":         "Query block",
	"ordering_operation":  "Order",
	"grouping_operation":  "Group",
	"duplicates_removal":  "Distinct",
	"materialized":        "Materialize",
	"materialised":        "Materialize",
	"union_result":        "Union",
	"attached_subqueries": "Subquery",
	"subqueries":          "Subquery",
}

func parseMySQLPlan(raw string, analyzed bool) (QueryPlan, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return QueryPlan{}, fmt.Errorf("plan is not JSON: %w", err)
	}
	return QueryPlan{
		Nodes:    walkMySQLPlan(root, 0),
		Raw:      raw,
		Analyzed: analyzed,
	}, nil
}

func walkMySQLPlan(value any, depth int) []PlanNode {
	switch typed := value.(type) {
	case []any:
		var out []PlanNode
		for _, item := range typed {
			out = append(out, walkMySQLPlan(item, depth)...)
		}
		return out

	case map[string]any:
		if table, ok := typed["table"].(map[string]any); ok {
			return []PlanNode{mysqlTableNode(table, depth)}
		}
		if _, ok := typed["table_name"]; ok {
			return []PlanNode{mysqlTableNode(typed, depth)}
		}

		var out []PlanNode
		for key, child := range typed {
			label, isStep := mysqlPlanSteps[key]
			if isStep {
				node := PlanNode{Operation: label, Depth: depth}
				node.Children = walkMySQLPlan(child, depth+1)
				out = append(out, node)
				continue
			}
			// nested_loop and friends are plain containers: their contents sit
			// at the same depth as the step that holds them.
			out = append(out, walkMySQLPlan(child, depth)...)
		}
		return out
	}
	return nil
}

func mysqlTableNode(table map[string]any, depth int) PlanNode {
	node := PlanNode{
		Operation:     firstString(table, "access_type"),
		Depth:         depth,
		EstimatedRows: firstNumber(table, "rows", "rows_examined_per_scan"),
		ActualRows:    firstNumber(table, "r_rows", "r_rows_examined_per_scan"),
		SelfMillis:    firstNumber(table, "r_total_time_ms"),
	}
	if node.Operation == "" {
		node.Operation = "table"
	}

	detail := []string{firstString(table, "table_name")}
	if key := firstString(table, "key"); key != "" {
		detail = append(detail, "using "+key)
	}
	node.Detail = strings.Join(detail, " ")
	node.Children = walkMySQLPlan(table["attached_subqueries"], depth+1)
	return node
}

func firstString(from map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := from[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func firstNumber(from map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if n, ok := from[key].(float64); ok {
			return n
		}
	}
	return 0
}
