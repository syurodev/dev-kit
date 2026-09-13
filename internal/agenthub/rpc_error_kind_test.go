package agenthub

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"devkit/internal/acp"
)

// An expired login and a crash both arrive as -32603 "Internal error"; only
// the errorKind in data tells them apart.
func TestRPCErrorKindReadsTheAgentsReason(t *testing.T) {
	auth := fmt.Errorf("wrapped: %w", &acp.RPCError{
		Code: -32603, Message: "Internal error",
		Data: json.RawMessage(`{"errorKind":"authentication_failed"}`),
	})
	if got := rpcErrorKind(auth); got != "authentication_failed" {
		t.Fatalf("rpcErrorKind = %q", got)
	}
	for _, err := range []error{
		errors.New("plain"),
		&acp.RPCError{Code: -32603, Message: "Internal error"},
		&acp.RPCError{Code: -32603, Message: "Internal error", Data: json.RawMessage(`"not an object"`)},
	} {
		if got := rpcErrorKind(err); got != "" {
			t.Fatalf("rpcErrorKind(%v) = %q, want empty", err, got)
		}
	}
}
