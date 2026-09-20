package bridge

import (
	"context"

	gitservice "devkit/internal/git"
	"devkit/internal/gitparse"
)

// GitAvailability explains a machine with no usable git, so the module can
// show a panel that names the reason rather than an error.
type GitAvailability struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

func (s *AppService) GitAvailability() GitAvailability {
	availability := s.container.Git.Availability()
	return GitAvailability{
		OK: availability.OK(), Path: availability.Path,
		Version: availability.Version, Reason: availability.Reason,
	}
}

// GitOpen points the module at a project.
func (s *AppService) GitOpen(projectPath string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:read", "repo:status", map[string]any{
		"action": "open",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Open(ctx, projectPath)
	})
	return err
}

// GitStatus is the single source of what changed. Code Graph's file-tree
// markers read this rather than running git of their own.
func (s *AppService) GitStatus() (gitservice.Status, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:status", map[string]any{
		"action": "status",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Status(ctx)
	})
	if err != nil {
		return gitservice.Status{}, err
	}
	return result.(gitservice.Status), nil
}

func (s *AppService) GitDiff(rev string, staged bool) ([]gitparse.Change, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:diff", map[string]any{
		"action": "diff",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Diff(ctx, rev, staged)
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitparse.Change), nil
}

func (s *AppService) GitLog(limit int) ([]gitservice.Commit, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:log", map[string]any{
		"action": "log",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Log(ctx, limit)
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitservice.Commit), nil
}

// GitCommitsForTask is the task-to-commit link, asked rather than stored: a
// SHA rots under rebase, amend and squash, a trailer does not.
func (s *AppService) GitCommitsForTask(key string, limit int) ([]gitservice.Commit, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:log", map[string]any{
		"action": "commits_for_task",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.CommitsForTask(ctx, key, limit)
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitservice.Commit), nil
}

func (s *AppService) GitStage(paths []string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stage", map[string]any{
		"action": "stage",
		"count":  len(paths),
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Stage(ctx, paths)
	})
	return err
}

func (s *AppService) GitUnstage(paths []string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stage", map[string]any{
		"action": "unstage",
		"count":  len(paths),
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Unstage(ctx, paths)
	})
	return err
}

// GitStageHunk applies one hunk the user ticked.
func (s *AppService) GitStageHunk(patch string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stage", map[string]any{
		"action": "stage_hunk",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.StageHunk(ctx, patch)
	})
	return err
}

// GitStageTurn stages exactly what one agent turn changed — the reason this
// module exists rather than a terminal.
func (s *AppService) GitStageTurn(paths []string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stage", map[string]any{
		"action": "stage_turn",
		"count":  len(paths),
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.StageTurn(ctx, paths)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (s *AppService) GitCommit(message, taskKey string, amend bool) (gitservice.Commit, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:commit", map[string]any{
		"action": "commit",
		"amend":  amend,
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Commit(ctx, gitservice.CommitInput{
			Message: message, TaskKey: taskKey, Amend: amend,
		})
	})
	if err != nil {
		return gitservice.Commit{}, err
	}
	return result.(gitservice.Commit), nil
}

func (s *AppService) GitBranches() ([]gitservice.Branch, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:branches", map[string]any{
		"action": "branches",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Branches(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitservice.Branch), nil
}

// GitCreateBranch returns the name so the caller can record it on a task,
// which is what makes an agent's push permission outlive its session.
func (s *AppService) GitCreateBranch(name, from string) (string, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:branches", map[string]any{
		"action": "create_branch",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.CreateBranch(ctx, name, from)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) GitCheckout(name string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:branches", map[string]any{
		"action": "checkout",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Checkout(ctx, name)
	})
	return err
}

func (s *AppService) GitStash(message string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stash", map[string]any{
		"action": "stash",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Stash(ctx, message)
	})
	return err
}

func (s *AppService) GitStashPop() error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stash", map[string]any{
		"action": "stash_pop",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.StashPop(ctx)
	})
	return err
}

// GitConflict reads one unmerged path's three stages, for the resolve panel.
func (s *AppService) GitConflict(path string) (gitservice.Conflict, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:conflicts", map[string]any{
		"action": "conflict",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Conflict(ctx, path)
	})
	if err != nil {
		return gitservice.Conflict{}, err
	}
	return result.(gitservice.Conflict), nil
}

func (s *AppService) GitResolve(path, merged string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:conflicts", map[string]any{
		"action": "resolve",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Resolve(ctx, path, merged)
	})
	return err
}

// GitAbortMerge throws a whole merge away. UI-only: it is not in the MCP tool
// surface, because discarding everyone's in-flight merge is a person's call.
func (s *AppService) GitAbortMerge() error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:conflicts", map[string]any{
		"action": "abort_merge",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.AbortMerge(ctx)
	})
	return err
}

func (s *AppService) GitRemotes() ([]gitservice.Remote, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:remotes", map[string]any{
		"action": "remotes",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.Remotes(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitservice.Remote), nil
}

// GitPush is its own capability because it leaves the machine and cannot be
// undone. There is no force parameter anywhere in this file.
func (s *AppService) GitPush(remote, branch string, setUpstream bool) error {
	_, err := s.call(context.Background(), "builtin.git", "git:push", "remote:"+remote, map[string]any{
		"action": "push",
		"remote": remote,
		"branch": branch,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Push(ctx, gitservice.PushInput{
			Remote: remote, Branch: branch, SetUpstream: setUpstream,
		})
	})
	return err
}

func (s *AppService) GitFetch(remote string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:push", "remote:"+remote, map[string]any{
		"action": "fetch",
		"remote": remote,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Fetch(ctx, remote)
	})
	return err
}

func (s *AppService) GitPull(remote string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:push", "remote:"+remote, map[string]any{
		"action": "pull",
		"remote": remote,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Git.Pull(ctx, remote)
	})
	return err
}

// GitGuards lists the branch patterns protected in the open project.
func (s *AppService) GitGuards() ([]gitservice.Guard, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:guards", map[string]any{
		"action": "guards",
	}, func(context.Context) (any, error) {
		return s.container.GitBindings.ListGuards(s.container.Git.Project())
	})
	if err != nil {
		return nil, err
	}
	return result.([]gitservice.Guard), nil
}

// GitCreateGuard protects a branch pattern with an environment tag, which is
// what turns commit gating on for this repository. Without a guard, committing
// is never gated — that is the default, and it is deliberate.
func (s *AppService) GitCreateGuard(pattern, env string) (gitservice.Guard, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:guards", map[string]any{
		"action":  "create_guard",
		"pattern": pattern,
		"env":     env,
	}, func(context.Context) (any, error) {
		return s.container.GitBindings.CreateGuard(s.container.Git.Project(), pattern, env)
	})
	if err != nil {
		return gitservice.Guard{}, err
	}
	return result.(gitservice.Guard), nil
}

func (s *AppService) GitDeleteGuard(id string) error {
	_, err := s.call(context.Background(), "builtin.git", "git:write", "repo:guards", map[string]any{
		"action": "delete_guard",
	}, func(context.Context) (any, error) {
		return nil, s.container.GitBindings.DeleteGuard(id)
	})
	return err
}

// GitBindRemote points a remote at an account and an environment tag. An empty
// account clears the binding and git's own configuration takes over again.
func (s *AppService) GitBindRemote(name, accountID, env string) (gitservice.Remote, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:remotes", map[string]any{
		"action": "bind_remote",
		"remote": name,
		"env":    env,
	}, func(context.Context) (any, error) {
		return s.container.GitBindings.BindRemote(s.container.Git.Project(), name, accountID, env)
	})
	if err != nil {
		return gitservice.Remote{}, err
	}
	return result.(gitservice.Remote), nil
}

// GitFilePatch returns one file's unified diff, work tree against the index —
// the pairing hunk staging needs, since GitStageHunk applies to the index.
func (s *AppService) GitFilePatch(path string) (string, error) {
	result, err := s.call(context.Background(), "builtin.git", "git:read", "repo:diff", map[string]any{
		"action": "file_patch",
	}, func(ctx context.Context) (any, error) {
		return s.container.Git.FilePatch(ctx, path)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
