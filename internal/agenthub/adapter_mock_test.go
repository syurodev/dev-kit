package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

	out, err := rt.Prompt(context.Background(), agenthub.TextPrompt("please NEED_PERMISSION to continue"), nil)
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

	out, err := rt.Prompt(context.Background(), agenthub.TextPrompt("hello"), nil)
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

func TestMockLimitMarkers(t *testing.T) {
	cases := []struct {
		marker  string
		chunks  int
		want    error
		stopFor agenthub.StopKind
	}{
		{"FAIL_USAGE_LIMIT", 0, agenthub.ErrAgentUsageLimit, agenthub.StopUsageLimit},
		{"FAIL_USAGE_LIMIT_AFTER_OUTPUT", 1, agenthub.ErrAgentUsageLimit, agenthub.StopUsageLimit},
		{"FAIL_RATE_LIMIT_429", 0, agenthub.ErrTurnFailed, agenthub.StopRateLimited},
	}
	for _, c := range cases {
		t.Run(c.marker, func(t *testing.T) {
			rt, err := agenthub.NewMockAdapter().Open(context.Background(), agenthub.OpenSpec{Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rt.Close(context.Background()) }()
			var chunks int
			rt.SetHandlers(agenthub.RuntimeHandlers{OnUpdate: func(u json.RawMessage, _ json.RawMessage) {
				if strings.Contains(string(u), "agent_message_chunk") {
					chunks++
				}
			}})
			_, err = rt.Prompt(context.Background(), agenthub.TextPrompt("please "+c.marker), nil)
			if !errors.Is(err, c.want) {
				t.Fatalf("error = %v, want %v", err, c.want)
			}
			if chunks != c.chunks {
				t.Errorf("agent message chunks before the refusal = %d, want %d", chunks, c.chunks)
			}
			if got := agenthub.StopFromError(err, time.Now(), false).Kind; got != c.stopFor {
				t.Errorf("stop kind = %q, want %q", got, c.stopFor)
			}
		})
	}
}
