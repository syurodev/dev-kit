package mcp

import (
	"context"

	"devkit/internal/identity"
	"devkit/internal/tasks"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TasksAPI is the board surface MCP tools invoke.
type TasksAPI interface {
	Boards(caller tasks.Caller) ([]tasks.Board, error)
	List(caller tasks.Caller, in tasks.ListInput) ([]tasks.Task, error)
	Get(caller tasks.Caller, id string) (tasks.Task, error)
	Create(caller tasks.Caller, in tasks.CreateInput) (tasks.Task, error)
	Update(caller tasks.Caller, id, title, description string) (tasks.Task, error)
	SetFields(caller tasks.Caller, id string, fields tasks.Fields) (tasks.Task, error)
	Transition(ctx context.Context, caller tasks.Caller, in tasks.TransitionInput) (tasks.TransitionResult, error)
	Claim(caller tasks.Caller, id string) (tasks.Task, error)
	Release(caller tasks.Caller, id string) (tasks.Task, error)
	Archive(caller tasks.Caller, id string) (tasks.Task, error)
	AddComment(caller tasks.Caller, taskID, body string) (tasks.Comment, error)
	Comments(caller tasks.Caller, taskID string) ([]tasks.Comment, error)
	Progress(caller tasks.Caller, parentID string) (tasks.Progress, error)
	Search(caller tasks.Caller, boardID, query string) ([]tasks.Task, error)
	LinkPlanNode(caller tasks.Caller, id, planNodeID string) (tasks.Task, error)
}

// PlanBacklog turns an approved Code Graph plan into tasks. It is a function
// rather than a method on TasksAPI because it is the join of two modules that
// must not import each other, and only the composition root can see both.
type PlanBacklog interface {
	CreateTasksFromPlan(ctx context.Context, caller tasks.Caller, boardID string) ([]tasks.Task, error)
}

// taskCaller turns the request's identity into the board's. An agent's
// authorship comes from here, never from a tool parameter — which is what
// stops an agent signing a comment as the user.
func taskCaller(ctx context.Context) tasks.Caller {
	if caller, ok := identity.FromContext(ctx); ok && caller.IsAgent() {
		return tasks.Caller{SessionID: caller.SessionID}
	}
	return tasks.Caller{}
}

func (s *Server) registerTasks(ms *sdk.Server) {
	if s.deps.Tasks == nil {
		return
	}

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.board.list",
		Description: "List boards, with each board's workflow so you know which statuses exist and what category each one is",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksBoardList)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.list",
		Description: "List a board's tasks (summaries; no description)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksList)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.get",
		Description: "Get one task with its description and history; comments come from tasks.comment.list",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksGet)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.create",
		Description: "Create a task, including a subtask of the one you are working on",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksCreate)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.update",
		Description: "Update a task's title and description",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksUpdate)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.setFields",
		Description: "Set metadata: priority, assignee, parent, or the branch this task's work is on",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksSetFields)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.claim",
		Description: "Take a task. Does not touch the work tree — create a branch separately if you want one",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksClaim)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.release",
		Description: "Give a task back without moving it",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksRelease)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "tasks.transition",
		Description: "Move a task. Statuses in the open and active categories go straight through; " +
			"a terminal one goes through the board's gate — either a verify command DevKit runs, " +
			"whose exit code decides, or a confirmation every time. Pass expectedStatus to be " +
			"refused rather than overwrite someone else's move",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksTransition)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.comment.add",
		Description: "Comment on a task. Authorship is recorded from your session; you cannot post as the user",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksCommentAdd)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.comment.list",
		Description: "List a task's comments",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksCommentList)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.progress",
		Description: "Count a task's subtasks and how many are finished",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksProgress)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "tasks.search",
		Description: "Find tasks by key, title or label. Descriptions are not searched — they are " +
			"encrypted, and searching them would mean decrypting the whole board",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolTasksSearch)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "tasks.linkPlanNode",
		Description: "Point a task at a Code Graph plan node, or pass an empty id to clear it. " +
			"The link lives on the task",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolTasksLinkPlanNode)

	if s.deps.PlanBacklog != nil {
		sdk.AddTool(ms, &sdk.Tool{
			Name: "tasks.createFromPlan",
			Description: "Create one task per node of the project's APPROVED plan, each linked to its " +
				"node. A draft plan is refused. Safe to call twice: a node that already has a task is skipped",
			Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
		}, s.toolTasksCreateFromPlan)
	}

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "tasks.archive",
		Description: "Archive a task. Recoverable, which is why an agent may do it and delete stays with the user",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, s.toolTasksArchive)

	// Deliberately absent, whatever the grant:
	//
	//   tasks.delete — permanent; archive is the reversible one.
	//   tasks.board.create / update / delete — changing a board's workflow or
	//     its verify command is changing the rule that binds the agent. An
	//     agent that can edit the gate has no gate.
}

func (s *Server) toolTasksBoardList(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "board:list", meta("board_list", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Boards(taskCaller(ctx))
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksListInput struct {
	BoardID  string `json:"boardId"`
	Status   string `json:"status,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Label    string `json:"label,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (s *Server) toolTasksList(ctx context.Context, _ *sdk.CallToolRequest, in tasksListInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "task:list", meta("list", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.List(taskCaller(ctx), tasks.ListInput{
			BoardID: in.BoardID, Status: in.Status, Assignee: in.Assignee,
			Label: in.Label, Limit: in.Limit,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type taskIDInput struct {
	TaskID string `json:"taskId"`
}

func (s *Server) toolTasksGet(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "task:"+in.TaskID, meta("get", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Get(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksCreateInput struct {
	BoardID     string   `json:"boardId"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	ParentID    string   `json:"parentId,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

func (s *Server) toolTasksCreate(ctx context.Context, _ *sdk.CallToolRequest, in tasksCreateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:new", meta("create", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Create(taskCaller(ctx), tasks.CreateInput{
			BoardID: in.BoardID, Title: in.Title, Description: in.Description,
			Priority: in.Priority, ParentID: in.ParentID, Labels: in.Labels,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksUpdateInput struct {
	TaskID      string `json:"taskId"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (s *Server) toolTasksUpdate(ctx context.Context, _ *sdk.CallToolRequest, in tasksUpdateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("update", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Update(taskCaller(ctx), in.TaskID, in.Title, in.Description)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksSetFieldsInput struct {
	TaskID   string   `json:"taskId"`
	Priority string   `json:"priority,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	ParentID string   `json:"parentId,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Labels   []string `json:"labels,omitempty"`
}

func (s *Server) toolTasksSetFields(ctx context.Context, _ *sdk.CallToolRequest, in tasksSetFieldsInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("set_fields", nil), func(context.Context) (any, error) {
		fields := tasks.Fields{}
		if in.Priority != "" {
			fields.Priority = &in.Priority
		}
		if in.Assignee != "" {
			fields.Assignee = &in.Assignee
		}
		if in.ParentID != "" {
			fields.ParentID = &in.ParentID
		}
		if in.Branch != "" {
			fields.Branch = &in.Branch
		}
		if in.Labels != nil {
			fields.Labels = &in.Labels
		}
		return s.deps.Tasks.SetFields(taskCaller(ctx), in.TaskID, fields)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolTasksClaim(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("claim", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Claim(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolTasksRelease(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("release", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Release(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksTransitionInput struct {
	TaskID string `json:"taskId"`
	To     string `json:"to"`
	// ExpectedStatus makes the move optimistic. Send the status you last saw
	// and a task someone else moved is refused instead of overwritten.
	ExpectedStatus string `json:"expectedStatus,omitempty"`
}

func (s *Server) toolTasksTransition(ctx context.Context, _ *sdk.CallToolRequest, in tasksTransitionInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("transition", map[string]any{
		"to": in.To,
	}), func(ctx context.Context) (any, error) {
		return s.deps.Tasks.Transition(ctx, taskCaller(ctx), tasks.TransitionInput{
			TaskID: in.TaskID, To: in.To, ExpectedStatus: in.ExpectedStatus,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksCommentInput struct {
	TaskID string `json:"taskId"`
	Body   string `json:"body"`
}

func (s *Server) toolTasksCommentAdd(ctx context.Context, _ *sdk.CallToolRequest, in tasksCommentInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("comment_add", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.AddComment(taskCaller(ctx), in.TaskID, in.Body)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolTasksCommentList(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "task:"+in.TaskID, meta("comment_list", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Comments(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolTasksProgress(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "task:"+in.TaskID, meta("progress", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Progress(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolTasksArchive(ctx context.Context, _ *sdk.CallToolRequest, in taskIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("archive", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Archive(taskCaller(ctx), in.TaskID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksSearchInput struct {
	BoardID string `json:"boardId"`
	Query   string `json:"query"`
}

func (s *Server) toolTasksSearch(ctx context.Context, _ *sdk.CallToolRequest, in tasksSearchInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:read", "task:search", meta("search", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.Search(taskCaller(ctx), in.BoardID, in.Query)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksLinkPlanNodeInput struct {
	TaskID string `json:"taskId"`
	// PlanNodeID empty clears the link.
	PlanNodeID string `json:"planNodeId"`
}

func (s *Server) toolTasksLinkPlanNode(ctx context.Context, _ *sdk.CallToolRequest, in tasksLinkPlanNodeInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:"+in.TaskID, meta("link_plan_node", nil), func(context.Context) (any, error) {
		return s.deps.Tasks.LinkPlanNode(taskCaller(ctx), in.TaskID, in.PlanNodeID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type tasksCreateFromPlanInput struct {
	BoardID string `json:"boardId"`
}

func (s *Server) toolTasksCreateFromPlan(ctx context.Context, _ *sdk.CallToolRequest, in tasksCreateFromPlanInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.tasks", "tasks:write", "task:new", meta("create_from_plan", nil), func(ctx context.Context) (any, error) {
		return s.deps.PlanBacklog.CreateTasksFromPlan(ctx, taskCaller(ctx), in.BoardID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
