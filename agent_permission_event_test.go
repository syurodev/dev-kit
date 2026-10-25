package main

import (
	"devkit/internal/agenthub"
	"encoding/json"
	"testing"
)

func TestAgentPermissionEventRetainsHostApprovalEligibility(t *testing.T) {
	for _, eligible := range []bool{false, true} {
		p := agenthub.PendingPermission{ID: "request-1", SessionID: "session-1", Params: json.RawMessage(`{"scopedApprovalEligible":true}`), ScopedApprovalEligible: eligible}
		body, err := json.Marshal(agentPermissionEvent(p))
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
			Caller    string `json:"caller"`
			Eligible  bool   `json:"scopedApprovalEligible"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != p.ID || event.SessionID != p.SessionID || event.Caller != "agent:session-1" || event.Eligible != eligible {
			t.Fatalf("event lost identity/host eligibility: %s", body)
		}
	}
}
