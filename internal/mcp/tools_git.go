package mcp

import (
	"context"

	"devkit/internal/apperrors"
	"devkit/internal/workspace"

	gitservice "devkit/internal/git"
	"devkit/internal/gitparse"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GitAPI is the repository surface MCP tools invoke.
//
// Note what is not on it: no force push, no reset, no remote configuration and
// no config. Those are the three ways an agent destroys work that history
// cannot give back, so they are absent from the interface rather than guarded
// behind a grant.
type GitAPI interface {
	Status(ctx context.Context) (gitservice.Status, error)
	Diff(ctx context.Context, rev string, staged bool) ([]gitparse.Change, error)
	Log(ctx context.Context, limit int) ([]gitservice.Commit, error)
	CommitsForTask(ctx context.Context, key string, limit int) ([]gitservice.Commit, error)
	Stage(ctx context.Context, paths []string) error
	StageTurn(ctx context.Context, paths []string) ([]string, error)
	Commit(ctx context.Context, in gitservice.CommitInput) (gitservice.Commit, error)
	Branches(ctx context.Context) ([]gitservice.Branch, error)
	CreateBranch(ctx context.Context, name, from string) (string, error)
	Checkout(ctx context.Context, name string) error
	Stash(ctx context.Context, message string) error
	Push(ctx context.Context, in gitservice.PushInput) error
	Conflicts(ctx context.Context) ([]string, error)
	Conflict(ctx context.Context, path string) (gitservice.Conflict, error)
	Resolve(ctx context.Context, path, merged string) error
}

// TaskBranches reports the branches recorded on tasks a session holds. It is
// what makes an agent's push permission durable: a list living in the session
// dies with it, a branch written on a task does not.
type TaskBranches interface {
	BranchesForSession(sessionID string) []string
}

// gitRepoInput is the argument every git tool shares: which repository.
type gitRepoInput struct {
	Repo string `json:"repo,omitempty" jsonschema:"optional; which repository: a folder name from workspace.list or git.repos (e.g. kong-plugin) or an absolute path. Defaults to your session's working directory — from a terminal, to the repository open in DevKit"`
}

// gitStatusResult is git.status's answer, naming the repository it read so an
// agent never has to guess which one "the work tree" was.
type gitStatusResult struct {
	Repo string `json:"repo,omitempty"`
	gitservice.Status
}

// repoMeta adds the repository asked for to an audit line.
func repoMeta(repo string, extra map[string]any) map[string]any {
	if repo == "" {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["repo"] = repo
	return extra
}

func (s *Server) registerGit(ms *sdk.Server) {
	if s.deps.Git == nil {
		return
	}

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.repos",
		Description: "The repositories of your workspace — one per folder — with branch, ahead/behind and how " +
			"many files changed. `cwd` marks the one every git tool uses when you pass no `repo`; `granted` the ones " +
			"you may write. Pass a name from here as `repo` to any git tool to work in another service",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitRepos)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.status",
		Description: "What changed in the work tree, the branch, and how far ahead or behind it is. " +
			"`repo` in the answer says which repository that was",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitStatus)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.diff",
		Description: "Per-file change counts, for the work tree or for one commit",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitDiff)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.log",
		Description: "The branch's history, newest first and bounded",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitLog)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.commitsForTask",
		Description: "The commits carrying a task's key in their trailer",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitCommitsForTask)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.branch.list",
		Description: "List local branches",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitBranchList)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.branch.create",
		Description: "Create and switch to a branch. Returns the name — write it onto the task you " +
			"hold with tasks.setFields, which is what lets you push it later",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitBranchCreate)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.checkout",
		Description: "Switch branches. Refused while there is uncommitted work — stash it first",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitCheckout)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.stage",
		Description: "Stage paths",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitStage)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.commit",
		Description: "Commit the index. Local and reversible, so it is not gated unless the branch is guarded",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitCommit)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.stash",
		Description: "Stash the work tree, including untracked files",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitStash)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.push",
		Description: "Push a branch. Never the default branch, and only a branch recorded on a task " +
			"you hold. A remote with an environment tag asks first, every time",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitPush)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.conflicts",
		Description: "List unmerged paths, or read one path's three versions",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitConflicts)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.resolve",
		Description: "Write the merged content for one conflicted path and mark it resolved",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitResolve)

	// Deliberately absent, whatever the grant: a force push, a hard reset,
	// changing a remote's URL and writing git config. Each is a way to lose
	// work that the history cannot give back, and `git merge --abort` is left
	// out for the same reason — discarding everyone's in-flight merge is a
	// person's decision.
}

func (s *Server) toolGitStatus(ctx context.Context, _ *sdk.CallToolRequest, in gitRepoInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:status", meta("status", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, repo, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		status, err := git.Status(ctx)
		return gitStatusResult{Repo: repo, Status: status}, err
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitDiffInput struct {
	gitRepoInput
	Rev    string `json:"rev,omitempty"`
	Staged bool   `json:"staged,omitempty"`
}

func (s *Server) toolGitDiff(ctx context.Context, _ *sdk.CallToolRequest, in gitDiffInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:diff", meta("diff", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		return git.Diff(ctx, in.Rev, in.Staged)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitLimitInput struct {
	gitRepoInput
	Limit int `json:"limit,omitempty"`
}

func (s *Server) toolGitLog(ctx context.Context, _ *sdk.CallToolRequest, in gitLimitInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:log", meta("log", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		return git.Log(ctx, in.Limit)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitTaskKeyInput struct {
	gitRepoInput
	Key   string `json:"key"`
	Limit int    `json:"limit,omitempty"`
}

func (s *Server) toolGitCommitsForTask(ctx context.Context, _ *sdk.CallToolRequest, in gitTaskKeyInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:log", meta("commits_for_task", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		return git.CommitsForTask(ctx, in.Key, in.Limit)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolGitBranchList(ctx context.Context, _ *sdk.CallToolRequest, in gitRepoInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:branches", meta("branches", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		return git.Branches(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitBranchCreateInput struct {
	gitRepoInput
	Name string `json:"name"`
	From string `json:"from,omitempty"`
}

func (s *Server) toolGitBranchCreate(ctx context.Context, _ *sdk.CallToolRequest, in gitBranchCreateInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:branches", meta("create_branch", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return git.CreateBranch(ctx, in.Name, in.From)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitNameInput struct {
	gitRepoInput
	Name string `json:"name"`
}

func (s *Server) toolGitCheckout(ctx context.Context, _ *sdk.CallToolRequest, in gitNameInput) (*sdk.CallToolResult, any, error) {
	_, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:branches", meta("checkout", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return nil, git.Checkout(ctx, in.Name)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"branch": in.Name}), nil
}

type gitPathsInput struct {
	gitRepoInput
	Paths []string `json:"paths"`
	// Turn marks these as one agent turn's paths: ones it changed and changed
	// back are dropped rather than staged as empty entries.
	Turn bool `json:"turn,omitempty"`
}

func (s *Server) toolGitStage(ctx context.Context, _ *sdk.CallToolRequest, in gitPathsInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:stage", meta("stage", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		if in.Turn {
			return git.StageTurn(ctx, in.Paths)
		}
		return in.Paths, git.Stage(ctx, in.Paths)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"staged": v}), nil
}

type gitCommitInput struct {
	gitRepoInput
	Message string `json:"message"`
	// TaskKey adds the `Task: <key>` trailer, which is the entire link between
	// a commit and a task: nothing stores a SHA.
	TaskKey string `json:"taskKey,omitempty"`
}

func (s *Server) toolGitCommit(ctx context.Context, _ *sdk.CallToolRequest, in gitCommitInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:commit", meta("commit", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return git.Commit(ctx, gitservice.CommitInput{
			Message: in.Message, TaskKey: in.TaskKey, ByAgent: true,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitStashInput struct {
	gitRepoInput
	Message string `json:"message,omitempty"`
}

func (s *Server) toolGitStash(ctx context.Context, _ *sdk.CallToolRequest, in gitStashInput) (*sdk.CallToolResult, any, error) {
	_, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:stash", meta("stash", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return nil, git.Stash(ctx, in.Message)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"stashed": true}), nil
}

type gitPushInput struct {
	gitRepoInput
	Remote string `json:"remote,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// toolGitPush is the one tool that reads the caller's identity for more than
// audit: the branches it may push are the ones written on tasks this session
// holds.
func (s *Server) toolGitPush(ctx context.Context, _ *sdk.CallToolRequest, in gitPushInput) (*sdk.CallToolResult, any, error) {
	var allowed []string
	if caller, ok := identity.FromContext(ctx); ok && caller.IsAgent() && s.deps.TaskBranches != nil {
		allowed = s.deps.TaskBranches.BranchesForSession(caller.SessionID)
	}
	remote := in.Remote
	if remote == "" {
		remote = "origin"
	}
	_, err := s.deps.Caller.Call(ctx, "builtin.git", "git:push", "remote:"+remote, meta("push", repoMeta(in.Repo, map[string]any{
		"remote": remote,
		"branch": in.Branch,
	})), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return nil, git.Push(ctx, gitservice.PushInput{
			Remote: remote, Branch: in.Branch, ByAgent: true, TaskBranches: allowed,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"pushed": true, "remote": remote, "branch": in.Branch}), nil
}

type gitConflictInput struct {
	gitRepoInput
	// Path reads one file's three versions; empty lists the conflicted paths.
	Path string `json:"path,omitempty"`
}

func (s *Server) toolGitConflicts(ctx context.Context, _ *sdk.CallToolRequest, in gitConflictInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:conflicts", meta("conflicts", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		if in.Path == "" {
			return git.Conflicts(ctx)
		}
		return git.Conflict(ctx, in.Path)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitResolveInput struct {
	gitRepoInput
	Path   string `json:"path"`
	Merged string `json:"merged"`
}

func (s *Server) toolGitResolve(ctx context.Context, _ *sdk.CallToolRequest, in gitResolveInput) (*sdk.CallToolResult, any, error) {
	_, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:conflicts", meta("resolve", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return nil, git.Resolve(ctx, in.Path, in.Merged)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"resolved": in.Path}), nil
}

// GitRepoInfo is one row of git.repos.
type GitRepoInfo struct {
	gitservice.Repository
	// Cwd marks the session's own repository — the default for every tool.
	Cwd bool `json:"cwd,omitempty"`
	// Granted says an agent may write here; always true for the person.
	Granted bool `json:"granted"`
}

func (s *Server) toolGitRepos(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:status", meta("repos", nil), func(ctx context.Context) (any, error) {
		return s.gitRepos(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) gitRepos(ctx context.Context) ([]GitRepoInfo, error) {
	scope, err := s.gitScopeFor(ctx)
	if err != nil {
		return nil, err
	}
	if s.deps.GitRepos == nil {
		return nil, apperrors.New("internal_error", "Git is not available for other repositories.", true)
	}
	paths := make([]string, 0, len(scope.folders))
	names := map[string]string{}
	for _, f := range scope.folders {
		if _, seen := names[f.Path]; !seen {
			paths = append(paths, f.Path)
			names[f.Path] = f.Name
		}
	}
	cwdFolder := scope.folderOf(scope.cwd)
	if scope.agent && scope.cwd != "" && cwdFolder == "" {
		// A session started outside every workspace still has a repository.
		paths = append(paths, scope.cwd)
		cwdFolder = scope.cwd
	}
	repos, err := s.deps.GitRepos.Summaries(ctx, paths)
	if err != nil {
		return nil, err
	}
	out := make([]GitRepoInfo, 0, len(repos))
	for _, repo := range repos {
		if name := names[repo.Path]; name != "" {
			repo.Name = name
		}
		out = append(out, GitRepoInfo{
			Repository: repo,
			Cwd:        scope.agent && repo.Path == cwdFolder,
			Granted:    !scope.agent || workspace.Covered(repo.Path, scope.granted),
		})
	}
	return out, nil
}
