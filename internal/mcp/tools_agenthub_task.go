package mcp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/tasks"
)

// A child started on a task (agenthub.createAgentSession with taskId).
//
// The brief of a job that changes code lives on a task: the person sees it on
// the board, it survives a restart, and its description is one place instead of
// a message the parent retyped. So instead of the parent creating a session,
// then telling it which task to take, DevKit does the three steps with nothing
// for the model to get wrong: it checks the task can be given away before the
// session exists, claims it for the new session, and sends the session the task
// as its first message — which the parent never writes.

// taskBriefRunes bounds the description copied into the child's first message;
// the rest stays on the task, which the child reads in full.
const taskBriefRunes = 8000

// childOnTask is what the parent is told: the new session, the task it holds,
// and what became of the first message.
type childOnTask struct {
	agenthub.AgentSession
	Task      taskRef               `json:"task"`
	Delivery  agenthub.PeerDelivery `json:"delivery"`
	Code      string                `json:"code,omitempty"`
	Reason    string                `json:"reason,omitempty"`
	MessageID string                `json:"messageId,omitempty"`
}

type taskRef struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// taskBrief is the child's first message: which task, that it is already
// claimed, and the description the parent wrote. The rules of working on a
// task are in the system prompt; the message does not repeat them.
func taskBrief(t tasks.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your task: %s — %s\nTask id: %s. It is already claimed for you.\n\n", t.Key, oneLine(t.Title), t.ID)
	desc := strings.TrimSpace(t.Description)
	switch {
	case desc == "":
		b.WriteString("The task has no description beyond its title.")
	case utf8.RuneCountInString(desc) > taskBriefRunes:
		b.WriteString(string([]rune(desc)[:taskBriefRunes]))
		fmt.Fprintf(&b, "\n\n[The description is cut at %d characters; the rest is on the task.]", taskBriefRunes)
	default:
		b.WriteString(desc)
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// prepareTaskForChild reads the task an agent wants to start a child on and
// refuses, before any session exists, one it cannot give away: out of its
// scope, finished or archived, or held by another live session.
func (s *Server) prepareTaskForChild(ctx context.Context, taskID string) (tasks.Task, error) {
	if s.deps.Tasks == nil {
		return tasks.Task{}, apperrors.New("internal_error", "The task board is not available.", true)
	}
	tc := taskCaller(ctx)
	if !tc.IsAgent() {
		return tasks.Task{}, apperrors.New("validation_failed", "taskId is for an agent session starting its own child.", true)
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+taskID, meta("assignable", nil), func(context.Context) (any, error) {
		if _, err := s.deps.Tasks.Assignable(tc, taskID); err != nil {
			return nil, err
		}
		return s.deps.Tasks.Get(tc, taskID)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	task, _ := v.(tasks.Task)
	return task, nil
}

// giveChildItsTask claims the task for the child that was just created and
// sends it the task as its first message. A claim that fails (it was taken in
// the meantime) closes the child again: a session nobody asked for is not left
// behind. A message that fails does not undo the claim — the child holds the
// task and the parent is told the delivery, so it can send the message itself.
func (s *Server) giveChildItsTask(ctx context.Context, sess agenthub.AgentSession, task tasks.Task) (any, error) {
	tc := taskCaller(ctx)
	if _, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+task.ID, meta("assign", map[string]any{"child_session": sess.ID}), func(context.Context) (any, error) {
		return s.deps.Tasks.Assign(tc, task.ID, sess.ID)
	}); err != nil {
		if closeErr := s.deps.AgentHub.CloseSession(ctx, sess.ID); closeErr != nil {
			log.Printf("mcp: close session %s after its task could not be assigned: %v", sess.ID, closeErr)
		}
		return nil, err
	}

	out := childOnTask{AgentSession: sess, Task: taskRef{ID: task.ID, Key: task.Key, Title: oneLine(task.Title)}}
	text := taskBrief(task)
	subject, capability, scope := peerSendGrant(sess.ID)
	v, err := s.deps.Caller.Call(ctx, subject, capability, scope, meta("send_message", map[string]any{
		"session_id": sess.ID,
		"length":     utf8.RuneCountInString(text),
		"task_id":    task.ID,
	}), func(callCtx context.Context) (any, error) {
		return s.deps.AgentHub.SendMessage(callCtx, agenthub.SendMessageInput{SessionID: sess.ID, Message: text})
	})
	if err != nil {
		out.Delivery, out.Code = agenthub.DeliveryRefused, "not_sent"
		out.Reason = "The task is claimed for the new session but its first message could not be sent; send it with agenthub.sendMessage."
		log.Printf("mcp: first message to session %s on task %s: %v", sess.ID, task.ID, err)
		return out, nil
	}
	if sent, ok := v.(agenthub.SendMessageResult); ok {
		out.Delivery, out.Code, out.Reason, out.MessageID = sent.Delivery, sent.Code, sent.Reason, sent.MessageID
	}
	return out, nil
}
