package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"changeme/internal/app"
	"changeme/internal/capabilities"
	"changeme/internal/permissions"
	"changeme/internal/services/syncclient"
)

type fakeSyncService struct {
	status         syncclient.Status
	report         syncclient.Report
	connectErr     error
	syncErr        error
	endpoint       string
	accessToken    string
	disconnections int
	conflicts      []syncclient.ConflictInfo
	conflictsErr   error
}

func (f *fakeSyncService) Connect(_ context.Context, endpoint, accessToken string) error {
	f.endpoint = endpoint
	f.accessToken = accessToken
	return f.connectErr
}

func (f *fakeSyncService) Disconnect()               { f.disconnections++ }
func (f *fakeSyncService) Status() syncclient.Status { return f.status }
func (f *fakeSyncService) SyncNow(context.Context) (syncclient.Report, error) {
	return f.report, f.syncErr
}
func (f *fakeSyncService) Conflicts(context.Context) ([]syncclient.ConflictInfo, error) {
	return f.conflicts, f.conflictsErr
}

func newSyncBridge(t *testing.T, service app.SyncService) (*AppService, *app.Container) {
	t.Helper()
	container := app.NewContainer()
	container.Sync = service
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	return NewAppService(container), container
}

func TestSyncBridgeMapsSafeStatusAndCountDTOs(t *testing.T) {
	expiresAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	fake := &fakeSyncService{
		status: syncclient.Status{
			Connected: true, Endpoint: "https://sync.example.test",
			AccountID: "account-1", DeviceID: "device-1", ExpiresAt: expiresAt,
		},
		report: syncclient.Report{PushAttempted: 3, PushSent: 2, PushRetried: 1, Pulled: 4, Applied: 4},
	}
	service, _ := newSyncBridge(t, fake)

	status, err := service.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if !status.Connected || status.AccountID != "account-1" || status.DeviceID != "device-1" || status.ExpiresAt != expiresAt {
		t.Fatalf("SyncStatus = %#v", status)
	}
	report, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if report.PushAttempted != 3 || report.PushSent != 2 || report.Pulled != 4 || report.Applied != 4 {
		t.Fatalf("SyncNow report = %#v", report)
	}

	serialized, err := json.Marshal(struct {
		Status SyncStatusDTO `json:"status"`
		Report SyncReportDTO `json:"report"`
	}{status, report})
	if err != nil {
		t.Fatalf("json.Marshal DTOs: %v", err)
	}
	for _, forbidden := range []string{"access_token", "bearer", "password", "payload", "response_body"} {
		if strings.Contains(strings.ToLower(string(serialized)), forbidden) {
			t.Fatalf("DTO JSON exposed forbidden field %q: %s", forbidden, serialized)
		}
	}
}

func TestSyncConnectAuditDoesNotContainCredentialsOrIdentity(t *testing.T) {
	const (
		endpoint = "https://private.example.test/path"
		token    = "secret-bearer-token"
	)
	fake := &fakeSyncService{}
	service, container := newSyncBridge(t, fake)

	if err := service.SyncConnect(context.Background(), endpoint, token); err != nil {
		t.Fatalf("SyncConnect: %v", err)
	}
	if fake.endpoint != endpoint || fake.accessToken != token {
		t.Fatal("SyncConnect did not pass credentials to the runtime service")
	}

	events := container.Audit.Events()
	if len(events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.SubjectID != "builtin.sync" || event.Capability != "sync:manage" || event.Scope != "sync:connect" {
		t.Fatalf("unexpected audit event: %#v", event)
	}
	if got := fmt.Sprint(event.Metadata); got != "map[action:connect]" {
		t.Fatalf("audit metadata = %s, want action only", got)
	}
	serialized, _ := json.Marshal(events)
	for _, forbidden := range []string{endpoint, token, "account-1", "device-1"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("audit event leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestSyncBridgeListsConflicts(t *testing.T) {
	detected := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	fake := &fakeSyncService{conflicts: []syncclient.ConflictInfo{
		{RecordID: "rec-1", RecordType: "note", LocalChangeKey: "k1", RemoteChangeKey: "k2", DetectedAt: detected},
	}}
	service, container := newSyncBridge(t, fake)

	conflicts, err := service.SyncConflicts()
	if err != nil {
		t.Fatalf("SyncConflicts: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].RecordID != "rec-1" || conflicts[0].RecordType != "note" ||
		conflicts[0].LocalChangeKey != "k1" || conflicts[0].RemoteChangeKey != "k2" || conflicts[0].DetectedAt != detected {
		t.Fatalf("conflicts = %#v", conflicts)
	}

	serialized, _ := json.Marshal(conflicts)
	for _, forbidden := range []string{"payload", "ciphertext", "secret", "access_token"} {
		if strings.Contains(strings.ToLower(string(serialized)), forbidden) {
			t.Fatalf("conflict DTO leaked %q: %s", forbidden, serialized)
		}
	}

	events := container.Audit.Events()
	if len(events) != 1 || events[0].Scope != "sync:conflicts" {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestSyncBridgeDeniesMissingGrantBeforeCallingService(t *testing.T) {
	fake := &fakeSyncService{}
	service, container := newSyncBridge(t, fake)
	container.Permissions = permissions.NewPolicy(nil)
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)

	const token = "must-not-reach-service"
	if err := service.SyncConnect(context.Background(), "https://sync.example.test", token); err == nil || err.Error() != "permission_denied: Permission denied." {
		t.Fatalf("SyncConnect error = %v, want permission_denied", err)
	}
	if fake.accessToken != "" || fake.endpoint != "" {
		t.Fatal("denied SyncConnect called the runtime service")
	}
}

func TestSyncBridgeFailsSafelyWhenUnavailable(t *testing.T) {
	service, _ := newSyncBridge(t, nil)
	if _, err := service.SyncStatus(); err == nil || err.Error() != "connection_failed: Sync is unavailable." {
		t.Fatalf("SyncStatus error = %v", err)
	}
	if err := service.SyncDisconnect(); err == nil || err.Error() != "connection_failed: Sync is unavailable." {
		t.Fatalf("SyncDisconnect error = %v", err)
	}
}

func TestSyncBridgeRedactsUpstreamErrors(t *testing.T) {
	const secret = "secret-token-in-upstream-error"
	fake := &fakeSyncService{connectErr: errors.New("https://private.example: " + secret)}
	service, _ := newSyncBridge(t, fake)

	err := service.SyncConnect(context.Background(), "https://private.example", secret)
	if err == nil {
		t.Fatal("SyncConnect returned nil error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private.example") {
		t.Fatalf("public error leaked upstream detail: %v", err)
	}
	if err.Error() != "connection_failed: Sync operation failed." {
		t.Fatalf("public error = %v", err)
	}
}
