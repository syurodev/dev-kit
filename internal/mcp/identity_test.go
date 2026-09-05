package mcp

import (
	"context"
	"testing"

	"devkit/internal/identity"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callVaultStatus(t *testing.T, ms *sdk.Server) {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "vault.status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestSDKServerForStampsCaller(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.sdkServerFor(identity.Agent("11111111-2222-3333-4444-555555555555")))
	if caller.last.caller != "agent:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("caller reaching the gateway = %q", caller.last.caller)
	}
}

func TestDefaultServerIsUserTerminal(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, nil))
	callVaultStatus(t, srv.MCP())
	if caller.last.caller != "user-terminal" {
		t.Fatalf("shared-socket caller = %q, want user-terminal", caller.last.caller)
	}
}
