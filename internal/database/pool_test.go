package database

import (
	"context"
	"testing"
	"time"
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

// stuckTunnel is an SSH host that never answers.
type stuckTunnel struct{ entered chan struct{} }

func (s *stuckTunnel) Open(ctx context.Context, _ TunnelSpec) (TunnelHandle, error) {
	close(s.entered)
	<-ctx.Done()
	return TunnelHandle{}, ctx.Err()
}

func (s *stuckTunnel) Close(string) error { return nil }

// #46. Opening used to hold one lock for every profile, so a tunnel that hung
// held every other profile's first query behind it.
func TestPoolManagerOpensProfilesIndependently(t *testing.T) {
	manager := NewPoolManager()
	t.Cleanup(manager.Close)
	tunnel := &stuckTunnel{entered: make(chan struct{})}
	manager.SetTunnel(tunnel)

	stuck := pgSpec("behind-ssh", false)
	stuck.tunnel = &TunnelSpec{SSHProfileID: "ssh", TargetHost: "db.internal", TargetPort: 5432}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = manager.Get(ctx, stuck) }()
	<-tunnel.entered

	done := make(chan error, 1)
	go func() {
		_, err := manager.Get(context.Background(), pgSpec("direct", false))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Get direct: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a profile with no tunnel waited on another profile's stuck tunnel")
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
