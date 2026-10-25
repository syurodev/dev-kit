// Package agentwork coordinates handing a task to a newly created agent child.
// Host-specific authorization remains in the supplied operations.
package agentwork

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const BriefRunes = 8000

// Task is the task data needed for a handoff. The host maps its domain task to
// this small DTO so orchestration does not depend on a task-board package.
type Task struct {
	ID          string
	Key         string
	Title       string
	Description string
}

// Child contains the host's session value and stable identifier for task
// assignment and rollback.
type Child struct {
	ID     string
	Value  any
	Branch string
}

// Delivery reports the result of sending the task brief.
type Delivery struct {
	State     string
	Code      string
	Reason    string
	MessageID string
}

// Result is the completed handoff. Child.Value is passed through unchanged for
// the host to expose in its existing wire format.
type Result struct {
	Child    any
	Task     Task
	Delivery Delivery
}

// Operations are host adapters. Authorization and caller identity must be
// applied by each operation at the point where the host performs the action.
type Operations interface {
	CheckTask(context.Context, string) (Task, error)
	CreateChild(context.Context, Task) (Child, error)
	AssignTask(context.Context, Task, Child) error
	RecordBranch(context.Context, Task, Child, string) error
	SendBrief(context.Context, Task, Child, string) (Delivery, error)
	CloseChild(context.Context, Child) error
	Logf(format string, args ...any)
}

// Handoff checks the task before creating a child, claims it, records an
// optional branch, and sends the task brief. Assignment or branch failures
// close the child; delivery failures preserve the assignment and are returned
// as a refusal so the parent can retry the message itself.
func Handoff(ctx context.Context, taskID string, ops Operations) (Result, error) {
	task, err := ops.CheckTask(ctx, taskID)
	if err != nil {
		return Result{}, err
	}
	child, err := ops.CreateChild(ctx, task)
	if err != nil {
		return Result{}, err
	}
	if err := ops.AssignTask(ctx, task, child); err != nil {
		closeAfterFailure(ctx, ops, child, "its task could not be assigned")
		return Result{}, err
	}
	if child.Branch != "" {
		if err := ops.RecordBranch(ctx, task, child, child.Branch); err != nil {
			closeAfterFailure(ctx, ops, child, "its worktree branch could not be recorded")
			return Result{}, err
		}
	}

	result := Result{Child: child.Value, Task: task}
	brief := Brief(task)
	delivery, err := ops.SendBrief(ctx, task, child, brief)
	if err != nil {
		result.Delivery = Delivery{
			State:  "refused",
			Code:   "not_sent",
			Reason: "The task is claimed for the new session but its first message could not be sent; send it with agenthub.sendMessage.",
		}
		ops.Logf("mcp: first message to session %s on task %s: %v", child.ID, task.ID, err)
		return result, nil
	}
	result.Delivery = delivery
	return result, nil
}

func closeAfterFailure(ctx context.Context, ops Operations, child Child, reason string) {
	// Assignment and branch writes can fail as the request is being canceled.
	// Preserve caller values for the close operation, but give rollback a short
	// independent deadline so cancellation cannot leak a newly spawned child.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := ops.CloseChild(closeCtx, child); err != nil {
		ops.Logf("mcp: close session %s after %s: %v", child.ID, reason, err)
	}
}

// Brief creates the bounded first message sent to a task's child.
func Brief(task Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Your task: %s — %s\nTask id: %s. It is already claimed for you.\n\n", task.Key, oneLine(task.Title), task.ID)
	desc := strings.TrimSpace(task.Description)
	switch {
	case desc == "":
		b.WriteString("The task has no description beyond its title.")
	case utf8.RuneCountInString(desc) > BriefRunes:
		b.WriteString(string([]rune(desc)[:BriefRunes]))
		fmt.Fprintf(&b, "\n\n[The description is cut at %d characters; the rest is on the task.]", BriefRunes)
	default:
		b.WriteString(desc)
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
