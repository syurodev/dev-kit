package app

import (
	"context"
	"errors"
	"log"

	"devkit/internal/codegraph"
	"devkit/internal/tasks"
)

// ErrPlanNotApproved is a backlog asked for from a plan nobody has reviewed.
var ErrPlanNotApproved = errors.New("app: the plan is still a draft")

// CreateTasksFromPlan turns an approved plan into a backlog.
//
// It lives here because it is the join of two modules that must not import
// each other: Code Graph owns the plan and whether it is approved, Tasks owns
// what a task is. Neither can answer the other's half, and the composition
// root is the only place that can see both.
//
// Approval is checked here for the same reason. Tasks cannot see a plan's
// state, and teaching it to would be teaching it a fact that is Code Graph's.
//
// The caller is passed through rather than assumed to be the user: MCP's
// tasks.createFromPlan reaches here too, and an agent must not be able to
// fill a board outside its workspace by going round through the plan.
func CreateTasksFromPlan(ctx context.Context, c *Container, caller tasks.Caller, boardID string) ([]tasks.Task, error) {
	if c == nil || c.CodeGraph == nil || c.Tasks == nil {
		return nil, ErrPlanNotApproved
	}
	plan, err := c.CodeGraph.GetPlannedFlow(ctx)
	if err != nil {
		return nil, err
	}
	seeds, err := seedsFromPlan(plan)
	if err != nil {
		return nil, err
	}
	return c.Tasks.CreateFromPlan(caller, boardID, seeds)
}

// seedsFromPlan is the whole of the decision, kept out of the I/O so it can be
// tested without an indexed project.
func seedsFromPlan(plan codegraph.Plan) ([]tasks.PlanSeed, error) {
	if plan.State != codegraph.PlanStateApproved {
		// A draft is a proposal under discussion. Turning one into a backlog
		// would put work on the board that nobody agreed to.
		return nil, ErrPlanNotApproved
	}
	seeds := make([]tasks.PlanSeed, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		seeds = append(seeds, tasks.PlanSeed{
			PlanNodeID:  node.PlanNodeID,
			Label:       node.Label,
			Description: node.Description,
		})
	}
	return seeds, nil
}

// statusesFromProgress narrows plan progress to the one fact Tasks acts on.
func statusesFromProgress(progress codegraph.PlanProgress) map[string]string {
	statuses := make(map[string]string, len(progress.Nodes))
	for _, node := range progress.Nodes {
		statuses[node.PlanNodeID] = node.Status
	}
	return statuses
}

// ReconcilePlanTasks moves linked tasks after the index caught up with, or
// drifted from, the plan.
//
// Code Graph owns "does this code exist and match" — it is derived from the
// index, and nobody writes it by hand. Tasks owns "which step is this work
// at". This copies neither: it reads one and moves the other, which is the
// only direction that does not create a second source of truth.
func ReconcilePlanTasks(ctx context.Context, c *Container, boardID string) (tasks.Reconciled, error) {
	if c == nil || c.CodeGraph == nil || c.Tasks == nil || boardID == "" {
		return tasks.Reconciled{}, nil
	}
	progress, err := c.CodeGraph.GetPlanProgress(ctx)
	if err != nil {
		return tasks.Reconciled{}, err
	}
	return c.Tasks.ReconcilePlan(ctx, boardID, statusesFromProgress(progress))
}

// WirePlanReconcile runs a reconcile after every re-index that changed the
// index.
//
// The hook chains rather than replaces: OnIndexUpdated already has a listener
// that tells the frontend, and taking it over would leave the UI silent. It
// must not block either — the contract on that field says non-blocking
// implementations only — so the pass runs on its own goroutine.
//
// Which board is a per-project answer nobody has modelled yet, so a board
// naming this project reconciles and the rest do not. A project with no board
// is the common case and does nothing at all.
func WirePlanReconcile(c *Container) {
	if c == nil || c.CodeGraph == nil || c.Tasks == nil {
		return
	}
	previous := c.CodeGraph.OnIndexUpdated
	c.CodeGraph.OnIndexUpdated = func(version int) {
		if previous != nil {
			previous(version)
		}
		go reconcileBoardsForProject(c)
	}
}

func reconcileBoardsForProject(c *Container) {
	root := c.CodeGraph.ProjectRoot()
	if root == "" {
		return
	}
	// The app's own pass after a re-index, not anybody's request: every board.
	boards, err := c.Tasks.Boards(tasks.Caller{})
	if err != nil {
		return
	}
	ctx := context.Background()
	for _, board := range boards {
		if board.ProjectPath != root {
			continue
		}
		result, err := ReconcilePlanTasks(ctx, c, board.ID)
		if err != nil {
			// A reconcile that fails is not a reason to fail an index: the
			// board simply stays where it was, which is the safe end.
			log.Printf("tasks: reconcile board %s after re-index: %v", board.ID, err)
			continue
		}
		if len(result.MovedToReview) > 0 || len(result.MovedBackToWIP) > 0 {
			log.Printf("tasks: re-index moved %d to review, %d back to in progress",
				len(result.MovedToReview), len(result.MovedBackToWIP))
		}
	}
}

// PlanBacklog adapts the container to the narrow interface MCP declares, so
// the tool surface does not take a *Container.
type PlanBacklog struct{ container *Container }

// NewPlanBacklog builds the adapter.
//
// It always returns a value, never a nil pointer, and that is deliberate: the
// result is assigned to an INTERFACE field, and a nil *PlanBacklog in an
// interface is not a nil interface. The caller's `!= nil` check would pass and
// the first call would panic. Returning a real adapter that reports the
// unwired case is the version with no trap in it.
func NewPlanBacklog(c *Container) *PlanBacklog {
	return &PlanBacklog{container: c}
}

func (p *PlanBacklog) CreateTasksFromPlan(ctx context.Context, caller tasks.Caller, boardID string) ([]tasks.Task, error) {
	if p == nil {
		return nil, ErrPlanNotApproved
	}
	// CreateTasksFromPlan already answers for a container with no Code Graph
	// or no Tasks, so an unwired adapter reports rather than panics.
	return CreateTasksFromPlan(ctx, p.container, caller, boardID)
}
