package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	gitservice "devkit/internal/git"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A link inside a granted folder is only inside it if its target is. These are the
// containment decisions gitFor makes, so a repository outside the workspace cannot
// be reached through a name that sits under a granted folder.
func TestScopeContainmentFollowsLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"out": outside, "in": filepath.Join(root, "pkg")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	scope := gitScope{granted: []string{root}, folders: []WorkspaceFolder{{Name: "ws", Path: root}}}
	for name, tc := range map[string]struct {
		path string
		in   bool
	}{
		"the folder itself":                {root, true},
		"a plain subfolder":                {filepath.Join(root, "pkg"), true},
		"a link to a subfolder":            {filepath.Join(root, "in"), true},
		"a link out":                       {filepath.Join(root, "out"), false},
		"inside a link out":                {filepath.Join(root, "out", "deep"), false},
		"not yet existing inside a link":   {filepath.Join(root, "out", "new", "x"), false},
		"not yet existing inside a folder": {filepath.Join(root, "new", "x"), true},
		"a sibling sharing the prefix":     {root + "-v2", false},
		"an unrelated folder":              {outside, false},
	} {
		if got := scope.covers(tc.path); got != tc.in {
			t.Errorf("%s: covers(%s) = %v, want %v", name, tc.path, got, tc.in)
		}
		if got := scope.folderOf(tc.path) != ""; got != tc.in {
			t.Errorf("%s: folderOf(%s) found a folder = %v, want %v", name, tc.path, got, tc.in)
		}
	}
}

// recordingRepos stands in for the Git module: it records the path it is opened at
// and answers with a repository whose checkout root the test chooses.
type recordingRepos struct {
	opened []string
	top    func(path string) string
}

type rootedGit struct {
	*fakeGit
	top string
}

func (r rootedGit) Toplevel() string { return r.top }

func (r *recordingRepos) At(_ context.Context, path string) (GitAPI, error) {
	r.opened = append(r.opened, path)
	return rootedGit{fakeGit: &fakeGit{name: "repo"}, top: r.top(path)}, nil
}

func (r *recordingRepos) Summaries(context.Context, []string) ([]gitservice.Repository, error) {
	return nil, nil
}

func linkedScopeServer(t *testing.T, repos GitRepos) (*Server, string, string) {
	t.Helper()
	root, outside := t.TempDir(), t.TempDir()
	// The temp dirs may themselves sit behind a link (/var -> /private/var on macOS).
	root, outside = realPath(root), realPath(outside)
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := agentHubTestDeps(&fakeCaller{}, &fakeAgentHub{status: agenthub.AgentSession{ID: gitSession, ProjectPath: root}})
	d.Workspace = fakeWorkspaces{
		workspaces: []workspace.Workspace{{ID: "ws", Name: "ws", Root: root}},
		services:   map[string][]workspace.WorkspaceService{"ws": {{Name: "ws", Path: root}}},
	}
	d.Git = &fakeGit{name: "open"}
	d.GitRepos = repos
	return NewServer(d), root, outside
}

// The module is opened at the path that was checked, not at the name the caller
// gave. A link inside the folder that is pointed elsewhere after the check then
// cannot change where Git runs.
func TestGitIsOpenedAtTheCheckedPathNotTheCallersName(t *testing.T) {
	repos := &recordingRepos{top: func(p string) string { return p }}
	srv, root, _ := linkedScopeServer(t, repos)
	link := filepath.Join(root, "alias")
	if err := os.Mkdir(filepath.Join(root, "pkg", ".git"), 0o700); err != nil { // pkg is a repository of its own
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "pkg"), link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.toolGitStatus(agentCtx(), &sdk.CallToolRequest{}, gitRepoInput{Repo: link}); err != nil {
		t.Fatal(err)
	}
	// Whatever the link points at from now on, the open was made at the resolved path.
	if want := filepath.Join(root, "pkg"); len(repos.opened) != 1 || repos.opened[0] != want {
		t.Fatalf("opened at %v, want exactly %s", repos.opened, want)
	}
}

// A repository whose checkout root does not contain the path that was checked is
// not the repository that was checked (a folder swapped for a link between the
// check and the open): refused, and nothing runs in it.
func TestARepositoryThatIsNotTheOneCheckedIsRefused(t *testing.T) {
	outside := ""
	repos := &recordingRepos{top: func(string) string { return outside }}
	srv, root, out := linkedScopeServer(t, repos)
	outside = out
	for name, call := range map[string]func() error{
		"read": func() error {
			_, _, err := srv.toolGitStatus(agentCtx(), &sdk.CallToolRequest{}, gitRepoInput{Repo: filepath.Join(root, "pkg")})
			return err
		},
		"write": func() error {
			_, _, err := srv.toolGitStage(agentCtx(), &sdk.CallToolRequest{}, gitPathsInput{gitRepoInput: gitRepoInput{Repo: filepath.Join(root, "pkg")}, Paths: []string{"x"}})
			return err
		},
	} {
		if err := call(); appCode(err) != "git.repoOutsideWorkspace" {
			t.Errorf("%s in a repository other than the one checked = %v", name, err)
		}
	}
}

// The path is resolved once. A link repointed after that resolution, even to a
// repository whose own root would satisfy a check made against the new target,
// changes nothing: Git is opened at the path the scope was judged on.
func TestALinkRepointedAfterTheSingleResolutionIsNeverFollowed(t *testing.T) {
	repos := &recordingRepos{top: func(p string) string { return p }} // any repository "contains" itself
	srv, root, outside := linkedScopeServer(t, repos)
	pkg := filepath.Join(root, "pkg")
	if err := os.Mkdir(filepath.Join(pkg, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outside, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(pkg, alias); err != nil {
		t.Fatal(err)
	}
	srv.afterScopeResolve = func() {
		_ = os.Remove(alias)
		if err := os.Symlink(outside, alias); err != nil {
			t.Error(err)
		}
	}
	if _, _, err := srv.toolGitStatus(agentCtx(), &sdk.CallToolRequest{}, gitRepoInput{Repo: alias}); err != nil {
		t.Fatal(err)
	}
	if len(repos.opened) != 1 || repos.opened[0] != pkg {
		t.Fatalf("opened at %v, want only the path the scope was judged on, %s", repos.opened, pkg)
	}
	for _, p := range repos.opened {
		if p == outside {
			t.Fatalf("Git was opened in the repository outside the workspace")
		}
	}
}
