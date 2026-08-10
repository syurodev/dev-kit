package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"changeme/internal/syncclient"
)

func TestG4RevokedDeviceRejected(t *testing.T) {
	resetDeployedSyncState(t)
	baseURL, token := requireDeployed(t)
	vlt := newSharedVault(t)
	a := newBootstrappedDevice(t, baseURL, token, vlt)
	a.createNote(t, "revoke-me", "body")

	status := a.sync.Status()
	if !status.Connected || status.DeviceID == "" {
		t.Fatalf("G4: device not connected: %+v", status)
	}
	revokeDevice(t, status.DeviceID)

	_, err := a.sync.SyncNow(context.Background())
	if !errors.Is(err, syncclient.ErrPushFailed) && !errors.Is(err, syncclient.ErrPullFailed) {
		t.Fatalf("G4: after revoke SyncNow err = %v, want push/pull auth failure", err)
	}
	if cleared := a.sync.Status(); cleared.Connected {
		t.Fatalf("G4: session still connected after revoke: %+v", cleared)
	}
}

func TestG4ExpiredCredentialsClearSession(t *testing.T) {
	resetDeployedSyncState(t)
	baseURL, token := requireDeployed(t)
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

func revokeDevice(t *testing.T, deviceID string) {
	t.Helper()
	if deviceID == "" {
		t.Fatal("G4: device id is required")
	}
	if dsn := deployedDatabaseDSN(t); dsn != "" {
		revokeDeviceViaSQL(t, dsn, deviceID)
		return
	}
	revokeDeviceViaDocker(t, deviceID)
}

func deployedDatabaseDSN(t *testing.T) string {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("DEVKIT_DEPLOYED_E2E_DATABASE_URL")); dsn != "" {
		return dsn
	}
	user := dockerPostgresEnv(t, "POSTGRES_USER")
	db := dockerPostgresEnv(t, "POSTGRES_DB")
	password := dockerPostgresEnv(t, "POSTGRES_PASSWORD")
	if user == "" || db == "" || password == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv("DEVKIT_DEPLOYED_E2E_DATABASE_PORT"))
	if port == "" {
		port = "5433"
	}
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%s/%s?sslmode=disable",
		user, password, port, db)
}

func dockerPostgresEnv(t *testing.T, key string) string {
	t.Helper()
	cmd := exec.Command("docker", "exec", "devkit-db", "printenv", key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func revokeDeviceViaSQL(t *testing.T, dsn, deviceID string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("G4: open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := db.ExecContext(ctx,
		`UPDATE devices SET status = 'revoked' WHERE device_id = $1`, deviceID)
	if err != nil {
		t.Fatalf("G4: revoke device via sql: %v", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("G4: revoke rows affected: %v", err)
	}
	if rows != 1 {
		t.Fatalf("G4: revoke rows affected = %d, want 1", rows)
	}
}

func revokeDeviceViaDocker(t *testing.T, deviceID string) {
	t.Helper()
	if strings.ContainsAny(deviceID, `'";\`) {
		t.Fatal("G4: device id contains unexpected characters")
	}
	cmd := exec.Command(
		"docker", "exec", "devkit-db", "sh", "-c",
		`psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "UPDATE devices SET status='revoked' WHERE device_id='`+deviceID+`'"`,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("G4: revoke device via docker: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}
