package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devkit/internal/apperrors"
	gitservice "devkit/internal/git"
	"devkit/internal/identity"
	"devkit/internal/workspace"
)

// GitRepos opens the Git module at one folder without moving the repository
// the app has open. That one is the person's — the Git view shows it and it
// follows Code Graph — and an agent working in another service of the
// workspace must neither have to move it nor act on it because it happened to
// be open.
type GitRepos interface {
	At(ctx context.Context, path string) (GitAPI, error)
	Summaries(ctx context.Context, paths []string) ([]gitservice.Repository, error)
}

// GitReposOf adapts the Git service; *gitservice.Service.At returns the
// concrete type, which an interface method cannot.
func GitReposOf(svc *gitservice.Service) GitRepos {
	if svc == nil {
		return nil
	}
	return gitRepos{svc}
}

type gitRepos struct{ svc *gitservice.Service }

func (g gitRepos) At(ctx context.Context, path string) (GitAPI, error) { return g.svc.At(ctx, path) }
func (g gitRepos) Summaries(ctx context.Context, paths []string) ([]gitservice.Repository, error) {
	return g.svc.Summaries(ctx, paths)
}

// gitScope is what a caller may reach: the workspace folders it can read, the
// ones it may write, and where it stands.
type gitScope struct {
	agent bool
	// cwd is an agent session's working directory, and its default repository.
	cwd string
	// folders are the workspace folders in reach: an agent's own workspaces,
	// or every workspace for the person's terminal.
	folders []WorkspaceFolder
	// granted are the roots an agent may write; nil for the person, whose
	// writes the tools' own gates (guards, the push rule) already cover.
	granted []string

	// realFolders and realGranted are folders and granted with their links
	// resolved, read once by seal. A decision is made against these and against
	// one resolved form of the path asked for, never against a second resolution.
	realFolders, realGranted []string
	sealed                   bool
}

func (s *Server) gitScopeFor(ctx context.Context) (gitScope, error) {
	caller, _ := identity.FromContext(ctx)
	scope := gitScope{agent: caller.IsAgent()}
	if scope.agent {
		if s.deps.AgentHub == nil {
			return gitScope{}, apperrors.New("internal_error", "Agent Hub is not available.", true)
		}
		session, err := s.deps.AgentHub.GetStatus(ctx, caller.SessionID)
		if err != nil {
			return gitScope{}, err
		}
		scope.cwd = session.ProjectPath
		scope.granted = append([]string{session.ProjectPath}, session.ExtraReadWritePaths...)
	}
	if s.deps.Workspace != nil {
		// workspace.list's own answer, so the names an agent reads there are
		// the names it can pass here.
		list, err := s.workspaceList(ctx)
		if err != nil {
			return gitScope{}, err
		}
		for _, ws := range list.Workspaces {
			scope.folders = append(scope.folders, ws.Folders...)
		}
	}
	return scope, nil
}

// realPath resolves the symbolic links of path, or of its longest existing
// ancestor when the path itself does not exist yet. A link inside a folder is
// only inside it if what it points at is, so every containment decision below
// compares resolved paths: the repository a git command then runs in is the one
// the scope was checked against, not a lexical name that happens to sit under a
// granted folder.
func realPath(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for p := path; ; {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// seal resolves the links of the folders and the granted roots once.
func (g gitScope) seal() gitScope {
	if g.sealed {
		return g
	}
	g.realFolders = make([]string, len(g.folders))
	for i, f := range g.folders {
		g.realFolders[i] = realPath(f.Path)
	}
	g.realGranted = make([]string, len(g.granted))
	for i, root := range g.granted {
		g.realGranted[i] = realPath(root)
	}
	g.sealed = true
	return g
}

// coversReal reports whether real, an already resolved path, is one of the
// granted roots or inside one.
func (g gitScope) coversReal(real string) bool {
	for _, root := range g.realGranted {
		if root != "" && workspace.Within(real, root) {
			return true
		}
	}
	return false
}

// folderOfReal is the workspace folder holding real, an already resolved path —
// the deepest, for a service nested in another — as written and as resolved, or
// "" when none does.
func (g gitScope) folderOfReal(real string) (folder, realFolder string) {
	for i, f := range g.folders {
		if g.realFolders[i] != "" && workspace.Within(real, g.realFolders[i]) && len(g.realFolders[i]) > len(realFolder) {
			folder, realFolder = f.Path, g.realFolders[i]
		}
	}
	return folder, realFolder
}

// covers reports whether path, once links are resolved, is one of the roots the
// session was granted or inside one.
func (g gitScope) covers(path string) bool { return g.seal().coversReal(realPath(path)) }

// folderOf is the workspace folder holding path — the deepest, for a service
// nested in another — or "" when none does.
func (g gitScope) folderOf(path string) string {
	folder, _ := g.seal().folderOfReal(realPath(path))
	return folder
}

// resolve turns a `repo` argument into a folder, or explains which it may be.
func (g gitScope) resolve(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return g.cwd, nil
	}
	if filepath.IsAbs(repo) {
		return filepath.Clean(repo), nil
	}
	var matches []string
	for _, f := range g.folders {
		if strings.EqualFold(f.Name, repo) || strings.EqualFold(filepath.Base(f.Path), repo) {
			matches = append(matches, f.Path)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", apperrors.New("git.repoAmbiguous",
			"More than one workspace folder is called "+repo+": pass its absolute path ("+strings.Join(matches, ", ")+").", true)
	case g.cwd != "" && (strings.ContainsRune(repo, '/') || strings.HasPrefix(repo, ".")):
		// A path relative to where the agent stands: "../kong-plugin". A bare
		// word is only ever a folder's name — joined onto the cwd it would
		// name a folder inside this repository and quietly answer for it.
		return filepath.Join(g.cwd, repo), nil
	}
	return "", g.unknown(repo)
}

func (g gitScope) unknown(repo string) error {
	names := make([]string, 0, len(g.folders))
	for _, f := range g.folders {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	msg := repo + " is not a folder of your workspace."
	if len(names) > 0 {
		msg += " Known folders: " + strings.Join(names, ", ") + " (see workspace.list or git.repos)."
	}
	return apperrors.New("git.repoOutsideWorkspace", msg, true)
}

// gitFor is the repository a git tool acts on, and its path for the answer.
//
// An agent's default is its own working directory, so an agent that edits
// files and then commits them commits them where it edited them, whatever the
// app has open. It may read any folder of its workspace and write only the
// folders its session was granted — the same rule its file tools live under.
// The person's terminal keeps the old default (the repository DevKit has open)
// and may name any workspace folder.
func (s *Server) gitFor(ctx context.Context, repo string, write bool) (GitAPI, string, error) {
	scope, err := s.gitScopeFor(ctx)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(repo) == "" && (!scope.agent || scope.cwd == "") {
		return s.deps.Git, "", nil
	}
	scope = scope.seal()
	target, err := scope.resolve(repo)
	if err != nil {
		return nil, "", err
	}
	// The one resolution of the path asked for. Every decision below, and the path
	// Git is opened at, use this value: resolving the name a second time would let
	// a link that is repointed in between be judged as one place and opened as
	// another.
	real := realPath(target)
	if s.afterScopeResolve != nil {
		s.afterScopeResolve()
	}
	folder, realFolder := scope.folderOfReal(real)
	if scope.agent {
		switch {
		case write && !scope.coversReal(real):
			return nil, "", apperrors.New("git.repoNotGranted",
				target+" is not a folder your session may write. Ask the person to add it to the session, "+
					"or work in "+scope.cwd+".", false)
		case !write && folder == "" && !scope.coversReal(real):
			return nil, "", scope.unknown(repo)
		}
	} else if folder == "" {
		return nil, "", scope.unknown(repo)
	}
	if s.deps.GitRepos == nil {
		return nil, "", apperrors.New("internal_error", "Git is not available for other repositories.", true)
	}
	// Keyed by the workspace folder when it is the repository: that is the
	// path the Git view opens a service at, so guards and remotes set there
	// apply here too. A folder that merely holds repositories is not one, and
	// then the path asked for is.
	// A repository of its own inside the folder (a submodule, a nested
	// checkout) is itself, not the folder around it.
	type candidate struct{ key, real string }
	candidates := []candidate{{target, real}}
	if folder != "" && realFolder != real && workspace.Within(real, realFolder) && !hasGitDir(real) {
		candidates = []candidate{{folder, realFolder}, {target, real}}
	}
	for _, c := range candidates {
		api, err := s.deps.GitRepos.At(ctx, c.real)
		if errors.Is(err, gitservice.ErrNoRepository) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		// The repository that opened is the one that path belongs to. A folder
		// swapped for a link after the path was resolved shows up here as a
		// checkout whose root does not contain it. A swap of a real directory
		// component after this point is not closed by a path-based check.
		if rooted, ok := api.(interface{ Toplevel() string }); ok {
			if top := rooted.Toplevel(); top == "" || !workspace.Within(c.real, top) {
				return nil, "", scope.unknown(repo)
			}
		}
		if scope.agent {
			if err := confineToScope(api, scope); err != nil {
				return nil, "", scope.unknown(repo)
			}
		}
		return api, c.key, nil
	}
	return nil, "", apperrors.New("git.notARepository", target+" is not inside a git repository.", true)
}

// confinable is the half of the Git module that lets an agent-origin handle be
// kept to the repository that was checked.
type confinable interface {
	Toplevel() string
	GitDirs() (gitDir, commonDir string)
	Confine(gitDir, workTree string)
}

// inReach reports whether real, an already resolved path, is somewhere the
// request was judged against: a folder of the workspace or a root the session was
// granted. A linked worktree's git directory lives in the main repository, which
// is a workspace folder even when the session was granted only the worktree.
func (g gitScope) inReach(real string) bool {
	folder, _ := g.folderOfReal(real)
	return folder != "" || g.coversReal(real)
}

// confineToScope is what keeps a checkout from being another repository's work
// tree. Git decides which repository a folder belongs to from its .git: a
// directory is the repository itself, and a file (a linked worktree or a
// submodule has one) says `gitdir: <path>`, which can name any repository on the
// machine. An agent can write that file in a folder it was granted, and the
// checkout root would still be the granted folder. So the .git at the top of
// the checkout is read, and where it points, with the common directory the git
// directory names, must be what git itself found and must be inside the scope
// the request was judged against; the handle is then pinned to exactly those
// with GIT_DIR and GIT_WORK_TREE. A .git directory is the repository and is
// accepted as it always was. Handles that are not the real Git module (the
// tests' fakes) have nothing to confine.
func confineToScope(api GitAPI, scope gitScope) error {
	c, ok := api.(confinable)
	if !ok {
		return nil
	}
	top := c.Toplevel()
	gitDir, commonDir := c.GitDirs()
	if top == "" || gitDir == "" || commonDir == "" {
		return errOutsideScope
	}
	dotGit := filepath.Join(top, ".git")
	linked := false
	info, err := os.Lstat(dotGit)
	if err != nil {
		return errOutsideScope // a work tree with no .git at its top: configured from elsewhere
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// A link is only the repository it resolves to.
		real := realPath(dotGit)
		if !scope.inReach(real) || real != gitDir {
			return errOutsideScope
		}
	case info.IsDir():
		if realPath(dotGit) != gitDir {
			return errOutsideScope
		}
	default:
		target, err := readGitdirFile(dotGit)
		if err != nil {
			return errOutsideScope
		}
		real := realPath(target)
		if real != gitDir {
			return errOutsideScope
		}
		linked = linkedWorktreeOf(dotGit, gitDir, commonDir)
		if !linked && !scope.inReach(real) {
			return errOutsideScope
		}
	}
	// The git directory can name another common directory (a linked worktree's
	// commondir file), and git follows it whatever its own location is.
	declared := gitDir
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		declared = strings.TrimSpace(string(raw))
		if !filepath.IsAbs(declared) {
			declared = filepath.Join(gitDir, declared)
		}
		declared = realPath(declared)
	}
	if declared != commonDir || (commonDir != gitDir && !linked && !scope.inReach(commonDir)) {
		return errOutsideScope
	}
	c.Confine(gitDir, top)
	return nil
}

// linkedWorktreeOf reports whether dotGit, the .git file of a checkout, and gitDir,
// the git directory it names, are the two halves of a linked worktree: gitDir is
// <commonDir>/worktrees/<name>, and the file git wrote there to find its checkout
// (`gitdir`) names this very .git file. A code-editing child is granted its checkout
// alone, so the repository it was cut from is not one of its granted roots (and may
// belong to no workspace); the two names pointing at each other is what a checkout
// that was really added to that repository looks like, and what a folder redirected
// to some other repository does not have: that repository's worktree entries name
// other checkouts.
func linkedWorktreeOf(dotGit, gitDir, commonDir string) bool {
	if gitDir != filepath.Join(commonDir, "worktrees", filepath.Base(gitDir)) {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return false
	}
	back := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(back) {
		back = filepath.Join(gitDir, back)
	}
	return realPath(back) == realPath(dotGit)
}

var errOutsideScope = errors.New("repository outside the scope")

// readGitdirFile is where a .git file says the git directory is. A relative path
// is relative to the folder holding the file.
func readGitdirFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	target, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return "", errors.New("not a gitdir file")
	}
	if target = strings.TrimSpace(target); target == "" {
		return "", errors.New("empty gitdir")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return target, nil
}

// hasGitDir reports whether path is the top of a work tree: a .git directory,
// or the .git file a submodule or linked worktree has.
func hasGitDir(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
