package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"devkit/internal/app"
	"devkit/internal/auth"
	"devkit/internal/capabilities"
	"devkit/internal/permissions"
	"devkit/internal/syncclient"
)

type fakeSyncService struct {
	status         syncclient.Status
	report         syncclient.Report
	connectErr     error
	syncErr        error
	endpoint       string
	accessToken    string
	tokenSource    syncclient.AccessTokenSource
	disconnections int
	conflicts      []syncclient.ConflictInfo
	conflictsErr   error
	devices        []syncclient.DeviceInfo
	devicesErr     error
	revokeTarget   string
	revokeSelf     bool
	revokeErr      error
}

func (f *fakeSyncService) Connect(_ context.Context, endpoint, accessToken string) error {
	f.endpoint = endpoint
	f.accessToken = accessToken
	return f.connectErr
}

func (f *fakeSyncService) ConnectWithTokenSource(_ context.Context, endpoint string, tokens syncclient.AccessTokenSource) error {
	f.endpoint = endpoint
	f.tokenSource = tokens
	return f.connectErr
}

func (f *fakeSyncService) Disconnect()               { f.disconnections++ }
func (f *fakeSyncService) Status() syncclient.Status { return f.status }
func (f *fakeSyncService) SyncNow(context.Context) (syncclient.Report, error) {
	return f.report, f.syncErr
}

type fakeAuthService struct {
	status auth.Status
	token  string
	err    error
}

func (f *fakeAuthService) EnsureAuthenticated(context.Context) (auth.Status, error) {
	return f.status, f.err
}
func (f *fakeAuthService) AccessToken(context.Context) (string, time.Time, error) {
	return f.token, f.status.ExpiresAt, f.err
}
func (f *fakeAuthService) Logout() error       { return f.err }
func (f *fakeAuthService) Status() auth.Status { return f.status }
func (f *fakeSyncService) Conflicts(context.Context) ([]syncclient.ConflictInfo, error) {
	return f.conflicts, f.conflictsErr
}

func (f *fakeSyncService) ListDevices(_ context.Context, tokens syncclient.AccessTokenSource, baseURL string) ([]syncclient.DeviceInfo, error) {
	f.tokenSource = tokens
	f.endpoint = baseURL
	return f.devices, f.devicesErr
}

func (f *fakeSyncService) RevokeDevice(_ context.Context, tokens syncclient.AccessTokenSource, baseURL, targetDeviceID string) (bool, error) {
	f.tokenSource = tokens
	f.endpoint = baseURL
	f.revokeTarget = targetDeviceID
	return f.revokeSelf, f.revokeErr
}

func newSyncBridge(t *testing.T, service app.SyncService) (*AppService, *app.Container) {
	t.Helper()
	container := app.NewContainer()
	container.Sync = service
	container.SetAuth(&fakeAuthService{status: auth.Status{Authenticated: true, ExpiresAt: time.Now().Add(time.Hour)}, token: "test-token"})
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

func TestSyncConnectUsesPinnedGatewayWithoutAuditingCredentials(t *testing.T) {
	fake := &fakeSyncService{}
	service, container := newSyncBridge(t, fake)

	if err := service.SyncConnect(context.Background()); err != nil {
		t.Fatalf("SyncConnect: %v", err)
	}
	if fake.endpoint != productionSyncGateway || fake.tokenSource == nil {
		t.Fatal("SyncConnect did not pass the pinned endpoint and runtime token source")
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
	for _, forbidden := range []string{"test-token", "account-1", "device-1"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("audit event leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestSyncBridgeListsConflicts(t *testing.T) {
	detected := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	fake := &fakeSyncService{conflicts: []syncclient.ConflictInfo{
		{RecordID: "rec-1", RecordType: "note", RemoteOperation: "update", RemoteEntityVersion: 2, DetectedAt: detected},
		{RecordID: "rec-2", RecordType: "note", RemoteOperation: "delete", RemoteEntityVersion: 3, TheirsDeleted: true, DetectedAt: detected},
	}}
	service, container := newSyncBridge(t, fake)

	conflicts, err := service.SyncConflicts()
	if err != nil {
		t.Fatalf("SyncConflicts: %v", err)
	}
	if len(conflicts) != 2 || conflicts[0].RecordID != "rec-1" || conflicts[0].RecordType != "note" ||
		conflicts[0].RemoteOperation != "update" || conflicts[0].RemoteEntityVersion != 2 || conflicts[0].DetectedAt != detected {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	if !conflicts[1].TheirsDeleted || conflicts[1].RemoteOperation != "delete" {
		t.Fatalf("delete conflict = %#v", conflicts[1])
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

func TestSyncBridgeListsDevices(t *testing.T) {
	created := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	lastSeen := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	fake := &fakeSyncService{devices: []syncclient.DeviceInfo{
		{DeviceID: "device-1", Status: "active", CreatedAt: created, LastSeenAt: lastSeen, Current: true},
		{DeviceID: "device-2", Status: "active", CreatedAt: created, LastSeenAt: lastSeen, Current: false},
	}}
	service, container := newSyncBridge(t, fake)

	devices, err := service.SyncListDevices(context.Background())
	if err != nil {
		t.Fatalf("SyncListDevices: %v", err)
	}
	if len(devices) != 2 || devices[0].DeviceID != "device-1" || !devices[0].Current ||
		devices[1].DeviceID != "device-2" || devices[1].Current {
		t.Fatalf("devices = %#v", devices)
	}
	if fake.endpoint != productionSyncGateway || fake.tokenSource == nil {
		t.Fatal("SyncListDevices did not pass pinned gateway and auth token source")
	}

	serialized, _ := json.Marshal(devices)
	for _, forbidden := range []string{"access_token", "bearer", "password", "payload"} {
		if strings.Contains(strings.ToLower(string(serialized)), forbidden) {
			t.Fatalf("device DTO leaked %q: %s", forbidden, serialized)
		}
	}

	events := container.Audit.Events()
	if len(events) != 1 || events[0].Scope != "sync:devices" {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestSyncBridgeRevokeDeviceClearsSessionOnSelfRevoke(t *testing.T) {
	fake := &fakeSyncService{
		status:     syncclient.Status{Connected: true, DeviceID: "device-1"},
		revokeSelf: true,
	}
	service, _ := newSyncBridge(t, fake)

	if err := service.SyncRevokeDevice(context.Background(), "device-1"); err != nil {
		t.Fatalf("SyncRevokeDevice: %v", err)
	}
	if fake.revokeTarget != "device-1" {
		t.Fatalf("revoke target = %q, want device-1", fake.revokeTarget)
	}
	if fake.disconnections != 1 {
		t.Fatalf("disconnections = %d, want 1", fake.disconnections)
	}
}

func TestSyncBridgeRevokeDeviceKeepsSessionForRemoteTarget(t *testing.T) {
	fake := &fakeSyncService{
		status:     syncclient.Status{Connected: true, DeviceID: "device-1"},
		revokeSelf: false,
	}
	service, _ := newSyncBridge(t, fake)

	if err := service.SyncRevokeDevice(context.Background(), "device-2"); err != nil {
		t.Fatalf("SyncRevokeDevice: %v", err)
	}
	if fake.disconnections != 0 {
		t.Fatalf("disconnections = %d, want 0", fake.disconnections)
	}
}

func TestSyncBridgeDeniesMissingGrantBeforeCallingService(t *testing.T) {
	fake := &fakeSyncService{}
	service, container := newSyncBridge(t, fake)
	container.Permissions = permissions.NewPolicy(nil)
	container.Capabilities = capabilities.NewGateway(container.Permissions, container.Audit)

	if err := service.SyncConnect(context.Background()); err == nil || err.Error() != "permission_denied: Permission denied." {
		t.Fatalf("SyncConnect error = %v, want permission_denied", err)
	}
	if fake.accessToken != "" || fake.endpoint != "" || fake.tokenSource != nil {
		t.Fatal("denied SyncConnect called the runtime service")
	}

	if _, err := service.SyncListDevices(context.Background()); err == nil || err.Error() != "permission_denied: Permission denied." {
		t.Fatalf("SyncListDevices error = %v, want permission_denied", err)
	}
	if fake.tokenSource != nil {
		t.Fatal("denied SyncListDevices called the runtime service")
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

	err := service.SyncConnect(context.Background())
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

func TestSyncBridgeDescribesSafeAuthenticationRejection(t *testing.T) {
	fake := &fakeSyncService{connectErr: syncclient.ErrConnectAuthenticationRejected}
	service, _ := newSyncBridge(t, fake)

	err := service.SyncConnect(context.Background())
	if err == nil || err.Error() != "connection_failed: The sync gateway rejected the sign-in token." {
		t.Fatalf("SyncConnect error = %v", err)
	}
}

func TestSyncBridgeDescribesSafeSyncFailureStage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "local capture",
			err:  syncclient.ErrLocalCaptureFailed,
			want: "connection_failed: Local changes could not be prepared for sync.",
		},
		{
			name: "push",
			err:  syncclient.ErrPushFailed,
			want: "connection_failed: Sync upload failed.",
		},
		{
			name: "pull",
			err:  syncclient.ErrPullFailed,
			want: "connection_failed: Sync download failed.",
		},
		{
			name: "apply",
			err:  syncclient.ErrApplyFailed,
			want: "connection_failed: Downloaded sync data could not be applied.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSyncService{syncErr: test.err}
			service, _ := newSyncBridge(t, fake)

			_, err := service.SyncNow(context.Background())
			if err == nil || err.Error() != test.want {
				t.Fatalf("SyncNow error = %v, want %q", err, test.want)
			}
		})
	}
}
