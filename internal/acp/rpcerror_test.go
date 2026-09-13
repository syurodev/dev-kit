package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

// The reason is as often in Data as in Message: an expired Claude login arrives
// as "Internal error" with {"errorKind":"authentication_failed"}.
func TestRPCErrorKeepsItsData(t *testing.T) {
	bare := &RPCError{Code: -32603, Message: "Internal error"}
	if got := bare.Error(); got != "acp: rpc error -32603: Internal error" {
		t.Fatalf("bare error = %q", got)
	}
	withData := &RPCError{Code: -32603, Message: "Internal error", Data: json.RawMessage(`{"errorKind":"authentication_failed"}`)}
	if got := withData.Error(); !strings.Contains(got, "authentication_failed") {
		t.Fatalf("error dropped its data: %q", got)
	}
}
