// internal/codegraph/planstore.go
package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SavePlan replaces the project's single active plan wholesale — same
// "swap, don't merge" contract as ReplaceIndex, matching the doc's "đề xuất
// plan mới trong lúc plan cũ chưa xong thì thay thế".
//
// Comment threads are the one thing carried over, and only for a node the
// new plan still has: the same planNodeId AND the same FQN. An agent
// re-proposing a revised plan keeps its unchanged nodes, and wiping every
// thread on that — including the human's review notes the revision was
// answering — lost the conversation the revision was part of. Both halves are
// required. An ID alone is not identity: agents number nodes "pn-001",
// "pn-002" afresh in every proposal, and a thread saying "not this table"
// must not reappear on whatever unrelated node took the number. An FQN alone
// is the migration codegraph.mdx § Non-goals rules out. A node whose ID or
// FQN changed is a new node, and starts with no thread.
func (s *sqliteStore) SavePlan(ctx context.Context, plan Plan) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codegraph: begin save plan: %w", err)
	}
	defer tx.Rollback()

	previous := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT plan_node_id, fqn FROM plan_nodes`)
	if err != nil {
		return fmt.Errorf("codegraph: read previous plan nodes: %w", err)
	}
	for rows.Next() {
		var id, fqn string
		if err := rows.Scan(&id, &fqn); err != nil {
			rows.Close()
			return fmt.Errorf("codegraph: scan previous plan node: %w", err)
		}
		previous[id] = fqn
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("codegraph: read previous plan nodes: %w", err)
	}
	rows.Close()

	var keep []string
	for _, n := range plan.Nodes {
		if fqn, ok := previous[n.PlanNodeID]; ok && fqn == n.FQN {
			keep = append(keep, n.PlanNodeID)
		}
	}
	dropComments := `DELETE FROM plan_node_comments`
	if len(keep) > 0 {
		dropComments += ` WHERE plan_node_id NOT IN (` + placeholders(len(keep)) + `)`
	}
	if _, err := tx.ExecContext(ctx, dropComments, stringArgs(keep)...); err != nil {
		return fmt.Errorf("codegraph: clear dropped plan node comments: %w", err)
	}
	for _, table := range []string{"plan_meta", "plan_nodes", "plan_edges"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO plan_meta (id, description, state) VALUES (1, ?, ?)`,
		plan.Description, plan.State); err != nil {
		return fmt.Errorf("codegraph: insert plan meta: %w", err)
	}
	for i, n := range plan.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_nodes (plan_node_id, label, description, fqn, kind, ordinal) VALUES (?, ?, ?, ?, ?, ?)`,
			n.PlanNodeID, n.Label, n.Description, n.FQN, n.Kind, i); err != nil {
			return fmt.Errorf("codegraph: insert plan node %q: %w", n.PlanNodeID, err)
		}
	}
	for _, e := range plan.Edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_edges (from_plan_node_id, to_plan_node_id, condition) VALUES (?, ?, ?)`,
			e.From, e.To, e.Condition); err != nil {
			return fmt.Errorf("codegraph: insert plan edge %q->%q: %w", e.From, e.To, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit save plan: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetPlan(ctx context.Context) (Plan, error) {
	var plan Plan
	err := s.db.QueryRowContext(ctx, `SELECT description, state FROM plan_meta WHERE id = 1`).
		Scan(&plan.Description, &plan.State)
	if err == sql.ErrNoRows {
		return Plan{}, ErrNoPlan
	}
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan: %w", err)
	}

	nodeRows, err := s.db.QueryContext(ctx,
		`SELECT plan_node_id, label, description, fqn, kind FROM plan_nodes ORDER BY ordinal`)
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan nodes: %w", err)
	}
	defer nodeRows.Close()
	for nodeRows.Next() {
		var n PlanNode
		if err := nodeRows.Scan(&n.PlanNodeID, &n.Label, &n.Description, &n.FQN, &n.Kind); err != nil {
			return Plan{}, fmt.Errorf("codegraph: scan plan node: %w", err)
		}
		plan.Nodes = append(plan.Nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return Plan{}, err
	}

	edgeRows, err := s.db.QueryContext(ctx,
		`SELECT from_plan_node_id, to_plan_node_id, condition FROM plan_edges ORDER BY from_plan_node_id, to_plan_node_id`)
	if err != nil {
		return Plan{}, fmt.Errorf("codegraph: GetPlan edges: %w", err)
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e PlanEdge
		if err := edgeRows.Scan(&e.From, &e.To, &e.Condition); err != nil {
			return Plan{}, fmt.Errorf("codegraph: scan plan edge: %w", err)
		}
		plan.Edges = append(plan.Edges, e)
	}
	return plan, edgeRows.Err()
}

func (s *sqliteStore) SetPlanState(ctx context.Context, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plan_meta SET state = ? WHERE id = 1`, state)
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanState: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("codegraph: SetPlanState: %w", err)
	}
	if affected == 0 {
		return ErrNoPlan
	}
	return nil
}

// SetPlanNodeFQN repoints one plan node, and reports whether that took an
// approved plan back to draft (see Service.RelinkPlanNode). Setting the FQN a
// node already has changes nothing, approval included.
func (s *sqliteStore) SetPlanNodeFQN(ctx context.Context, planNodeID, fqn string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	defer tx.Rollback()

	var current string
	err = tx.QueryRowContext(ctx, `SELECT fqn FROM plan_nodes WHERE plan_node_id = ?`, planNodeID).Scan(&current)
	if err == sql.ErrNoRows {
		return false, ErrPlanNodeNotFound
	}
	if err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	if current == fqn {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plan_nodes SET fqn = ? WHERE plan_node_id = ?`, fqn, planNodeID); err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE plan_meta SET state = ? WHERE id = 1 AND state = ?`, PlanStateDraft, PlanStateApproved)
	if err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: reset approval: %w", err)
	}
	reset, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("codegraph: SetPlanNodeFQN: commit: %w", err)
	}
	return reset > 0, nil
}

// ResolveFQN finds the indexed node a plan FQN refers to. An FQN carrying
// parameter types (it contains "(") matches nodes.id exactly; a plain dotted
// FQN matches nodes.label. A label can match several overloads — the lowest
// id wins so repeated calls are deterministic, which is exactly the v1
// overload ambiguity the design doc already accepts.
func (s *sqliteStore) ResolveFQN(ctx context.Context, fqn string) (Node, bool, error) {
	column := "label"
	if strings.Contains(fqn, "(") {
		column = "id"
	}
	var n Node
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line FROM nodes WHERE `+column+` = ? ORDER BY id LIMIT 1`,
		fqn,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine)
	if err == sql.ErrNoRows {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, fmt.Errorf("codegraph: ResolveFQN: %w", err)
	}
	return n, true, nil
}
