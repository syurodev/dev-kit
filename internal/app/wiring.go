package app

import (
	"context"
	"database/sql"
	"errors"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/apiclient"
	gitservice "devkit/internal/git"
	"devkit/internal/interaction"
	"devkit/internal/notify"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vault"
)

// WireCredentialsAndWork upgrades the four modules from the shapes an
// ephemeral container can build to the ones that need a database, a vault and
// the interaction hub. main calls it once the DB is open.
//
// It exists so main.go does not have to know which module needs which
// collaborator, and so the wiring is testable without a Wails application.
func WireCredentialsAndWork(c *Container, db *sql.DB, vaultService *vault.Service) {
	// Accounts, with its local table: without a *sql.DB it can only hold
	// shared accounts, and the scoped ones are exactly the credentials that
	// must not sync.
	c.Accounts = accounts.NewService(
		c.Items, db, storage.TableAccountSecrets, vaultService,
		accounts.NewRegistry(accounts.DefaultProviders()), c.Environments,
	)

	// The API client resolves its credentials through Accounts and asks the
	// interaction hub before sending on a gated environment. A nil hub is
	// headless, and the service already treats that as a refusal.
	c.APIClient.SetCredentials(accountResolver{accounts: c.Accounts})
	if c.Interaction != nil {
		c.APIClient.SetConfirmer(hubConfirmer{hub: c.Interaction, kind: interaction.KindAPI})
		c.Git.SetConfirmer(hubConfirmer{hub: c.Interaction, kind: interaction.KindGit})
	}
	c.Git.SetEnvs(c.Environments)
	c.Git.SetCredentials(accountResolver{accounts: c.Accounts})
	// Without these two the Git module's guards and remote bindings are nil,
	// which silently turns off commit gating and means a push never resolves
	// a credential at all.
	c.Git.SetGuards(c.GitBindings)
	c.Git.SetRemotes(c.GitBindings)

	// The close gate. Without the notifier a run that lands while the user is
	// elsewhere would be silent, which is the worst case for a durable queue.
	gate := tasks.NewGate(db, storage.TableTaskVerifyRuns, nil, nil)
	if c.Interaction != nil {
		gate = tasks.NewGate(db, storage.TableTaskVerifyRuns,
			hubConfirmer{hub: c.Interaction, kind: interaction.KindTask},
			taskNotifier{notifier: c.AgentNotifications})
	}
	c.Tasks.SetGate(gate)
	// A run interrupted by a quit comes back as queued, never as a result.
	if err := gate.Recover(); err != nil {
		// A queue that cannot be recovered is not a reason to refuse to start:
		// the tasks it held simply stay where they were, which is the safe end.
		c.recoverErr = err
	}

	// Accounts refuses to delete one something still binds, which needs each
	// module that binds to answer for itself.
	c.Accounts.RegisterUsage(apiUsage{api: c.APIClient})
	c.Accounts.RegisterUsage(c.GitBindings)

	// One answer to "what changed": Code Graph's file-tree markers now read
	// the Git module rather than running a second `git status` of their own.
	if c.CodeGraph != nil {
		c.CodeGraph.SetWorkTreeStatus(gitWorkTree{git: c.Git, roots: c.CodeGraph})
	}
}

// accountResolver adapts Accounts to the narrow interfaces git and apiclient
// declare, so neither imports the credential store.
type accountResolver struct{ accounts *accounts.Service }

func (r accountResolver) ResolveForRequest(_ context.Context, accountID, env string) (string, string, error) {
	return r.resolve(accountID, env, "apiclient")
}

func (r accountResolver) ResolveForRemote(_ context.Context, accountID, env string) (string, string, error) {
	return r.resolve(accountID, env, "git")
}

func (r accountResolver) resolve(accountID, env, by string) (string, string, error) {
	credential, err := r.accounts.Resolve(accounts.ResolveInput{
		AccountID: accountID, Env: env, By: by,
	})
	if err != nil {
		return "", "", err
	}
	return credential.User, credential.Secret, nil
}

// hubConfirmer adapts the interaction hub to the two-string Confirmer that
// git, apiclient and tasks each declare locally.
//
// EnvBlocks is always true here, and that is the whole rule: every caller of
// this only asks BECAUSE an environment or a guard demanded it, so the hub
// must neither consult an allow_always grant nor offer one. Env wins, every
// time — the same rule the MCP confirmation policy already keeps.
type hubConfirmer struct {
	hub  *interaction.Hub
	kind interaction.Kind
}

func (c hubConfirmer) Confirm(ctx context.Context, title, detail string) (bool, error) {
	if c.hub == nil {
		// Headless. A gate nobody can answer is a refusal, not a pass.
		return false, nil
	}
	err := c.hub.Confirm(ctx, interaction.ConfirmRequest{
		Tool:      title,
		Summary:   detail,
		Kind:      c.kind,
		EnvBlocks: true,
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, interaction.ErrConfirmationDenied) {
		return false, nil
	}
	// Anything else — no prompter, a timeout — is the fail-closed path, and
	// it carries its reason so the caller can say which it was.
	return false, err
}

// taskNotifier adapts notify.Notifier to the Notifier tasks declares, which
// takes strings so the board never sees a notify.Target.
type taskNotifier struct{ notifier *notify.Notifier }

func (n taskNotifier) Notify(module, kind, id, title, body string) {
	n.notifier.Notify(notify.Target{Module: module, Kind: kind, ID: id}, title, body)
}

// apiUsage lets Accounts say which requests bind an account before letting it
// be deleted.
type apiUsage struct{ api *apiclient.Service }

func (u apiUsage) UsedBy(accountID string) []string {
	requests, err := u.api.Requests()
	if err != nil {
		return nil
	}
	var out []string
	for _, request := range requests {
		if request.Auth.AccountID == accountID {
			out = append(out, "API request "+request.Name)
		}
	}
	return out
}

// TaskBranches answers which branches an agent session may push, by reading
// the tasks it holds. A list in the session dies with it; this does not.
type TaskBranches struct{ tasks *tasks.Service }

// NewTaskBranches builds the push-permission source for MCP.
func NewTaskBranches(service *tasks.Service) *TaskBranches {
	return &TaskBranches{tasks: service}
}

func (t *TaskBranches) BranchesForSession(sessionID string) []string {
	if t == nil || t.tasks == nil || sessionID == "" {
		return nil
	}
	boards, err := t.tasks.Boards()
	if err != nil {
		return nil
	}
	var branches []string
	for _, board := range boards {
		held, err := t.tasks.List(tasks.ListInput{BoardID: board.ID})
		if err != nil {
			continue
		}
		for _, task := range held {
			if task.SessionID == sessionID && task.Branch != "" {
				branches = append(branches, task.Branch)
			}
		}
	}
	return branches
}

// gitWorkTree adapts the Git module to the status source codegraph declares,
// so there is one answer to "what changed" rather than two git processes
// disagreeing about the same work tree.
//
// It also keeps the two pointed at the same repository. Opening a project is
// Code Graph's event, and rather than adding a callback for the Git module to
// subscribe to — a second thing to remember on every path that opens a
// project — this notices the root has changed and follows it. A directory that
// is not a work tree is not an error: Status then reports nothing and the file
// tree simply draws no markers.
type gitWorkTree struct {
	git   *gitservice.Service
	roots projectRoot
}

// projectRoot is the one thing this needs from Code Graph: where the open
// project is.
type projectRoot interface{ ProjectRoot() string }

func (g gitWorkTree) Changes(ctx context.Context) (map[string]string, error) {
	if g.roots != nil {
		if root := g.roots.ProjectRoot(); root != "" && root != g.git.Project() {
			if err := g.git.Open(ctx, root); err != nil {
				return map[string]string{}, nil
			}
		}
	}
	status, err := g.git.Status(ctx)
	if err != nil {
		return nil, err
	}
	return status.Files, nil
}

// agentLiveness answers whether an agent session is still running, which is
// what lets a claim held by a dead one be taken over.
//
// Without it the tasks service sees a nil liveness and refuses every takeover,
// so a task claimed by a session that has since died stays claimed forever.
// That is the safe direction to fail — refusing beats stealing work from a
// live agent — but it is not the behaviour the design asked for.
type agentLiveness struct{ hub agentSessions }

// agentSessions is the slice of *agenthub.Service used here.
type agentSessions interface {
	GetStatus(ctx context.Context, id string) (agenthub.AgentSession, error)
}

func (l agentLiveness) Alive(sessionID string) bool {
	if l.hub == nil || sessionID == "" {
		return false
	}
	session, err := l.hub.GetStatus(context.Background(), sessionID)
	if err != nil {
		// A session the hub cannot find is gone. Reading that as "alive" would
		// make its claim permanent, which is the failure this exists to fix.
		return false
	}
	switch session.Status {
	case agenthub.SessionStatusClosed, agenthub.SessionStatusError:
		return false
	default:
		return true
	}
}

// WireAgentLiveness connects Agent Hub to the board. It is separate from
// WireCredentialsAndWork because main builds the hub later, after the vendor
// registry and the sandbox runner.
func WireAgentLiveness(c *Container) {
	if c == nil || c.Tasks == nil || c.AgentHub == nil {
		return
	}
	c.Tasks.SetLiveness(agentLiveness{hub: c.AgentHub})
}
