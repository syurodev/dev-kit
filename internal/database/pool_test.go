package database

import (
	"context"
	"testing"
	"time"
)

func TestPoolManagerCreatesBoundedPoolAndReusesItPerProfile(t *testing.T) {
	manager := NewPoolManager()
	t.Cleanup(manager.Close)

	first, err := manager.Get(context.Background(), connectionSpec{profileID: "profile-1", dsn: "postgres://user:pass@127.0.0.1:1/devkit?sslmode=disable", readOnly: false})
	if err != nil {
		t.Fatalf("Get first pool: %v", err)
	}
	second, err := manager.Get(context.Background(), connectionSpec{profileID: "profile-1", dsn: "postgres://user:pass@127.0.0.1:1/devkit?sslmode=disable", readOnly: false})
	if err != nil {
		t.Fatalf("Get second pool: %v", err)
	}
	if first != second {
		t.Fatal("Get created a second pool for the same profile")
	}
	config := first.Config()
	if config.MaxConns != 4 {
		t.Fatalf("MaxConns = %d, want 4", config.MaxConns)
	}
	if config.MinConns != 0 {
		t.Fatalf("MinConns = %d, want 0", config.MinConns)
	}
	if config.MaxConnLifetime != 30*time.Minute {
		t.Fatalf("MaxConnLifetime = %s, want 30m", config.MaxConnLifetime)
	}
	if config.AfterConnect != nil {
		t.Fatal("expected AfterConnect to be nil when readOnly is false")
	}
}

func TestPoolManagerConfiguresAfterConnectWhenReadOnly(t *testing.T) {
	manager := NewPoolManager()
	t.Cleanup(manager.Close)

	pool, err := manager.Get(context.Background(), connectionSpec{profileID: "profile-ro", dsn: "postgres://user:pass@127.0.0.1:1/devkit?sslmode=disable", readOnly: true})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	config := pool.Config()
	if config.AfterConnect == nil {
		t.Fatal("expected AfterConnect to be configured when readOnly is true")
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
