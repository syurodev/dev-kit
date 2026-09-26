package tasks

import (
	"context"
	"sort"
	"strings"
)

// The link between a task and a Code Graph plan node.
//
// The design called for two ids pointing at each other — `task.plan_node_id`
// and `plan_nodes.task_id`. Only the first is stored, deliberately.
//
// `SavePlan` deletes every row of `plan_nodes` and re-inserts them on each
// proposal, keeping only the comment threads of nodes whose id and FQN are
// unchanged. A `task_id` column there would be wiped every time an agent
// re-proposed a plan, which is the normal thing to do, while the task's own
// `plan_node_id` survived — two copies of one fact, one of them routinely
// destroyed. So the link lives on the task, which is durable and synced, and
// the canvas asks the other way round with ByPlanNode.
//
// The ownership rule from the design is unchanged and is in fact easier to
// keep: Tasks never writes to `plan_nodes` at all now.

// PlanSeed is one plan node as Tasks needs it. The plan itself stays Code
// Graph's; this package never sees a Plan, a status, or an FQN.
type PlanSeed struct {
	PlanNodeID  string
	Label       string
	Description string
}

// LinkPlanNode points a task at a plan node, or clears the link with "".
func (s *Service) LinkPlanNode(caller Caller, id, planNodeID string) (Task, error) {
	return s.mutateAs(caller, id, func(meta *taskMetadata) error {
		meta.PlanNodeID = planNodeID
		return nil
	})
}

// ByPlanNode groups a board's tasks by the plan node they implement.
//
// This is what the plan canvas draws its task chips from: one bounded query
// instead of a column in a store that gets rebuilt.
func (s *Service) ByPlanNode(caller Caller, boardID string) (map[string][]Task, error) {
	if err := s.guard(caller, boardID); err != nil {
		return nil, err
	}
	return s.byPlanNode(boardID)
}

// byPlanNode is ByPlanNode with no scope check, for reconcile.
func (s *Service) byPlanNode(boardID string) (map[string][]Task, error) {
	all, err := s.list(ListInput{BoardID: boardID, IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := map[string][]Task{}
	for _, task := range all {
		if task.PlanNodeID != "" {
			out[task.PlanNodeID] = append(out[task.PlanNodeID], task)
		}
	}
	return out, nil
}

// CreateFromPlan turns an approved plan into a backlog, one task per node.
//
// It is idempotent: a node that already has a task on this board is skipped
// rather than duplicated, because approving a plan twice, or running this
// after adding one node, must not produce two tasks for the same work.
//
// Whether the plan is approved is checked by the caller — this package cannot
// see a plan's state, and asking it to would be asking it to own a fact that
// is Code Graph's.
func (s *Service) CreateFromPlan(caller Caller, boardID string, seeds []PlanSeed) ([]Task, error) {
	if boardID == "" {
		return nil, ErrInvalid
	}
	existing, err := s.ByPlanNode(caller, boardID)
	if err != nil {
		return nil, err
	}
	created := make([]Task, 0, len(seeds))
	for _, seed := range seeds {
		if seed.PlanNodeID == "" || strings.TrimSpace(seed.Label) == "" {
			continue
		}
		if len(existing[seed.PlanNodeID]) > 0 {
			continue
		}
		task, err := s.Create(caller, CreateInput{
			BoardID:     boardID,
			Title:       seed.Label,
			Description: seed.Description,
			PlanNodeID:  seed.PlanNodeID,
		})
		if err != nil {
			return created, err
		}
		created = append(created, task)
	}
	return created, nil
}

// Plan node statuses, restated here rather than imported: tasks must not
// depend on codegraph, and these three strings are the whole of what it needs
// to know about a plan.
const (
	PlanImplementedMatching = "implemented_matching"
	PlanImplementedDiverged = "implemented_diverged"
)

// Reconciled is what a reconcile pass did, so a caller can say so.
type Reconciled struct {
	MovedToReview  []string `json:"movedToReview"`
	MovedBackToWIP []string `json:"movedBackToWip"`
}

// ReconcilePlan moves linked tasks when the real code caught up with the plan,
// or stopped matching it.
//
// Matching moves a task to review and **never** to a terminal status: "code
// exists and its name matches" is not "the work is done as intended", and the
// one gate this module has must not be opened by a name lookup. Diverging
// moves it back to the first active status with a system comment, because a
// task sitting in review over code that no longer matches is a lie.
//
// A task with no plan node is untouched. Nothing else on the board is
// automatic, which is the point.
func (s *Service) ReconcilePlan(ctx context.Context, boardID string, statuses map[string]string) (Reconciled, error) {
	out := Reconciled{MovedToReview: []string{}, MovedBackToWIP: []string{}}
	if boardID == "" || len(statuses) == 0 {
		return out, nil
	}
	board, err := s.board(boardID)
	if err != nil {
		return out, err
	}
	review, hasReview := board.Workflow.LastOf(CategoryActive)
	working, hasWorking := board.Workflow.FirstOf(CategoryActive)
	if !hasReview || !hasWorking {
		return out, nil
	}
	linked, err := s.byPlanNode(boardID)
	if err != nil {
		return out, err
	}

	planNodes := make([]string, 0, len(statuses))
	for planNodeID := range statuses {
		planNodes = append(planNodes, planNodeID)
	}
	// Deterministic order so the reported lists read the same twice.
	sort.Strings(planNodes)

	for _, planNodeID := range planNodes {
		for _, task := range linked[planNodeID] {
			if task.ArchivedAt != "" {
				continue
			}
			category, known := board.Workflow.CategoryOf(task.Status)
			if !known {
				continue
			}
			switch statuses[planNodeID] {
			case PlanImplementedMatching:
				// Already finished, or already in review: nothing to say.
				if category == CategoryTerminal || task.Status == review {
					continue
				}
				if _, err := s.applyTransition(task.ID, Caller{}, review); err != nil {
					return out, err
				}
				out.MovedToReview = append(out.MovedToReview, task.Key)
			case PlanImplementedDiverged:
				// A closed task is not reopened by a diff: somebody decided it
				// was done, and the index is not entitled to overrule that.
				if category == CategoryTerminal || task.Status == working {
					continue
				}
				if _, err := s.applyTransition(task.ID, Caller{}, working); err != nil {
					return out, err
				}
				if _, err := s.systemComment(task.ID,
					"Code Graph: the code for this plan node no longer matches the plan, so this went back to "+working+"."); err != nil {
					return out, err
				}
				out.MovedBackToWIP = append(out.MovedBackToWIP, task.Key)
			}
		}
	}
	return out, nil
}

// Search finds tasks by key, title or label.
//
// Descriptions are deliberately not searched: they are the encrypted half, and
// searching them would mean decrypting every task on the board to answer one
// query. Key and title are plaintext metadata, so this works with the vault
// locked — the same trade note titles already make.
func (s *Service) Search(caller Caller, boardID, query string) ([]Task, error) {
	if boardID == "" {
		return nil, ErrInvalid
	}
	query = strings.TrimSpace(strings.ToLower(query))
	all, err := s.List(caller, ListInput{BoardID: boardID})
	if err != nil {
		return nil, err
	}
	if query == "" {
		return all, nil
	}
	matches := make([]Task, 0, len(all))
	for _, task := range all {
		if matchesQuery(task, query) {
			matches = append(matches, task)
		}
	}
	return matches, nil
}

func matchesQuery(task Task, query string) bool {
	if strings.Contains(strings.ToLower(task.Key), query) ||
		strings.Contains(strings.ToLower(task.Title), query) {
		return true
	}
	for _, label := range task.Labels {
		if strings.Contains(strings.ToLower(label), query) {
			return true
		}
	}
	return false
}
