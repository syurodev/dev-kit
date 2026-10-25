package interaction

import (
	"context"
	"testing"
	"time"

	"devkit/internal/identity"
)

func TestScopedApprovalExpiresAfterReservationBeforeStart(t *testing.T) {
	h := NewHub(NoopPrompter{})
	req := ConfirmRequest{
		Tool: "db.query", Kind: KindDatabase, ProfileID: "profile-1", Env: "dev",
		EnvRevision: "env-v1", ApprovalFingerprint: Fingerprint([]byte("select 1")), TargetRevision: "target-v1",
	}
	if _, err := h.CreateScopedApprovalForAgent("session-1", req, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent("session-1"))
	approved, token, err := h.ConfirmDecision(ctx, req)
	if err != nil || !approved || token == "" {
		t.Fatalf("reserve = (%v, %q, %v)", approved, token, err)
	}

	// Move the expiry under the Hub lock to deterministically exercise the
	// interval after a use is reserved but before the external effect starts.
	h.mu.Lock()
	for _, record := range h.approvals {
		record.ExpiresAt = time.Now().Add(-time.Second)
	}
	h.mu.Unlock()
	if h.StartScopedApproval(ctx, req, token) {
		t.Fatal("expired reserved approval started an effect")
	}
	if approvals := h.ScopedApprovals(); len(approvals) != 0 {
		t.Fatalf("expired approval remains visible: %+v", approvals)
	}
}
