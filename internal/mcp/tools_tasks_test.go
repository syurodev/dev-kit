package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
	ctxTerminal := identity.WithCaller(context.Background(), identity.UserTerminal())
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
