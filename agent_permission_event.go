package main

import (
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"time"
)

// agentPermissionEvent is shared by the live notifier and its wire contract test.
// Eligibility is host-owned metadata, never inferred from the vendor's params.
func agentPermissionEvent(p agenthub.PendingPermission) map[string]any {
	return map[string]any{
		"id":                     p.ID,
		"sessionId":              p.SessionID,
		"params":                 rawJSON(p.Params),
		"caller":                 identity.Agent(p.SessionID).String(),
		"scopedApprovalEligible": p.ScopedApprovalEligible,
		// The UI shows the deadline; it must not invent one. A countdown
		// started when a toast mounts is wrong whenever the toast mounts
		// late, which is the normal case for a request that arrives while
		// the rail is closed.
		"createdAt": p.CreatedAt.Format(time.RFC3339Nano),
		"expiresAt": p.ExpiresAt.Format(time.RFC3339Nano),
	}
}
