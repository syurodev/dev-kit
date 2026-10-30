package mcp

import (
	"context"
	"log"
	"strings"
	"unicode/utf8"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/tasks"
)

// A child started on a task (agenthub.createAgentSession with taskId).
const taskBriefRunes = agentwork.BriefRunes

type childOnTask struct {
	agenthub.AgentSession
	Task      taskRef               `json:"task"`
	Delivery  agenthub.PeerDelivery `json:"delivery"`
	Code      string                `json:"code,omitempty"`
	Reason    string                `json:"reason,omitempty"`
	MessageID string                `json:"messageId,omitempty"`
	// ToolGroups says what became of the child's starting tool groups, when any
	// were planned.
	ToolGroups *childGroupsReport `json:"toolGroups,omitempty"`
}

type taskRef struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// taskBrief preserves the package's existing helper for focused MCP tests.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func taskBrief(t tasks.Task) string {
	return agentwork.Brief(agentwork.Task{ID: t.ID, Key: t.Key, Title: t.Title, Description: t.Description})
}

type taskHandoffOps struct {
	server *Server
	create func(tasks.Task) (agenthub.AgentSession, string, error)
}

func (o taskHandoffOps) CheckTask(ctx context.Context, taskID string) (agentwork.Task, error) {
	s := o.server
	if s.deps.Tasks == nil {
		return agentwork.Task{}, apperrors.New("internal_error", "The task board is not available.", true)
	}
	tc := taskCaller(ctx)
	if !tc.IsAgent() {
		return agentwork.Task{}, apperrors.New("validation_failed", "taskId is for an agent session starting its own child.", true)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+taskID, meta("assignable", nil), func(context.Context) (any, error) {
		if _, err := s.deps.Tasks.Assignable(tc, taskID); err != nil {
			return nil, err
		}
		return s.deps.Tasks.Get(tc, taskID)
	})
	if err != nil {
		return agentwork.Task{}, err
	}
	task, _ := v.(tasks.Task)
	return toAgentworkTask(task), nil
}

func (o taskHandoffOps) CreateChild(_ context.Context, task agentwork.Task) (agentwork.Child, error) {
	created, branch, err := o.create(tasks.Task{ID: task.ID, Key: task.Key, Title: task.Title, Description: task.Description})
	if err != nil {
		return agentwork.Child{}, err
	}
	return agentwork.Child{ID: created.ID, Value: created, Branch: branch}, nil
}

func (o taskHandoffOps) AssignTask(ctx context.Context, task agentwork.Task, child agentwork.Child) error {
	tc := taskCaller(ctx)
	_, err := o.server.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+task.ID, meta("assign", map[string]any{"child_session": child.ID}), func(context.Context) (any, error) {
		return o.server.deps.Tasks.Assign(tc, task.ID, child.ID)
	})
	return err
}

func (o taskHandoffOps) RecordBranch(ctx context.Context, task agentwork.Task, child agentwork.Child, branch string) error {
	childCtx := identity.WithCaller(ctx, identity.Agent(child.ID))
	_, err := o.server.deps.Caller.Call(childCtx, "builtin.tasks", "tasks:write", "task:"+task.ID, meta("record_worktree_branch", map[string]any{"child_session": child.ID}), func(context.Context) (any, error) {
		return o.server.deps.Tasks.SetFields(taskCaller(childCtx), task.ID, tasks.Fields{Branch: &branch})
	})
	return err
}

func (o taskHandoffOps) SendBrief(ctx context.Context, task agentwork.Task, child agentwork.Child, brief string) (agentwork.Delivery, error) {
	subject, capability, scope := peerSendGrant(child.ID)
	v, err := o.server.deps.Caller.Call(ctx, subject, capability, scope, meta("send_message", map[string]any{
		"session_id": child.ID,
		"length":     utf8.RuneCountInString(brief),
		"task_id":    task.ID,
	}), func(callCtx context.Context) (any, error) {
		return o.server.deps.AgentHub.SendMessage(callCtx, agenthub.SendMessageInput{SessionID: child.ID, Message: brief})
	})
	if err != nil {
		return agentwork.Delivery{}, err
	}
	if sent, ok := v.(agenthub.SendMessageResult); ok {
		return agentwork.Delivery{State: string(sent.Delivery), Code: sent.Code, Reason: sent.Reason, MessageID: sent.MessageID}, nil
	}
	return agentwork.Delivery{}, nil
}

func (o taskHandoffOps) CloseChild(ctx context.Context, child agentwork.Child) error {
	return o.server.deps.AgentHub.CloseSession(ctx, child.ID)
}

func (o taskHandoffOps) Logf(format string, args ...any) { log.Printf(format, args...) }

func toAgentworkTask(t tasks.Task) agentwork.Task {
	return agentwork.Task{ID: t.ID, Key: t.Key, Title: t.Title, Description: t.Description}
}
