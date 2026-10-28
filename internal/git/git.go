// Package git is the project's own repository: status, diffs, staging,
// commits, branches and remotes on the real work tree.
//
// It is NOT internal/checkpoint. That package keeps a shadow repository with
// its own GIT_DIR and writes $GIT_DIR/info/attributes to disable the project's
// .gitattributes, so bytes go in and come out untouched. That is right for a
// snapshot and silently wrong for a commit: a repo declaring `* text=auto` or
// `*.bin filter=lfs` would get the wrong bytes written and nobody would notice
// until someone else checked out. Everything here runs in the work tree with
// the repository's own configuration, except for a call that comes from an
// agent: the repository is the agent's to write, so what its configuration
// would make git run is turned off for it (harden.go).
//
// What it does borrow from checkpoint is Locate: finding a usable git on a
// machine that has none on PATH is a solved problem there, including the macOS
// Command Line Tools shim that exits non-zero instead of running.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"devkit/internal/checkpoint"
	"devkit/internal/gitssh"
)

var (
	// ErrNoRepository is a project that is not a work tree. It is not a
	// failure: the module says so and everything else keeps working.
	ErrNoRepository = errors.New("git.noRepository")
	// ErrGitUnavailable is a machine with no usable git.
	ErrGitUnavailable = errors.New("git.unavailable")
	ErrNoProject      = errors.New("git.noProject")
	ErrConflict       = errors.New("git.conflict")
	// ErrProtectedBranch is a push an agent may not make.
	ErrProtectedBranch = errors.New("git.protectedBranch")
	ErrNotAllowed      = errors.New("git.notAllowed")
)

// Status is what changed in the work tree, and where the branch stands.
type Status struct {
	Branch string `json:"branch"`
	// Upstream is the tracked remote branch, empty when there is none.
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Detached bool   `json:"detached"`
	// Files maps a project-relative path to a one-letter state (M/A/D/R).
	Files map[string]string `json:"files"`
	// Staged and Unstaged split the same paths by index state, which is what
	// a commit screen needs and a file-tree marker does not.
	Staged    []string `json:"staged"`
	Unstaged  []string `json:"unstaged"`
	Untracked []string `json:"untracked"`
	// Conflicted paths need FR-14's three-pane resolve, not a diff.
	Conflicted []string `json:"conflicted"`
}

// Commit is one entry of the log.
type Commit struct {
	SHA   string `json:"sha"`
	Short string `json:"short"`
	// Parent is the first parent's short hash, empty for a root commit.
	Parent  string `json:"parent"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// Branch is one local branch.
type Branch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	// Upstream is its tracked remote branch, empty when it tracks nothing.
	Upstream string `json:"upstream"`
}

// runner runs git in a work tree. It exists so tests drive the real binary
// against a real repository — there is no useful fake of git's output.
type runner struct {
	git  string
	work string
	// hardened runs every command with repository-supplied execution turned off
	// (harden.go). It is set for a service opened by an agent-origin call; a
	// command whose own context carries an agent is hardened regardless.
	hardened bool
	// gitDir and workTree, when set, are passed as GIT_DIR and GIT_WORK_TREE so
	// discovery from the folder cannot select another repository than the one
	// that was checked. Only a hardened runner is pinned.
	gitDir, workTree string
	// bound, when set, holds the checkout open by descriptor and every command
	// runs in it without a path lookup (bound.go). It is set for an agent-origin
	// handle on a platform that has descriptors; its pins replace gitDir/workTree.
	bound *boundCheckout
}

// command builds one git invocation. extraEnv is added last: it is the host's
// own decision (an account's askpass, a key's ssh command, an identity).
func (r runner) command(ctx context.Context, extraEnv []string, args ...string) (*exec.Cmd, error) {
	prefix := []string{"--no-optional-locks"}
	hardened := r.isHardened(ctx)
	if hardened {
		for _, kv := range neutralConfig() {
			prefix = append(prefix, "-c", kv)
		}
		// rev-parse reads the repository's shape and runs nothing it names.
		if subcommand(args) != "rev-parse" {
			prefix = append(prefix, r.repositoryDrivers(ctx)...)
		}
	}
	cmd, pins, err := r.build(ctx, prefix, args)
	if err != nil {
		return nil, err
	}
	if hardened {
		cmd.Env = r.hardenedEnv(cmd.Environ(), pins, extraEnv)
	} else {
		cmd.Env = append(cmd.Environ(), extraEnv...)
	}
	return cmd, nil
}

// build is the process for `git <prefix> <args>` in this runner's work tree: the
// helper that changes into the held directory by descriptor when the checkout is
// bound, `git -C <path>` otherwise.
func (r runner) build(ctx context.Context, prefix, args []string) (*exec.Cmd, []string, error) {
	if r.bound != nil {
		return r.bound.command(ctx, r.git, append(append([]string{}, prefix...), args...))
	}
	prefix = append(append([]string{}, prefix...), "-C", r.work)
	return exec.CommandContext(ctx, r.git, append(prefix, args...)...), nil, nil
}

// guarded runs one command with the checkout's identity checked before and after:
// a name that was rebound to another directory refuses the command, or discards the
// answer of one that ran while it was.
func (r runner) guarded(cmd *exec.Cmd) error {
	if err := r.bound.verify(); err != nil {
		return err
	}
	var runErr error
	if r.bound != nil {
		runErr = r.bound.run(cmd)
	} else {
		runErr = cmd.Run()
	}
	if err := r.bound.verify(); err != nil {
		return err
	}
	return runErr
}

// runWithEnv runs git with extra environment, for the credential helper.
func (r runner) runWithEnv(ctx context.Context, env []string, args ...string) error {
	cmd, err := r.command(ctx, append([]string{"GIT_TERMINAL_PROMPT=0"}, env...), args...)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := r.guarded(cmd); err != nil {
		if isMoved(err) {
			return err
		}
		return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r runner) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	// Never let git ask: a prompt in a subprocess nobody is watching is a hang.
	cmd, err := r.command(ctx, []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never"}, args...)
	if err != nil {
		return nil, err
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := r.guarded(cmd); err != nil {
		if isMoved(err) {
			return nil, err
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Service is the module. One project at a time, like Code Graph.
type Service struct {
	availability checkpoint.Availability

	// The folder follows the app's work folder (internal/workfolder), so it is
	// changed from whichever goroutine announced the change while a request is
	// reading it.
	mu      sync.RWMutex
	project string
	// policyProject is the canonical repository identity used for guards and
	// remote bindings. A linked worktree has its own project/cwd but shares the
	// main repository's Git metadata and policy.
	policyProject string
	// toplevel is the resolved root of the checkout this service was opened at,
	// read when it was opened: the repository its commands really run in.
	toplevel string
	// gitDir and commonDir are the resolved git directory and common directory
	// git found for the checkout, read when it was opened.
	gitDir, commonDir string
	runner            runner

	guards  GuardStore
	remotes RemoteStore
	envs    EnvLookup
	creds   CredentialResolver
	picker  AccountPicker
	confirm Confirmer
	// hostOf turns the host in an SSH URL into the host ssh really connects to,
	// which differs when it is an alias in ~/.ssh/config. Nil means gitssh.
	hostOf func(ctx context.Context, host string) string
	// sshOptions are added to the ssh command; only a test sets them.
	sshOptions []string
}

// EnvLookup is the environment registry as this package needs it, declared
// locally like ssh/database/kafka do.
type EnvLookup interface {
	RequiresConfirmation(name string) bool
}

// CredentialResolver hands back a credential for a remote at the moment of
// use. It is internal/accounts, declared as an interface so git does not
// import it.
type CredentialResolver interface {
	ResolveForRemote(ctx context.Context, accountID, env string) (user, secret string, err error)
}

// SSHKeyResolver is the half of the credential resolver that can hand over an
// SSH key. isKey is false for an account that is not one (a token bound to a
// remote that turned out to be SSH): that is "nothing to do here", not a
// failure. Optional, like the label: a resolver without it simply leaves SSH
// remotes to git's own configuration.
type SSHKeyResolver interface {
	ResolveSSHKey(ctx context.Context, accountID, env string) (m gitssh.Material, isKey bool, err error)
}

// Confirmer is the interaction gate. Nil means headless, which fails closed.
type Confirmer interface {
	Confirm(ctx context.Context, title, detail string) (bool, error)
}

// NewService probes for git once. A machine without it still gets a Service —
// every call then reports ErrGitUnavailable, which the UI renders as a panel
// rather than an error.
func NewService(ctx context.Context) *Service {
	return &Service{availability: checkpoint.Locate(ctx)}
}

// Available reports whether git itself is usable here.
func (s *Service) Available() bool { return s.availability.OK() }

// Availability explains a machine without git, in the words checkpoint
// already worked out.
func (s *Service) Availability() checkpoint.Availability { return s.availability }

// SetGuards, SetEnvs, SetCredentials and SetConfirmer wire the collaborators
// the composition root owns.
func (s *Service) SetGuards(g GuardStore)              { s.guards = g }
func (s *Service) SetEnvs(e EnvLookup)                 { s.envs = e }
func (s *Service) SetCredentials(c CredentialResolver) { s.creds = c }
func (s *Service) SetConfirmer(c Confirmer)            { s.confirm = c }

// Open points the service at a project. A directory that is not a work tree
// is ErrNoRepository, which callers treat as "nothing to show" rather than as
// a failure.
func (s *Service) Open(ctx context.Context, projectPath string) error {
	if !s.availability.OK() {
		return ErrGitUnavailable
	}
	r := runner{git: s.availability.Path, work: projectPath}
	if _, err := r.run(ctx, nil, "rev-parse", "--is-inside-work-tree"); err != nil {
		return ErrNoRepository
	}
	policyProject, top, gitDir, commonDir, err := repositoryPolicyKey(ctx, r, projectPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.toplevel, s.gitDir, s.commonDir = top, gitDir, commonDir
	s.project, s.policyProject, s.runner = projectPath, policyProject, r
	s.mu.Unlock()
	return nil
}

// At is this module pointed at one other work tree, leaving the open project
// alone. The open project is the app's — the one the Git view shows and Code
// Graph follows — and an agent working in another service of the workspace
// must not have to move it, nor act on it by mistake because it is what
// happened to be open.
//
// The view shares everything but the folder: guards, remotes, credentials and
// the confirmation gate are looked up for projectPath exactly as they would be
// had it been opened, so a guarded branch is guarded whichever way it is
// reached. A folder that is not a work tree is ErrNoRepository.
func (s *Service) At(ctx context.Context, projectPath string) (*Service, error) {
	if !s.availability.OK() {
		return nil, ErrGitUnavailable
	}
	// An agent-origin call opens a hardened handle (harden.go); the person's keeps
	// the repository's own configuration.
	r := runner{git: s.availability.Path, work: projectPath, hardened: agentOrigin(ctx)}
	if r.hardened {
		// And a bound one where the platform has descriptors: the folder is opened
		// once, without following a link, and every command runs in that directory
		// (bound.go). Where it has none the handle stays path-based, as hardened.
		b, err := openBound(projectPath)
		switch {
		case err == nil:
			r.bound, r.work = b, b.path
		case errors.Is(err, errBoundUnsupported):
		case errors.Is(err, fs.ErrNotExist):
			return nil, ErrNoRepository
		case isMoved(err):
			return nil, err
		default:
			return nil, ErrNoRepository
		}
	}
	svc, err := s.openAt(ctx, r, projectPath)
	if err != nil {
		r.bound.Close()
		return nil, err
	}
	return svc, nil
}

func (s *Service) openAt(ctx context.Context, r runner, projectPath string) (*Service, error) {
	if r.bound == nil {
		if _, err := r.run(ctx, nil, "rev-parse", "--is-inside-work-tree"); err != nil {
			return nil, ErrNoRepository
		}
	}
	policyProject, top, gitDir, commonDir, err := repositoryPolicyKey(ctx, r, r.work)
	if err != nil {
		if r.bound != nil && !isMoved(err) {
			return nil, ErrNoRepository // not a work tree: the one rev-parse failed
		}
		return nil, err
	}
	if r.bound != nil {
		// What git said is checked against what is held: the top reached by walking
		// up from the directory, the git directory a name leads to.
		if err := r.bound.attach(top, gitDir); err != nil {
			return nil, err
		}
	}
	return &Service{
		availability:  s.availability,
		project:       projectPath,
		policyProject: policyProject,
		toplevel:      top,
		gitDir:        gitDir,
		commonDir:     commonDir,
		runner:        r,
		guards:        s.guards,
		remotes:       s.remotes,
		envs:          s.envs,
		creds:         s.creds,
		picker:        s.picker,
		confirm:       s.confirm,
		hostOf:        s.hostOf,
		sshOptions:    s.sshOptions,
	}, nil
}

// Reset lets go of the open project. A folder that is not a repository leaves
// Open failing without touching what was open before, which is right for a
// person typing a path and wrong for a module following the work folder: it
// would go on showing the previous repository under the new folder's name.
func (s *Service) Reset() {
	s.mu.Lock()
	s.project, s.policyProject, s.toplevel, s.gitDir, s.commonDir, s.runner = "", "", "", "", "", runner{}
	s.mu.Unlock()
}

// repositoryPolicyKey returns the repository root whose guards and remote
// bindings apply to this checkout. For linked worktrees Git points the
// per-worktree git dir at a common metadata directory under the main repo's
// .git; ordinary repositories (including submodules) use their own top-level.
//
// It also returns the resolved git directory and common directory git found
// for the checkout: what the checkout really is, which a caller that must keep
// an agent inside its scope compares with the scope.
func repositoryPolicyKey(ctx context.Context, r runner, projectPath string) (policy, top, gitDir, commonDir string, err error) {
	var topOut, gitDirOut, commonOut []byte
	if r.bound != nil {
		// One process for the three answers: a bound handle starts each command
		// through the helper on a platform without /proc, which costs a process image.
		out, err := r.run(ctx, nil, "rev-parse", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
		if err != nil {
			return "", "", "", "", err
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) != 3 {
			return "", "", "", "", ErrNoRepository
		}
		topOut, gitDirOut, commonOut = []byte(lines[0]), []byte(lines[1]), []byte(lines[2])
	} else {
		topOut, err = r.run(ctx, nil, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", "", "", "", err
		}
	}
	top = strings.TrimSpace(string(topOut))
	if !filepath.IsAbs(top) {
		top, err = filepath.Abs(filepath.Join(projectPath, top))
		if err != nil {
			return "", "", "", "", err
		}
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return "", "", "", "", err
	}
	if r.bound == nil {
		if gitDirOut, err = r.run(ctx, nil, "rev-parse", "--absolute-git-dir"); err != nil {
			return "", "", "", "", err
		}
		if commonOut, err = r.run(ctx, nil, "rev-parse", "--git-common-dir"); err != nil {
			return "", "", "", "", err
		}
	}
	gitDir = strings.TrimSpace(string(gitDirOut))
	commonDir = strings.TrimSpace(string(commonOut))
	if !filepath.IsAbs(commonDir) {
		// --git-common-dir is relative to the invocation directory, which can
		// be a subdirectory of the worktree root.
		commonDir = filepath.Join(projectPath, commonDir)
	}
	gitDir, err = filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", "", "", "", err
	}
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		return "", "", "", "", err
	}
	if gitDir != commonDir && filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), top, gitDir, commonDir, nil
	}
	return top, top, gitDir, commonDir, nil
}

// GitDirs are the resolved git directory and common directory of the checkout
// this service was opened at, as git found them; "" when it was not opened.
func (s *Service) GitDirs() (gitDir, commonDir string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gitDir, s.commonDir
}

// Confine pins every later command of an agent-origin handle to the git
// directory and work tree a caller has checked, passing them as GIT_DIR and
// GIT_WORK_TREE so discovery from the folder cannot select another repository
// (a .git file, a core.worktree, a link) than the one that was judged. It has no
// effect on a handle that is not hardened: the person's own handle keeps the
// repository's own configuration and discovery.
func (s *Service) Confine(gitDir, workTree string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runner.hardened {
		s.runner.gitDir, s.runner.workTree = gitDir, workTree
	}
}

// Toplevel is the resolved root of the checkout this service was opened at, or ""
// when it was not opened. A caller that opened it from a name it had checked
// compares this with what it checked, to know the repository it got is that one.
func (s *Service) Toplevel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.toplevel
}

func (s *Service) policyKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.policyProject != "" {
		return s.policyProject
	}
	return s.project
}

// PolicyProject is the canonical repository identity used for guards and
// remote bindings. Project remains the actual checkout path shown by the Git
// view, which can be a linked worktree.
func (s *Service) PolicyProject() string { return s.policyKey() }

// PolicyAliases includes the checkout path for backward-compatible reads of
// records created before policy keys were canonicalized.
func (s *Service) PolicyAliases() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == "" || s.project == s.policyProject {
		return nil
	}
	return []string{s.project}
}

// WorktreeIdentity returns the canonical checkout root and per-worktree Git
// directory for the service's active project. Unlike PolicyProject, this does
// not collapse linked worktrees to their shared common repository: each
// checkout is an independent writer resource. Callers must authorize the
// active project path before using this host-internal identity.
func (s *Service) WorktreeIdentity(ctx context.Context) (root, gitDir string, err error) {
	r, err := s.active()
	if err != nil {
		return "", "", err
	}
	rootOut, err := r.run(ctx, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", ErrNoRepository
	}
	root = strings.TrimSpace(string(rootOut))
	if !filepath.IsAbs(root) {
		root, err = filepath.Abs(filepath.Join(r.work, root))
		if err != nil {
			return "", "", err
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	gitOut, err := r.run(ctx, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", "", ErrNoRepository
	}
	gitDir = strings.TrimSpace(string(gitOut))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	gitDir, err = filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", "", err
	}
	return filepath.Clean(root), filepath.Clean(gitDir), nil
}

// Project is the open project's path, empty when none is open.
func (s *Service) Project() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.project
}

func (s *Service) active() (runner, error) {
	if !s.availability.OK() {
		return runner{}, ErrGitUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == "" {
		return runner{}, ErrNoProject
	}
	return s.runner, nil
}
