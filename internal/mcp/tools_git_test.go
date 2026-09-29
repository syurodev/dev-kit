package mcp

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	gitservice "devkit/internal/git"
	"devkit/internal/gitparse"
	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeGit is one repository; it says which by its branch name.
type fakeGit struct {
	name   string
	staged []string
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

// fakeRepos holds a repository per workspace folder; anything else is not one.
type fakeRepos map[string]*fakeGit

func (f fakeRepos) At(_ context.Context, path string) (GitAPI, error) {
	if repo, ok := f[path]; ok {
		return repo, nil
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
