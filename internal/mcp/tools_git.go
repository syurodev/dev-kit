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
// behind a grant. The history tools that are here (amend, squash, rebase)
// rewrite only commits no remote has — without a force push, a rewritten
// pushed commit is a branch that can never be pushed again — and revert and
// cherry-pick only add commits (internal/git/history.go).
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
	Amend(ctx context.Context, in gitservice.AmendInput) (gitservice.Commit, error)
	Squash(ctx context.Context, in gitservice.SquashInput) (gitservice.Commit, error)
	Rebase(ctx context.Context, in gitservice.RebaseInput) (gitservice.HistoryResult, error)
	CherryPick(ctx context.Context, in gitservice.PickInput) (gitservice.HistoryResult, error)
	Revert(ctx context.Context, in gitservice.PickInput) (gitservice.HistoryResult, error)
	Fetch(ctx context.Context, remote string) error
	Pull(ctx context.Context, remote string) error
	WorktreeAdd(ctx context.Context, in gitservice.WorktreeAddInput) (gitservice.Worktree, error)
	WorktreeTarget(ctx context.Context, branch string) (root, target string, err error)
	Worktrees(ctx context.Context) ([]gitservice.Worktree, error)
	WorktreeRemove(ctx context.Context, name string) error
}

// GitWorktreeGrants are checked against the real default policy as well as
// used by the worktree handlers, so a test fake cannot hide a missing grant.
func GitWorktreeGrants() [][3]string {
	return [][3]string{{"builtin.git", "git:read", "repo:worktree"}, {"builtin.git", "git:write", "repo:worktree"}}
}

// TaskBranches reports the branches recorded on tasks a session holds. It is
// what makes an agent's push permission durable: a list living in the session
// dies with it, a branch written on a task does not.
type TaskBranches interface {
	BranchesForSession(sessionID string) []string
}

// gitRepoInput is the argument every git tool shares: which repository.
type gitRepoInput struct {
	Repo string `json:"repo,omitempty" jsonschema:"optional; workspace folder name or path; defaults to your session repository"`
}

// gitStatusResult is git.status's answer, naming the repository it read so an
// agent never has to guess which one "the work tree" was. It is the compact
// form of gitservice.Status: that one lists a path in `files` and again in
// `staged`/`unstaged`/`untracked`, and spells every empty field out — about
// 2.2 KB a call, 62 calls in the stored sessions. Here a path appears once
// per category it is in, with its one-letter state in front, and an empty
// field is absent (no `upstream` = no upstream; no `ahead` = 0; no lists = a
// clean tree, which `clean` says).
type gitStatusResult struct {
	Repo     string `json:"repo,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Detached bool   `json:"detached,omitempty"`
	Clean    bool   `json:"clean,omitempty"`
	// Each entry is "<state> <path>": M modified, A added, D deleted, R
	// renamed, ? untracked (no letter is kept for those — they are listed
	// under untracked).
	Staged     []string `json:"staged,omitempty"`
	Unstaged   []string `json:"unstaged,omitempty"`
	Untracked  []string `json:"untracked,omitempty"`
	Conflicted []string `json:"conflicted,omitempty"`
	// Omitted counts the paths a category was cut to statusListCap, by
	// category name: git.diff names the files of a large change.
	Omitted map[string]int `json:"omitted,omitempty"`
}

// statusListCap bounds one category of git.status. A tree with a build output
// not in .gitignore has thousands of untracked paths; the first hundred say
// what kind of mess it is, and `omitted` says how much more there is.
const statusListCap = 100

func compactGitStatus(repo string, st gitservice.Status) gitStatusResult {
	out := gitStatusResult{Repo: repo, Branch: st.Branch, Upstream: st.Upstream, Ahead: st.Ahead, Behind: st.Behind, Detached: st.Detached}
	lettered := func(name string, paths []string, withState bool) []string {
		if len(paths) == 0 {
			return nil
		}
		if len(paths) > statusListCap {
			if out.Omitted == nil {
				out.Omitted = map[string]int{}
			}
			out.Omitted[name] = len(paths) - statusListCap
			paths = paths[:statusListCap]
		}
		rows := make([]string, len(paths))
		for i, p := range paths {
			if withState && st.Files[p] != "" {
				rows[i] = st.Files[p] + " " + p
			} else {
				rows[i] = p
			}
		}
		return rows
	}
	out.Staged = lettered("staged", st.Staged, true)
	out.Unstaged = lettered("unstaged", st.Unstaged, true)
	out.Untracked = lettered("untracked", st.Untracked, false)
	out.Conflicted = lettered("conflicted", st.Conflicted, false)
	out.Clean = len(st.Files) == 0 && len(st.Staged)+len(st.Unstaged)+len(st.Untracked)+len(st.Conflicted) == 0
	return out
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

func (s *Server) requireGitWritePaths(ctx context.Context, paths ...string) error {
	scope, err := s.gitScopeFor(ctx)
	if err != nil {
		return err
	}
	if !scope.agent {
		return nil
	}
	for _, path := range paths {
		if !workspace.Covered(path, scope.granted) {
			return apperrors.New("git.repoNotGranted", path+" is not a folder your session may write. Ask the person to add it to the session, or work in "+scope.cwd+".", false)
		}
	}
	return nil
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
			"`repo` in the answer says which repository that was. Lists are staged/unstaged/untracked/conflicted, " +
			"each entry \"<state> <path>\" (M, A, D, R); an absent field is empty or zero, and `clean` means nothing changed",
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
		Name: "git.fetch",
		Description: "Fetch a remote (default origin) with the account bound to it, then report the branch's " +
			"upstream and how far ahead/behind it is. Changes no branch and no file",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(true)},
	}, s.toolGitFetch)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.pull",
		Description: "Bring the current branch up to its upstream on a remote (default origin), fast-forward only: " +
			"a branch that has diverged is refused rather than merged — rebase your unpushed commits onto the " +
			"upstream with git.rebase instead. Reports ahead/behind afterwards",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(true)},
	}, s.toolGitPull)

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

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.amend",
		Description: "Fold what is staged into the last commit, and/or replace its message (empty keeps it). " +
			"Only an unpushed commit: DevKit never force-pushes, so a pushed one is refused — commit again instead",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitAmend)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.squash",
		Description: "Combine the newest `count` (≥2) commits into one, keeping every change and every Task trailer. " +
			"All of them must be unpushed and none a merge; the work tree must be clean. Empty message joins theirs",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitSquash)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.rebase",
		Description: "Replay the branch's own commits onto `onto` (a branch or commit), non-interactively. Every " +
			"replayed commit must be unpushed (pushed work: merge instead). On a conflict it stops and returns the " +
			"paths: resolve each with git.resolve, then call again with action \"continue\" — or \"abort\" to put " +
			"the branch back exactly as it was",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitRebase)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.cherryPick",
		Description: "Copy commits (oldest first) onto the current branch as new commits, recording where each came " +
			"from. Adds history, so pushed commits may be picked. Stops on a conflict like git.rebase: resolve, then " +
			"action \"continue\" or \"abort\"",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitCherryPick)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "git.revert",
		Description: "Write commits that undo the given ones. The way to take back a commit that is already pushed. " +
			"Stops on a conflict like git.rebase: resolve, then action \"continue\" or \"abort\"",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitRevert)

	// Deliberately absent, whatever the grant: a force push, a hard reset,
	// changing a remote's URL and writing git config. Each is a way to lose
	// work that the history cannot give back, and `git merge --abort` is left
	// out for the same reason — discarding everyone's in-flight merge is a
	// person's decision.

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.worktree.add",
		Description: "Create or attach a branch worktree under `.superpowers/.worktrees/`; guarded branches ask the person.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitWorktreeAdd)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.worktree.list",
		Description: "List worktree paths, branches, heads and locked, prunable, and dirty states.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolGitWorktreeList)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "git.worktree.remove",
		Description: "Remove a clean worktree by branch or path. Keeps the branch.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolGitWorktreeRemove)

}

func (s *Server) toolGitStatus(ctx context.Context, _ *sdk.CallToolRequest, in gitRepoInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:status", meta("status", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, repo, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		status, err := git.Status(ctx)
		return compactGitStatus(repo, status), err
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

type gitAmendInput struct {
	gitRepoInput
	Message string `json:"message,omitempty" jsonschema:"the new message; empty keeps the commit's own"`
	TaskKey string `json:"taskKey,omitempty"`
}

func (s *Server) toolGitAmend(ctx context.Context, _ *sdk.CallToolRequest, in gitAmendInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:commit", meta("amend", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return git.Amend(ctx, gitservice.AmendInput{Message: in.Message, TaskKey: in.TaskKey})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitSquashInput struct {
	gitRepoInput
	Count   int    `json:"count" jsonschema:"how many of the newest commits become one; at least 2"`
	Message string `json:"message,omitempty" jsonschema:"the combined commit's message; empty joins theirs, oldest first"`
}

func (s *Server) toolGitSquash(ctx context.Context, _ *sdk.CallToolRequest, in gitSquashInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:commit", meta("squash", repoMeta(in.Repo, map[string]any{"count": in.Count})), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return git.Squash(ctx, gitservice.SquashInput{Count: in.Count, Message: in.Message})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitRebaseInput struct {
	gitRepoInput
	Action string `json:"action,omitempty" jsonschema:"start (default), continue or abort"`
	Onto   string `json:"onto,omitempty" jsonschema:"the branch or commit to replay onto; required to start"`
}

func (s *Server) toolGitRebase(ctx context.Context, _ *sdk.CallToolRequest, in gitRebaseInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:history", meta("rebase", repoMeta(in.Repo, map[string]any{
		"action": in.Action,
		"onto":   in.Onto,
	})), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return git.Rebase(ctx, gitservice.RebaseInput{Action: gitservice.RebaseAction(in.Action), Onto: in.Onto})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitPickInput struct {
	gitRepoInput
	Action  string   `json:"action,omitempty" jsonschema:"start (default), continue or abort"`
	Commits []string `json:"commits,omitempty" jsonschema:"commits to apply, oldest first; required to start"`
	TaskKey string   `json:"taskKey,omitempty"`
}

func (s *Server) toolGitCherryPick(ctx context.Context, _ *sdk.CallToolRequest, in gitPickInput) (*sdk.CallToolResult, any, error) {
	return s.gitPick(ctx, in, "cherry_pick", func(git GitAPI, ctx context.Context, p gitservice.PickInput) (any, error) {
		return git.CherryPick(ctx, p)
	})
}

func (s *Server) toolGitRevert(ctx context.Context, _ *sdk.CallToolRequest, in gitPickInput) (*sdk.CallToolResult, any, error) {
	return s.gitPick(ctx, in, "revert", func(git GitAPI, ctx context.Context, p gitservice.PickInput) (any, error) {
		return git.Revert(ctx, p)
	})
}

func (s *Server) gitPick(ctx context.Context, in gitPickInput, op string, run func(GitAPI, context.Context, gitservice.PickInput) (any, error)) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:commit", meta(op, repoMeta(in.Repo, map[string]any{
		"action":  in.Action,
		"commits": len(in.Commits),
	})), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		return run(git, ctx, gitservice.PickInput{Action: gitservice.PickAction(in.Action), Commits: in.Commits, TaskKey: in.TaskKey})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitRemoteInput struct {
	gitRepoInput
	Remote string `json:"remote,omitempty" jsonschema:"the remote to talk to; default origin"`
}

// gitSyncResult is what git.fetch and git.pull answer: where the branch
// stands against its upstream afterwards, which is why one calls them.
type gitSyncResult struct {
	Repo     string `json:"repo,omitempty"`
	Remote   string `json:"remote"`
	Branch   string `json:"branch"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
}

func (s *Server) toolGitFetch(ctx context.Context, _ *sdk.CallToolRequest, in gitRemoteInput) (*sdk.CallToolResult, any, error) {
	return s.gitSync(ctx, in, "fetch", GitAPI.Fetch)
}

func (s *Server) toolGitPull(ctx context.Context, _ *sdk.CallToolRequest, in gitRemoteInput) (*sdk.CallToolResult, any, error) {
	return s.gitSync(ctx, in, "pull", GitAPI.Pull)
}

// GitSyncGrant is the capability request git.fetch and git.pull make. Exported
// so the app can check it against the grants it really installs: it once asked
// for git:write on "remote:<name>", which the repo:* grant does not cover, and
// every agent fetch and pull was refused while the fakes in this package's
// tests, which skip the policy, passed.
//
// It is git:write on repo:sync, not git:push: neither sends anything to the
// remote (push is the gated one), and both write the repository.
func GitSyncGrant() (subject, capability, scope string) {
	return "builtin.git", "git:write", "repo:sync"
}

// gitSync is fetch and pull: both write the repository (remote-tracking refs,
// and for pull the branch and work tree), so both take a granted folder.
func (s *Server) gitSync(ctx context.Context, in gitRemoteInput, op string, run func(GitAPI, context.Context, string) error) (*sdk.CallToolResult, any, error) {
	remote := in.Remote
	if remote == "" {
		remote = "origin"
	}
	subject, capability, scope := GitSyncGrant()
	v, err := s.deps.Caller.Call(ctx, subject, capability, scope, meta(op, repoMeta(in.Repo, map[string]any{"remote": remote})), func(ctx context.Context) (any, error) {
		git, repo, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		if err := run(git, ctx, remote); err != nil {
			return nil, err
		}
		status, err := git.Status(ctx)
		if err != nil {
			return nil, err
		}
		return gitSyncResult{Repo: repo, Remote: remote, Branch: status.Branch, Upstream: status.Upstream, Ahead: status.Ahead, Behind: status.Behind}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// Worktree tools

type gitWorktreeAddInput struct {
	gitRepoInput
	Branch string `json:"branch"`
	Base   string `json:"base,omitempty"`
}

func (s *Server) toolGitWorktreeAdd(ctx context.Context, _ *sdk.CallToolRequest, in gitWorktreeAddInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:worktree", meta("worktree_add", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		root, target, err := git.WorktreeTarget(ctx, in.Branch)
		if err != nil {
			return nil, err
		}
		if err := s.requireGitWritePaths(ctx, root, target); err != nil {
			return nil, err
		}
		return git.WorktreeAdd(ctx, gitservice.WorktreeAddInput{Branch: in.Branch, Base: in.Base})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitWorktreeListInput struct {
	gitRepoInput
}

func (s *Server) toolGitWorktreeList(ctx context.Context, _ *sdk.CallToolRequest, in gitWorktreeListInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.git", "git:read", "repo:worktree", meta("worktree_list", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, false)
		if err != nil {
			return nil, err
		}
		return git.Worktrees(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type gitWorktreeRemoveInput struct {
	gitRepoInput
	Name string `json:"name"`
}

func (s *Server) toolGitWorktreeRemove(ctx context.Context, _ *sdk.CallToolRequest, in gitWorktreeRemoveInput) (*sdk.CallToolResult, any, error) {
	_, err := s.deps.Caller.Call(ctx, "builtin.git", "git:write", "repo:worktree", meta("worktree_remove", repoMeta(in.Repo, nil)), func(ctx context.Context) (any, error) {
		git, _, err := s.gitFor(ctx, in.Repo, true)
		if err != nil {
			return nil, err
		}
		worktrees, err := git.Worktrees(ctx)
		if err != nil {
			return nil, err
		}
		for _, worktree := range worktrees {
			if worktree.Branch != in.Name && worktree.Path != in.Name {
				continue
			}
			if err := s.refuseLiveWorktree(ctx, worktree.Path); err != nil {
				return nil, err
			}
			break
		}
		return nil, git.WorktreeRemove(ctx, in.Name)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(map[string]any{"removed": in.Name}), nil
}

func (s *Server) refuseLiveWorktree(ctx context.Context, worktreePath string) error {
	if s.deps.AgentHub == nil {
		return apperrors.New("internal_error", "Agent Hub is not available to check worktree use.", true)
	}
	inUse, err := s.deps.AgentHub.WorktreeInUse(ctx, worktreePath)
	if err != nil {
		return apperrors.New("git.worktreeCheckFailed", "Could not verify whether an Agent Hub session is using this worktree, so it was not removed.", false)
	}
	if inUse {
		return apperrors.New("git.worktreeInUse", "A live Agent Hub session is using this worktree. Close that session before removing it.", false)
	}
	return nil
}
