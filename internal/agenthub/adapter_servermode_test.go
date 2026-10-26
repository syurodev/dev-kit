package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

func TestServerModeAdapterRefusesEverySession(t *testing.T) {
	a := NewServerModeAdapter(AgentTypeClaude)
	if a.Type() != AgentTypeClaude {
		t.Fatalf("Type = %q, want %q", a.Type(), AgentTypeClaude)
	}
	if _, err := a.Open(context.Background(), OpenSpec{Cwd: "/tmp"}); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("Open err = %v, want ErrUnavailableInServerMode", err)
	}
	var loader SessionLoader = a
	if _, err := loader.Load(context.Background(), OpenSpec{Cwd: "/tmp"}, "acp-1"); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("Load err = %v, want ErrUnavailableInServerMode", err)
	}
}

func TestForServerModeKeepsTypes(t *testing.T) {
	got := ForServerMode(NewMockAdapter())
	if len(got) != 1 || got[0].Type() != AgentTypeMock {
		t.Fatalf("ForServerMode = %+v", got)
	}
	if _, err := got[0].Open(context.Background(), OpenSpec{Cwd: "/tmp"}); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("stub Open err = %v", err)
	}
}

func TestServiceCreateSessionInServerModeReportsWhy(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	for _, a := range ForServerMode(NewMockAdapter()) {
		reg.Register(a)
	}
	svc := NewService(reg, NewSQLStore(db), nil)
	_, err = svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("CreateSession err = %v, want ErrUnavailableInServerMode", err)
	}
}

func TestServerModeAdapterRefusesSignInAndFork(t *testing.T) {
	a := NewServerModeAdapter(AgentTypeClaude)
	var provider AuthProvider = a
	if _, err := provider.AuthMethods(context.Background()); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("AuthMethods err = %v, want ErrUnavailableInServerMode", err)
	}
	if _, err := provider.SignInCommand(context.Background(), "any"); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("SignInCommand err = %v, want ErrUnavailableInServerMode", err)
	}
	var forker RuntimeForker = a
	if _, err := forker.Fork(context.Background(), OpenSpec{Cwd: "/tmp"}, "acp-1", "msg-1"); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("Fork err = %v, want ErrUnavailableInServerMode", err)
	}
}

// Server mode refuses to run an agent, but its Settings report still reads
// the usage that desktop sessions recorded with that vendor's decoder.
func TestForServerModePreservesStoredUsageNormalization(t *testing.T) {
	adapter := ForServerMode(NewClaudeAdapter(ClaudeOptions{}))[0]
	raw := json.RawMessage(`{"quota":{"token_count":{"totalTokens":100,"inputTokens":5,"cachedInputTokens":80,"cachedWriteTokens":3,"outputTokens":12}}}`)
	usage := DeriveTurnUsage(adapter, json.RawMessage(`{"InputTokens":0,"OutputTokens":0}`), raw)
	if usage.Total == nil || *usage.Total != 100 || usage.CacheRead == nil || *usage.CacheRead != 80 || usage.FreshInput == nil || *usage.FreshInput != 5 {
		t.Fatalf("server report lost the vendor's stored usage: %+v", usage)
	}
	if _, err := adapter.Open(context.Background(), OpenSpec{}); !errors.Is(err, ErrUnavailableInServerMode) {
		t.Fatalf("preserving the decoder must not enable execution: %v", err)
	}
}
