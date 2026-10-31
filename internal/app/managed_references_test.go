package app

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agentwork"
	"devkit/internal/interaction"
	"devkit/internal/tasks"
)

type referenceFixture struct {
	c          *Container
	host       *managedOwnershipHost
	group      agentwork.GroupState
	dependent  agentwork.WorkItem // tracked task that is blocked by dep
	dep        tasks.Task
	depItem    agentwork.WorkItem
	outside    tasks.Task // a board task the group does not track
	scope      agentwork.PackageScope
	parentTask tasks.Task
}

func newReferenceFixture(t *testing.T) *referenceFixture {
	t.Helper()
	c, _, ledger, _, ws, board, parent, child, hub := managedTeamFixture(t)
	c.SetInteraction(interaction.NewHub(nil))
	ctx := managedTeamContext()
	dep, err := c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: board.ID, ParentID: parent.ID, Title: "Dep", Description: "Produce the dependency"})
	if err != nil {
		t.Fatal(err)
	}
	outside, err := c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: board.ID, ParentID: parent.ID, Title: "Outside", Description: "Not tracked by the group"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := []string{dep.ID, outside.ID}
	if _, err = c.Tasks.SetFields(tasks.Caller{}, child.ID, tasks.Fields{BlockedBy: &blocked}); err != nil {
		t.Fatalf("set blockedBy: %v", err)
	}
	group, err := c.ManagedTeams.CreateGroup(ctx, parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	group, err = c.ManagedTeams.ConfigureGroup(ctx, group.ID, group.Revision, group.PolicyRevision+1, managedStatuses(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	depItem, err := c.ManagedTeams.TrackWorkItem(ctx, group.ID, dep.ID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	item, err := c.ManagedTeams.TrackWorkItem(ctx, group.ID, child.ID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	item, err = ledger.GetWorkItem(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	runner := hub["runner"]
	runner.ProjectPath = ws.Root
	hub["runner"] = runner
	attempt, err := ledger.BeginAttempt(context.Background(), agentwork.Attempt{
		ID: "attempt-refs", WorkItemID: item.ID, SessionID: "runner", Role: item.Role,
		TaskRevision: item.TaskRevision, RequirementsRevision: item.RequirementsRevision,
	}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	host := &managedOwnershipHost{container: c, hubOverride: hub, store: ledger}
	return &referenceFixture{
		c: c, host: host, group: group, dependent: item, dep: dep, depItem: depItem, outside: outside, parentTask: parent,
		scope: agentwork.PackageScope{
			WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, ParentTaskID: group.ParentTaskID, GroupID: group.ID,
			WorkItemID: item.ID, TaskID: child.ID, AttemptID: attempt.ID, Generation: attempt.Generation,
			RequirementsRevision: item.RequirementsRevision,
		},
	}
}

func TestPackageSourceSelectsOnlyTrackedDependenciesBoundToTheirRequirementsDigest(t *testing.T) {
	f := newReferenceFixture(t)
	source, err := f.host.BuildPackageSource(managedTeamContext(), f.scope)
	if err != nil {
		t.Fatalf("BuildPackageSource: %v", err)
	}
	if len(source.References) != 1 {
		t.Fatalf("references = %+v, want only the tracked dependency (the untracked blocker is board ordering, not an input)", source.References)
	}
	ref := source.References[0]
	depReqs, err := f.host.requirementsOf(managedTeamContext(), f.group, f.dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentwork.RequirementsDigest(depReqs)
	if err != nil {
		t.Fatal(err)
	}
	want := agentwork.PackageReference{Kind: agentwork.ManagedReferenceTask, RecordID: f.dep.ID, Revision: digest, WorkspaceID: f.group.WorkspaceID, GroupID: f.group.ID}
	if ref != want {
		t.Fatalf("reference = %+v, want %+v", ref, want)
	}
	// Task text, comments and history never ride along: only the id and a digest.
	if ref.Digest != "" {
		t.Fatalf("task reference carries a body hash: %+v", ref)
	}
}

func TestResolvePackageReferenceClassifiesCurrentStaleMissingAndForeign(t *testing.T) {
	f := newReferenceFixture(t)
	ctx := managedTeamContext()
	source, err := f.host.BuildPackageSource(ctx, f.scope)
	if err != nil || len(source.References) != 1 {
		t.Fatalf("source = %+v, %v", source, err)
	}
	ref := source.References[0]
	got, err := f.host.ResolvePackageReference(ctx, f.scope, ref)
	if err != nil || got != ref {
		t.Fatalf("current reference = %+v, %v", got, err)
	}

	// The dependency's requirements change: the same id now resolves to another
	// digest, so comparing with the stored reference says stale.
	if _, err = f.c.Tasks.Update(tasks.Caller{}, f.dep.ID, "Dep", "Produce something different"); err != nil {
		t.Fatal(err)
	}
	got, err = f.host.ResolvePackageReference(ctx, f.scope, ref)
	if err != nil || got == ref || got.RecordID != ref.RecordID || got.Revision == ref.Revision {
		t.Fatalf("after the dependency changed = %+v, %v", got, err)
	}

	// A task that is not one of this task's blockers, however real, is foreign.
	foreign := agentwork.PackageReference{Kind: agentwork.ManagedReferenceTask, RecordID: f.parentTask.ID, Revision: "sha256:v2:x", WorkspaceID: f.group.WorkspaceID, GroupID: f.group.ID}
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, foreign); !errors.Is(err, agentwork.ErrManagedReferenceForeign) {
		t.Fatalf("non-blocker = %v", err)
	}
	// A task the group tracks but that does not block this one is no input either.
	sibling, err := f.c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: f.group.BoardID, ParentID: f.group.ParentTaskID, Title: "Sibling", Description: "Tracked, unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.ManagedTeams.TrackWorkItem(ctx, f.group.ID, sibling.ID, "implementer"); err != nil {
		t.Fatal(err)
	}
	trackedUnrelated := foreign
	trackedUnrelated.RecordID = sibling.ID
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, trackedUnrelated); !errors.Is(err, agentwork.ErrManagedReferenceForeign) {
		t.Fatalf("tracked non-blocker = %v", err)
	}
	// A real blocker the group does not track is also not an input.
	untracked := foreign
	untracked.RecordID = f.outside.ID
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, untracked); !errors.Is(err, agentwork.ErrManagedReferenceForeign) {
		t.Fatalf("untracked blocker = %v", err)
	}
	// Another workspace or group is foreign before anything is read.
	other := ref
	other.WorkspaceID = "someone-else"
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, other); !errors.Is(err, agentwork.ErrManagedReferenceForeign) {
		t.Fatalf("other workspace = %v", err)
	}
	other = ref
	other.GroupID = "another-group"
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, other); !errors.Is(err, agentwork.ErrManagedReferenceForeign) {
		t.Fatalf("other group = %v", err)
	}
	// Unknown kinds and documents fail closed.
	for _, kind := range []string{agentwork.ManagedReferenceDocument, "memory"} {
		bad := ref
		bad.Kind = kind
		if _, err = f.host.ResolvePackageReference(ctx, f.scope, bad); !errors.Is(err, agentwork.ErrManagedReferenceUnsupported) {
			t.Fatalf("kind %q = %v", kind, err)
		}
	}
	// An artifact id nobody wrote is missing, never an empty success.
	missing := agentwork.PackageReference{Kind: agentwork.ManagedReferenceArtifact, RecordID: "no-such-artifact", Revision: "v1.g1", Digest: "sha256:00", WorkspaceID: f.group.WorkspaceID, GroupID: f.group.ID}
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, missing); !errors.Is(err, agentwork.ErrManagedReferenceMissing) {
		t.Fatalf("unknown artifact = %v", err)
	}
}

func TestResolvePackageReferenceFailsClosedWhileTheVaultIsLocked(t *testing.T) {
	f := newReferenceFixture(t)
	ctx := managedTeamContext()
	source, err := f.host.BuildPackageSource(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	f.c.Vault.Lock()
	if _, err = f.host.ResolvePackageReference(ctx, f.scope, source.References[0]); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked vault = %v", err)
	}
	if _, err = f.host.BuildPackageSource(ctx, f.scope); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked vault build = %v", err)
	}
}

func TestPackageSourceFailsWhenATrackedDependencyTaskIsGone(t *testing.T) {
	f := newReferenceFixture(t)
	ctx := managedTeamContext()
	if err := f.c.Tasks.Delete(ctx, tasks.Caller{}, f.dep.ID); err != nil {
		t.Fatalf("delete dependency: %v", err)
	}
	if _, err := f.host.BuildPackageSource(ctx, f.scope); err == nil {
		t.Fatal("a tracked dependency that vanished was silently dropped from the package")
	}
}
