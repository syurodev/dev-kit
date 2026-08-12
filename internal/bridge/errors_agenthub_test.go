package bridge

import (
	"testing"

	"devkit/internal/agenthub"
)

func TestPublicErrorMapsAgentHubUnavailable(t *testing.T) {
	err := publicError("builtin.agenthub", errAgentHubUnavailable)
	if err == nil || err.Error() != "internal_error: Agent Hub is not available." {
		t.Fatalf("public error = %v", err)
	}
}

func TestPublicErrorMapsAgentHubSentinels(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{agenthub.ErrSessionNotFound, "validation_failed: Agent session not found."},
		{agenthub.ErrSessionBusy, "validation_failed: Agent session is busy."},
		{agenthub.ErrDepthLimitExceeded, "validation_failed: Agent session depth limit exceeded."},
		{agenthub.ErrCycleDetected, "validation_failed: Agent session cycle detected."},
		{agenthub.ErrVendorUnknown, "validation_failed: Unknown agent vendor."},
		{agenthub.ErrPermissionDenied, "permission_denied: Permission denied."},
		{agenthub.ErrCancelled, "operation_cancelled: Operation cancelled."},
		{agenthub.ErrProcessFailed, "connection_failed: Agent process failed."},
		{agenthub.ErrSandboxUnavailable, "connection_failed: Agent process failed."},
		{agenthub.ErrUnavailable, "internal_error: Agent Hub is not available."},
	}
	for _, tc := range cases {
		got := publicError("builtin.agenthub", tc.in)
		if got == nil || got.Error() != tc.want {
			t.Fatalf("%v → %v, want %q", tc.in, got, tc.want)
		}
	}
}
