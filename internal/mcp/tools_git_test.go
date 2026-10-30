package mcp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	gitservice "devkit/internal/git"
	"devkit/internal/gitparse"
	"devkit/internal/identity"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeGit is one repository; it says which by its branch name.
type fakeGit struct {
	name          string
	staged        []string
	synced        []string
	worktreeInput gitservice.WorktreeAddInput
	worktrees     []gitservice.Worktree
	removed       string
	basePath      string

	// What WorktreeRemove saw, and how a test makes it fail. The fake respects the
	// context it is given, as the real Git service does: a removal on a cancelled
	// context fails and removes nothing.
	removeCalls    int
	removeCtxErr   error
	removeDeadline bool
	removeCaller   string
	removeErr      error
	removeBlocks   bool
}

func (f *fakeGit) Status(context.Context) (gitservice.Status, error) {
	return gitservice.Status{Branch: f.name}, nil
}
func (f *fakeGit) Diff(context.Context, string, bool) ([]gitparse.Change, error) { return nil, nil }
func (f *fakeGit) Log(context.Context, int) ([]gitservice.Commit, error)         { return nil, nil }
func (f *fakeGit) CommitsForTask(context.Context, string, int) ([]gitservice.Commit, error) {
	return nil, nil
}
func (f *fakeGit) Stage(_ context.Context, paths []string) error {
	f.staged = append(f.staged, paths...)
	return nil
}
func (f *fakeGit) StageTurn(_ context.Context, paths []string) ([]string, error) { return paths, nil }
func (f *fakeGit) Commit(context.Context, gitservice.CommitInput) (gitservice.Commit, error) {
	return gitservice.Commit{Subject: f.name}, nil
}
func (f *fakeGit) Branches(context.Context) ([]gitservice.Branch, error) { return nil, nil }
func (f *fakeGit) CreateBranch(_ context.Context, name, _ string) (string, error) {
	return name, nil
}
func (f *fakeGit) Checkout(context.Context, string) error           { return nil }
func (f *fakeGit) Stash(context.Context, string) error              { return nil }
func (f *fakeGit) Push(context.Context, gitservice.PushInput) error { return nil }
func (f *fakeGit) Conflicts(context.Context) ([]string, error)      { return nil, nil }
func (f *fakeGit) Conflict(context.Context, string) (gitservice.Conflict, error) {
	return gitservice.Conflict{}, nil
}
func (f *fakeGit) Resolve(context.Context, string, string) error { return nil }
func (f *fakeGit) Amend(context.Context, gitservice.AmendInput) (gitservice.Commit, error) {
	return gitservice.Commit{Subject: f.name}, nil
}
func (f *fakeGit) Squash(context.Context, gitservice.SquashInput) (gitservice.Commit, error) {
	return gitservice.Commit{Subject: f.name}, nil
}
func (f *fakeGit) Rebase(context.Context, gitservice.RebaseInput) (gitservice.HistoryResult, error) {
	return gitservice.HistoryResult{Head: gitservice.Commit{Subject: f.name}}, nil
}
func (f *fakeGit) CherryPick(context.Context, gitservice.PickInput) (gitservice.HistoryResult, error) {
	return gitservice.HistoryResult{Head: gitservice.Commit{Subject: f.name}}, nil
}
func (f *fakeGit) Revert(context.Context, gitservice.PickInput) (gitservice.HistoryResult, error) {
	return gitservice.HistoryResult{Head: gitservice.Commit{Subject: f.name}}, nil
}
func (f *fakeGit) Fetch(_ context.Context, remote string) error {
	f.synced = append(f.synced, "fetch "+remote)
	return nil
}
func (f *fakeGit) Pull(_ context.Context, remote string) error {
	f.synced = append(f.synced, "pull "+remote)
	return nil
}
func (f *fakeGit) WorktreeAdd(_ context.Context, in gitservice.WorktreeAddInput) (gitservice.Worktree, error) {
	f.worktreeInput = in
	_, path, _ := f.WorktreeTarget(context.Background(), in.Branch)
	return gitservice.Worktree{Branch: in.Branch, Path: path, Head: "abc"}, nil
}
func (f *fakeGit) WorktreeTarget(_ context.Context, branch string) (string, string, error) {
	root := f.basePath
	if root == "" {
		root = "/repo"
	}
	return root, filepath.Join(root, ".superpowers", ".worktrees", strings.ReplaceAll(branch, "/", "-")), nil
}
func (f *fakeGit) Worktrees(context.Context) ([]gitservice.Worktree, error) { return f.worktrees, nil }
func (f *fakeGit) WorktreeRemove(ctx context.Context, name string) error {
	f.removeCalls++
	f.removeCtxErr = ctx.Err()
	_, f.removeDeadline = ctx.Deadline()
	if c, ok := identity.FromContext(ctx); ok {
		f.removeCaller = c.SessionID
	}
	if f.removeBlocks {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = name
	return nil
}
func (f *fakeGit) WorktreePrune(context.Context) ([]string, error) { return nil, nil }

// fakeRepos holds a repository per workspace folder; anything else is not one.
type fakeRepos map[string]*fakeGit

// At matches by resolved path, as the Git module does: the server opens it at the
// resolved form of a name it checked, which on macOS is not /tmp/... as written.
func (f fakeRepos) At(_ context.Context, path string) (GitAPI, error) {
	for root, repo := range f {
		if realPath(root) == realPath(path) {
			repo.basePath = root
			return repo, nil
		}
	}
	for root, repo := range f {
		if workspace.Within(realPath(path), realPath(root)) {
			repo.basePath = root
			return repo, nil
		}
	}
	return nil, gitservice.ErrNoRepository
}

func (f fakeRepos) Summaries(_ context.Context, paths []string) ([]gitservice.Repository, error) {
	out := make([]gitservice.Repository, 0, len(paths))
	for _, p := range paths {
		row := gitservice.Repository{Path: p}
		if repo, ok := f[p]; ok {
			row.Branch = repo.name
		} else {
			row.Error = "not a repository"
		}
		out = append(out, row)
	}
	return out, nil
}

const gitSession = "11111111-2222-3333-4444-555555555555"

// The app has the primary (kong-plugin) open; each service is its own repo.
func gitServer(session agenthub.AgentSession) (*Server, *fakeGit, fakeRepos) {
	open := &fakeGit{name: "open"}
	repos := fakeRepos{
		kong:   {name: "kong-plugin"},
		auth:   {name: "auth"},
		config: {name: "config"},
		other:  {name: "billing"},
	}
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{status: session})
	d.Workspace = workspaceFixture()
	d.Git = open
	d.GitRepos = repos
	return NewServer(d), open, repos
}

func agentCtx() context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(gitSession))
}

func statusOf(t *testing.T, srv *Server, ctx context.Context, repo string) gitStatusResult {
	t.Helper()
	_, v, err := srv.toolGitStatus(ctx, &sdk.CallToolRequest{}, gitRepoInput{Repo: repo})
	if err != nil {
		t.Fatalf("git.status(%q): %v", repo, err)
	}
	return unwrap(v).(gitStatusResult)
}

// An agent's git is the repository it works in, not the one the app has open.
func TestGitToolsDefaultToTheSessionsOwnRepository(t *testing.T) {
	srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: auth})
	got := statusOf(t, srv, agentCtx(), "")
	if got.Branch != "auth" || got.Repo != auth {
		t.Fatalf("status = %+v, want the session's own repository %s", got, auth)
	}
	// Staging goes there too.
	if _, _, err := srv.toolGitStage(agentCtx(), &sdk.CallToolRequest{}, gitPathsInput{Paths: []string{"a.go"}}); err != nil {
		t.Fatal(err)
	}
	if len(repos[auth].staged) != 1 || len(repos[kong].staged) != 0 {
		t.Fatalf("staged in the wrong repository: auth=%v kong=%v", repos[auth].staged, repos[kong].staged)
	}
}

// A session working in a subfolder commits in its service's repository.
func TestGitToolsFindTheServiceAboveASubfolder(t *testing.T) {
	srv, _, _ := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: config + "/src/main"})
	if got := statusOf(t, srv, agentCtx(), ""); got.Repo != config {
		t.Fatalf("repo = %q, want the service folder %s", got.Repo, config)
	}
}

// Another service of the workspace can be read by name; writing it takes a grant.
func TestGitToolsReadAnyWorkspaceFolderAndWriteOnlyGrantedOnes(t *testing.T) {
	srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: kong, ExtraReadWritePaths: []string{auth}})

	if got := statusOf(t, srv, agentCtx(), "fsap-config-service"); got.Branch != "config" {
		t.Fatalf("read by name = %+v", got)
	}
	_, _, err := srv.toolGitStage(agentCtx(), &sdk.CallToolRequest{}, gitPathsInput{gitRepoInput: gitRepoInput{Repo: "fsap-config-service"}, Paths: []string{"x"}})
	if appCode(err) != "git.repoNotGranted" || len(repos[config].staged) != 0 {
		t.Fatalf("writing an ungranted folder = %v (staged %v)", err, repos[config].staged)
	}
	if _, _, err := srv.toolGitStage(agentCtx(), &sdk.CallToolRequest{}, gitPathsInput{gitRepoInput: gitRepoInput{Repo: auth}, Paths: []string{"x"}}); err != nil {
		t.Fatalf("writing a granted folder: %v", err)
	}
	// Another workspace is out of reach altogether, by name or by path.
	if _, _, err := srv.toolGitStatus(agentCtx(), &sdk.CallToolRequest{}, gitRepoInput{Repo: other}); appCode(err) != "git.repoOutsideWorkspace" {
		t.Fatalf("reading another workspace = %v", err)
	}
	if _, _, err := srv.toolGitStatus(agentCtx(), &sdk.CallToolRequest{}, gitRepoInput{Repo: "billing-service"}); err == nil {
		t.Fatal("a folder of another workspace resolved by name")
	}
}

func TestGitWorktreeToolsUseRepositoryAndWriteGrants(t *testing.T) {
	srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: kong, ExtraReadWritePaths: []string{auth}})
	repos[auth].worktrees = []gitservice.Worktree{{Path: auth + "/.superpowers/.worktrees/feature", Branch: "feature", Head: "abc", Dirty: true}}
	_, result, err := srv.toolGitWorktreeAdd(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeAddInput{gitRepoInput: gitRepoInput{Repo: auth}, Branch: "feature", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if repos[auth].worktreeInput.Branch != "feature" || repos[auth].worktreeInput.Base != "main" {
		t.Fatalf("add input = %+v", repos[auth].worktreeInput)
	}
	if got := unwrap(result).(gitservice.Worktree); got.Branch != "feature" {
		t.Fatalf("add result = %+v", got)
	}
	_, result, err = srv.toolGitWorktreeList(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeListInput{gitRepoInput: gitRepoInput{Repo: auth}})
	if err != nil || len(unwrap(result).([]gitservice.Worktree)) != 1 {
		t.Fatalf("list = %v, %v", result, err)
	}
	_, _, err = srv.toolGitWorktreeRemove(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeRemoveInput{gitRepoInput: gitRepoInput{Repo: auth}, Name: "feature"})
	if err != nil || repos[auth].removed != "feature" {
		t.Fatalf("remove = %v, name %q", err, repos[auth].removed)
	}
	if _, _, err := srv.toolGitWorktreeAdd(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeAddInput{gitRepoInput: gitRepoInput{Repo: config}, Branch: "forbidden"}); appCode(err) != "git.repoNotGranted" {
		t.Fatalf("write to ungranted folder = %v", err)
	}
}

func TestGitWorktreeAddChecksResolvedRootAgainstSessionGrant(t *testing.T) {
	srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: auth + "/subdir"})
	_, _, err := srv.toolGitWorktreeAdd(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeAddInput{Branch: "feature/restricted"})
	if appCode(err) != "git.repoNotGranted" {
		t.Fatalf("add from subdirectory-only grant = %v, want git.repoNotGranted", err)
	}
	if repos[auth].worktreeInput.Branch != "" {
		t.Fatalf("worktree was created before root authorization: %+v", repos[auth].worktreeInput)
	}
}

func TestGitWorktreeRemoveRefusesIdleAndRunningSessionCwds(t *testing.T) {
	for _, status := range []agenthub.SessionStatus{agenthub.SessionStatusIdle, agenthub.SessionStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: auth})
			path := auth + "/.superpowers/.worktrees/feature"
			repos[auth].worktrees = []gitservice.Worktree{{Path: path, Branch: "feature"}}
			hub := &fakeAgentHub{status: agenthub.AgentSession{ID: gitSession, ProjectPath: auth}, worktreeInUse: true}
			srv.deps.AgentHub = hub
			_, listed, err := srv.toolGitWorktreeList(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeListInput{})
			if err != nil {
				t.Fatalf("list before remove: %v", err)
			}
			if got := len(unwrap(listed).([]gitservice.Worktree)); got != 1 {
				t.Fatalf("listed %d worktrees", got)
			}
			_, _, err = srv.toolGitWorktreeRemove(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeRemoveInput{Name: "feature"})
			if appCode(err) != "git.worktreeInUse" {
				t.Fatalf("remove while %s session uses checkout = %v (check called %v)", status, err, hub.worktreeCheckCalled)
			}
			if repos[auth].removed != "" {
				t.Fatalf("live checkout was removed: %q", repos[auth].removed)
			}
		})
	}
	closed, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: auth})
	path := auth + "/.superpowers/.worktrees/feature"
	repos[auth].worktrees = []gitservice.Worktree{{Path: path, Branch: "feature"}}
	closed.deps.AgentHub = &fakeAgentHub{status: agenthub.AgentSession{ID: gitSession, ProjectPath: auth}}
	if _, _, err := closed.toolGitWorktreeRemove(agentCtx(), &sdk.CallToolRequest{}, gitWorktreeRemoveInput{Name: "feature"}); err != nil {
		t.Fatalf("remove after close = %v", err)
	}
	if repos[auth].removed != "feature" {
		t.Fatalf("removed = %q, want feature", repos[auth].removed)
	}
}

type mcpGitGuards map[string][]gitservice.Guard

func (g mcpGitGuards) ListGuards(project string) ([]gitservice.Guard, error) { return g[project], nil }

type mcpGitEnvs map[string]bool

func (e mcpGitEnvs) RequiresConfirmation(name string) bool { return e[name] }

type mcpGitConfirmer struct {
	allow bool
	asked int
}

func (c *mcpGitConfirmer) Confirm(context.Context, string, string) (bool, error) {
	c.asked++
	return c.allow, nil
}

func TestGitCommitThroughMCPKeepsParentGuardInLinkedWorktree(t *testing.T) {
	ctx := context.Background()
	git := gitservice.NewService(ctx)
	availability := git.Availability()
	if !availability.OK() {
		t.Skip("checkpoint.Locate found no usable Git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(availability.Path, append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := git.Open(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := git.Stage(ctx, []string{"base.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := git.Commit(ctx, gitservice.CommitInput{Message: "base"}); err != nil {
		t.Fatal(err)
	}
	// Guard keys use the canonical repository identity. On macOS, t.TempDir
	// may be under /var/folders while Git resolves the repository through
	// /private/var, so key this fixture the same way the guard store does.
	git.SetGuards(mcpGitGuards{git.PolicyProject(): {{Pattern: "feature/*", Env: "prod"}}})
	git.SetEnvs(mcpGitEnvs{"prod": true})
	allow := &mcpGitConfirmer{allow: true}
	git.SetConfirmer(allow)
	worktree, err := git.WorktreeAdd(ctx, gitservice.WorktreeAddInput{Branch: "feature/mcp-guard"})
	if err != nil {
		t.Fatal(err)
	}
	deny := &mcpGitConfirmer{}
	git.SetConfirmer(deny)
	if err := os.WriteFile(filepath.Join(worktree.Path, "change.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub := &fakeAgentHub{status: agenthub.AgentSession{ID: gitSession, ProjectPath: worktree.Path}}
	deps := agentHubTestDeps(&fakeCaller{}, hub)
	deps.GitRepos = GitReposOf(git)
	srv := NewServer(deps)
	if _, _, err := srv.toolGitStage(agentCtx(), &sdk.CallToolRequest{}, gitPathsInput{Paths: []string{"change.txt"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err = srv.toolGitCommit(agentCtx(), &sdk.CallToolRequest{}, gitCommitInput{Message: "must be denied"})
	if !errors.Is(err, gitservice.ErrNotAllowed) {
		t.Fatalf("MCP commit on guarded linked branch = %v, want ErrNotAllowed", err)
	}
	if deny.asked != 1 {
		t.Fatalf("denying confirmer asked %d times, want once", deny.asked)
	}
}

// Real Git: the same branch is already checked out in the parent repo. A child
// for another granted repo must still succeed there, with that repo's content
// and branch guard, regardless of which repository the app has open.
func TestAgentHubChildWorktreeUsesRealRequestedRepoAndItsGuard(t *testing.T) {
	ctx := context.Background()
	git := gitservice.NewService(ctx)
	availability := git.Availability()
	if !availability.OK() {
		t.Skip("no usable Git")
	}
	initRepo := func(marker string) string {
		t.Helper()
		repo, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) {
			t.Helper()
			if out, err := exec.Command(availability.Path, append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		run("init", "--initial-branch=main")
		run("config", "user.name", "Test")
		run("config", "user.email", "test@example.invalid")
		if err := os.WriteFile(filepath.Join(repo, "identity.txt"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		run("add", "identity.txt")
		run("commit", "-m", "initial")
		return repo
	}
	parentRepo, targetRepo := initRepo("parent"), initRepo("target")
	if err := git.Open(ctx, parentRepo); err != nil {
		t.Fatal(err)
	}
	branch := "feature/shared-child"
	if _, err := git.WorktreeAdd(ctx, gitservice.WorktreeAddInput{Branch: branch}); err != nil {
		t.Fatal(err)
	}
	parent := agenthub.AgentSession{ID: gitSession, ProjectPath: parentRepo, ExtraReadWritePaths: []string{targetRepo}}
	hub := &fakeAgentHub{status: parent, created: agenthub.AgentSession{ID: "child", AgentType: agenthub.AgentTypeMock}}
	deps := agentHubTestDeps(&fakeCaller{}, hub)
	deps.Git = git
	deps.GitRepos = GitReposOf(git)
	srv := NewServer(deps)
	_, _, err := srv.toolAgentHubCreateAgentSession(agentCtx(), &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", CallerSessionID: gitSession, Cwd: targetRepo, Worktree: branch})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(targetRepo, ".superpowers", ".worktrees", "feature-shared-child")
	if hub.createIn.Cwd != want {
		t.Fatalf("cwd=%s; want %s", hub.createIn.Cwd, want)
	}
	if content, err := os.ReadFile(filepath.Join(hub.createIn.Cwd, "identity.txt")); err != nil || string(content) != "target" {
		t.Fatalf("wrong repository content: %q %v", content, err)
	}
	if got := git.Project(); got != parentRepo {
		t.Fatalf("app's open repository moved: %s", got)
	}
	targetGit, err := git.At(ctx, targetRepo)
	if err != nil {
		t.Fatal(err)
	}
	git.SetGuards(mcpGitGuards{targetGit.PolicyProject(): {{Pattern: "feature/*", Env: "prod"}}})
	git.SetEnvs(mcpGitEnvs{"prod": true})
	deny := &mcpGitConfirmer{}
	git.SetConfirmer(deny)
	_, _, err = srv.toolAgentHubCreateAgentSession(agentCtx(), &sdk.CallToolRequest{}, agentHubCreateInput{AgentType: "mock", CallerSessionID: gitSession, Cwd: targetRepo, Worktree: "feature/guarded-child"})
	if !errors.Is(err, gitservice.ErrNotAllowed) || deny.asked != 1 {
		t.Fatalf("selected repo's guard was bypassed: %v; asked=%d", err, deny.asked)
	}
	if _, err := os.Stat(filepath.Join(targetRepo, ".superpowers", ".worktrees", "feature-guarded-child")); !os.IsNotExist(err) {
		t.Fatalf("guard refusal created checkout: %v", err)
	}
}

// The person's terminal keeps what it had — the repository DevKit has open —
// and may name any workspace folder.
func TestGitToolsFromTheTerminal(t *testing.T) {
	srv, _, _ := gitServer(agenthub.AgentSession{})
	ctx := identity.WithCaller(context.Background(), identity.UserTerminal())
	if got := statusOf(t, srv, ctx, ""); got.Branch != "open" || got.Repo != "" {
		t.Fatalf("default = %+v, want the open repository", got)
	}
	if got := statusOf(t, srv, ctx, "billing-service"); got.Branch != "billing" {
		t.Fatalf("by name = %+v", got)
	}
	if _, _, err := srv.toolGitStatus(ctx, &sdk.CallToolRequest{}, gitRepoInput{Repo: "/etc"}); appCode(err) != "git.repoOutsideWorkspace" {
		t.Fatalf("a path outside every workspace = %v", err)
	}
}

func TestGitReposListsTheSessionsWorkspace(t *testing.T) {
	srv, _, _ := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: kong, ExtraReadWritePaths: []string{auth}})
	_, v, err := srv.toolGitRepos(agentCtx(), &sdk.CallToolRequest{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	rows := unwrap(v).([]GitRepoInfo)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want the three folders of fsap-security", rows)
	}
	byName := map[string]GitRepoInfo{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if k := byName["kong-plugin"]; !k.Cwd || !k.Granted || k.Branch != "kong-plugin" {
		t.Errorf("kong-plugin = %+v", k)
	}
	if a := byName["fsap-authentication-service"]; a.Cwd || !a.Granted {
		t.Errorf("auth = %+v", a)
	}
	if c := byName["fsap-config-service"]; c.Cwd || c.Granted {
		t.Errorf("config = %+v", c)
	}
}

func appCode(err error) string {
	var app *apperrors.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

// The history tools write: they take the same grant as a commit, and the
// session's own repository needs none.
func TestGitHistoryToolsWriteOnlyGrantedFolders(t *testing.T) {
	srv, _, _ := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: kong, ExtraReadWritePaths: []string{auth}})
	ungranted := gitRepoInput{Repo: "fsap-config-service"}
	calls := map[string]func(gitRepoInput) error{
		"amend": func(r gitRepoInput) error {
			_, _, err := srv.toolGitAmend(agentCtx(), &sdk.CallToolRequest{}, gitAmendInput{gitRepoInput: r})
			return err
		},
		"squash": func(r gitRepoInput) error {
			_, _, err := srv.toolGitSquash(agentCtx(), &sdk.CallToolRequest{}, gitSquashInput{gitRepoInput: r, Count: 2})
			return err
		},
		"rebase": func(r gitRepoInput) error {
			_, _, err := srv.toolGitRebase(agentCtx(), &sdk.CallToolRequest{}, gitRebaseInput{gitRepoInput: r, Onto: "main"})
			return err
		},
		"cherryPick": func(r gitRepoInput) error {
			_, _, err := srv.toolGitCherryPick(agentCtx(), &sdk.CallToolRequest{}, gitPickInput{gitRepoInput: r, Commits: []string{"abc"}})
			return err
		},
		"fetch": func(r gitRepoInput) error {
			_, _, err := srv.toolGitFetch(agentCtx(), &sdk.CallToolRequest{}, gitRemoteInput{gitRepoInput: r})
			return err
		},
		"pull": func(r gitRepoInput) error {
			_, _, err := srv.toolGitPull(agentCtx(), &sdk.CallToolRequest{}, gitRemoteInput{gitRepoInput: r})
			return err
		},
		"revert": func(r gitRepoInput) error {
			_, _, err := srv.toolGitRevert(agentCtx(), &sdk.CallToolRequest{}, gitPickInput{gitRepoInput: r, Commits: []string{"abc"}})
			return err
		},
	}
	for name, call := range calls {
		if err := call(ungranted); appCode(err) != "git.repoNotGranted" {
			t.Errorf("%s on an ungranted folder = %v, want git.repoNotGranted", name, err)
		}
		if err := call(gitRepoInput{}); err != nil {
			t.Errorf("%s on the session's own repository: %v", name, err)
		}
	}
}

// Fetch and pull reach the session's own repository, default to origin, and
// answer where the branch stands.
func TestGitFetchAndPullReportTheBranch(t *testing.T) {
	srv, _, repos := gitServer(agenthub.AgentSession{ID: gitSession, ProjectPath: auth})
	_, v, err := srv.toolGitPull(agentCtx(), &sdk.CallToolRequest{}, gitRemoteInput{})
	if err != nil {
		t.Fatal(err)
	}
	got := unwrap(v).(gitSyncResult)
	if got.Repo != auth || got.Remote != "origin" || got.Branch != "auth" {
		t.Fatalf("pull = %+v", got)
	}
	if _, _, err := srv.toolGitFetch(agentCtx(), &sdk.CallToolRequest{}, gitRemoteInput{Remote: "upstream"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"pull origin", "fetch upstream"}; len(repos[auth].synced) != 2 || repos[auth].synced[0] != want[0] || repos[auth].synced[1] != want[1] {
		t.Fatalf("synced = %v, want %v", repos[auth].synced, want)
	}
}
