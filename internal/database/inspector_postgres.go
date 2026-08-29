package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// postgresConn implements both optional interfaces.
var (
	_ SessionInspector = (*postgresConn)(nil)
	_ QueryPlanner     = (*postgresConn)(nil)
)

func (c *postgresConn) ListActivity(ctx context.Context) ([]SessionRow, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT pid::text,
		       coalesce(state, ''),
		       coalesce(to_char(now() - coalesce(xact_start, query_start, backend_start), 'HH24:MI:SS'), ''),
		       coalesce(usename, ''),
		       coalesce(host(client_addr), 'local'),
		       trim(both ' ' from coalesce(wait_event_type, '') || ' ' || coalesce(wait_event, '')),
		       coalesce(pg_blocking_pids(pid), '{}'),
		       coalesce(query, ''),
		       coalesce(application_name, ''),
		       coalesce(xact_start, query_start, backend_start)
		FROM pg_stat_activity
		WHERE datname = current_database()
		ORDER BY coalesce(xact_start, query_start, backend_start)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var row SessionRow
		var blocking []int32
		var started *time.Time
		if err := rows.Scan(&row.PID, &row.State, &row.Duration, &row.User, &row.Client,
			&row.WaitEvent, &blocking, &row.Query, &row.Application, &started); err != nil {
			return nil, err
		}
		for _, pid := range blocking {
			row.BlockedBy = append(row.BlockedBy, fmt.Sprintf("%d", pid))
		}
		if started != nil {
			row.StartedAt = *started
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (c *postgresConn) CancelQuery(ctx context.Context, pid string) error {
	_, err := c.pool.Exec(ctx, "SELECT pg_cancel_backend($1::int)", pid)
	return err
}

func (c *postgresConn) TerminateSession(ctx context.Context, pid string) error {
	_, err := c.pool.Exec(ctx, "SELECT pg_terminate_backend($1::int)", pid)
	return err
}

func (c *postgresConn) Explain(ctx context.Context, sql string) (QueryPlan, error) {
	raw, err := c.explainJSON(ctx, "EXPLAIN (FORMAT JSON) "+sql)
	if err != nil {
		return QueryPlan{}, err
	}
	return parsePostgresPlan(raw, false)
}

func (c *postgresConn) ExplainAnalyze(ctx context.Context, sql string) (QueryPlan, error) {
	// ANALYZE executes the statement. DevKit does not parse SQL and so cannot
	// know whether it mutates, which is why this always runs inside a
	// transaction that is rolled back.
	//
	// Rollback only undoes what MVCC covers: sequences do not rewind, pg_notify
	// has already fired, and triggers reaching outside the database are gone
	// for good.
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return QueryPlan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var raw string
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql).Scan(&raw); err != nil {
		return QueryPlan{}, err
	}
	return parsePostgresPlan(raw, true)
}

func (c *postgresConn) explainJSON(ctx context.Context, statement string) (string, error) {
	var raw string
	if err := c.pool.QueryRow(ctx, statement).Scan(&raw); err != nil {
		return "", err
	}
	return raw, nil
}

type pgPlanEnvelope struct {
	Plan pgPlanNode `json:"Plan"`
}

type pgPlanNode struct {
	NodeType     string       `json:"Node Type"`
	RelationName string       `json:"Relation Name"`
	IndexName    string       `json:"Index Name"`
	PlanRows     float64      `json:"Plan Rows"`
	ActualRows   float64      `json:"Actual Rows"`
	ActualTotal  float64      `json:"Actual Total Time"`
	TotalCost    float64      `json:"Total Cost"`
	Filter       string       `json:"Filter"`
	RowsRemoved  float64      `json:"Rows Removed by Filter"`
	Plans        []pgPlanNode `json:"Plans"`
}

func parsePostgresPlan(raw string, analyzed bool) (QueryPlan, error) {
	var envelopes []pgPlanEnvelope
	if err := json.Unmarshal([]byte(raw), &envelopes); err != nil {
		return QueryPlan{}, err
	}
	plan := QueryPlan{Raw: raw, Analyzed: analyzed}
	if len(envelopes) == 0 {
		return plan, nil
	}
	plan.Nodes = []PlanNode{convertPGNode(envelopes[0].Plan, 0)}
	return plan, nil
}

func convertPGNode(node pgPlanNode, depth int) PlanNode {
	converted := PlanNode{
		Operation:     planOperation(node),
		Depth:         depth,
		EstimatedRows: node.PlanRows,
		ActualRows:    node.ActualRows,
		// Self time, not inclusive: children are subtracted below so one slow
		// node stands out instead of every ancestor reading as slow.
		SelfMillis: node.ActualTotal,
		Detail:     planDetail(node),
	}
	for _, child := range node.Plans {
		childNode := convertPGNode(child, depth+1)
		converted.SelfMillis -= child.ActualTotal
		converted.Children = append(converted.Children, childNode)
	}
	if converted.SelfMillis < 0 {
		converted.SelfMillis = 0
	}
	return converted
}

func planOperation(node pgPlanNode) string {
	switch {
	case node.IndexName != "":
		return fmt.Sprintf("%s using %s", node.NodeType, node.IndexName)
	case node.RelationName != "":
		return fmt.Sprintf("%s on %s", node.NodeType, node.RelationName)
	default:
		return node.NodeType
	}
}

func planDetail(node pgPlanNode) string {
	var parts []string
	if node.Filter != "" {
		parts = append(parts, "Filter: "+node.Filter)
	}
	if node.RowsRemoved > 0 {
		parts = append(parts, fmt.Sprintf("Rows Removed by Filter: %.0f", node.RowsRemoved))
	}
	return strings.Join(parts, "   ·   ")
}
