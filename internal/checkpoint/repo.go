// internal/checkpoint/repo.go
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The shadow repository.
//
// A checkpoint is a git tree of the project, written by a git that is not the
// user's: its git dir lives under the app's data directory and the project is
// only ever its work tree. Nothing here writes to the project's own .git, index
// or HEAD — `git status` in the user's terminal must look exactly the same
// before and after a snapshot, which is the whole reason this is a separate
// repository instead of a stash or a temporary branch.
//
// Everything a project can configure that would change bytes on the way in or
// out — autocrlf, clean/smudge filters, LFS, working-tree-encoding, ident
// expansion — is turned off here, because a checkpoint has to restore the file
// the agent wrote, not a re-derived version of it.

var (
	// ErrUnavailable is returned when no usable git was found.
	ErrUnavailable = errors.New("checkpoint: git is not available")
	// ErrProjectTooLarge is returned when a snapshot would have to index more
	// files than Limits.MaxFiles.
	ErrProjectTooLarge = errors.New("checkpoint: project is too large to checkpoint")
)

// Limits are the knobs Settings exposes plus the engine's own guard rails.
type Limits struct {
	// MaxFileBytes is the per-file ceiling; bigger files are skipped, and a
	// tracked file that grows past it is dropped from the tree so the tree
	// never claims to hold a version it does not.
	MaxFileBytes int64
	// MaxFiles bounds one snapshot's candidate list. The first snapshot of a
	// project enumerates everything, so this is what says "too large".
	MaxFiles int
	// ExtraExcludes are gitignore-syntax patterns from Settings, applied with
	// the same top priority as the built-in deny list.
	ExtraExcludes []string
	// CommandTimeout bounds one git invocation when the caller's context has no
	// earlier deadline.
	CommandTimeout time.Duration
	// RetentionDays, MaxPerSession and MaxProjectBytes are maintenance's.
	RetentionDays   int
	MaxPerSession   int
	MaxProjectBytes int64
}

// DefaultLimits are the numbers the plan settled on.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:    10 << 20,
		MaxFiles:        50000,
		CommandTimeout:  60 * time.Second,
		RetentionDays:   30,
		MaxPerSession:   200,
		MaxProjectBytes: 1 << 30,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = d.MaxFiles
	}
	if l.CommandTimeout <= 0 {
		l.CommandTimeout = d.CommandTimeout
	}
	if l.RetentionDays <= 0 {
		l.RetentionDays = d.RetentionDays
	}
	if l.MaxPerSession <= 0 {
		l.MaxPerSession = d.MaxPerSession
	}
	if l.MaxProjectBytes <= 0 {
		l.MaxProjectBytes = d.MaxProjectBytes
	}
	return l
}

// Engine owns the shadow repositories under one root directory.
type Engine struct {
	git      Availability
	root     string
	limits   Limits
	activity *Activity

	mu    sync.Mutex
	repos map[string]*Repo
}

// New returns an engine rooted at dir (conventionally <dataDir>/checkpoints).
// An engine with an unavailable git is still constructed: callers ask
// Available() and show the reason rather than branching on nil.
func New(dir string, git Availability, limits Limits) *Engine {
	return &Engine{
		git:      git,
		root:     dir,
		limits:   limits.withDefaults(),
		activity: NewActivity(),
		repos:    map[string]*Repo{},
	}
}

func (e *Engine) Available() bool            { return e.git.OK() }
func (e *Engine) Availability() Availability { return e.git }
func (e *Engine) Limits() Limits             { return e.limits }
func (e *Engine) Activity() *Activity        { return e.activity }
func (e *Engine) Root() string               { return e.root }

// Open returns the shadow repository for a project, creating it on first use.
//
// The returned *Repo is shared: its mutex is what serialises snapshots and
// restores for one project, however many sessions are pointed at it.
func (e *Engine) Open(ctx context.Context, projectPath string) (*Repo, error) {
	if !e.git.OK() {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, e.git.Reason)
	}
	key, work, err := ProjectKey(projectPath)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	repo, ok := e.repos[key]
	if !ok {
		repo = &Repo{engine: e, key: key, work: work, gitDir: filepath.Join(e.root, key), lock: make(chan struct{}, 1)}
		e.repos[key] = repo
	}
	e.mu.Unlock()

	if err := repo.acquire(ctx); err != nil {
		return nil, err
	}
	defer repo.release()
	if err := repo.init(ctx); err != nil {
		return nil, err
	}
	return repo, nil
}

// ProjectKey is the stable name of a project's shadow repository: the hash of
// the path with symlinks resolved, so /tmp and /private/tmp on macOS — or two
// sessions that spelled the same project differently — land on one repository
// rather than two that would each miss the other's writes.
func ProjectKey(projectPath string) (key, resolved string, err error) {
	if projectPath == "" {
		return "", "", errors.New("checkpoint: empty project path")
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return "", "", err
	}
	resolved = abs
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		resolved = real
	}
	sum := sha256.Sum256([]byte(resolved))
	return hex.EncodeToString(sum[:16]), resolved, nil
}

// Repo is one project's shadow repository.
type Repo struct {
	engine *Engine
	key    string
	work   string
	gitDir string

	// lock is a semaphore rather than a sync.Mutex because it has to be
	// acquirable with a deadline. A turn's opening snapshot has five seconds
	// before the prompt goes without it; if another session in the same project
	// is mid-snapshot, a sync.Mutex would hold this one past that budget with
	// no way to give up, and the turn would wait on git after all.
	lock  chan struct{}
	ready bool
}

func (r *Repo) Key() string      { return r.key }
func (r *Repo) GitDir() string   { return r.gitDir }
func (r *Repo) WorkTree() string { return r.work }
func (r *Repo) Limits() Limits   { return r.engine.limits }

// projectFileName records which project a shadow directory belongs to, so a
// human staring at <dataDir>/checkpoints can tell, and so maintenance can
// report a directory it is about to remove.
const projectFileName = "devkit-project"

// init creates the repository once. Callers hold r.mu.
func (r *Repo) init(ctx context.Context) error {
	if r.ready {
		return nil
	}
	if info, err := os.Stat(r.work); err != nil || !info.IsDir() {
		return fmt.Errorf("checkpoint: project %s is not a directory", r.work)
	}
	if err := os.MkdirAll(r.engine.root, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(r.gitDir, "objects")); err != nil {
		// --template= keeps the machine's init templates (hooks above all) out
		// of a repository the app runs git in unattended. `init` is the one
		// command that cannot take --git-dir/--work-tree: it is creating the
		// git dir, and git rejects a work tree for a repository that does not
		// exist yet.
		if _, err := r.gitBare(ctx, "init", "--bare", "--template=", r.gitDir); err != nil {
			return err
		}
	}
	if err := os.Chmod(r.gitDir, 0o700); err != nil {
		return err
	}
	// A bare repository refuses a work tree; this one is bare only in that it
	// has no work tree of its own.
	for _, kv := range r.config() {
		if _, err := r.git(ctx, nil, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	infoDir := filepath.Join(r.gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	// $GIT_DIR/info/attributes outranks any .gitattributes in the project, which
	// is the point: a project that declares `* text=auto eol=crlf` or
	// `*.bin filter=lfs` must not get those applied on the way into a tree or
	// back out of it. `-diff` is deliberately NOT here — it would make every
	// file binary to `diff-tree --numstat`, and the per-file +/- counts are what
	// the turn header shows. Diffing does not change bytes, so it costs nothing
	// to leave on.
	attributes := "* -text -eol -filter -ident -working-tree-encoding\n"
	if err := os.WriteFile(filepath.Join(infoDir, "attributes"), []byte(attributes), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.gitDir, projectFileName), []byte(r.work+"\n"), 0o600); err != nil {
		return err
	}
	r.ready = true
	return nil
}

func (r *Repo) config() [][2]string {
	out := [][2]string{
		{"core.bare", "false"},
		// Bytes in, bytes out.
		{"core.autocrlf", "false"},
		{"core.symlinks", "true"},
		{"core.filemode", "true"},
		// Watching a tree the app does not own is somebody else's cache to get
		// wrong; both of these are stale-state machines.
		{"core.fsmonitor", "false"},
		{"core.untrackedCache", "false"},
		// Maintenance decides when to gc, under the project mutex. An
		// opportunistic gc inside a snapshot would add seconds to a turn.
		{"gc.auto", "0"},
		{"gc.reflogExpire", "never"},
		{"gc.reflogExpireUnreachable", "never"},
	}
	if runtime.GOOS == "darwin" {
		// HFS+/APFS hand back decomposed names; without this a file named "é"
		// round-trips through the index under a different name each snapshot.
		out = append(out, [2]string{"core.precomposeunicode", "true"})
	}
	return out
}

// git runs one git command against this repository.
//
// Every invocation is explicit about --git-dir and --work-tree and runs with a
// scrubbed environment, so nothing the app inherited — a GIT_DIR exported by
// the terminal the app was launched from, a GIT_INDEX_FILE left by a hook —
// can redirect it at the user's own repository.
func (r *Repo) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return r.gitWithEnv(ctx, nil, stdin, args...)
}

// gitBare runs git without pointing it at this repository, for `init`.
func (r *Repo) gitBare(ctx context.Context, args ...string) ([]byte, error) {
	return r.exec(ctx, nil, nil, args)
}

func (r *Repo) gitWithEnv(ctx context.Context, extraEnv []string, stdin []byte, args ...string) ([]byte, error) {
	return r.exec(ctx, extraEnv, stdin, append([]string{"--git-dir=" + r.gitDir, "--work-tree=" + r.work}, args...))
}

func (r *Repo) exec(ctx context.Context, extraEnv []string, stdin []byte, argv []string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.engine.limits.CommandTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, r.engine.git.Path, argv...)
	cmd.Dir = r.work
	cmd.Env = append(cleanGitEnv(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// CommandContext kills on ctx done; WaitDelay stops a child that ignored
	// the kill (or left a pipe open) from holding the caller forever.
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("checkpoint: git %s: %w", verbOf(argv), ctxErr)
		}
		return nil, fmt.Errorf("checkpoint: git %s: %w: %s", verbOf(argv), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// verbOf names the subcommand in an argv for an error message, stepping over
// the --git-dir/--work-tree prefix.
func verbOf(argv []string) string {
	for _, arg := range argv {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return "git"
}

// cleanGitEnv drops every GIT_* the app inherited and puts back only what this
// repository needs.
func cleanGitEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+6)
	for _, kv := range src {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		// The user's ~/.gitconfig and /etc/gitconfig are theirs; none of their
		// settings should reach a tree the app writes.
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ATTR_NOSYSTEM=1",
		// Nothing here is interactive: a credential or passphrase prompt would
		// be a hang with no UI attached to it.
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_OPTIONAL_LOCKS=0",
		// Machine-readable output, whatever the user's locale.
		"LC_ALL=C",
	)
}

// acquire takes the project lock, or gives up when the context does.
func (r *Repo) acquire(ctx context.Context) error {
	select {
	case r.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("checkpoint: waiting for the %s lock: %w", filepath.Base(r.work), ctx.Err())
	}
}

func (r *Repo) release() { <-r.lock }

// runLocked runs fn with the project lock held. Snapshots and restores for one
// project are serialised: two `update-index` runs against one index file would
// interleave, and a restore reading the tree a snapshot is still writing would
// see half of it.
func (r *Repo) runLocked(ctx context.Context, fn func() error) error {
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer r.release()
	return fn()
}
