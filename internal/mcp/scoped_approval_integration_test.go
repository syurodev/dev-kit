package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/bridge"
	"devkit/internal/capabilities"
	"devkit/internal/database"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/permissions"
	"devkit/internal/storage"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type revisionedFakeSSH struct {
	fakeSSH
	revision string
}

func (s *revisionedFakeSSH) ApprovalRevision(id string) (string, error) {
	return id + ":" + s.revision, nil
}

type mutablePolicyEnvs struct {
	fakeEnvs
	revision string
}

func (e *mutablePolicyEnvs) PolicyRevision(name string) (string, error) {
	if e.RequiresConfirmation(name) {
		return "", errors.New("environment blocks unattended approval")
	}
	return name + ":" + e.revision, nil
}

type beforeHandlerCaller struct {
	*fakeCaller
	before func()
}

func (c *beforeHandlerCaller) Call(ctx context.Context, subject, capability, scope string, metadata map[string]any, handler capabilities.Handler) (any, error) {
	if c.before != nil {
		c.before()
	}
	return c.fakeCaller.Call(ctx, subject, capability, scope, metadata, handler)
}

// This goes through the actual Agent Hub gate card, bridge issuer, MCP handler,
// and capability gateway. Only the remote database operation itself is faked.
func TestScopedApprovalUsesLiveAgentHubCardAndMCPGateway(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	container := app.NewContainer()
	hub := interaction.NewHub(interaction.NoopPrompter{})
	// Mirror the desktop host's startup: a persisted or unconfigured vault
	// begins locked, while first-run setup itself unlocks the vault.
	container.SetInteraction(hub)
	appService := bridge.NewAppService(container)
	if _, err := appService.SetupVault("integration-test-master-password"); err != nil {
		t.Fatal(err)
	}

	registry := agenthub.NewRegistry()
	registry.Register(agenthub.NewMockAdapter())
	waiter := agenthub.NewPermissionWaiter(nil)
	waiter.Timeout = 5 * time.Second
	store := agenthub.NewSQLStore(db)
	agents := agenthub.NewService(registry, store, nil)
	agents.ResolveGatePermission = waiter.ResolveGate
	container.AgentHub = agents
	container.AgentHubPermissions = waiter
	hub.ConfirmAgent = agents.ConfirmTool

	session, err := agents.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	card := make(chan agenthub.PendingPermission, 1)
	waiter = agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { card <- p })
	waiter.Timeout = 5 * time.Second
	agents.ResolveGatePermission = waiter.ResolveGate
	container.AgentHubPermissions = waiter

	profileID := "0123456789abcdef01234567"
	dbAPI := &fakeDB{activeID: profileID, profiles: []database.ProfileSummary{{ID: profileID, Name: "primary", Env: "dev"}}, queryOut: database.QueryResult{Columns: []string{"value"}}}
	envs := container.Environments
	srv := NewServer(Deps{
		Caller: bridge.NewCaller(appService),
		Notes:  &fakeNotes{},
		Vault:  &fakeVault{},
		Search: &fakeSearch{},
		Tools:  fakeTools{},
		Envs:   envs,
		SSH:    &fakeSSH{},
		DB:     dbAPI,
		Kafka:  &fakeKafka{},
		Hub:    hub,
	})

	ctx := identity.WithCaller(context.Background(), identity.Agent(session.ID))
	finished := make(chan error, 1)
	go func() {
		_, _, err := srv.toolDBQuery(ctx, nil, dbQueryInput{SQL: "SELECT 1"})
		finished <- err
	}()

	var pending agenthub.PendingPermission
	select {
	case pending = <-card:
	case <-time.After(2 * time.Second):
		t.Fatal("MCP call did not create an Agent Hub permission card")
	}
	if !pending.ScopedApprovalEligible {
		t.Fatalf("live gate card was not eligible: %+v", pending)
	}
	if _, err := appService.AgentHubCreateScopedApproval(pending.ID, 1, 0); err != nil {
		t.Fatalf("issue from live Agent Hub card: %v", err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("MCP query: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MCP query did not finish after the live card was answered")
	}
	if dbAPI.queriedID != profileID {
		t.Fatalf("query profile = %q, want captured target %q", dbAPI.queriedID, profileID)
	}

	// A production-tagged request remains a human-confirmed gate. The desktop
	// issuer refuses its card, and the person can still allow this one call.
	dbAPI.profiles[0].Env = "prod"
	finished = make(chan error, 1)
	go func() {
		_, _, err := srv.toolDBQuery(ctx, nil, dbQueryInput{SQL: "SELECT 2"})
		finished <- err
	}()
	select {
	case pending = <-card:
	case <-time.After(2 * time.Second):
		t.Fatal("production query did not create an Agent Hub permission card")
	}
	if pending.ScopedApprovalEligible {
		t.Fatal("production permission card was eligible for unattended approval")
	}
	if _, err := appService.AgentHubCreateScopedApproval(pending.ID, 1, 0); err == nil {
		t.Fatal("production permission card was converted into a scoped approval")
	}
	if err := waiter.Complete(pending.ID, []byte(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("manually confirmed production query: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual production confirmation did not finish")
	}
}

func TestScopedApprovalIssuerRefusesServerModeAndLockedVault(t *testing.T) {
	newService := func(serverMode func() bool) (*bridge.AppService, *app.Container) {
		t.Helper()
		container := app.NewContainer()
		hub := interaction.NewHub(interaction.NoopPrompter{})
		container.SetInteraction(hub)
		service := bridge.NewAppServiceWithMode(container, nil, serverMode)
		if _, err := service.SetupVault("integration-test-master-password"); err != nil {
			t.Fatal(err)
		}
		return service, container
	}

	serverService, _ := newService(func() bool { return true })
	if _, err := serverService.AgentHubCreateScopedApproval("untrusted", 1, 0); err == nil {
		t.Fatal("server mode issued a scoped approval")
	}

	desktopService, _ := newService(func() bool { return false })
	if err := desktopService.LockVault(); err != nil {
		t.Fatal(err)
	}
	if _, err := desktopService.AgentHubCreateScopedApproval("untrusted", 1, 0); err == nil {
		t.Fatal("locked vault issued a scoped approval")
	}

	deniedService, deniedContainer := newService(func() bool { return false })
	// Replace the gateway after vault setup to exercise the issuer's actual
	// capability check with no matching grant.
	deniedContainer.Capabilities = capabilities.NewGateway(permissions.NewPolicy(nil), deniedContainer.Audit)
	if _, err := deniedService.AgentHubCreateScopedApproval("untrusted", 1, 0); err == nil {
		t.Fatal("issuer passed the gateway without its explicit grant")
	}
	if got := deniedContainer.Interaction.ScopedApprovals(); len(got) != 0 {
		t.Fatalf("denied issuer created approvals: %+v", got)
	}
}

func TestScopedApprovalRechecksEnvironmentDatabaseAndTunnelRevisionsBeforeEffect(t *testing.T) {
	const profileID, sshID, sql = "0123456789abcdef01234567", "abcdef0123456789abcdef01", "SELECT 1"
	for _, mutate := range []struct {
		name string
		edit func(*fakeDB, *revisionedFakeSSH, *mutablePolicyEnvs)
	}{
		{name: "environment generation ABA", edit: func(_ *fakeDB, _ *revisionedFakeSSH, env *mutablePolicyEnvs) { env.revision = "policy-v2" }},
		{name: "environment now requires confirmation", edit: func(_ *fakeDB, _ *revisionedFakeSSH, env *mutablePolicyEnvs) { env.req["dev"] = true }},
		{name: "database profile generation", edit: func(db *fakeDB, _ *revisionedFakeSSH, _ *mutablePolicyEnvs) { db.revision = "db-v2" }},
		{name: "SSH tunnel generation", edit: func(_ *fakeDB, ssh *revisionedFakeSSH, _ *mutablePolicyEnvs) { ssh.revision = "ssh-v2" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			dbAPI := &fakeDB{activeID: profileID, profiles: []database.ProfileSummary{{ID: profileID, Env: "dev", SSHProfileID: sshID}}, revision: "db-v1"}
			sshAPI := &revisionedFakeSSH{revision: "ssh-v1"}
			envs := &mutablePolicyEnvs{fakeEnvs: fakeEnvs{req: map[string]bool{"dev": false}}, revision: "policy-v1"}
			hub := interaction.NewHub(interaction.NoopPrompter{})
			hub.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) {
				t.Fatal("revision mismatch should refuse before asking for a manual fallback")
				return "", interaction.ErrConfirmationDenied
			}
			targetRevision := interaction.Fingerprint([]byte(profileID + ":db-v1:ssh:" + sshID + ":ssh-v1"))
			req := interaction.ConfirmRequest{Tool: "db.query", Kind: interaction.KindDatabase, ProfileID: profileID, Env: "dev", EnvRevision: "dev:policy-v1", ApprovalFingerprint: interaction.Fingerprint([]byte(sql)), TargetRevision: targetRevision}
			if _, err := hub.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err != nil {
				t.Fatal(err)
			}
			caller := &beforeHandlerCaller{fakeCaller: &fakeCaller{}, before: func() { mutate.edit(dbAPI, sshAPI, envs) }}
			srv := NewServer(Deps{Caller: caller, Notes: &fakeNotes{}, Vault: &fakeVault{}, Search: &fakeSearch{}, Tools: fakeTools{}, Envs: envs, SSH: sshAPI, DB: dbAPI, Kafka: &fakeKafka{}, Hub: hub})
			ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
			if _, _, err := srv.toolDBQuery(ctx, &sdk.CallToolRequest{}, dbQueryInput{SQL: sql}); !errors.Is(err, interaction.ErrConfirmationDenied) {
				t.Fatalf("stale approval error = %v, want confirmation denied", err)
			}
			if dbAPI.queriedID != "" {
				t.Fatalf("query ran after revision changed: profile %q", dbAPI.queriedID)
			}
			if approvals := hub.ScopedApprovals(); len(approvals) != 0 {
				t.Fatalf("stale approval survived revalidation: %+v", approvals)
			}
		})
	}
}
