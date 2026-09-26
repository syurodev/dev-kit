package tasks

import (
	"context"
	"strings"
	"testing"
)

func planBoard(t *testing.T) (*Service, Board) {
	t.Helper()
	svc, _ := newService(t)
	return svc, newBoard(t, svc)
}

func TestCreateFromPlanIsIdempotent(t *testing.T) {
	svc, board := planBoard(t)
	seeds := []PlanSeed{
		{PlanNodeID: "n1", Label: "RefundService.refund", Description: "issue the refund"},
		{PlanNodeID: "n2", Label: "RefundController.post"},
	}
	created, err := svc.CreateFromPlan(user, board.ID, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("created %d tasks", len(created))
	}

	// Approving the same plan again, or running this after one node was added,
	// must not produce a second task for work that already has one.
	seeds = append(seeds, PlanSeed{PlanNodeID: "n3", Label: "RefundRepository.save"})
	again, err := svc.CreateFromPlan(user, board.ID, seeds)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].PlanNodeID != "n3" {
		t.Fatalf("second pass created %+v; only the new node should be made", again)
	}
	all, _ := svc.List(user, ListInput{BoardID: board.ID})
	if len(all) != 3 {
		t.Fatalf("board has %d tasks, want 3", len(all))
	}
}

func TestByPlanNodeGroupsTheLink(t *testing.T) {
	svc, board := planBoard(t)
	if _, err := svc.CreateFromPlan(user, board.ID, []PlanSeed{{PlanNodeID: "n1", Label: "a"}}); err != nil {
		t.Fatal(err)
	}
	loose, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "unlinked"})
	if err != nil {
		t.Fatal(err)
	}

	byNode, err := svc.ByPlanNode(user, board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byNode["n1"]) != 1 {
		t.Fatalf("byPlanNode = %+v", byNode)
	}
	for _, tasks := range byNode {
		for _, task := range tasks {
			if task.ID == loose.ID {
				t.Fatal("a task with no plan node was grouped under one")
			}
		}
	}

	// Linking and clearing go through the task, which is the only place the
	// link is stored.
	linked, err := svc.LinkPlanNode(user, loose.ID, "n9")
	if err != nil || linked.PlanNodeID != "n9" {
		t.Fatalf("link = %+v, %v", linked, err)
	}
	cleared, err := svc.LinkPlanNode(user, loose.ID, "")
	if err != nil || cleared.PlanNodeID != "" {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
}

// Matching code moves a task to review and never past it: "the code exists and
// its name matches" is not "the work is done as intended", and the one gate
// this module has must not open on a name lookup.
func TestReconcileMovesToReviewButNeverCloses(t *testing.T) {
	svc, board := planBoard(t)
	created, err := svc.CreateFromPlan(user, board.ID, []PlanSeed{{PlanNodeID: "n1", Label: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	result, err := svc.ReconcilePlan(ctx, board.ID, map[string]string{"n1": PlanImplementedMatching})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MovedToReview) != 1 || result.MovedToReview[0] != created[0].Key {
		t.Fatalf("reconciled = %+v", result)
	}
	after, _ := svc.Get(user, created[0].ID)
	if after.Status != "review" {
		t.Fatalf("status = %q, want review", after.Status)
	}

	// Running it again says nothing new.
	repeat, err := svc.ReconcilePlan(ctx, board.ID, map[string]string{"n1": PlanImplementedMatching})
	if err != nil {
		t.Fatal(err)
	}
	if len(repeat.MovedToReview) != 0 {
		t.Fatalf("a second pass moved it again: %+v", repeat)
	}
}

func TestReconcileMovesBackWhenTheCodeDiverges(t *testing.T) {
	svc, board := planBoard(t)
	created, _ := svc.CreateFromPlan(user, board.ID, []PlanSeed{{PlanNodeID: "n1", Label: "a"}})
	ctx := context.Background()
	if _, err := svc.ReconcilePlan(ctx, board.ID, map[string]string{"n1": PlanImplementedMatching}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ReconcilePlan(ctx, board.ID, map[string]string{"n1": PlanImplementedDiverged})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MovedBackToWIP) != 1 {
		t.Fatalf("reconciled = %+v", result)
	}
	after, _ := svc.Get(user, created[0].ID)
	if after.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", after.Status)
	}
	// The move is explained, in the Activity tab rather than the conversation.
	comments, _ := svc.Comments(user, created[0].ID)
	var explained bool
	for _, comment := range comments {
		if comment.Kind == CommentSystem && strings.Contains(comment.Body, "no longer matches") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the move back was not explained: %+v", comments)
	}
}

// Somebody decided the task was done. An index diff is not entitled to
// overrule that.
func TestReconcileNeverReopensAClosedTask(t *testing.T) {
	svc, board := planBoard(t)
	created, _ := svc.CreateFromPlan(user, board.ID, []PlanSeed{{PlanNodeID: "n1", Label: "a"}})
	ctx := context.Background()
	if _, err := svc.Transition(ctx, user, TransitionInput{TaskID: created[0].ID, To: "done"}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ReconcilePlan(ctx, board.ID, map[string]string{"n1": PlanImplementedDiverged})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MovedBackToWIP) != 0 {
		t.Fatalf("a closed task was reopened: %+v", result)
	}
	after, _ := svc.Get(user, created[0].ID)
	if after.Status != "done" {
		t.Fatalf("status = %q", after.Status)
	}
}

// A task with no plan node is untouched: nothing else on the board is
// automatic, which is the point.
func TestReconcileLeavesUnlinkedTasksAlone(t *testing.T) {
	svc, board := planBoard(t)
	loose, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "unlinked"})
	if _, err := svc.ReconcilePlan(context.Background(), board.ID, map[string]string{"n1": PlanImplementedMatching}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Get(user, loose.ID)
	if after.Status != "todo" {
		t.Fatalf("an unlinked task moved to %q", after.Status)
	}
}

func TestSearchMatchesKeyTitleAndLabelButNotDescription(t *testing.T) {
	svc, board := planBoard(t)
	created, err := svc.Create(user, CreateInput{
		BoardID: board.ID, Title: "Add refunds", Labels: []string{"billing"},
		Description: "a secret body nobody should find by searching",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"refund", "BILLING", strings.ToLower(created.Key)} {
		found, err := svc.Search(user, board.ID, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 {
			t.Fatalf("Search(%q) = %d results", query, len(found))
		}
	}
	// The description is the encrypted half; searching it would mean
	// decrypting every task on the board to answer one query.
	if found, err := svc.Search(user, board.ID, "nobody should find"); err != nil || len(found) != 0 {
		t.Fatalf("Search matched a description: %d, %v", len(found), err)
	}
	if _, err := svc.Search(user, "", "x"); err == nil {
		t.Fatal("an unbounded search was allowed")
	}
}
