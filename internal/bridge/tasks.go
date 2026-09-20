package bridge

import (
	"context"

	"devkit/internal/app"
	"devkit/internal/tasks"
)

// Tasks over the bridge is the user's path: caller is always the person, so
// terminal transitions here do not go through the agent gate. The gate stands
// in the MCP path, which is the one an agent takes.

func (s *AppService) TasksBoardList() ([]tasks.Board, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "board:list", map[string]any{
		"action": "board_list",
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Boards()
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Board), nil
}

func (s *AppService) TasksBoardCreate(name, keyPrefix, color string) (tasks.Board, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:admin", "board:new", map[string]any{
		"action": "board_create",
	}, func(context.Context) (any, error) {
		return s.container.Tasks.CreateBoard(name, keyPrefix, color, tasks.Workflow{})
	})
	if err != nil {
		return tasks.Board{}, err
	}
	return result.(tasks.Board), nil
}

// TasksBoardSetVerify sets the command that closes tasks on this board.
//
// tasks:admin and UI-only, deliberately: changing it is changing the rule that
// binds every agent on the board, which is the same reason the workflow is not
// reachable from MCP.
func (s *AppService) TasksBoardSetVerify(boardID, command string, timeoutSec int, projectPath string) (tasks.Board, error) {
	opaqueID := recordID(boardID)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:admin", "board:"+opaqueID, map[string]any{
		"action":   "board_set_verify",
		"board_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.UpdateBoard(boardID, func(b *tasks.Board) {
			if command == "" {
				b.Verify = nil
			} else {
				b.Verify = &tasks.Verify{Command: command, TimeoutSec: timeoutSec}
			}
			b.ProjectPath = projectPath
		})
	})
	if err != nil {
		return tasks.Board{}, err
	}
	return result.(tasks.Board), nil
}

func (s *AppService) TasksList(boardID, status, assignee, label string, includeArchived bool, limit int) ([]tasks.Task, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:list", map[string]any{
		"action":   "list",
		"board_id": recordID(boardID),
	}, func(context.Context) (any, error) {
		return s.container.Tasks.List(tasks.ListInput{
			BoardID: boardID, Status: status, Assignee: assignee, Label: label,
			IncludeArchived: includeArchived, Limit: limit,
		})
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Task), nil
}

func (s *AppService) TasksGet(id string) (tasks.Task, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:"+opaqueID, map[string]any{
		"action":  "get",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Get(id)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

func (s *AppService) TasksCreate(boardID, title, description, priority, parentID string, labels []string) (tasks.Task, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:new", map[string]any{
		"action":   "create",
		"board_id": recordID(boardID),
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Create(tasks.Caller{}, tasks.CreateInput{
			BoardID: boardID, Title: title, Description: description,
			Priority: priority, ParentID: parentID, Labels: labels,
		})
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

func (s *AppService) TasksUpdate(id, title, description string) (tasks.Task, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "update",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Update(id, title, description)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

// TasksSetFields is the metadata-only path: it never reseals the description,
// so moving a card or recording a branch is not a decrypt and re-encrypt.
func (s *AppService) TasksSetFields(id, priority, assignee, parentID, branch string, labels []string) (tasks.Task, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "set_fields",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		fields := tasks.Fields{}
		if priority != "" {
			fields.Priority = &priority
		}
		if assignee != "" {
			fields.Assignee = &assignee
		}
		if parentID != "" {
			fields.ParentID = &parentID
		}
		if branch != "" {
			fields.Branch = &branch
		}
		if labels != nil {
			fields.Labels = &labels
		}
		return s.container.Tasks.SetFields(id, fields)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

func (s *AppService) TasksTransition(id, to, expectedStatus string) (tasks.TransitionResult, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "transition",
		"task_id": opaqueID,
		"to":      to,
	}, func(ctx context.Context) (any, error) {
		return s.container.Tasks.Transition(ctx, tasks.Caller{}, tasks.TransitionInput{
			TaskID: id, To: to, ExpectedStatus: expectedStatus,
		})
	})
	if err != nil {
		return tasks.TransitionResult{}, err
	}
	return result.(tasks.TransitionResult), nil
}

func (s *AppService) TasksArchive(id string) (tasks.Task, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "archive",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Archive(id)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

// TasksDelete is UI-only and tasks:admin: it destroys a record of the project.
func (s *AppService) TasksDelete(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.tasks", "tasks:admin", "task:"+opaqueID, map[string]any{
		"action":  "delete",
		"task_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Tasks.Delete(ctx, id)
	})
	return err
}

func (s *AppService) TasksCommentAdd(taskID, body string) (tasks.Comment, error) {
	opaqueID := recordID(taskID)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "comment_add",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.AddComment(tasks.Caller{}, taskID, body)
	})
	if err != nil {
		return tasks.Comment{}, err
	}
	return result.(tasks.Comment), nil
}

func (s *AppService) TasksCommentList(taskID string) ([]tasks.Comment, error) {
	opaqueID := recordID(taskID)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:"+opaqueID, map[string]any{
		"action":  "comment_list",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Comments(taskID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Comment), nil
}

// TasksProgress is the subtask counter the board card shows. It counts by
// category, so a renamed Done column still counts.
func (s *AppService) TasksProgress(parentID string) (tasks.Progress, error) {
	opaqueID := recordID(parentID)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:"+opaqueID, map[string]any{
		"action":  "progress",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Progress(parentID)
	})
	if err != nil {
		return tasks.Progress{}, err
	}
	return result.(tasks.Progress), nil
}

// TasksVerifyRuns is a task's verify history — the record of what DevKit ran
// and what it exited with.
func (s *AppService) TasksVerifyRuns(taskID string, limit int) ([]tasks.Run, error) {
	opaqueID := recordID(taskID)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:"+opaqueID, map[string]any{
		"action":  "verify_runs",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		gate := s.container.Tasks.Gate()
		if gate == nil {
			return []tasks.Run{}, nil
		}
		return gate.Runs(taskID, limit)
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Run), nil
}

func (s *AppService) TasksVerifyCancel(runID string) error {
	_, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+recordID(runID), map[string]any{
		"action": "verify_cancel",
	}, func(context.Context) (any, error) {
		gate := s.container.Tasks.Gate()
		if gate == nil {
			return nil, nil
		}
		return nil, gate.Cancel(runID)
	})
	return err
}

// TasksSearch finds tasks by key, title or label.
//
// Not by description: that is the encrypted half, and searching it would mean
// decrypting every task on the board to answer one query.
func (s *AppService) TasksSearch(boardID, query string) ([]tasks.Task, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:search", map[string]any{
		"action":   "search",
		"board_id": recordID(boardID),
	}, func(context.Context) (any, error) {
		return s.container.Tasks.Search(boardID, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Task), nil
}

// TasksLinkPlanNode points a task at a Code Graph plan node, or clears it.
//
// The link is stored here and only here. `plan_nodes` is rewritten wholesale
// on every proposal, so a back-reference there would be wiped each time an
// agent re-proposed a plan while this one survived.
func (s *AppService) TasksLinkPlanNode(id, planNodeID string) (tasks.Task, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:"+opaqueID, map[string]any{
		"action":  "link_plan_node",
		"task_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Tasks.LinkPlanNode(id, planNodeID)
	})
	if err != nil {
		return tasks.Task{}, err
	}
	return result.(tasks.Task), nil
}

// TasksByPlanNode groups a board's tasks by the plan node they implement, for
// the chips on the plan canvas.
func (s *AppService) TasksByPlanNode(boardID string) (map[string][]tasks.Task, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:read", "task:list", map[string]any{
		"action":   "by_plan_node",
		"board_id": recordID(boardID),
	}, func(context.Context) (any, error) {
		return s.container.Tasks.ByPlanNode(boardID)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string][]tasks.Task), nil
}

// TasksCreateFromPlan turns an approved plan into a backlog. A draft is
// refused: it is a proposal under discussion, not agreed work.
func (s *AppService) TasksCreateFromPlan(boardID string) ([]tasks.Task, error) {
	result, err := s.call(context.Background(), "builtin.tasks", "tasks:write", "task:new", map[string]any{
		"action":   "create_from_plan",
		"board_id": recordID(boardID),
	}, func(ctx context.Context) (any, error) {
		return app.CreateTasksFromPlan(ctx, s.container, boardID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]tasks.Task), nil
}
