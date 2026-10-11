package app

import (
	"context"
	"database/sql"
	"errors"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/apiclient"
	"devkit/internal/codexsessions"
	gitservice "devkit/internal/git"
	"devkit/internal/gitssh"
	"devkit/internal/interaction"
	"devkit/internal/notify"
	"devkit/internal/tasks"
	"devkit/internal/vault"
)

// Tables names the two local SQL tables this wiring hands to the modules that
// own rows in them.
//
// They are passed in rather than read from internal/storage because app sits
// above the persistence kernel and must not import it (internal/arch pins
// that). Which table a module writes to stays a composition-root decision,
// which is the same reason accounts and tasks take a name rather than a
// package.
type Tables struct {
	AccountSecrets       string
	AccountHostChoices   string
	AccountRemoteChoices string
	AccountIdentities    string
	APIRequestHistory    string
	CodexHookEvents      string
	TaskVerifyRuns       string
	TaskBoardBindings    string
}

// WireCredentialsAndWork upgrades the four modules from the shapes an
// ephemeral container can build to the ones that need a database, a vault and
// the interaction hub. main calls it once the DB is open.
//
// It exists so main.go does not have to know which module needs which
// collaborator, and so the wiring is testable without a Wails application.
func WireCredentialsAndWork(c *Container, db *sql.DB, vaultService *vault.Service, tables Tables) {
	// Accounts, with its local table: without a *sql.DB it can only hold
	// shared accounts, and the scoped ones are exactly the credentials that
	// must not sync.
	c.Accounts = accounts.NewService(
		c.Items, db, tables.AccountSecrets, vaultService,
		accounts.NewRegistry(accounts.DefaultProviders()), c.Environments,
	)
	// Without this the host rule still decides correctly and simply asks every
	// time; with it, once per (host, env).
	c.Accounts.SetHostChoices(db, tables.AccountHostChoices)
	// The git counterpart, keyed by (host, owner): two accounts on one host
	// split by who owns the repository, not by environment.
	c.Accounts.SetRemoteChoices(db, tables.AccountRemoteChoices)
	// Who each account commits as, so a commit under the work account is not
	// written with the personal email.
	c.Accounts.SetIdentities(db, tables.AccountIdentities)

	// The API client resolves its credentials through Accounts and asks the
	// interaction hub before sending on a gated environment. A nil hub is
	// headless, and the service already treats that as a refusal.
	// Without this a send still goes out and records nothing: history is a
	// convenience, not a precondition.
	c.APIClient.SetHistory(db, tables.APIRequestHistory, vaultService)
	// Prompt text and tool input/output are sealed the same way the URL in
	// api_request_history is: DevKit did not write either, and both can carry
	// a secret. Without this the MCP tool that receives Codex's hooks still
	// answers them — see codexsessions.Service's nil methods — it simply
	// records nothing.
	c.CodexSessions = codexsessions.NewService(db, tables.CodexHookEvents, vaultService)
	c.APIClient.SetCredentials(accountResolver{accounts: c.Accounts})
	if c.Interaction != nil {
		c.APIClient.SetConfirmer(hubConfirmer{hub: c.Interaction, kind: interaction.KindAPI})
		c.Git.SetConfirmer(hubConfirmer{hub: c.Interaction, kind: interaction.KindGit})
	}
	c.Git.SetEnvs(c.Environments)
	c.Git.SetCredentials(accountResolver{accounts: c.Accounts})
	// An HTTPS remote with no binding is matched to an account by its host and
	// owner, and the person is asked when two accounts fit.
	c.Git.SetAccountPicker(remotePicker{accounts: c.Accounts, hub: c.Interaction})
	// Without these two the Git module's guards and remote bindings are nil,
	// which silently turns off commit gating and means a push never resolves
	// a credential at all.
	c.Git.SetGuards(c.GitBindings)
	c.Git.SetRemotes(c.GitBindings)

	// The close gate. Without the notifier a run that lands while the user is
	// elsewhere would be silent, which is the worst case for a durable queue.
	gate := tasks.NewGate(db, tables.TaskVerifyRuns, nil, nil)
	if c.Interaction != nil {
		gate = tasks.NewGate(db, tables.TaskVerifyRuns,
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
	// Recover only rewinds an interrupted run to queued; nothing runs it again
	// without this. Without the sweep, a run a quit caught mid-flight would sit
	// there until some unrelated request happened to enqueue against the same
	// project and trigger a drain of its own.
	gate.DrainAll()

	// Accounts refuses to delete one something still binds, which needs each
	// module that binds to answer for itself.
	c.Accounts.RegisterUsage(apiUsage{api: c.APIClient})
	c.Accounts.RegisterUsage(c.GitBindings)
	c.Accounts.RegisterUsage(CodexAPIKey{C: c})

	// One store for every credential DevKit holds. From here a new database,
	// SSH or Kafka profile keeps its metadata in vault_items — where it syncs
	// — and its secret in the local scoped table, where it does not. Profiles
	// written before this keep working on their payload until the migration
	// copies them across.
	if credentials := NewProfileCredentials(c.Accounts); credentials != nil {
		c.Database.SetCredentialStore(credentials)
		c.SSH.SetCredentialStore(credentials)
		c.Kafka.SetCredentialStore(credentials)
	}

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

// AccountLabel names an account for an export's placeholder variable. A
// label is not a secret; an account that cannot be read is simply unnamed.
func (r accountResolver) AccountLabel(id string) string {
	account, err := r.accounts.Get(id)
	if err != nil {
		return ""
	}
	return account.Label
}

func (r accountResolver) ResolveForRemote(_ context.Context, accountID, env string) (string, string, error) {
	user, secret, err := r.resolve(accountID, env, "git")
	if err != nil {
		return "", "", err
	}
	if user == "" {
		// A personal access token is the password half of a pair whose other half
		// the host does not look at, but git asks for it and an empty answer is
		// refused. The login the provider reported is the honest value; "git" is
		// what is left for a token that was never verified.
		user = "git"
		if account, err := r.accounts.Get(accountID); err == nil && account.Subject != "" {
			user = account.Subject
		}
	}
	return user, secret, nil
}

// ResolveSSHKey hands git an SSH-key account's key. An account that is not one
// is "nothing to do", not an error: a token bound to a remote that turned out
// to be SSH is left to git's own configuration.
func (r accountResolver) ResolveSSHKey(_ context.Context, accountID, env string) (gitssh.Material, bool, error) {
	material, err := r.accounts.ResolveSSHKey(accountID, env, "git")
	if errors.Is(err, accounts.ErrNotSSHKey) {
		return gitssh.Material{}, false, nil
	}
	if err != nil {
		return gitssh.Material{}, false, err
	}
	return material, true, nil
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
	// As the session: only boards in its scope are scanned. The session_id
	// match below is what decides; this is scanning the right place rather
	// than the whole vault.
	caller := tasks.Caller{SessionID: sessionID}
	boards, err := t.tasks.Boards(caller)
	if err != nil {
		return nil
	}
	var branches []string
	for _, board := range boards {
		held, err := t.tasks.List(caller, tasks.ListInput{BoardID: board.ID})
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
// It reads Code Graph's own root through a view (Service.At) and never moves
// the Git module: which repository Git has open is the work folder's decision
// (WireWorkFolder), and a person who pointed Git at another repository must
// not find it pulled back the next time Code Graph refreshes its sidebar —
// which reopening here used to do on every IndexInfo. A directory that is not
// a work tree is not an error: Status then reports nothing and the file tree
// simply draws no markers.
type gitWorkTree struct {
	git   *gitservice.Service
	roots projectRoot
}

// projectRoot is the one thing this needs from Code Graph: where the open
// project is.
type projectRoot interface{ ProjectRoot() string }

func (g gitWorkTree) Changes(ctx context.Context) (map[string]string, error) {
	repo := g.git
	if g.roots != nil {
		if root := g.roots.ProjectRoot(); root != "" && root != g.git.Project() {
			view, err := g.git.At(ctx, root)
			if err != nil {
				return map[string]string{}, nil
			}
			repo = view
		}
	}
	status, err := repo.Status(ctx)
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

// WireAgentLiveness connects Agent Hub to the board — both the liveness check
// a claim takeover reads, and (with db and the board-bindings table) which
// boards an agent session may see at all. It is separate from
// WireCredentialsAndWork because main builds the hub later, after the vendor
// registry and the sandbox runner, and both of these need it.
//
// db and bindingsTable are taken as parameters rather than read off the
// Container because nothing on Container holds a raw *sql.DB —
// WireCredentialsAndWork takes the same two, for the same reason (see
// Tables).
func WireAgentLiveness(c *Container, db *sql.DB, bindingsTable string) {
	if c == nil || c.Tasks == nil {
		return
	}
	// A nil AgentHub (an ephemeral container, or a build that never wires one)
	// must reach taskScope as a nil interface, not a typed-nil *agenthub.Service
	// — assigning the latter would make the interface's own nil check lie.
	var hub agentSessions
	if c.AgentHub != nil {
		hub = c.AgentHub
		c.Tasks.SetLiveness(agentLiveness{hub: c.AgentHub})
	}
	if c.Workspace != nil {
		// A nil hub here is not a reason to skip scoping: SessionWorkspaces
		// simply reads every session as unknown, which is scope's own
		// fail-closed answer, not a crash.
		c.Tasks.SetScope(newTaskScope(c.Workspace, hub), db, bindingsTable)
	}
}

// ConfirmGit preserves target metadata for desktop and remote confirmations.
func (c hubConfirmer) ConfirmGit(ctx context.Context, req gitservice.Confirmation) (bool, error) {
	if c.hub == nil {
		return false, nil
	}
	err := c.hub.Confirm(ctx, interaction.ConfirmRequest{Tool: req.Title, Summary: req.Detail, Kind: interaction.KindGit, Env: req.Env, EnvBlocks: true, ProfileID: req.Repo, Target: interaction.Target{Repo: req.Repo, Branch: req.Branch, Remote: req.Remote, Destination: req.Destination, Action: req.Action}})
	if errors.Is(err, interaction.ErrConfirmationDenied) {
		return false, nil
	}
	return err == nil, err
}
