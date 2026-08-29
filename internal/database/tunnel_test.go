package database

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeTunnel struct {
	opened []TunnelSpec
	closed []string
	handle TunnelHandle
	err    error
}

func (f *fakeTunnel) Open(_ context.Context, spec TunnelSpec) (TunnelHandle, error) {
	f.opened = append(f.opened, spec)
	if f.err != nil {
		return TunnelHandle{}, f.err
	}
	return f.handle, nil
}

func (f *fakeTunnel) Close(id string) error {
	f.closed = append(f.closed, id)
	return nil
}

func tunnelledProfile(t *testing.T, svc *Service, sshProfileID string) string {
	t.Helper()
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "remote", Host: "db.internal", Port: 5432, Database: "app",
		Username: "devkit", Password: "pw", SSLMode: "verify-full", Env: "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if _, err := svc.UpdateProfileOptions(summary.ID, UpdateProfileOptionsInput{Env: "dev", SSHProfileID: sshProfileID}); err != nil {
		t.Fatalf("UpdateProfileOptions: %v", err)
	}
	return summary.ID
}

func TestTunnelRepointsTheConnectionWithoutWeakeningTLS(t *testing.T) {
	svc := newService(t)
	tunnel := &fakeTunnel{handle: TunnelHandle{ID: "fwd-1", LocalHost: "127.0.0.1", LocalPort: 54413}}
	svc.SetTunnel(tunnel)
	id := tunnelledProfile(t, svc, "bastion-1")

	conn, err := svc.connect(context.Background(), id)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)

	config := conn.(*postgresConn).pool.Config().ConnConfig
	if config.Host != "127.0.0.1" || config.Port != 54413 {
		t.Fatalf("connection went to %s:%d, want the local end of the forward", config.Host, config.Port)
	}
	if config.TLSConfig == nil {
		t.Fatal("verify-full produced no TLS config")
	}
	// The certificate is issued for db.internal; checking it against loopback
	// would fail, and the only escape would be weakening sslmode.
	if config.TLSConfig.ServerName != "db.internal" {
		t.Fatalf("ServerName = %q, want the profile's real host", config.TLSConfig.ServerName)
	}
	if config.TLSConfig.InsecureSkipVerify {
		t.Fatal("tunnelling must not disable certificate verification")
	}
	if config.User != "devkit" || config.Database != "app" {
		t.Fatalf("repointing lost identity: user=%q db=%q", config.User, config.Database)
	}
}

func TestTunnelSpecCarriesTheServerSideAddress(t *testing.T) {
	svc := newService(t)
	tunnel := &fakeTunnel{handle: TunnelHandle{ID: "fwd-1", LocalHost: "127.0.0.1", LocalPort: 5001}}
	svc.SetTunnel(tunnel)
	id := tunnelledProfile(t, svc, "bastion-1")

	// Dial fails (no server), but the tunnel must have been asked for first,
	// with the address as seen from the SSH server — not from this machine.
	_, _ = svc.TestConnection(context.Background(), id), error(nil)

	if len(tunnel.opened) == 0 {
		t.Fatal("no tunnel was requested for a profile configured with one")
	}
	got := tunnel.opened[0]
	if got.SSHProfileID != "bastion-1" || got.TargetHost != "db.internal" || got.TargetPort != 5432 {
		t.Fatalf("tunnel spec = %+v, want the profile's own host/port via bastion-1", got)
	}
}

func TestTunnelFailureIsNotADirectConnection(t *testing.T) {
	svc := newService(t)
	svc.SetTunnel(&fakeTunnel{err: ErrSSHProfileMissing})
	id := tunnelledProfile(t, svc, "deleted-bastion")

	err := svc.TestConnection(context.Background(), id)
	if !errors.Is(err, ErrSSHProfileMissing) {
		t.Fatalf("err = %v, want ErrSSHProfileMissing — a silent direct connection would reach a different machine entirely", err)
	}
}

func TestNoTunnelConfiguredMeansNoForward(t *testing.T) {
	svc := newService(t)
	tunnel := &fakeTunnel{handle: TunnelHandle{ID: "fwd-1", LocalHost: "127.0.0.1", LocalPort: 5001}}
	svc.SetTunnel(tunnel)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "direct", Host: "127.0.0.1", Port: 5432, Database: "app",
		Username: "devkit", Password: "pw", SSLMode: "disable", Env: "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	_ = svc.TestConnection(context.Background(), summary.ID)
	if len(tunnel.opened) != 0 {
		t.Fatalf("a direct profile opened %d tunnels", len(tunnel.opened))
	}
}

func TestChangingTheTunnelDropsPoolAndForward(t *testing.T) {
	svc := newService(t)
	tunnel := &fakeTunnel{handle: TunnelHandle{ID: "fwd-1", LocalHost: "127.0.0.1", LocalPort: 5001}}
	svc.SetTunnel(tunnel)
	id := tunnelledProfile(t, svc, "bastion-1")

	// Register a pool and its forward the way a successful connect would.
	svc.pools.mu.Lock()
	svc.pools.conns[id] = nil
	svc.pools.forwards[id] = "fwd-1"
	svc.pools.mu.Unlock()

	if _, err := svc.UpdateProfileOptions(id, UpdateProfileOptionsInput{Env: "dev", SSHProfileID: "bastion-2"}); err != nil {
		t.Fatalf("UpdateProfileOptions: %v", err)
	}
	svc.pools.mu.Lock()
	_, poolLeft := svc.pools.conns[id]
	_, forwardLeft := svc.pools.forwards[id]
	svc.pools.mu.Unlock()
	if poolLeft || forwardLeft {
		t.Fatalf("switching bastions left pool=%v forward=%v behind", poolLeft, forwardLeft)
	}
	if len(tunnel.closed) != 1 || tunnel.closed[0] != "fwd-1" {
		t.Fatalf("old forward was not closed: %v", tunnel.closed)
	}
}

func TestDeletingAProfileClosesItsForward(t *testing.T) {
	svc := newService(t)
	tunnel := &fakeTunnel{handle: TunnelHandle{ID: "fwd-9", LocalHost: "127.0.0.1", LocalPort: 5002}}
	svc.SetTunnel(tunnel)
	id := tunnelledProfile(t, svc, "bastion-1")

	svc.pools.mu.Lock()
	svc.pools.forwards[id] = "fwd-9"
	svc.pools.mu.Unlock()

	if err := svc.DeleteProfile(id); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if len(tunnel.closed) != 1 || tunnel.closed[0] != "fwd-9" {
		t.Fatalf("forward outlived its profile: %v", tunnel.closed)
	}
}

func TestTunnelWithoutAnAdapterFailsClosed(t *testing.T) {
	svc := newService(t)
	id := tunnelledProfile(t, svc, "bastion-1")
	err := svc.TestConnection(context.Background(), id)
	if !errors.Is(err, ErrSSHProfileMissing) {
		t.Fatalf("err = %v, want a closed failure when no tunnel adapter is wired", err)
	}
	if err != nil && strings.Contains(err.Error(), "db.internal") {
		t.Fatal("the error leaked the server-side address as if it were dialable")
	}
}
