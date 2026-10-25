package app

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vaultitem"
	"devkit/internal/workspace"
)

func managedTeamFixture(t *testing.T) (*Container, *managedTeamHost, *storage.ManagedLedger, *sql.DB, workspace.Workspace, tasks.Board, tasks.Task, tasks.Task, sessionHub) {
	t.Helper()
	c := NewContainer()
	if _, err := c.Vault.Setup("managed-team-test"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	c.Workspace = workspace.NewService(workspace.NewSQLRepository(db))
	ws, _, err := c.Workspace.AddWorkspace("Managed", root)
	if err != nil {
		t.Fatal(err)
	}
	extraFolder := t.TempDir()
	ws, _, err = c.Workspace.UpdateWorkspace(ws.ID, "Managed", []string{root, extraFolder})
	if err != nil {
		t.Fatal(err)
	}
	c.WorkFolder.SetCurrent(root)
	c.Items = vaultitem.NewStore(vaultitem.NewSQLMutationRepository(db), c.Vault)
	c.Tasks = tasks.NewService(c.Items)
	hub := sessionHub{
		"coordinator": {ID: "coordinator", WorkspaceID: ws.ID, ProjectPath: root, ExtraReadWritePaths: []string{extraFolder}, Status: agenthub.SessionStatusIdle},
		"runner":      {ID: "runner", WorkspaceID: ws.ID, ProjectPath: root, Status: agenthub.SessionStatusIdle},
	}
	c.Tasks.SetScope(newTaskScope(c.Workspace, hub), db, storage.TableTaskBoardBindings)
	board, err := c.Tasks.CreateBoard("Managed", "MNG", "", tasks.Workflow{})
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	if _, err := c.Tasks.BindBoard(board.ID, ws.ID); err != nil {
		t.Fatalf("BindBoard workspace=%q board=%q: %v", ws.ID, board.ID, err)
	}
	parent, err := c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: board.ID, Title: "Parent", Description: "Coordinate child work"})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if _, err := c.Tasks.Claim(tasks.Caller{SessionID: "coordinator"}, parent.ID); err != nil {
		t.Fatal(err)
	}
	child, err := c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: board.ID, ParentID: parent.ID, Title: "Child", Description: "Implement the bounded change", Labels: []string{"backend"}})
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewManagedLedger(db)
	host := &managedTeamHost{container: c, hubOverride: hub}
	service := agentwork.NewManagedService(host, store)
	c.ManagedTeams = service
	policy := &managedTerminalPolicy{service: service, tasks: c.Tasks}
	c.managedTerminalPolicy = policy
	c.Tasks.SetTerminalPolicy(policy)
	return c, host, store, db, ws, board, parent, child, hub
}

func managedTeamContext() context.Context {
	return identity.WithCaller(context.Background(), identity.UI())
}

func managedStatuses() agentwork.StatusMap {
	return agentwork.StatusMap{Ready: "todo", Running: "in_progress", AwaitingReview: "review", Accepted: "done"}
}

func TestManagedTeamHostUsesBoundBoardVaultAndWorkspaceFolders(t *testing.T) {
	c, host, _, _, ws, board, parent, child, _ := managedTeamFixture(t)
	if _, err := host.ResolveManagedParent(managedTeamContext(), parent.ID); err != nil {
		t.Fatalf("ResolveManagedParent: %v", err)
	}
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if group.Enabled || !group.Paused {
		t.Fatalf("new group policy = enabled:%v paused:%v, want disabled and paused", group.Enabled, group.Paused)
	}
	item, err := c.ManagedTeams.TrackWorkItem(managedTeamContext(), group.ID, child.ID, "implementer")
	if err != nil {
		t.Fatalf("TrackWorkItem: %v", err)
	}
	if item.WorkspaceID != ws.ID || item.BoardID != board.ID || item.ParentTaskID != parent.ID || item.TaskID != child.ID {
		t.Fatalf("tracked item scope = %+v", item)
	}
	if item.RequirementsRevision == "" || item.TaskRevision < 1 {
		t.Fatalf("tracked revisions = requirements %q task %d", item.RequirementsRevision, item.TaskRevision)
	}
	snapshot, err := host.LoadManagedTask(managedTeamContext(), group, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	services, err := c.Workspace.ListServices(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Requirements.Scope) != 2 {
		t.Fatalf("coordinator folder scope = %v, want project and one registered grant", snapshot.Requirements.Scope)
	}
	if !strings.Contains(snapshot.Requirements.Scope[0], "|project|"+filepath.Clean(ws.Root)) && !strings.Contains(snapshot.Requirements.Scope[1], "|project|"+filepath.Clean(ws.Root)) {
		t.Fatalf("scope omits project identity/path: %v", snapshot.Requirements.Scope)
	}
	grantedID := services[1].ID
	if !strings.Contains(snapshot.Requirements.Scope[0], grantedID+"|grant|") && !strings.Contains(snapshot.Requirements.Scope[1], grantedID+"|grant|") {
		t.Fatalf("scope omits registered grant identity: %v", snapshot.Requirements.Scope)
	}
	if _, err := c.Tasks.Update(tasks.Caller{}, child.ID, "Retitled child", "Implement the bounded change"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.Transition(context.Background(), tasks.Caller{}, tasks.TransitionInput{TaskID: child.ID, To: "review"}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := c.ManagedTeams.RefreshWorkItem(managedTeamContext(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.RequirementsRevision != item.RequirementsRevision || refreshed.TaskRevision != item.TaskRevision {
		t.Fatalf("title/status edits changed requirements revision: before=%+v after=%+v", item, refreshed)
	}
	if _, err := c.Tasks.Update(tasks.Caller{}, child.ID, "Retitled child", "Changed acceptance requirements"); err != nil {
		t.Fatal(err)
	}
	changed, err := c.ManagedTeams.RefreshWorkItem(managedTeamContext(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.RequirementsRevision == item.RequirementsRevision || changed.TaskRevision != item.TaskRevision+1 {
		t.Fatalf("description edit did not advance exactly the requirements revision: before=%+v after=%+v", item, changed)
	}
	contains, err := c.ManagedTeams.ManagedTaskMembership(context.Background(), board.ID, child.ID)
	if err != nil || !contains {
		t.Fatalf("managed membership = %v, %v", contains, err)
	}
	c.Vault.Lock()
	if _, err := host.LoadManagedTask(managedTeamContext(), group, child.ID); !errors.Is(err, agentwork.ErrManagedHostUnavailable) {
		t.Fatalf("locked-vault task read = %v, want fail closed", err)
	}
	if _, err := c.ManagedTeams.GetGroup(managedTeamContext(), group.ID); err != nil {
		t.Fatalf("locked-vault metadata read = %v, want scoped metadata access", err)
	}
	if _, err := c.ManagedTeams.ListGroups(managedTeamContext(), "", 10); err != nil {
		t.Fatalf("locked-vault group list = %v, want scoped metadata access", err)
	}
}

func TestManagedTaskClosureGuardSurvivesBoardRebind(t *testing.T) {
	c, _, _, db, wsA, board, parent, child, _ := managedTeamFixture(t)
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ManagedTeams.TrackWorkItem(managedTeamContext(), group.ID, child.ID, "implementer"); err != nil {
		t.Fatal(err)
	}
	wsB, _, err := c.Workspace.AddWorkspace("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.BindBoard(board.ID, wsB.ID); err != nil {
		t.Fatal(err)
	}
	c.Tasks.SetScope(newTaskScope(c.Workspace, sessionHub{
		"coordinator": {ID: "coordinator", WorkspaceID: wsA.ID, ProjectPath: wsA.Root},
		"runner":      {ID: "runner", WorkspaceID: wsB.ID, ProjectPath: wsB.Root},
	}), db, storage.TableTaskBoardBindings)
	if _, err := c.Tasks.Transition(context.Background(), tasks.Caller{SessionID: "runner"}, tasks.TransitionInput{TaskID: child.ID, To: "done"}); !errors.Is(err, tasks.ErrManagedTerminalNotReady) {
		t.Fatalf("closure after board rebind = %v, want managed review refusal", err)
	}
}

func TestManagedGroupRejectsCoordinatorDrift(t *testing.T) {
	c, _, _, _, _, _, parent, _, _ := managedTeamFixture(t)
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.Release(tasks.Caller{}, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.Claim(tasks.Caller{SessionID: "runner"}, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ManagedTeams.ReconcileGroup(managedTeamContext(), group.ID); !errors.Is(err, agentwork.ErrManagedTaskInvalid) {
		t.Fatalf("reconcile after coordinator drift = %v, want fail closed", err)
	}
}

func TestManagedGroupReconcilesDeletedParentFromStoredScope(t *testing.T) {
	c, _, _, _, _, _, parent, _, _ := managedTeamFixture(t)
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Tasks.Delete(context.Background(), tasks.Caller{}, parent.ID); err != nil {
		t.Fatalf("delete parent task: %v", err)
	}
	ended, err := c.ManagedTeams.ReconcileGroup(managedTeamContext(), group.ID)
	if err != nil {
		t.Fatalf("reconcile deleted parent: %v", err)
	}
	if ended.EndedAt == nil || ended.TombstoneReason != agentwork.TombstoneParentDeleted {
		t.Fatalf("reconciled group = %+v, want a parent-deleted tombstone", ended)
	}
}

func TestManagedTeamEnableNeedsInteractiveExactGuardAndAgentsStillCannotClose(t *testing.T) {
	c, _, _, _, _, _, parent, child, _ := managedTeamFixture(t)
	if _, err := c.ManagedTeams.ListGroups(context.Background(), "", 10); !errors.Is(err, errManagedHumanRequired) {
		t.Fatalf("unidentified group list = %v, want human identity refusal", err)
	}
	if _, err := c.ManagedTeams.ListGroups(identity.WithCaller(context.Background(), identity.Agent("runner")), "", 10); !errors.Is(err, errManagedHumanRequired) {
		t.Fatalf("agent group list = %v, want human identity refusal", err)
	}
	if _, err := c.ManagedTeams.CreateGroup(context.Background(), parent.ID, managedStatuses()); !errors.Is(err, errManagedHumanRequired) {
		t.Fatalf("unidentified CreateGroup = %v, want human identity refusal", err)
	}
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	c.Interaction = interaction.NewHub(nil)
	c.Interaction.Headless = true
	if _, err := c.ManagedTeams.ConfigureGroup(managedTeamContext(), group.ID, group.Revision, group.PolicyRevision+1, managedStatuses(), true, false); !errors.Is(err, agentwork.ErrManagedIneligible) {
		t.Fatalf("headless enable = %v, want fail closed", err)
	}
	c.Interaction.Headless = false
	enabled, err := c.ManagedTeams.ConfigureGroup(managedTeamContext(), group.ID, group.Revision, group.PolicyRevision+1, managedStatuses(), true, false)
	if err != nil || !enabled.Enabled || enabled.Paused {
		t.Fatalf("trusted UI enable = %+v, %v", enabled, err)
	}
	tracked, err := c.ManagedTeams.TrackWorkItem(managedTeamContext(), group.ID, child.ID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.Transition(identity.WithCaller(context.Background(), identity.Agent("runner")), tasks.Caller{SessionID: "runner"}, tasks.TransitionInput{TaskID: parent.ID, To: "done"}); !errors.Is(err, tasks.ErrManagedTerminalNotReady) {
		t.Fatalf("managed parent closure = %v, want managed review refusal", err)
	}
	if _, err := c.Tasks.Transition(identity.WithCaller(context.Background(), identity.Agent("runner")), tasks.Caller{SessionID: "runner"}, tasks.TransitionInput{TaskID: child.ID, To: "done"}); !errors.Is(err, tasks.ErrManagedTerminalNotReady) {
		t.Fatalf("enabled managed task closure = %v, want managed review refusal", err)
	}
	if _, err := c.Tasks.Transition(context.Background(), tasks.Caller{}, tasks.TransitionInput{TaskID: child.ID, To: "done"}); err != nil {
		t.Fatalf("human terminal transition = %v", err)
	}
	reconciled, err := c.ManagedTeams.ReconcileWorkItem(managedTeamContext(), tracked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State == "accepted" {
		t.Fatalf("human task terminal status inferred managed acceptance: %+v", reconciled)
	}
	ended, err := c.ManagedTeams.EndGroup(managedTeamContext(), group.ID, enabled.Revision, agentwork.TombstoneUserEnded)
	if err != nil || ended.EndedAt == nil {
		t.Fatalf("end group = %+v, %v", ended, err)
	}
	if _, err := c.Tasks.Transition(identity.WithCaller(context.Background(), identity.Agent("runner")), tasks.Caller{SessionID: "runner"}, tasks.TransitionInput{TaskID: parent.ID, To: "done"}); !errors.Is(err, tasks.ErrManagedTerminalNotReady) {
		t.Fatalf("managed parent closure after group end = %v, want history-based refusal", err)
	}
}
