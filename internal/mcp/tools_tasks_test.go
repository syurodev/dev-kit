package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"devkit/internal/identity"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeTaskScope implements tasks.WorkspaceScope for testing.
type fakeTaskScope struct {
	sessionWorkspaces map[string][]string
	workspaces        map[string]bool // workspace IDs that exist
}

func TestTasksCommentListPagingAndGet(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	tasksSvc := tasks.NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
	board, err := tasksSvc.CreateBoard("Comments", "CM", "blue", tasks.DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasksSvc.Create(tasks.Caller{}, tasks.CreateInput{BoardID: board.ID, Title: "Paging"})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{Caller: &fakeCaller{}, Notes: &fakeNotes{}, Vault: &fakeVault{state: vault.StateUnlocked}, Search: &fakeSearch{}, Tools: fakeTools{}, Envs: &fakeEnvs{}, Tasks: tasksSvc})
	ctx := context.Background()
	ids := make([]string, 0, 205)
	for i := 0; i < 205; i++ {
		body := fmt.Sprintf("comment-%02d", i)
		c, err := tasksSvc.AddComment(tasks.Caller{}, task.ID, body)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	call := func(in taskCommentListInput) map[string]any {
		t.Helper()
		_, v, err := srv.toolTasksCommentList(ctx, &sdk.CallToolRequest{}, in)
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)
	}
	first := call(taskCommentListInput{TaskID: task.ID, Limit: 50})
	rows := first["comments"].([]compactTaskComment)
	if len(rows) != 50 || rows[0].ID != ids[204] || first["nextBeforeId"] != ids[155] {
		t.Fatalf("first page shape/order: rows=%d first=%s cursor=%v", len(rows), rows[0].ID, first["nextBeforeId"])
	}
	second := call(taskCommentListInput{TaskID: task.ID, Limit: 50, BeforeID: first["nextBeforeId"].(string)})
	rows2 := second["comments"].([]compactTaskComment)
	if len(rows2) != 50 || rows2[0].ID != ids[154] || rows2[49].ID != ids[105] || rows2[0].ID == rows[49].ID {
		t.Fatalf("second page: %#v", second)
	}
	last := call(taskCommentListInput{TaskID: task.ID, Limit: 50, BeforeID: second["nextBeforeId"].(string)})
	if len(last["comments"].([]compactTaskComment)) != 50 || last["nextBeforeId"] == "" {
		t.Fatal("third page should continue")
	}
	final := call(taskCommentListInput{TaskID: task.ID, Limit: 50, BeforeID: last["nextBeforeId"].(string)})
	if len(final["comments"].([]compactTaskComment)) != 50 || final["nextBeforeId"] == "" {
		t.Fatal("fourth page should continue")
	}
	end := call(taskCommentListInput{TaskID: task.ID, Limit: 50, BeforeID: final["nextBeforeId"].(string)})
	if len(end["comments"].([]compactTaskComment)) != 5 || end["nextBeforeId"] != "" {
		t.Fatal("last page cursor should be empty")
	}
	clamped := call(taskCommentListInput{TaskID: task.ID, Limit: 999})
	if len(clamped["comments"].([]compactTaskComment)) != 200 {
		t.Fatal("limit clamp should allow 200 comments")
	}
	if _, _, err := srv.toolTasksCommentList(ctx, &sdk.CallToolRequest{}, taskCommentListInput{TaskID: task.ID, BeforeID: "missing"}); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("unknown cursor err=%v", err)
	}
	long := strings.Repeat("a", 2047) + "é" + strings.Repeat("b", 20)
	longComment, err := tasksSvc.AddComment(tasks.Caller{}, task.ID, long)
	if err != nil {
		t.Fatal(err)
	}
	p := call(taskCommentListInput{TaskID: task.ID, Limit: 1})["comments"].([]compactTaskComment)[0]
	if p.ID != longComment.ID || !p.BodyTruncated || !utf8.ValidString(p.Body) {
		t.Fatalf("bad multibyte preview: truncated=%v valid=%v", p.BodyTruncated, utf8.ValidString(p.Body))
	}
	_, full, err := srv.toolTasksCommentGet(ctx, &sdk.CallToolRequest{}, taskCommentGetInput{TaskID: task.ID, CommentID: longComment.ID})
	if err != nil {
		t.Fatal(err)
	}
	if full.(tasks.Comment).Body != long {
		t.Fatal("comment.get did not return full body")
	}
	if _, _, err := srv.toolTasksCommentGet(ctx, &sdk.CallToolRequest{}, taskCommentGetInput{TaskID: task.ID, CommentID: "missing"}); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("unknown comment err=%v", err)
	}
}

func (f *fakeTaskScope) ServicePath(workspaceID, service string) (string, bool) {
	// For this test, we don't care about service paths
	return "/path/" + workspaceID, true
}

func (f *fakeTaskScope) HasPath(workspaceID, path string) bool {
	return false // Not tested here
}

func (f *fakeTaskScope) SessionWorkspaces(sessionID string) []string {
	return f.sessionWorkspaces[sessionID]
}

func (f *fakeTaskScope) Exists(workspaceID string) bool {
	return f.workspaces[workspaceID]
}

// TestTasksWorkspaceScopedAccessMCP verifies that taskCaller(ctx) correctly
// extracts agent identity and that workspace scope is enforced through the
// MCP tool handlers. An agent in workspace A cannot access tasks bound to
// workspace B through toolTasksGet or see B's board in toolTasksBoardList.
func TestTasksWorkspaceScopedAccessMCP(t *testing.T) {
	// Set up vault
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}

	// Set up database
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	// Set up services
	items := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	tasksService := tasks.NewService(items)
	wsService := workspace.NewService(workspace.NewSQLRepository(db))

	// Create two workspaces
	wsA, _, err := wsService.AddWorkspace("Workspace A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wsB, _, err := wsService.AddWorkspace("Workspace B", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Wire workspace scope into tasks
	scope := &fakeTaskScope{
		sessionWorkspaces: map[string][]string{
			"sess-a": {wsA.ID},
		},
		workspaces: map[string]bool{
			wsA.ID: true,
			wsB.ID: true,
		},
	}
	tasksService.SetScope(scope, db, storage.TableTaskBoardBindings)

	// Create boards and bind them
	boardA, err := tasksService.CreateBoard("Board A", "BA", "blue", tasks.DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasksService.BindBoard(boardA.ID, wsA.ID); err != nil {
		t.Fatal(err)
	}

	boardB, err := tasksService.CreateBoard("Board B", "BB", "red", tasks.DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasksService.BindBoard(boardB.ID, wsB.ID); err != nil {
		t.Fatal(err)
	}

	// Create a task in workspace B's board
	taskB, err := tasksService.Create(tasks.Caller{}, tasks.CreateInput{
		BoardID: boardB.ID,
		Title:   "Task in B",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set up MCP server with the scoped tasks service
	caller := &fakeCaller{}
	srv := NewServer(Deps{
		Caller: caller,
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{state: vault.StateUnlocked},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   &fakeEnvs{},
		Tasks:  tasksService,
	})

	// Test 1: agent in workspace A calls tasks.get on task in workspace B
	// → taskCaller(ctx) extracts "sess-a", which is scoped to wsA only
	// → should return tasks.notFound
	const sessA = "sess-a"
	ctx := identity.WithCaller(context.Background(), identity.Agent(sessA))
	_, _, errGet1 := srv.toolTasksGet(ctx, &sdk.CallToolRequest{}, taskIDInput{TaskID: taskB.ID})
	if errGet1 == nil {
		t.Fatal("tasks.get on out-of-scope task should error")
	}
	if !errors.Is(errGet1, tasks.ErrNotFound) {
		t.Fatalf("err = %v, want tasks.ErrNotFound", errGet1)
	}

	// Test 2: same agent calls tasks.get on task in workspace A → succeeds
	taskA, errCreate := tasksService.Create(tasks.Caller{}, tasks.CreateInput{
		BoardID: boardA.ID,
		Title:   "Task in A",
	})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	_, vGet, errGet := srv.toolTasksGet(ctx, &sdk.CallToolRequest{}, taskIDInput{TaskID: taskA.ID})
	if errGet != nil {
		t.Fatalf("tasks.get on in-scope task should succeed: %v", errGet)
	}
	if vGet == nil {
		t.Fatal("expected task result")
	}
	blockerA, err := tasksService.Create(tasks.Caller{}, tasks.CreateInput{BoardID: boardA.ID, Title: "Blocker in A"})
	if err != nil {
		t.Fatal(err)
	}
	_, setResult, err := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{blockerA.ID, blockerA.ID}, ExpectedBlockedBy: []string{},
	})
	if err != nil {
		t.Fatalf("tasks.setFields blockedBy: %v", err)
	}
	if got := unwrap(setResult).(taskOut).BlockedBy; len(got) != 1 || got[0] != blockerA.ID {
		t.Fatalf("set result blockedBy = %v", got)
	}
	_, omittedResult, err := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{TaskID: taskA.ID, Priority: "high"})
	if err != nil || len(unwrap(omittedResult).(taskOut).BlockedBy) != 1 {
		t.Fatalf("omitted blockedBy changed dependencies: %#v, err=%v", unwrap(omittedResult), err)
	}
	_, _, missingExpectedErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{TaskID: taskA.ID, BlockedBy: []string{}})
	if !errors.Is(missingExpectedErr, tasks.ErrExpectedDependenciesRequired) {
		t.Fatalf("missing expectedBlockedBy err = %v", missingExpectedErr)
	}
	_, _, selfErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{taskA.ID}, ExpectedBlockedBy: []string{blockerA.ID},
	})
	if !errors.Is(selfErr, tasks.ErrDependencyCycle) {
		t.Fatalf("self dependency err = %v", selfErr)
	}
	_, _, cycleErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: blockerA.ID, BlockedBy: []string{taskA.ID}, ExpectedBlockedBy: []string{},
	})
	if !errors.Is(cycleErr, tasks.ErrDependencyCycle) {
		t.Fatalf("cycle dependency err = %v", cycleErr)
	}
	ctxTerminal := identity.WithCaller(context.Background(), identity.UserTerminal())
	_, _, crossBoardErr := srv.toolTasksSetFields(ctxTerminal, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{taskB.ID}, ExpectedBlockedBy: []string{blockerA.ID},
	})
	if !errors.Is(crossBoardErr, tasks.ErrInvalid) {
		t.Fatalf("cross-board dependency err = %v", crossBoardErr)
	}
	_, _, inaccessibleErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{taskB.ID}, ExpectedBlockedBy: []string{blockerA.ID},
	})
	_, _, missingTargetErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{"not-a-task"}, ExpectedBlockedBy: []string{blockerA.ID},
	})
	if !errors.Is(inaccessibleErr, tasks.ErrNotFound) || !errors.Is(missingTargetErr, tasks.ErrNotFound) {
		t.Fatalf("inaccessible target err=%v, missing target err=%v", inaccessibleErr, missingTargetErr)
	}
	competingA, err := tasksService.Create(tasks.Caller{}, tasks.CreateInput{BoardID: boardA.ID, Title: "Competing blocker"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, replacement := range [][]string{{}, {competingA.ID}} {
		replacement := replacement
		go func() {
			<-start
			_, _, err := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
				TaskID: taskA.ID, BlockedBy: replacement, ExpectedBlockedBy: []string{blockerA.ID},
			})
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	wins := 0
	for _, err := range []error{first, second} {
		if err == nil {
			wins++
		} else if !errors.Is(err, tasks.ErrDependenciesChanged) {
			t.Fatalf("concurrent replacement err = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent replacement successes = %d, want 1", wins)
	}
	_, readResult, err := srv.toolTasksGet(ctx, &sdk.CallToolRequest{}, taskIDInput{TaskID: taskA.ID})
	if err != nil {
		t.Fatalf("tasks.get dependency = %#v, err=%v", unwrap(readResult), err)
	}
	_, errResult, staleErr := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{}, ExpectedBlockedBy: []string{"stale-snapshot"},
	})
	if staleErr == nil || errResult != nil || !errors.Is(staleErr, tasks.ErrDependenciesChanged) {
		t.Fatalf("stale replacement result=%v err=%v", errResult, staleErr)
	}
	// Either replacement may have won. When the empty one did, BlockedBy reads
	// back nil, and a nil expected set means "not supplied" (expectedDependenciesRequired).
	// An empty, non-nil snapshot is the same state and is accepted.
	expected := append([]string{}, unwrap(readResult).(tasks.Task).BlockedBy...)
	_, clearResult, err := srv.toolTasksSetFields(ctx, &sdk.CallToolRequest{}, tasksSetFieldsInput{
		TaskID: taskA.ID, BlockedBy: []string{}, ExpectedBlockedBy: expected,
	})
	if err != nil || len(unwrap(clearResult).(taskOut).BlockedBy) != 0 {
		t.Fatalf("clear blockedBy = %#v, err=%v", unwrap(clearResult), err)
	}

	// Test 3: agent in workspace A calls tasks.board.list
	// → should only see board A, not board B
	_, vBoardList, errBoardList := srv.toolTasksBoardList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if errBoardList != nil {
		t.Fatalf("tasks.board.list: %v", errBoardList)
	}
	boardsResult, ok := unwrap(vBoardList).([]tasks.Board)
	if !ok || len(boardsResult) != 1 || boardsResult[0].ID != boardA.ID {
		t.Fatalf("agent should see only board A, got %#v", boardsResult)
	}

	// Test 4: user terminal (unscoped) calls tasks.board.list
	// → should see all boards
	_, vTerminal, err := srv.toolTasksBoardList(ctxTerminal, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatalf("tasks.board.list for terminal: %v", err)
	}
	boardsTerminal, ok := unwrap(vTerminal).([]tasks.Board)
	if !ok || len(boardsTerminal) != 2 {
		t.Fatalf("terminal should see both boards, got %d boards", len(boardsTerminal))
	}

	// Test 5: the agent makes a board. It is asked for as tasks:write, not
	// admin; it lands in the agent's own workspace, carries no verify command,
	// and the agent sees it next to board A — and still not board B.
	_, vCreated, err := srv.toolTasksBoardCreate(ctx, &sdk.CallToolRequest{}, tasksBoardCreateInput{
		Name: "Agent board", KeyPrefix: "ag",
		Statuses: []tasksBoardStatusInput{
			{Name: "Todo", Category: "open"}, {Name: "Doing", Category: "active"}, {Name: "Done", Category: "terminal"},
		},
	})
	if err != nil {
		t.Fatalf("tasks.board.create: %v", err)
	}
	if caller.last.capability != "tasks:write" || caller.last.scope != "board:new" {
		t.Fatalf("board create asked for %s %s, want tasks:write board:new", caller.last.capability, caller.last.scope)
	}
	created, ok := unwrap(vCreated).(tasks.Board)
	if !ok || created.WorkspaceID != wsA.ID || created.Verify != nil || created.Workflow.Statuses[1].ID != "doing" {
		t.Fatalf("created board = %#v", unwrap(vCreated))
	}
	_, vAfter, err := srv.toolTasksBoardList(ctx, &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if boards, _ := unwrap(vAfter).([]tasks.Board); len(boards) != 2 {
		t.Fatalf("agent should see board A and its own board, got %#v", boards)
	}

	// Test 6: nor can it put one in workspace B.
	if _, _, err := srv.toolTasksBoardCreate(ctx, &sdk.CallToolRequest{}, tasksBoardCreateInput{
		Name: "Sneaky", KeyPrefix: "sn", WorkspaceID: wsB.ID,
	}); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("board in another workspace: err = %v, want tasks.ErrNotFound", err)
	}
}
