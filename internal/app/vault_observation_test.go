package app

import (
	"context"
	"testing"
	"time"

	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/vault"
)

func TestVaultInteractionInvalidationIsAppOwnedWithoutBridge(t *testing.T) {
	c := NewContainer()
	hub := interaction.NewHub(interaction.NoopPrompter{})
	hub.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) { return "allow", nil }
	c.SetInteraction(hub)
	recovery, err := c.Vault.Setup("master-password")
	if err != nil {
		t.Fatal(err)
	}
	c.Vault.Lock()
	if err := c.Vault.UnlockWithRecovery(recovery); err != nil {
		t.Fatal(err)
	}

	req := interaction.ConfirmRequest{Tool: "db.query", Kind: interaction.KindDatabase, ProfileID: "profile-1", Env: "dev", EnvRevision: "env-v1", Summary: "select 1", ApprovalFingerprint: interaction.Fingerprint([]byte("select 1")), TargetRevision: "target-v1"}
	if _, err := hub.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	matched, token, err := hub.ConfirmDecision(ctx, req)
	if err != nil || !matched {
		t.Fatalf("ConfirmDecision=(%v,%v)", matched, err)
	}
	replacement := interaction.NewHub(interaction.NoopPrompter{})
	c.SetInteraction(replacement)
	if hub.StartScopedApproval(ctx, req, token) {
		t.Fatal("detached hub retained a usable approval")
	}
	replacement.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) { return "allow", nil }
	if _, err := replacement.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	matched, token, err = replacement.ConfirmDecision(ctx, req)
	if err != nil || !matched {
		t.Fatalf("replacement ConfirmDecision=(%v,%v)", matched, err)
	}
	guardEntered, releaseGuard := make(chan struct{}), make(chan struct{})
	removeGuard := c.Vault.RegisterLockGuard(func(snapshot vault.Snapshot) {
		if snapshot.State == vault.StateLocked {
			close(guardEntered)
			<-releaseGuard
		}
	})
	lockDone := make(chan struct{})
	go func() { c.Vault.Lock(); close(lockDone) }()
	select {
	case <-guardEntered:
	case <-time.After(time.Second):
		t.Fatal("lock guard did not start")
	}
	// Simulate a delayed interaction notification. The owner check must refuse
	// consumption as soon as Vault.Lock marks its synchronous boundary.
	replacement.SetVaultLocked(false)
	if replacement.StartScopedApproval(ctx, req, token) {
		t.Fatal("consume passed while owner lock was in progress")
	}
	close(releaseGuard)
	select {
	case <-lockDone:
	case <-time.After(time.Second):
		t.Fatal("vault lock did not complete")
	}
	removeGuard()
	if replacement.StartScopedApproval(ctx, req, token) {
		t.Fatal("scoped approval consumed after vault lock returned")
	}
	if got := replacement.ScopedApprovals(); len(got) != 0 {
		t.Fatalf("approvals survived owner lock: %#v", got)
	}
}

func TestInteractionWithoutVaultStartsLocked(t *testing.T) {
	c := &Container{}
	hub := interaction.NewHub(interaction.NoopPrompter{})
	c.SetInteraction(hub)
	req := interaction.ConfirmRequest{Tool: "db.query", Kind: interaction.KindDatabase, ProfileID: "profile-1", Env: "dev", EnvRevision: "env-v1", Summary: "select 1", ApprovalFingerprint: interaction.Fingerprint([]byte("select 1")), TargetRevision: "target-v1"}
	if _, err := hub.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err == nil {
		t.Fatal("interaction without a vault accepted scoped approval")
	}
}

func TestInteractionReplacementWaitsForInFlightVaultCallback(t *testing.T) {
	c := NewContainer()
	oldHub := interaction.NewHub(interaction.NoopPrompter{})
	c.SetInteraction(oldHub)
	if _, err := c.Vault.Setup("master-password"); err != nil {
		t.Fatal(err)
	}
	c.Vault.Lock()
	entered, release := make(chan struct{}), make(chan struct{})
	remove := c.Vault.RegisterLockGuard(func(snapshot vault.Snapshot) {
		if snapshot.State == vault.StateUnlocked {
			close(entered)
			<-release
		}
	})
	unlockDone := make(chan error, 1)
	go func() { unlockDone <- c.Vault.Unlock("master-password") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("unlock guard did not start")
	}
	replacement := interaction.NewHub(interaction.NoopPrompter{})
	replaceStarted, replaceDone := make(chan struct{}), make(chan struct{})
	go func() { close(replaceStarted); c.SetInteraction(replacement); close(replaceDone) }()
	<-replaceStarted
	select {
	case <-replaceDone:
		t.Fatal("replacement crossed an in-flight owner callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-unlockDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("unlock remained blocked")
	}
	select {
	case <-replaceDone:
	case <-time.After(time.Second):
		t.Fatal("replacement remained blocked")
	}
	remove()
	req := interaction.ConfirmRequest{Tool: "db.query", Kind: interaction.KindDatabase, ProfileID: "profile-1", Env: "dev", EnvRevision: "env-v1", Summary: "select 1", ApprovalFingerprint: interaction.Fingerprint([]byte("select 1")), TargetRevision: "target-v1"}
	if _, err := oldHub.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err == nil {
		t.Fatal("detached hub was re-enabled by an in-flight unlock callback")
	}
}
