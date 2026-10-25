package agenthub

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPermissionWaiterMarksOnlyResolvedDBQueryGateEligible(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tool   string
		locked bool
		want   bool
	}{
		{"db query", "db.query", false, true},
		{"other action", "ssh.runCommand", false, false},
		{"environment block", "db.query", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan PendingPermission, 1)
			w := NewPermissionWaiter(func(p PendingPermission) { seen <- p })
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			params, _ := json.Marshal(map[string]any{"origin": "devkit_gate", "tool": tc.tool, "profileId": "profile-1", "env": "dev", "locked": tc.locked, "approvalFingerprint": "opaque-hash", "targetRevision": "opaque-revision", "envRevision": "policy-revision"})
			go func() { defer close(done); _, _ = w.ResolveGate(ctx, "session-1", params) }()
			pending := <-seen
			if pending.ScopedApprovalEligible != tc.want {
				t.Fatalf("eligible=%v, want %v", pending.ScopedApprovalEligible, tc.want)
			}
			cancel()
			<-done
		})
	}
}
