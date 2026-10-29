package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/git"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

func TestWireManagedOwnershipInstallsHarnessBindingWithoutMain(t *testing.T) {
	c, _, ledger, db, _, _, _, _, _ := managedTeamFixture(t)
	c.AgentHub = agenthub.NewService(agenthub.NewRegistry(), agenthub.NewSQLStore(db), nil)
	t.Cleanup(func() { _ = c.AgentHub.Shutdown(context.Background()) })
	if c.ManagedHarness != nil {
		t.Fatal("the service must not exist before wiring")
	}
	WireManagedOwnership(c, ledger)
	if c.ManagedHarness == nil || c.ManagedLeases == nil || c.ManagedPayloads == nil {
		t.Fatalf("wiring installed leases=%v payloads=%v harness=%v", c.ManagedLeases != nil, c.ManagedPayloads != nil, c.ManagedHarness != nil)
	}
	// Agent Hub has no harness attached (production today): the preflight is the
	// fixed "not configured" refusal, and nothing is written.
	_, err := c.ManagedHarness.PreflightIntended(context.Background(), "no-attempt", agentwork.IntendedChild{Vendor: "claude", Role: "implementer"})
	if !errors.Is(err, agentwork.ErrManagedRecordNotFound) {
		t.Fatalf("an unknown attempt is not found: %v", err)
	}
	// The composition root is not part of this change.
	raw, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"ManagedHarness", "NewManagedHarnessService", "managedHarnessHost"} {
		if strings.Contains(string(raw), word) {
			t.Errorf("main.go mentions %q: the binding is wired by WireManagedOwnership alone", word)
		}
	}
}

// harnessStubHost is a harness host that always resolves the same configuration,
// so the service's effect on the ledger can be seen without a harness graph.
type harnessStubHost struct{ res agentwork.HarnessResolution }

func (h harnessStubHost) RequiredGroups(context.Context, string) ([]string, error) {
	return []string{"agenthub", "git", "tasks"}, nil
}
func (h harnessStubHost) ResolveIntended(context.Context, agentwork.PackageScope, agentwork.IntendedChild) (agentwork.HarnessResolution, error) {
	return h.res, nil
}
func (h harnessStubHost) ResolveActual(context.Context, agentwork.Attempt) (agentwork.HarnessResolution, error) {
	return h.res, nil
}

func TestManagedHarnessPreservesPackageLeaseAndSeal(t *testing.T) {
	c, _, ledger, db, ws, _, parent, child, baseHub := managedTeamFixture(t)
	initTestGitRepository(t, ws.Root)
	c.Git = git.NewService(context.Background())
	c.Interaction = interaction.NewHub(nil)
	ctx := context.Background()
	group, err := c.ManagedTeams.CreateGroup(managedTeamContext(), parent.ID, managedStatuses())
	if err != nil {
		t.Fatal(err)
	}
	if group, err = c.ManagedTeams.ConfigureGroup(managedTeamContext(), group.ID, group.Revision, group.PolicyRevision+1, managedStatuses(), true, false); err != nil {
		t.Fatal(err)
	}
	item, err := c.ManagedTeams.TrackWorkItem(managedTeamContext(), group.ID, child.ID, "implementer")
	if err != nil {
		t.Fatal(err)
	}
	coordinatorID := group.CoordinatorID
	runner := baseHub["runner"]
	runner.ParentSessionID = &coordinatorID
	runner.ProjectPath = ws.Root
	runner.WorkspaceID = ws.ID
	proving := &provingHub{sessionHub: sessionHub{"coordinator": baseHub[coordinatorID], "runner": runner}}
	host := &managedOwnershipHost{container: c, hubOverride: proving, store: ledger}

	item, err = ledger.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.BeginAttempt(ctx, agentwork.Attempt{ID: "attempt-harness", WorkItemID: item.ID, SessionID: runner.ID,
		Role: item.Role, TaskRevision: item.TaskRevision, RequirementsRevision: item.RequirementsRevision}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	payloads := agentwork.NewManagedPayloadService(ledger, host, managedVaultPayloadCodec{vault: c.Vault})
	pkg, err := payloads.BuildContextPackage(identity.WithCaller(ctx, identity.UI()), attempt.ID)
	if err != nil {
		t.Fatalf("build package: %v", err)
	}

	facts := agentwork.HarnessFacts{SourceSchema: agentwork.HarnessSourceSchemaSupported, Vendor: "claude", WorkspaceID: ws.ID, VendorRevision: 1, Fingerprint: "fp", ExecutableDigest: "ex"}
	svc := agentwork.NewManagedHarnessService(ledger, harnessStubHost{res: agentwork.HarnessResolution{Facts: facts, Groups: []string{"agenthub", "git", "tasks"}}})
	intended, err := svc.BindIntended(ctx, pkg.ID, agentwork.IntendedChild{Vendor: "claude", Role: "implementer"}, attempt.Revision)
	if err != nil {
		t.Fatalf("bind intended: %v", err)
	}
	attempt, err = ledger.TransitionAttempt(ctx, attempt.ID, attempt.Revision, "session_created")
	if err != nil {
		t.Fatal(err)
	}
	leases := agentwork.NewManagedLeaseService(host, ledger)
	if _, err = leases.Acquire(ctx, attempt.ID, attempt.Revision); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	attempt, err = ledger.GetManagedAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := func(table string) string {
		t.Helper()
		rows, err := db.Query(`SELECT * FROM ` + table + ` ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		var out strings.Builder
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				out.WriteString(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toText(v), "\n", " "), "\r", " ")) + "|")
			}
			out.WriteString("\n")
		}
		return out.String()
	}
	packageBefore, leaseBefore := snapshot("agent_work_packages"), snapshot("agent_work_leases")
	leaseRow, err := leases.CurrentAttemptLease(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	sealsBefore, endsBefore := proving.seals, proving.ends

	if _, err = svc.BindActual(ctx, attempt.ID, attempt.Revision); err != nil {
		t.Fatalf("bind actual: %v", err)
	}
	if err = svc.VerifyForEnqueue(ctx, attempt.ID); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if st, err := svc.CheckBinding(ctx, attempt.ID); err != nil || st.State != agentwork.HarnessStateCurrent {
		t.Fatalf("check: %+v %v", st, err)
	}
	if got := snapshot("agent_work_packages"); got != packageBefore {
		t.Fatalf("the sealed package row changed:\n%s\n%s", packageBefore, got)
	}
	if got := snapshot("agent_work_leases"); got != leaseBefore {
		t.Fatalf("the lease row changed:\n%s\n%s", leaseBefore, got)
	}
	after, err := leases.CurrentAttemptLease(ctx, attempt.ID)
	if err != nil || after.Generation != leaseRow.Generation || after.Revision != leaseRow.Revision || after.ReleasedAt != nil {
		t.Fatalf("lease = %+v (was %+v), %v", after, leaseRow, err)
	}
	if proving.seals != sealsBefore || proving.ends != endsBefore {
		t.Fatalf("a binding call took the writer admission barrier: seals %d->%d", sealsBefore, proving.seals)
	}
	if err = leases.AuthorizeCurrentWriter(identity.WithCaller(ctx, identity.Agent(runner.ID)), runner.ID); err != nil {
		t.Fatalf("the leased writer must still pass its fence: %v", err)
	}
	stored, err := ledger.GetContextPackage(ctx, pkg.ID)
	if err != nil || stored.BodyHash != pkg.BodyHash || stored.SchemaVersion != agentwork.ManagedPackageSchemaVersion {
		t.Fatalf("the package no longer reads as built: %v", err)
	}
	if got, err := ledger.GetHarnessBinding(ctx, attempt.ID, agentwork.HarnessPhaseIntended); err != nil || got.ID != intended.ID {
		t.Fatalf("intended binding changed: %v", err)
	}
}

func toText(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}

func TestNoNewPublicSurfaceForHarnessBinding(t *testing.T) {
	// The binding is internal to the app package and the storage/agentwork layers.
	// No bridge file (AppService methods, error tables), no MCP tool source and no
	// frontend binding may name it or one of its refusal codes.
	words := []string{
		"ManagedHarness", "HarnessBinding", "managedHarness", "harness_binding_", "harness_not_configured", "harness_unavailable",
		"harness_schema_unsupported", "harness_override_present", "harness_required_group_off", "harness_config_changed",
		"harness_endpoint_not_live", "harness_child_", "team.configurationNeeded",
	}
	for _, dir := range []string{"../bridge", "../mcp", "../capabilities", "../../frontend/bindings"} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || strings.HasSuffix(path, "_test.go") {
				return err
			}
			if ext := filepath.Ext(path); ext != ".go" && ext != ".ts" && ext != ".js" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, word := range words {
				if strings.Contains(string(raw), word) {
					t.Errorf("%s mentions %q: the harness binding has no public surface in C1", path, word)
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	// Within this package only the adapter, the wiring and the container name it.
	allowed := map[string]bool{"managed_harness.go": true, "managed_ownership.go": true, "container.go": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range []string{"ManagedHarness", "managedHarnessHost", "HarnessBinding"} {
			if strings.Contains(string(raw), word) {
				t.Errorf("%s mentions %q: only the adapter, wiring and container may", f, word)
			}
		}
	}
}
