package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"devkit/internal/agentwork"
	"devkit/internal/git"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

func TestManagedOwnershipHostResolvesRealTaskScopeAndPinsUnknownWriter(t *testing.T) {
	c, _, ledger, _, ws, _, parent, child, baseHub := managedTeamFixture(t)
	initTestGitRepository(t, ws.Root)
	c.Git = git.NewService(context.Background())
	c.Interaction = interaction.NewHub(nil)
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	group, err = c.ManagedTeams.ConfigureGroup(managedTeamContext(), group.ID, group.Revision, group.PolicyRevision+1, managedStatuses(), true, false)
	if err != nil {
		t.Fatalf("enable group: %v", err)
	}
	item, err := c.ManagedTeams.TrackWorkItem(managedTeamContext(), group.ID, child.ID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	parentSessionID := group.CoordinatorID
	runner := baseHub["runner"]
	runner.ParentSessionID = &parentSessionID
	runner.ProjectPath = ws.Root
	runner.WorkspaceID = ws.ID
	hub := sessionHub{"coordinator": baseHub[group.CoordinatorID], "runner": runner}
	if hub["coordinator"].ID == "" {
		t.Fatal("coordinator session missing from fixture")
	}
	item, err = ledger.GetWorkItem(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.BeginAttempt(context.Background(), agentwork.Attempt{
		ID: "attempt-ownership", WorkItemID: item.ID, SessionID: runner.ID,
		Role: item.Role, TaskRevision: item.TaskRevision, RequirementsRevision: item.RequirementsRevision,
	}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err = ledger.TransitionAttempt(context.Background(), attempt.ID, attempt.Revision, "session_created")
	if err != nil {
		t.Fatal(err)
	}
	host := &managedOwnershipHost{container: c, hubOverride: hub, store: ledger}
	binding, err := host.ResolveManagedWriter(context.Background(), attempt)
	if err != nil {
		t.Fatalf("ResolveManagedWriter: %v", err)
	}
	if binding.WorkspaceID != ws.ID || binding.BoardID != parent.BoardID || binding.TaskID != child.ID || binding.SessionID != runner.ID || binding.ResourceKey == "" {
		t.Fatalf("resolved binding = %+v", binding)
	}
	resolution, err := host.ResolveCurrentWriter(context.Background(), runner.ID)
	if err != nil || !resolution.Managed || resolution.Binding != binding {
		t.Fatalf("ResolveCurrentWriter = %+v, %v", resolution, err)
	}

	leases := agentwork.NewManagedLeaseService(host, ledger)
	c.ManagedLeases = leases
	if err := leases.AuthorizeCurrentWriter(identity.WithCaller(context.Background(), identity.Agent(runner.ID)), runner.ID); err == nil {
		t.Fatal("writer without a lease passed the current-writer fence")
	}
	if _, err := leases.Acquire(context.Background(), attempt.ID, attempt.Revision); err != nil {
		t.Fatalf("Acquire lease: %v", err)
	}
	if err := leases.AuthorizeCurrentWriter(identity.WithCaller(context.Background(), identity.Agent(runner.ID)), runner.ID); err != nil {
		t.Fatalf("leased writer denied: %v", err)
	}
	if _, err := leases.Release(context.Background(), "", 0, 0); err == nil {
		t.Fatal("release without a real lease unexpectedly succeeded")
	}
	if _, err := host.CheckManagedWriterQuiescence(context.Background(), agentwork.ManagedWriterLease{Binding: binding, Generation: 1}); err == nil {
		t.Fatal("unknown host liveness was treated as quiescent")
	}
}

func TestManagedWriterResourceIdentityIsGlobalAndRejectsBroadGrants(t *testing.T) {
	root := t.TempDir()
	keyA := managedWriterResourceKey(root, filepath.Join(root, ".git"))
	// Workspace and folder IDs are deliberately absent from the resource key:
	// two board registrations of the same physical checkout must conflict.
	keyB := managedWriterResourceKey(filepath.Clean(root), filepath.Join(root, ".git"))
	if keyA != keyB {
		t.Fatalf("same checkout produced different resource keys: %q != %q", keyA, keyB)
	}
	linkedKey := managedWriterResourceKey(root+"-linked", filepath.Join(root, "linked.git"))
	if linkedKey == keyA {
		t.Fatal("linked worktree reused the same resource key")
	}
	inside := filepath.Join(root, "subdir")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := managedWriterGrantsContained([]string{inside}, root); err != nil {
		t.Fatalf("contained grant rejected: %v", err)
	}
	if err := managedWriterGrantsContained([]string{t.TempDir()}, root); err == nil {
		t.Fatal("grant to a second worktree was accepted")
	}
}

func TestManagedFolderContainmentUsesCanonicalSessionAndGitRoot(t *testing.T) {
	allowed := t.TempDir()
	worktree := filepath.Join(allowed, "repo")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(worktree, alias); err != nil {
		t.Fatal(err)
	}
	scope := []string{"folder-registered-id|project|" + allowed}
	if got := managedFolderForCanonicalPaths(alias, worktree, scope); got != "folder-registered-id" {
		t.Fatalf("canonical alias folder = %q, want preserved registered ID", got)
	}
	if got := managedFolderForCanonicalPaths(worktree, alias, scope); got != "folder-registered-id" {
		t.Fatalf("canonical Git-root alias folder = %q, want preserved registered ID", got)
	}

	outside := t.TempDir()
	escape := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	if got := managedFolderForCanonicalPaths(escape, outside, scope); got != "" {
		t.Fatalf("symlink escape resolved to registered folder %q", got)
	}

	narrow := filepath.Join(worktree, "subtree")
	if err := os.Mkdir(narrow, 0o755); err != nil {
		t.Fatal(err)
	}
	narrowScope := []string{"narrow-folder|project|" + narrow}
	if got := managedFolderForCanonicalPaths(narrow, worktree, narrowScope); got != "" {
		t.Fatalf("Git root outside narrow registered scope resolved to folder %q", got)
	}
}

func TestManagedOwnershipHostNilHubFailsClosed(t *testing.T) {
	host := &managedOwnershipHost{container: NewContainer()}
	if hub := host.hub(); hub != nil {
		t.Fatalf("nil AgentHub was wrapped as a nonnil interface: %T", hub)
	}
	if _, err := host.ResolveManagedWriter(context.Background(), agentwork.Attempt{}); err == nil {
		t.Fatal("managed writer resolved without AgentHub")
	}
}

func TestManagedPackageRoleContractsUseCanonicalClaimOutcomes(t *testing.T) {
	for _, role := range []string{"coordinator", "implementer", "reviewer", "integrator"} {
		contract, output, err := managedRoleContract(role)
		if err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
		if contract == "" || len(output.AllowedOutcomes) != 3 || output.AllowedOutcomes[0] != "ready_for_review" || output.AllowedOutcomes[1] != "blocked" || output.AllowedOutcomes[2] != "failed" {
			t.Fatalf("role %q contract/outcomes = %q / %v", role, contract, output.AllowedOutcomes)
		}
		if len(output.EvidenceKinds) != 4 || output.EvidenceKinds[0] != "file" || output.EvidenceKinds[1] != "patch" || output.EvidenceKinds[2] != "test_output" || output.EvidenceKinds[3] != "other" {
			t.Fatalf("role %q evidence kinds = %v", role, output.EvidenceKinds)
		}
	}
}

func initTestGitRepository(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(root+"/README.md", []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "test")
}
