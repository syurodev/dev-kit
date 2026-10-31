package interaction

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/identity"
)

func scopedRequest(sql, revision string) ConfirmRequest {
	return ConfirmRequest{Tool: "db.query", Kind: KindDatabase, ProfileID: "profile-1", Env: "dev", EnvRevision: "env-v1", Summary: sql, ApprovalFingerprint: Fingerprint([]byte(sql)), TargetRevision: revision}
}

func TestScopedApprovalFencesOneUseAndBindsCompleteScope(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { return "allow", nil }
	req := scopedRequest("delete from records", "target-v1")
	approved, err := h.CreateScopedApprovalForAgent("session-1", req, 1, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	used, token, err := h.ConfirmDecision(ctx, req)
	if err != nil || !used {
		t.Fatalf("ConfirmDecision = (%v,%v), want scoped approval", used, err)
	}
	if !h.StartScopedApproval(ctx, req, token) {
		t.Fatal("reserved scope did not start")
	}
	if h.StartScopedApproval(ctx, req, token) {
		t.Fatal("same reservation token started twice")
	}
	if got := h.ScopedApprovals(); len(got) != 0 {
		t.Fatalf("spent approval remains: %#v", got)
	}
	otherCaller := identity.WithCaller(context.Background(), identity.Agent("session-2"))
	used, _, err = h.ConfirmDecision(otherCaller, req)
	if err != nil || used {
		t.Fatalf("different caller used approval: (%v,%v)", used, err)
	}
	used, _, err = h.ConfirmDecision(ctx, scopedRequest("delete from records where id=1", "target-v1"))
	if err != nil || used {
		t.Fatalf("changed SQL used approval: (%v,%v)", used, err)
	}
	_ = approved
}

func TestScopedApprovalRevocationExpiryAndLockAreRecheckedAtStart(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { return "allow", nil }
	req := scopedRequest("select 1", "target-v1")
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	approval, err := h.CreateScopedApprovalForAgent("session-1", req, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	used, token, err := h.ConfirmDecision(ctx, req)
	if err != nil || !used {
		t.Fatalf("ConfirmDecision = (%v,%v)", used, err)
	}
	if err := h.RevokeScopedApproval(approval.ID); err != nil {
		t.Fatal(err)
	}
	if h.StartScopedApproval(ctx, req, token) {
		t.Fatal("revoked reservation started")
	}

	approval, err = h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	used, token, err = h.ConfirmDecision(ctx, req)
	if err != nil || !used {
		t.Fatalf("ConfirmDecision = (%v,%v)", used, err)
	}
	h.SetVaultLocked(true)
	if h.StartScopedApproval(ctx, req, token) {
		t.Fatal("locked-vault reservation started")
	}
	if got := h.ScopedApprovals(); len(got) != 0 {
		t.Fatalf("approval survived lock: %#v", got)
	}
	_ = approval
}

func TestScopedApprovalConsumeChecksVaultOwnerInsideCriticalSection(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { return "allow", nil }
	var ownerLocked atomic.Bool
	h.SetVaultLockCheck(ownerLocked.Load)
	req := scopedRequest("select 1", "target-v1")
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	if _, err := h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	ownerLocked.Store(true)
	matched, _, err := h.ConfirmDecision(ctx, req)
	if err != nil || matched {
		t.Fatalf("locked owner reserved approval=(%v,%v)", matched, err)
	}
	ownerLocked.Store(false)
	matched, token, err := h.ConfirmDecision(ctx, req)
	if err != nil || !matched {
		t.Fatalf("ConfirmDecision=(%v,%v)", matched, err)
	}
	ownerLocked.Store(true) // model Lock's owner boundary before Hub notification
	if h.StartScopedApproval(ctx, req, token) {
		t.Fatal("consume passed after the vault owner entered locking state")
	}
}

func TestScopedApprovalOneUseIsReservedAtomically(t *testing.T) {
	h := NewHub(NoopPrompter{})
	var prompted atomic.Int32
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { prompted.Add(1); return "allow", nil }
	req := scopedRequest("select 1", "target-v1")
	_, err := h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	var wg sync.WaitGroup
	used := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matched, token, err := h.ConfirmDecision(ctx, req)
			if err != nil {
				t.Errorf("ConfirmDecision: %v", err)
			}
			if matched && !h.StartScopedApproval(ctx, req, token) {
				t.Error("reserved concurrent call failed to start")
			}
			used <- matched
		}()
	}
	wg.Wait()
	close(used)
	matches := 0
	for match := range used {
		if match {
			matches++
		}
	}
	if matches != 1 || prompted.Load() != 1 {
		t.Fatalf("matched=%d prompted=%d; want one scoped reservation and one manual prompt", matches, prompted.Load())
	}
}

func TestScopedApprovalNeverOverridesEnvironmentOrUnknownAction(t *testing.T) {
	h := NewHub(NoopPrompter{})
	var prompted int
	h.ConfirmAgent = func(_ context.Context, req ConfirmRequest) (string, error) { prompted++; return "allow", nil }
	req := scopedRequest("select 1", "target-v1")
	if _, err := h.CreateScopedApprovalForAgent("session-1", req, 2, time.Minute); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	blocked := req
	blocked.EnvBlocks = true
	used, _, err := h.ConfirmDecision(ctx, blocked)
	if err != nil || used {
		t.Fatalf("env-blocked decision = (%v,%v)", used, err)
	}
	unknown := req
	unknown.Tool = "db.deleteProfile"
	used, _, err = h.ConfirmDecision(ctx, unknown)
	if err != nil || used {
		t.Fatalf("unknown action decision = (%v,%v)", used, err)
	}
	if prompted != 2 {
		t.Fatalf("prompts=%d, want 2", prompted)
	}
}

func TestScopedApprovalReservationTokensArePerCallAndFenceRevokedScopes(t *testing.T) {
	h := NewHub(NoopPrompter{})
	h.ConfirmAgent = func(context.Context, ConfirmRequest) (string, error) { return "allow", nil }
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	req := scopedRequest("update records set status='ready'", "target-v1")
	approval, err := h.CreateScopedApprovalForAgent("session-1", req, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, tokenA, err := h.ConfirmDecision(ctx, req)
	if err != nil || !first {
		t.Fatalf("first reserve=(%v,%v)", first, err)
	}
	second, tokenB, err := h.ConfirmDecision(ctx, req)
	if err != nil || !second {
		t.Fatalf("second reserve=(%v,%v)", second, err)
	}
	if tokenA == "" || tokenA == tokenB {
		t.Fatal("reservations did not receive distinct fencing tokens")
	}
	h.CancelScopedApproval(ctx, req, tokenA)
	h.CancelScopedApproval(ctx, req, tokenA)
	if !h.StartScopedApproval(ctx, req, tokenB) {
		t.Fatal("canceling token A affected token B")
	}
	if h.StartScopedApproval(ctx, req, tokenB) {
		t.Fatal("token B started twice")
	}
	if got := h.ScopedApprovals(); len(got) != 1 || got[0].Remaining != 1 {
		t.Fatalf("canceling A and starting B should leave one use: %#v", got)
	} else if got[0].LastUsedAt.IsZero() {
		t.Fatal("started approval did not record last-use time")
	}

	left := h.ScopedApprovals()
	if len(left) != 1 || left[0].Remaining != 1 {
		t.Fatalf("expected one remaining approval before replacement: %#v", left)
	}
	if err := h.RevokeScopedApproval(left[0].ID); err != nil {
		t.Fatal(err)
	}
	approval, err = h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, staleToken, err := h.ConfirmDecision(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.RevokeScopedApproval(approval.ID); err != nil {
		t.Fatal(err)
	}
	newApproval, err := h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if newApproval.ID == approval.ID {
		t.Fatal("replacement reused approval ID")
	}
	if h.StartScopedApproval(ctx, req, staleToken) {
		t.Fatal("old token crossed revoke and reissue")
	}
	_, freshToken, err := h.ConfirmDecision(ctx, req)
	if err != nil || freshToken == staleToken {
		t.Fatalf("fresh reservation token=%q, err=%v", freshToken, err)
	}
	if !h.StartScopedApproval(ctx, req, freshToken) {
		t.Fatal("fresh reservation did not start")
	}
}
