// internal/codegraph/comments_test.go
package codegraph

import (
	"context"
	"errors"
	"testing"
)

func TestStoreAddAndListComments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, c := range []PlanComment{
		{PlanNodeID: "pn-001", CommentID: "c1", Author: "user", Body: "use a queue here", CreatedAt: 100},
		{PlanNodeID: "pn-001", CommentID: "c2", Author: "agent", Body: "ok, switching", CreatedAt: 200, SessionID: "s1"},
		{PlanNodeID: "pn-002", CommentID: "c3", Author: "user", Body: "unrelated", CreatedAt: 300},
	} {
		if err := store.AddComment(ctx, c); err != nil {
			t.Fatalf("AddComment %s: %v", c.CommentID, err)
		}
	}

	thread, err := store.ListComments(ctx, "pn-001")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 2 {
		t.Fatalf("thread = %+v, want 2", thread)
	}
	if thread[0].CommentID != "c1" || thread[1].CommentID != "c2" {
		t.Fatalf("thread = %+v, want oldest first", thread)
	}
	if thread[1].Author != "agent" || thread[1].SessionID != "s1" {
		t.Fatalf("reply = %+v, want author agent in session s1", thread[1])
	}

	counts, err := store.CountComments(ctx)
	if err != nil {
		t.Fatalf("CountComments: %v", err)
	}
	if counts["pn-001"] != 2 || counts["pn-002"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestStoreListCommentsEmptyThread(t *testing.T) {
	store := newTestStore(t)
	thread, err := store.ListComments(context.Background(), "pn-nope")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 0 {
		t.Fatalf("thread = %+v, want empty", thread)
	}
}

// A new plan replaces the old one, and its threads go with it — comments do
// NOT migrate to a same-FQN node in the new plan (codegraph.mdx § Non-goals).
func TestStoreSavePlanClearsComments(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.AddComment(ctx, PlanComment{
		PlanNodeID: "pn-001", CommentID: "c1", Author: "user", Body: "x", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := store.SavePlan(ctx, Plan{
		Description: "next",
		State:       PlanStateDraft,
		Nodes:       []PlanNode{{PlanNodeID: "pn-001", Label: "A", FQN: "com.A", Kind: "class"}},
	}); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	thread, err := store.ListComments(ctx, "pn-001")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 0 {
		t.Fatalf("thread = %+v, want empty after a new plan replaced the old one", thread)
	}
}

func TestServiceCommentAndReplyRequireActiveProject(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	ctx := context.Background()
	if err := svc.CommentPlanNode(ctx, "pn-001", "hi"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("CommentPlanNode err = %v, want ErrNoActiveProject", err)
	}
	if err := svc.ReplyPlanNode(ctx, "pn-001", "hi", "s1"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("ReplyPlanNode err = %v, want ErrNoActiveProject", err)
	}
	if _, err := svc.ListPlanNodeComments(ctx, "pn-001"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("ListPlanNodeComments err = %v, want ErrNoActiveProject", err)
	}
}

// An empty body must be rejected before the store is reached, or a stray Enter
// key writes a blank turn into a thread the agent will read.
func TestServiceRejectsBlankComment(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	ctx := context.Background()
	if err := svc.CommentPlanNode(ctx, "pn-001", "   \n\t "); err == nil {
		t.Fatal("expected an error for a whitespace-only comment")
	} else if errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("err = %v, want the blank-body check to run BEFORE the store lookup", err)
	}
	if err := svc.ReplyPlanNode(ctx, "pn-001", "", "s1"); err == nil {
		t.Fatal("expected an error for an empty reply")
	} else if errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("err = %v, want the blank-body check first", err)
	}
}
