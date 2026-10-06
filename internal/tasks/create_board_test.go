package tasks

import (
	"errors"
	"testing"
)

func TestAnAgentsBoardLandsInItsOwnWorkspaceAndIsVisibleToIt(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	addWorkspace(t, db, "ws-b")
	scope.sessions[agent.SessionID] = []string{"ws-a", "ws-b"}

	board, err := svc.CreateBoardFor(agent, CreateBoardInput{Name: "Backend", KeyPrefix: "be"})
	if err != nil {
		t.Fatal(err)
	}
	// Empty workspace is the session's first; the default workflow; no gate
	// command of its own.
	if board.WorkspaceID != "ws-a" || board.KeyPrefix != "BE" || len(board.Workflow.Statuses) != len(DefaultWorkflow().Statuses) {
		t.Fatalf("board = %+v", board)
	}
	if board.Verify != nil {
		t.Fatalf("an agent's board must carry no verify command, got %+v", board.Verify)
	}
	if _, err := svc.Board(agent, board.ID); err != nil {
		t.Fatalf("the agent cannot see the board it made: %v", err)
	}

	second, err := svc.CreateBoardFor(agent, CreateBoardInput{Name: "Ops", KeyPrefix: "ops", WorkspaceID: "ws-b"})
	if err != nil || second.WorkspaceID != "ws-b" {
		t.Fatalf("named workspace: %+v, %v", second, err)
	}
}

func TestAnAgentCannotPutABoardOutsideItsWorkspaces(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	addWorkspace(t, db, "ws-b")
	scope.sessions[agent.SessionID] = []string{"ws-a"}

	if _, err := svc.CreateBoardFor(agent, CreateBoardInput{Name: "X", KeyPrefix: "x", WorkspaceID: "ws-b"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another workspace: err = %v, want ErrNotFound", err)
	}
	lost := Caller{SessionID: "sess-lost"}
	if _, err := svc.CreateBoardFor(lost, CreateBoardInput{Name: "X", KeyPrefix: "x"}); !errors.Is(err, ErrBoardUnbound) {
		t.Fatalf("no workspace at all: err = %v, want ErrBoardUnbound", err)
	}
	// Nothing was left behind by either refusal.
	all, err := svc.Boards(user)
	if err != nil || len(all) != 0 {
		t.Fatalf("boards after refusals = %+v, %v", all, err)
	}
}

func TestTheUserMayCreateAnUnboundBoardButNotIntoANonexistentWorkspace(t *testing.T) {
	svc, _, _ := scopedService(t)
	board, err := svc.CreateBoardFor(user, CreateBoardInput{Name: "Loose", KeyPrefix: "l"})
	if err != nil || board.WorkspaceID != "" {
		t.Fatalf("unbound: %+v, %v", board, err)
	}
	if _, err := svc.CreateBoardFor(user, CreateBoardInput{Name: "X", KeyPrefix: "x", WorkspaceID: "ws-nope"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing workspace: err = %v, want ErrInvalid", err)
	}
}

func TestStatusIDsAreMadeFromNamesAndTheWorkflowMustBeRunnable(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	scope.sessions[agent.SessionID] = []string{"ws-a"}

	board, err := svc.CreateBoardFor(agent, CreateBoardInput{Name: "B", KeyPrefix: "b", Workflow: Workflow{Statuses: []Status{
		{Name: "Backlog", Category: CategoryOpen},
		{Name: "Đang làm", Category: CategoryActive},
		{Name: "In review", Category: CategoryActive},
		{Name: "In review", Category: CategoryActive},
		{Name: "Đã ship!", Category: CategoryTerminal},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, status := range board.Workflow.Statuses {
		ids = append(ids, status.ID)
	}
	want := []string{"backlog", "đang_làm", "in_review", "in_review_2", "đã_ship"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}

	// No terminal column: a board nobody can finish work on is refused.
	_, err = svc.CreateBoardFor(agent, CreateBoardInput{Name: "B", KeyPrefix: "b", Workflow: Workflow{Statuses: []Status{
		{Name: "Todo", Category: CategoryOpen}, {Name: "Doing", Category: CategoryActive},
	}}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unrunnable workflow: err = %v, want ErrInvalid", err)
	}
}
