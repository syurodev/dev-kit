package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/tasks"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// childTasks is the slice of the board a child-on-a-task start touches.
type childTasks struct {
	TasksAPI
	task         tasks.Task
	assignable   error
	assignErr    error
	assignedID   string
	assignedTo   string
	assignedBy   tasks.Caller
	checkedBy    tasks.Caller
	assignCalls  int
	branch       string
	branchBy     tasks.Caller
	branchWrites int
}

func (f *childTasks) Assignable(c tasks.Caller, id string) (tasks.Task, error) {
	f.checkedBy = c
	return f.task, f.assignable
}

func (f *childTasks) Get(c tasks.Caller, id string) (tasks.Task, error) { return f.task, nil }

func (f *childTasks) Assign(c tasks.Caller, id, sessionID string) (tasks.Task, error) {
	f.assignCalls++
	f.assignedBy, f.assignedID, f.assignedTo = c, id, sessionID
	return f.task, f.assignErr
}

func (f *childTasks) SetFields(c tasks.Caller, id string, fields tasks.Fields) (tasks.Task, error) {
	if fields.Branch != nil {
		f.branchWrites++
		f.branchBy = c
		f.branch = *fields.Branch
	}
	return f.task, nil
}

func childTaskFixture() (*Server, *fakeAgentHub, *childTasks, context.Context) {
	hub := &fakeAgentHub{
		created: agenthub.AgentSession{ID: "child-1", AgentType: agenthub.AgentTypeMock, Status: agenthub.SessionStatusIdle},
		sendOut: agenthub.SendMessageResult{MessageID: "m1", Delivery: agenthub.DeliveryStarted},
	}
	board := &childTasks{task: tasks.Task{ID: "t1", Key: "WEB-abc123", Title: "Fix  the\nlogin", Description: "Make login keep the session.\nDone = the e2e passes."}}
	deps := agentHubTestDeps(&fakeCaller{}, hub)
	deps.Tasks = board
	ctx := identity.WithCaller(context.Background(), identity.Agent("parent-1"))
	return NewServer(deps), hub, board, ctx
}

func TestCreateAgentSessionOnATaskClaimsItAndSendsItAsTheFirstMessage(t *testing.T) {
	srv, hub, board, ctx := childTaskFixture()

	_, out, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p", TaskID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if board.assignedID != "t1" || board.assignedTo != "child-1" || board.assignedBy.SessionID != "parent-1" {
		t.Errorf("assign = task %q to %q by %+v, want t1 to child-1 by the parent", board.assignedID, board.assignedTo, board.assignedBy)
	}
	if hub.createIn.Title == nil || *hub.createIn.Title != "Fix the login" {
		t.Errorf("title = %v, want the task's title on one line", hub.createIn.Title)
	}
	if !hub.sendCalled || hub.sendIn.SessionID != "child-1" {
		t.Fatalf("first message: called %v to %q", hub.sendCalled, hub.sendIn.SessionID)
	}
	for _, want := range []string{"WEB-abc123", "Fix the login", "Task id: t1", "already claimed", "Done = the e2e passes."} {
		if !strings.Contains(hub.sendIn.Message, want) {
			t.Errorf("the first message lacks %q:\n%s", want, hub.sendIn.Message)
		}
	}
	raw, _ := json.Marshal(out)
	for _, want := range []string{`"ID":"child-1"`, `"key":"WEB-abc123"`, `"title":"Fix the login"`, `"delivery":"started"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the answer lacks %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "Make login keep") {
		t.Errorf("the answer repeats the description the parent wrote: %s", raw)
	}
}

func TestWorktreeBranchIsRecordedOnlyAfterChildReceivesTask(t *testing.T) {
	srv, hub, board, ctx := childTaskFixture()
	hub.status = agenthub.AgentSession{ID: "parent-1", ProjectPath: "/tmp/p"}
	srv.deps.GitRepos = fakeRepos{"/tmp/p": &fakeGit{}}
	_, _, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", TaskID: "t1", Worktree: "feature/task"})
	if err != nil {
		t.Fatal(err)
	}
	if board.branchWrites != 1 || board.branch != "feature/task" || board.branchBy.SessionID != "child-1" {
		t.Fatalf("branch writes=%d branch=%q by=%+v, want one write by the task-holding child", board.branchWrites, board.branch, board.branchBy)
	}
}

func TestATaskThatCannotBeGivenAwayLeavesNoSessionBehind(t *testing.T) {
	srv, hub, board, ctx := childTaskFixture()
	board.assignable = tasks.ErrNotAssignable

	_, _, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p", TaskID: "t1"})
	if !errors.Is(err, tasks.ErrNotAssignable) {
		t.Fatalf("err = %v", err)
	}
	if hub.createIn.AgentType != "" || hub.sendCalled || board.assignCalls != 0 {
		t.Errorf("a session was started for a task that cannot be given: create %+v, sent %v, assigns %d", hub.createIn, hub.sendCalled, board.assignCalls)
	}
}

func TestAClaimLostAfterTheSessionExistsClosesTheSession(t *testing.T) {
	srv, hub, board, ctx := childTaskFixture()
	board.assignErr = tasks.ErrClaimed

	_, _, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p", TaskID: "t1"})
	if !errors.Is(err, tasks.ErrClaimed) {
		t.Fatalf("err = %v", err)
	}
	if hub.closeID != "child-1" || hub.sendCalled {
		t.Errorf("closed %q, sent %v: want the new session closed and nothing sent", hub.closeID, hub.sendCalled)
	}
	if board.branchWrites != 0 {
		t.Errorf("recorded a branch on a task the child did not receive: %+v", board)
	}
}

func TestOnlyAnAgentStartsAChildOnATask(t *testing.T) {
	srv, hub, board, _ := childTaskFixture()
	for name, ctx := range map[string]context.Context{
		"no caller": context.Background(),
		"terminal":  identity.WithCaller(context.Background(), identity.UserTerminal()),
	} {
		if _, _, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p", TaskID: "t1"}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if hub.createIn.AgentType != "" || board.assignCalls != 0 {
		t.Errorf("something was started: %+v", hub.createIn)
	}
}

func TestAStartWithoutATaskNeverTouchesTheBoard(t *testing.T) {
	srv, hub, board, ctx := childTaskFixture()
	if _, _, err := srv.toolAgentHubCreateAgentSession(ctx, &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", Cwd: "/tmp/p"}); err != nil {
		t.Fatal(err)
	}
	if board.assignCalls != 0 || board.checkedBy.SessionID != "" || hub.sendCalled {
		t.Errorf("a plain start touched the board or sent a message: assigns %d, checked %+v, sent %v", board.assignCalls, board.checkedBy, hub.sendCalled)
	}
}

func TestTheTaskBriefIsShortAndCutsAHugeDescription(t *testing.T) {
	plain := taskBrief(tasks.Task{ID: "t1", Key: "WEB-1", Title: "x"})
	if !strings.Contains(plain, "no description beyond its title") {
		t.Errorf("empty description: %s", plain)
	}
	huge := taskBrief(tasks.Task{ID: "t1", Key: "WEB-1", Title: "x", Description: strings.Repeat("é", taskBriefRunes*2)})
	if got := len([]rune(huge)); got > taskBriefRunes+400 {
		t.Errorf("a huge description made a %d-character message", got)
	}
	if !strings.Contains(huge, "cut at") || !strings.Contains(huge, "the rest is on the task") {
		t.Errorf("the cut is not said: …%s", huge[len(huge)-120:])
	}
}
