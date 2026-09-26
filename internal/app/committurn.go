package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"devkit/internal/agenthub"
	gitservice "devkit/internal/git"
	"devkit/internal/tasks"
)

// TurnCommit is a commit drafted from one agent turn: the paths it changed,
// already staged, and a message seeded from what those paths are.
type TurnCommit struct {
	// Staged are the paths actually staged — the turn's, minus anything it
	// changed and changed back, which is no longer a change to commit.
	Staged []string `json:"staged"`
	// Skipped are the turn's paths that are no longer changed, named rather
	// than dropped silently so the count adds up on screen.
	Skipped []string `json:"skipped"`
	// Message is a draft. The subject line is left blank on purpose.
	Message string `json:"message"`
	// TaskKey is the trailer this commit will carry, when the session holds a
	// task.
	TaskKey string `json:"taskKey,omitempty"`
}

// CommitTurn stages exactly what one agent turn changed and drafts its message.
//
// This is the one feature that justifies a Git module inside DevKit rather than
// a terminal: the agent has already grouped the work by turn, and picking those
// paths back out of `git status` by hand is what people do instead. Everything
// else here — the seeded body, the task trailer — follows from having that
// grouping.
//
// It does not commit. Staging and drafting is the part a machine can do; the
// message and the decision stay with the person.
func CommitTurn(ctx context.Context, c *Container, sessionID, checkpointID string) (TurnCommit, error) {
	if c == nil || c.AgentHub == nil || c.Git == nil {
		return TurnCommit{}, gitservice.ErrNoProject
	}
	checkpoints, err := c.AgentHub.ListCheckpoints(ctx, sessionID)
	if err != nil {
		return TurnCommit{}, err
	}
	var turn agenthub.CheckpointSummary
	for _, candidate := range checkpoints {
		if candidate.ID == checkpointID {
			turn = candidate
			break
		}
	}
	if turn.ID == "" {
		return TurnCommit{}, fmt.Errorf("app: no checkpoint %s in session %s", checkpointID, sessionID)
	}

	paths := make([]string, 0, len(turn.Changed))
	for _, changed := range turn.Changed {
		paths = append(paths, changed.Path)
	}
	sort.Strings(paths)

	staged, err := c.Git.StageTurn(ctx, paths)
	if err != nil {
		return TurnCommit{}, err
	}
	stagedSet := make(map[string]bool, len(staged))
	for _, path := range staged {
		stagedSet[path] = true
	}
	skipped := make([]string, 0)
	for _, path := range paths {
		if !stagedSet[path] {
			skipped = append(skipped, path)
		}
	}

	draft := TurnCommit{Staged: staged, Skipped: skipped, TaskKey: taskKeyForSession(c, sessionID)}
	draft.Message = seedTurnMessage(staged, draft.TaskKey)
	return draft, nil
}

// seedTurnMessage writes the body and leaves the subject empty.
//
// The body is the list of paths: a statement of fact the person edits, not
// prose a model invented about work it did not do. The blank first line is
// deliberate — a subject filled in for you is a subject nobody rewrites.
func seedTurnMessage(staged []string, taskKey string) string {
	lines := []string{"", ""}
	const shown = 20
	for i, path := range staged {
		if i == shown {
			lines = append(lines, fmt.Sprintf("- …and %d more", len(staged)-shown))
			break
		}
		lines = append(lines, "- "+path)
	}
	message := strings.Join(lines, "\n")
	if taskKey != "" {
		message = gitservice.WithTaskTrailer(message, taskKey)
	}
	return message
}

// tasksForBoard is the bounded listing this needs.
func tasksForBoard(boardID string) tasks.ListInput {
	return tasks.ListInput{BoardID: boardID}
}

// taskKeyForSession finds the task this session holds, if any, so the commit
// carries its trailer. No task is not an error: plenty of turns are not a
// task's.
func taskKeyForSession(c *Container, sessionID string) string {
	if c.Tasks == nil || sessionID == "" {
		return ""
	}
	// As the session, so only boards in its scope are scanned.
	caller := tasks.Caller{SessionID: sessionID}
	boards, err := c.Tasks.Boards(caller)
	if err != nil {
		return ""
	}
	for _, board := range boards {
		held, err := c.Tasks.List(caller, tasksForBoard(board.ID))
		if err != nil {
			continue
		}
		for _, task := range held {
			if task.SessionID == sessionID {
				return task.Key
			}
		}
	}
	return ""
}
