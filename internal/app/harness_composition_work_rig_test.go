package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitservice "devkit/internal/git"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

// workRig is the real Tasks and Git side of the composed graph: the tasks service
// over a real (in-memory keyring) vault with the application's workspace scope
// and Agent Hub liveness, the real Git service with the application's guard and
// remote store, environment policy and hub confirmer, and the production
// TaskBranches adapter. It is built exactly as main.go wires them; nothing here
// decides what an agent may do.
//
// Every repository is a temporary directory and every remote a local bare
// repository, so no push can leave the machine and no credential is read.
type workRig struct {
	t        *testing.T
	tasks    *tasks.Service
	git      *gitservice.Service
	bindings *GitBindings
	board    tasks.Board
	gitBin   string
}

// isolateGit keeps the host's Git configuration (signing, hooks, autocrlf,
// credential helpers, a global identity) out of the fixture without touching it.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
}

// newWorkRig builds the rig with the real Tasks and Git services. The Git binary
// the application locates must exist or the test is skipped, as the Git module's
// own tests do.
func newWorkRig(t *testing.T, opt compOptions) *compRig {
	t.Helper()
	isolateGit(t)
	opt.work = true
	r := newCompRig(t, opt)
	return r
}

// wireWork is called from the rig's build with the dependencies it assembled.
// The tasks and git services survive a rig restart, as the vault and database do.
func (r *compRig) wireWork(deps *mcp.Deps) {
	t := r.t
	t.Helper()
	if r.work == nil {
		w := &workRig{t: t}
		v := vault.NewService(vault.NewMemoryKeyringStore())
		if _, err := v.Setup("correct-horse"); err != nil {
			t.Fatal(err)
		}
		items := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
		w.tasks = tasks.NewService(items)
		w.tasks.SetScope(newTaskScope(r.ws, r.agents), r.db, storage.TableTaskBoardBindings)
		w.tasks.SetLiveness(agentLiveness{hub: r.agents})
		w.bindings = NewGitBindings(items)
		w.git = gitservice.NewService(context.Background())
		if !w.git.Available() {
			t.Skip("no usable Git on this host")
		}
		w.gitBin = w.git.Availability().Path
		// The same wiring WireCredentialsAndWork gives the application's Git module.
		w.git.SetConfirmer(hubConfirmer{hub: r.hub, kind: interaction.KindGit})
		w.git.SetEnvs(NewContainer().Environments)
		w.git.SetGuards(w.bindings)
		w.git.SetRemotes(w.bindings)
		board, err := w.tasks.CreateBoard("Harness", "HAR", "blue", tasks.Workflow{})
		if err != nil {
			t.Fatal(err)
		}
		if board, err = w.tasks.BindBoard(board.ID, r.wsID); err != nil {
			t.Fatal(err)
		}
		w.board = board
		r.work = w
	}
	deps.Tasks = r.work.tasks
	deps.Git = r.work.git
	deps.GitRepos = mcp.GitReposOf(r.work.git)
	if r.opt.wrapGitRepos != nil {
		deps.GitRepos = r.opt.wrapGitRepos(deps.GitRepos)
	}
	deps.TaskBranches = NewTaskBranches(r.work.tasks)
	deps.AgentHub = r.agents
	deps.Workspace = r.ws
}

// run executes git in dir with the fixture identity and fails the test on error.
func (w *workRig) run(dir string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command(w.gitBin, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo turns dir into a work tree on main with one commit and a local bare
// remote named origin whose HEAD is main, and returns the bare path.
func (w *workRig) repo(dir string) (bare string) {
	w.t.Helper()
	w.run(dir, "init", "--initial-branch=main")
	w.run(dir, "config", "user.name", "Fixture")
	w.run(dir, "config", "user.email", "fixture@example.invalid")
	w.run(dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	w.run(dir, "add", "base.txt")
	w.run(dir, "commit", "-m", "base")
	bare = filepath.Join(w.t.TempDir(), "origin.git")
	w.run(w.t.TempDir(), "init", "--bare", "--initial-branch=main", bare)
	w.run(dir, "remote", "add", "origin", bare)
	w.run(dir, "push", "origin", "main")
	w.run(dir, "remote", "set-head", "origin", "main")
	return bare
}

// remoteRefs lists the refs the bare remote holds, sorted, as "name sha".
func (w *workRig) remoteRefs(bare string) string {
	w.t.Helper()
	return w.run(bare, "for-each-ref", "--format=%(refname) %(objectname)")
}

// localState is every local ref and the work tree's status: what a refused
// mutation must leave exactly as it was.
func (w *workRig) localState(dir string) string {
	w.t.Helper()
	return w.run(dir, "for-each-ref", "--format=%(refname) %(objectname)") + "\nHEAD=" +
		w.run(dir, "symbolic-ref", "-q", "--short", "HEAD") + "\n" + w.run(dir, "status", "--porcelain")
}

// guard records a branch guard on the repository at dir, keyed by the repository
// identity the Git module itself uses.
func (r *compRig) guard(dir, pattern, env string) {
	r.t.Helper()
	view, err := r.work.git.At(context.Background(), dir)
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.work.bindings.CreateGuard(view.PolicyProject(), pattern, env); err != nil {
		r.t.Fatal(err)
	}
}

// task creates a task on the workspace's board through the owning service.
func (r *compRig) task(title string) tasks.Task {
	r.t.Helper()
	task, err := r.work.tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: r.work.board.ID, Title: title})
	if err != nil {
		r.t.Fatal(err)
	}
	return task
}

// taskNow reads a task back from the owning service.
func (r *compRig) taskNow(id string) tasks.Task {
	r.t.Helper()
	task, err := r.work.tasks.Get(tasks.Caller{}, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return task
}
