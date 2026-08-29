package database

import (
	"context"
	"errors"
	"time"
)

// ErrEngineHasNoSessions means the engine cannot report sessions at all —
// SQLite has no server, so there is nothing to inspect. This is a normal
// answer, not a failure: the UI says so instead of showing an empty panel.
var ErrEngineHasNoSessions = errors.New("this engine does not report sessions")

// ErrEngineHasNoQueryPlan means the engine cannot produce a plan in a shape
// DevKit understands yet.
var ErrEngineHasNoQueryPlan = errors.New("this engine does not report query plans")

// SessionRow is one connection currently on the server.
type SessionRow struct {
	PID         string    `json:"pid"`
	State       string    `json:"state"`
	Duration    string    `json:"duration"`
	User        string    `json:"user"`
	Client      string    `json:"client"`
	WaitEvent   string    `json:"wait_event"`
	BlockedBy   []string  `json:"blocked_by"`
	Query       string    `json:"query"`
	Application string    `json:"application"`
	StartedAt   time.Time `json:"started_at"`
}

// SessionInspector is optional: a driver implements it only if its engine has a
// server with sessions to report. SQLite never will, and MySQL cannot answer
// BlockedBy, so this stays off Conn rather than forcing every driver to fake it.
type SessionInspector interface {
	ListActivity(ctx context.Context) ([]SessionRow, error)
	CancelQuery(ctx context.Context, pid string) error
	TerminateSession(ctx context.Context, pid string) error
}

// QueryPlan is the normalised spine plus whatever the engine said verbatim.
// The spine is enough to draw and compare; Raw keeps everything the spine
// cannot hold, the same "standard fields plus a passthrough bag" split
// driver_options already uses.
type QueryPlan struct {
	Nodes []PlanNode `json:"nodes"`
	Raw   string     `json:"raw"`
	// Analyzed reports whether the statement actually ran.
	Analyzed bool `json:"analyzed"`
}

// PlanNode is one step. SelfMillis, not inclusive time: with inclusive time the
// root is always 100% and the chart says nothing.
type PlanNode struct {
	Operation     string     `json:"operation"`
	Depth         int        `json:"depth"`
	EstimatedRows float64    `json:"estimated_rows"`
	ActualRows    float64    `json:"actual_rows"`
	SelfMillis    float64    `json:"self_millis"`
	Detail        string     `json:"detail"`
	Children      []PlanNode `json:"children,omitempty"`
}

// QueryPlanner is optional for the same reason SessionInspector is: EXPLAIN
// output shapes differ far more between engines than catalogue queries do.
type QueryPlanner interface {
	Explain(ctx context.Context, sql string) (QueryPlan, error)
	// ExplainAnalyze runs the statement. Implementations must wrap it in a
	// transaction that is rolled back, because DevKit does not parse SQL and
	// therefore cannot know whether the statement mutates.
	ExplainAnalyze(ctx context.Context, sql string) (QueryPlan, error)
}

func (s *Service) inspector(ctx context.Context, id string) (SessionInspector, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	inspector, ok := conn.(SessionInspector)
	if !ok {
		return nil, ErrEngineHasNoSessions
	}
	return inspector, nil
}

func (s *Service) planner(ctx context.Context, id string) (QueryPlanner, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	planner, ok := conn.(QueryPlanner)
	if !ok {
		return nil, ErrEngineHasNoQueryPlan
	}
	return planner, nil
}

// ListActivity reports the sessions currently on the server.
//
// The rows carry other people's SQL, literals included. That is why the bridge
// keeps this off the MCP surface, and why it is never written to query history.
func (s *Service) ListActivity(ctx context.Context, id string) ([]SessionRow, error) {
	inspector, err := s.inspector(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return inspector.ListActivity(ctx)
}

// CancelQuery stops one running statement and leaves the connection open.
func (s *Service) CancelQuery(ctx context.Context, id, pid string) error {
	inspector, err := s.inspector(ctx, id)
	if err != nil {
		return err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return inspector.CancelQuery(ctx, pid)
}

// TerminateSession closes a connection and rolls back its open transaction.
// Safe mode does not cover this: pg_cancel_backend is a SELECT, so a read-only
// profile can still end other people's sessions.
func (s *Service) TerminateSession(ctx context.Context, id, pid string) error {
	inspector, err := s.inspector(ctx, id)
	if err != nil {
		return err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return inspector.TerminateSession(ctx, pid)
}

// Explain plans the statement without running it.
func (s *Service) Explain(ctx context.Context, id, sql string) (QueryPlan, error) {
	planner, err := s.planner(ctx, id)
	if err != nil {
		return QueryPlan{}, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return planner.Explain(ctx, sql)
}

// ExplainAnalyze runs the statement inside a rolled-back transaction.
func (s *Service) ExplainAnalyze(ctx context.Context, id, sql string) (QueryPlan, error) {
	planner, err := s.planner(ctx, id)
	if err != nil {
		return QueryPlan{}, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return planner.ExplainAnalyze(ctx, sql)
}
