package database

import (
	"context"
	"testing"
)

func pgSpec(profileID string, readOnly bool) connectionSpec {
	return connectionSpec{profileID: profileID, cfg: ConnConfig{
		Engine: EnginePostgres, Host: "127.0.0.1", Port: 1,
		Database: "devkit", Username: "user", Password: "pass",
		SSLMode: "disable", ReadOnly: readOnly,
	}}
}

func TestPoolManagerReusesOneConnectionPerProfile(t *testing.T) {
	manager := NewPoolManager()
	t.Cleanup(manager.Close)

	first, err := manager.Get(context.Background(), pgSpec("profile-1", false))
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	second, err := manager.Get(context.Background(), pgSpec("profile-1", false))
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if first != second {
		t.Fatal("Get opened a second connection for the same profile")
	}
}

func TestPoolManagerRejectsAnUnknownEngine(t *testing.T) {
	manager := NewPoolManager()
	t.Cleanup(manager.Close)

	spec := pgSpec("profile-x", false)
	spec.cfg.Engine = "oracle"
	if _, err := manager.Get(context.Background(), spec); err == nil {
		t.Fatal("an unknown engine must fail rather than silently using Postgres")
	}
}

func TestQueryContextPreservesCallerCancellation(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	cancel()

	ctx, stop := queryContext(caller)
	defer stop()
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("context error = %v, want context.Canceled", err)
	}
}
