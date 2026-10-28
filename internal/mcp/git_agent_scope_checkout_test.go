package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	gitservice "devkit/internal/git"
	"devkit/internal/identity"
	"devkit/internal/workspace"
)

// scopedServer is linkedScopeServer for a session that stands in project, which can be
// outside the workspace folder: a code-editing child's checkout.
func scopedServer(t *testing.T, workspaceRoot, project string) *Server {
	t.Helper()
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{status: agenthub.AgentSession{ID: gitSession, ProjectPath: project}})
	d.Workspace = fakeWorkspaces{
		workspaces: []workspace.Workspace{{ID: "ws", Name: "ws", Root: workspaceRoot}},
		services:   map[string][]workspace.WorkspaceService{"ws": {{Name: "ws", Path: workspaceRoot}}},
	}
	d.Git = &fakeGit{name: "open"}
	d.GitRepos = GitReposOf(gitservice.NewService(context.Background()))
	return NewServer(d)
}

func requireGit(t *testing.T) {
	t.Helper()
	if !gitservice.NewService(context.Background()).Available() {
		t.Skip("no git")
	}
}

func requireRefused(t *testing.T, err error, what string) {
	t.Helper()
	if appCode(err) != "git.repoOutsideWorkspace" {
		t.Errorf("%s: err = %v, want git.repoOutsideWorkspace", what, err)
	}
}

func subject(t *testing.T, api GitAPI) string {
	t.Helper()
	commits, err := api.Log(context.Background(), 1)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %v", commits, err)
	}
	return commits[0].Subject
}

// addWorktree makes a linked worktree of repo at path, on a new branch, with one commit.
func addWorktree(t *testing.T, repo, path, branch, subject string) {
	t.Helper()
	probeGit(t, repo, "worktree", "add", "-q", "-b", branch, path)
	probeGit(t, path, "commit", "-q", "--allow-empty", "-m", subject)
}

// A linked worktree has a .git FILE naming a git directory under the main repository.
// That is legitimate and keeps working, beside the main repository and as a code-editing
// child's checkout under .superpowers/.worktrees, where the session is granted the
// checkout alone: the main repository is a workspace folder, not a granted root.
func TestAgentGitAcceptsALinkedWorktree(t *testing.T) {
	requireGit(t)
	for name, childCheckout := range map[string]bool{"beside the repository": false, "a child's own checkout": true} {
		t.Run(name, func(t *testing.T) {
			root := realPath(t.TempDir())
			main := filepath.Join(root, "main")
			probeRepo(t, main, "MAIN")
			wt, project := filepath.Join(root, "wt"), root
			if childCheckout {
				wt = filepath.Join(main, ".superpowers", ".worktrees", "wt")
				project = wt
			}
			addWorktree(t, main, wt, "wt-branch", "IN-THE-WORKTREE")
			srv := scopedServer(t, root, project)
			for _, write := range []bool{false, true} {
				api, _, err := srv.gitFor(agentCtx(), wt, write)
				if err != nil {
					t.Fatalf("write=%v: %v", write, err)
				}
				if got := subject(t, api); got != "IN-THE-WORKTREE" {
					t.Fatalf("answered %q", got)
				}
			}
			// The handle is the checked repository: rewriting the .git file afterwards
			// does not move it.
			victim := realPath(t.TempDir())
			probeRepo(t, victim, "VICTIM-REPO-OUTSIDE-THE-GRANT")
			api, _, err := srv.gitFor(agentCtx(), wt, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(victim, ".git")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := subject(t, api); got != "IN-THE-WORKTREE" {
				t.Errorf("after the .git file was rewritten the handle answered %q", got)
			}
			// A new request sees the rewritten file and is refused.
			_, _, err = srv.gitFor(agentCtx(), wt, false)
			requireRefused(t, err, "a new request after the rewrite")
		})
	}
}

// A submodule's .git is a file naming a directory under the superproject's .git/modules.
func TestAgentGitAcceptsASubmodule(t *testing.T) {
	requireGit(t)
	root := realPath(t.TempDir())
	upstream := filepath.Join(realPath(t.TempDir()), "upstream")
	probeRepo(t, upstream, "SUBMODULE-COMMIT")
	super := filepath.Join(root, "super")
	probeRepo(t, super, "SUPER")
	probeGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", upstream, "sub")
	probeGit(t, super, "commit", "-q", "-m", "add the submodule")
	sub := filepath.Join(super, "sub")
	if info, err := os.Lstat(filepath.Join(sub, ".git")); err != nil || info.IsDir() {
		t.Fatalf("the submodule's .git should be a file: %v", err)
	}
	srv := scopedServer(t, root, super)
	for _, write := range []bool{false, true} {
		api, _, err := srv.gitFor(agentCtx(), sub, write)
		if err != nil {
			t.Fatalf("write=%v: %v", write, err)
		}
		if got := subject(t, api); got != "SUBMODULE-COMMIT" {
			t.Fatalf("answered %q, want the submodule's own history", got)
		}
	}
}

// Where a .git file points, relative or absolute, decides the repository.
func TestAgentGitRefusesAGitFileThatPointsOutsideTheScope(t *testing.T) {
	requireGit(t)
	for name, pointer := range map[string]func(pkg, victim string) string{
		"absolute": func(_, victim string) string { return victim },
		"relative": func(pkg, victim string) string { rel, _ := filepath.Rel(pkg, victim); return rel },
		"through a dot": func(pkg, victim string) string {
			return filepath.Join(pkg, "..", "..", filepath.Base(filepath.Dir(victim)), ".git")
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, root, outside := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
			probeRepo(t, outside, "VICTIM-REPO-OUTSIDE-THE-GRANT")
			pkg := filepath.Join(root, "pkg")
			victim := filepath.Join(outside, ".git")
			if err := os.WriteFile(filepath.Join(pkg, ".git"), []byte("gitdir: "+pointer(pkg, victim)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, write := range []bool{false, true} {
				api, _, err := srv.gitFor(agentCtx(), pkg, write)
				if err == nil {
					t.Fatalf("write=%v: opened a handle that answers %q", write, subject(t, api))
				}
				if code := appCode(err); code != "git.repoOutsideWorkspace" && code != "git.notARepository" {
					t.Errorf("write=%v: err = %v", write, err)
				}
			}
		})
	}
}

// A worktree's git directory is in scope but its commondir file names another
// repository: git follows it, so it is checked too.
func TestAgentGitRefusesACommondirOutsideTheScope(t *testing.T) {
	requireGit(t)
	srv, root, outside := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
	main := filepath.Join(root, "main")
	probeRepo(t, main, "MAIN")
	wt := filepath.Join(root, "pkg", "wt")
	addWorktree(t, main, wt, "wt-branch", "IN-THE-WORKTREE")
	// The other repository has the branch the worktree's HEAD names, so git still opens it.
	probeRepo(t, outside, "VICTIM-REPO-OUTSIDE-THE-GRANT")
	probeGit(t, outside, "branch", "wt-branch")
	if _, _, err := srv.gitFor(agentCtx(), wt, false); err != nil {
		t.Fatalf("control, before the commondir is changed: %v", err)
	}
	commondir := filepath.Join(main, ".git", "worktrees", "wt", "commondir")
	if err := os.WriteFile(commondir, []byte(filepath.Join(outside, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, write := range []bool{false, true} {
		api, _, err := srv.gitFor(agentCtx(), wt, write)
		if err == nil {
			t.Fatalf("write=%v: opened a handle that answers %q", write, subject(t, api))
		}
		requireRefused(t, err, "commondir outside the scope")
	}
}

// A .git directory is the repository, but it can carry a commondir file too.
func TestAgentGitRefusesACommondirInsideAGitDirectory(t *testing.T) {
	requireGit(t)
	srv, root, outside := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
	probeRepo(t, outside, "VICTIM-REPO-OUTSIDE-THE-GRANT")
	pkg := filepath.Join(root, "pkg")
	probeRepo(t, pkg, "GRANTED-REPO")
	if err := os.WriteFile(filepath.Join(pkg, ".git", "commondir"), []byte(filepath.Join(outside, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err == nil {
		t.Fatalf("opened a handle that answers %q", subject(t, api))
	}
	requireRefused(t, err, "commondir file in a .git directory")
}

// A .git that is a link is the repository it resolves to, and that must be in scope.
func TestAgentGitRefusesADotGitLinkThatLeavesTheScope(t *testing.T) {
	requireGit(t)
	srv, root, outside := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
	probeRepo(t, outside, "VICTIM-REPO-OUTSIDE-THE-GRANT")
	pkg := filepath.Join(root, "pkg")
	if err := os.Symlink(filepath.Join(outside, ".git"), filepath.Join(pkg, ".git")); err != nil {
		t.Fatal(err)
	}
	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err == nil {
		t.Fatalf("opened a handle that answers %q", subject(t, api))
	}
	requireRefused(t, err, ".git link out of the scope")
}

// An ordinary repository and a subfolder of one are what they were.
func TestAgentGitStillOpensOrdinaryRepositories(t *testing.T) {
	requireGit(t)
	srv, root, _ := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
	pkg := filepath.Join(root, "pkg")
	probeRepo(t, pkg, "GRANTED-REPO")
	deeper := filepath.Join(pkg, "sub", "deeper")
	if err := os.MkdirAll(deeper, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{pkg, deeper} {
		for _, write := range []bool{false, true} {
			api, _, err := srv.gitFor(agentCtx(), dir, write)
			if err != nil {
				t.Fatalf("%s write=%v: %v", dir, write, err)
			}
			if got := subject(t, api); got != "GRANTED-REPO" {
				t.Fatalf("%s answered %q", dir, got)
			}
			if _, err := api.Status(agentCtx()); err != nil {
				t.Fatalf("%s status: %v", dir, err)
			}
		}
	}
}

// The person's terminal keeps the repository's own configuration: not hardened, not pinned.
func TestThePersonsTerminalKeepsTheRepositorysOwnConfiguration(t *testing.T) {
	requireGit(t)
	srv, root, _ := linkedScopeServer(t, GitReposOf(gitservice.NewService(context.Background())))
	pkg := filepath.Join(root, "pkg")
	probeRepo(t, pkg, "GRANTED-REPO")
	marker := filepath.Join(t.TempDir(), "ran")
	probeGit(t, pkg, "config", "core.fsmonitor", "touch "+marker+"; true")
	person := identity.WithCaller(context.Background(), identity.UserTerminal())
	api, _, err := srv.gitFor(person, pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Status(person); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the person's status no longer runs the repository's own core.fsmonitor")
	}
}
