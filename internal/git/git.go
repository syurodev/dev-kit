// Package git is the project's own repository: status, diffs, staging,
// commits, branches and remotes on the real work tree.
//
// It is NOT internal/checkpoint. That package keeps a shadow repository with
// its own GIT_DIR and writes $GIT_DIR/info/attributes to disable the project's
// .gitattributes, so bytes go in and come out untouched. That is right for a
// snapshot and silently wrong for a commit: a repo declaring `* text=auto` or
// `*.bin filter=lfs` would get the wrong bytes written and nobody would notice
// until someone else checked out. Everything here runs in the work tree with
// the repository's own configuration.
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
	"os/exec"
	"strings"
	"sync"

	"devkit/internal/checkpoint"
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
}

// runWithEnv runs git with extra environment, for the credential helper.
func (r runner) runWithEnv(ctx context.Context, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, r.git, append([]string{"--no-optional-locks", "-C", r.work}, args...)...)
	cmd.Env = append(append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r runner) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.git, append([]string{"--no-optional-locks", "-C", r.work}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	// Never let git ask: a prompt in a subprocess nobody is watching is a hang.
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GCM_INTERACTIVE=never",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
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
	runner  runner

	guards  GuardStore
	remotes RemoteStore
	envs    EnvLookup
	creds   CredentialResolver
	picker  AccountPicker
	confirm Confirmer
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
	s.mu.Lock()
	s.project, s.runner = projectPath, r
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
	r := runner{git: s.availability.Path, work: projectPath}
	if _, err := r.run(ctx, nil, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, ErrNoRepository
	}
	return &Service{
		availability: s.availability,
		project:      projectPath,
		runner:       r,
		guards:       s.guards,
		remotes:      s.remotes,
		envs:         s.envs,
		creds:        s.creds,
		picker:       s.picker,
		confirm:      s.confirm,
	}, nil
}

// Reset lets go of the open project. A folder that is not a repository leaves
// Open failing without touching what was open before, which is right for a
// person typing a path and wrong for a module following the work folder: it
// would go on showing the previous repository under the new folder's name.
func (s *Service) Reset() {
	s.mu.Lock()
	s.project, s.runner = "", runner{}
	s.mu.Unlock()
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
