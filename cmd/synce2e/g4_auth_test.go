package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/syncclient"
)

func TestG4RevokedDeviceRejected(t *testing.T) {
	baseURL, token := requireDeployed(t)
	resetDeployedSyncState(t)
	vlt := newSharedVault(t)

	a := newBootstrappedDevice(t, baseURL, token, vlt)
	b := newEnrolledDevice(t, baseURL, token, a, vlt)

	a.createNote(t, "revoke-me", "body")
	a.syncNow(t)
	b.syncNow(t)

	bStatus := b.sync.Status()
	if !bStatus.Connected || bStatus.DeviceID == "" {
		t.Fatalf("G4: device B not connected: %+v", bStatus)
	}

	revokeDeviceAPI(t, baseURL, token, a.localDeviceID, bStatus.DeviceID)

	_, err := b.sync.SyncNow(context.Background())
	if !errors.Is(err, syncclient.ErrPushFailed) && !errors.Is(err, syncclient.ErrPullFailed) {
		t.Fatalf("G4: after revoke SyncNow err = %v, want push/pull auth failure", err)
	}
	if cleared := b.sync.Status(); cleared.Connected {
		t.Fatalf("G4: session still connected after revoke: %+v", cleared)
	}

	aStatus := a.sync.Status()
	if !aStatus.Connected {
		t.Fatalf("G4: device A lost connection after revoking B: %+v", aStatus)
	}
}

func TestG4ExpiredCredentialsClearSession(t *testing.T) {
	baseURL, token := requireDeployed(t)
	resetDeployedSyncState(t)
	vlt := newSharedVault(t)

	a := newBootstrappedDeviceWithTokenSource(t, baseURL, &expiringTokenSource{token: token}, vlt)
	a.createNote(t, "expiry-me", "body")

	_, err := a.sync.SyncNow(context.Background())
	if !errors.Is(err, syncclient.ErrPushFailed) && !errors.Is(err, syncclient.ErrPullFailed) {
		t.Fatalf("G4: SyncNow with expired credentials err = %v, want push/pull auth failure", err)
	}
	if cleared := a.sync.Status(); cleared.Connected {
		t.Fatalf("G4: session still connected after expired credentials: %+v", cleared)
	}

	if err := a.sync.Connect(context.Background(), baseURL, token); err != nil {
		t.Fatalf("G4: reconnect after auth failure: %v", err)
	}
	if status := a.sync.Status(); !status.Connected {
		t.Fatalf("G4: reconnect did not restore session: %+v", status)
	}
	a.syncNow(t)
}

// expiringTokenSource returns a valid bearer on connect, then an already-expired
// expiry instant on every later refresh so SyncNow fails closed without a live
// Keycloak refresh grant. Post-connect calls stay expired through push retries.
type expiringTokenSource struct {
	token string
	calls int
}

func (s *expiringTokenSource) AccessToken(context.Context) (string, time.Time, error) {
	s.calls++
	if s.calls == 1 {
		return s.token, time.Now().UTC().Add(time.Hour), nil
	}
	return s.token, time.Now().UTC().Add(-time.Hour), nil
}
