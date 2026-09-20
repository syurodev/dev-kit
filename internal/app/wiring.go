package app

import (
	"context"
	"database/sql"
	"errors"

	"devkit/internal/accounts"
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

// GitProjectOpener points the Git module at whatever project Code Graph
// opened, so the two never disagree about which repository is in front.
func GitProjectOpener(c *Container) func(ctx context.Context, projectPath string) {
	return func(ctx context.Context, projectPath string) {
		if c.Git == nil || projectPath == "" {
			return
		}
		// A project that is not a work tree is not an error here: the module
		// shows a panel saying so and everything else carries on.
		_ = c.Git.Open(ctx, projectPath)
	}
}

var _ = gitservice.ErrNoRepository
