package agenthub_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"devkit/internal/agenthub"
)

func TestMockAdapterRegistered(t *testing.T) {
	reg := agenthub.NewRegistry()
	reg.Register(agenthub.NewMockAdapter())
	a, ok := reg.Get(agenthub.AgentTypeMock)
	if !ok {
		t.Fatal("missing mock")
	}
	if a.Type() != agenthub.AgentTypeMock {
		t.Fatalf("type=%q", a.Type())
	}
	rt, err := a.Open(context.Background(), agenthub.OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close(context.Background()) }()
	if rt.ACPSessionID() == "" {
		t.Fatal("expected non-empty ACP session id")
	}
}

func TestMockNormalizeUsageFromMeta(t *testing.T) {
	a := agenthub.NewMockAdapter()
	meta := json.RawMessage(`{"mock":{"costUsd":0.01,"inputTokens":10}}`)
	u, err := a.NormalizeUsage(meta)
	if err != nil {
		t.Fatal(err)
	}
	if u.CostUSD != 0.01 || u.InputTokens != 10 {
		t.Fatalf("%+v", u)
	}
	if string(u.RawMeta) != string(meta) {
		t.Fatalf("RawMeta not preserved: %s", u.RawMeta)
	}
}

func TestMockPromptEmitsUpdateAndPermission(t *testing.T) {
	a := agenthub.NewMockAdapter()
	rt, err := a.Open(context.Background(), agenthub.OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	var updates []json.RawMessage
	var permissionCalls int
	rt.SetHandlers(agenthub.RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, _ json.RawMessage) {
			updates = append(updates, append(json.RawMessage(nil), update...))
		},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			permissionCalls++
			return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`), nil
		},
	})

	out, err := rt.Prompt(context.Background(), "please NEED_PERMISSION to continue")
	if err != nil {
		t.Fatal(err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("stopReason=%q", out.StopReason)
	}
	if len(out.Meta) == 0 {
		t.Fatal("expected mock _meta on outcome")
	}
	if len(updates) != 1 {
		t.Fatalf("updates=%d want 1", len(updates))
	}
	if !strings.Contains(string(updates[0]), "agent_message_chunk") {
		t.Fatalf("update=%s", updates[0])
	}
	if permissionCalls != 1 {
		t.Fatalf("permissionCalls=%d want 1", permissionCalls)
	}
}

func TestMockPromptWithoutPermission(t *testing.T) {
	a := agenthub.NewMockAdapter()
	rt, err := a.Open(context.Background(), agenthub.OpenSpec{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	var permissionCalls int
	rt.SetHandlers(agenthub.RuntimeHandlers{
		OnUpdate: func(json.RawMessage, json.RawMessage) {},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			permissionCalls++
			return nil, nil
		},
	})

	out, err := rt.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("stopReason=%q", out.StopReason)
	}
	if permissionCalls != 0 {
		t.Fatalf("permissionCalls=%d want 0", permissionCalls)
	}
}
