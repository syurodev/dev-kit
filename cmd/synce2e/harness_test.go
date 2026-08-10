package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"changeme/internal/storage"
	"changeme/internal/sync"
	"changeme/internal/syncclient"
	"changeme/internal/syncstore"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
)

func TestHarnessCompiles(t *testing.T) {}

func requireDeployed(t *testing.T) (baseURL, token string) {
	t.Helper()
	baseURL = os.Getenv("DEVKIT_DEPLOYED_E2E_URL")
	token = os.Getenv("DEVKIT_DEPLOYED_E2E_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set DEVKIT_DEPLOYED_E2E_URL and DEVKIT_DEPLOYED_E2E_TOKEN to run deployed matrix")
	}
	return baseURL, token
}

func newSharedVault(t *testing.T) *vault.Service {
	t.Helper()
	vlt := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := vlt.Setup("master-password"); err != nil {
		t.Fatalf("vault setup: %v", err)
	}
	return vlt
}

type device struct {
	localDeviceID string
	repo          *vaultitem.SQLMutationRepository
	items         *vaultitem.Store
	sync          *syncclient.Service
	resolver      *syncstore.SQLConflictResolver
	conflicts     *syncstore.SQLConflictStore
	remote        *syncstore.SQLRemoteStore
}

func buildDevice(t *testing.T, vlt *vault.Service) *device {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "device.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	identity := syncstore.NewSQLDeviceIdentityStore(db)
	localDeviceID, err := identity.EnsureLocalDevice(ctx)
	if err != nil {
		t.Fatalf("ensure local device: %v", err)
	}
	repo := vaultitem.NewSQLMutationRepository(db)
	syncService, err := syncclient.New(syncclient.Dependencies{
		DeviceIdentity: identity,
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         syncstore.NewSQLRemoteStore(db),
		LocalMutations: syncstore.NewSQLLocalMutationStore(db),
		Outbox:         syncstore.NewSQLOutbox(db),
		Vault:          vlt,
	})
	if err != nil {
		t.Fatalf("new sync client: %v", err)
	}
	return &device{
		localDeviceID: localDeviceID,
		repo:          repo,
		items:         vaultitem.NewStore(repo, vlt),
		sync:          syncService,
		resolver:      syncstore.NewSQLConflictResolver(db),
		conflicts:     syncstore.NewSQLConflictStore(db),
		remote:        syncstore.NewSQLRemoteStore(db),
	}
}

func newBootstrappedDevice(t *testing.T, baseURL, token string, vlt *vault.Service) *device {
	t.Helper()
	device := buildDevice(t, vlt)
	if err := device.sync.Connect(context.Background(), baseURL, token); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return device
}

func newEnrolledDevice(t *testing.T, baseURL, token string, authorizing *device, vlt *vault.Service) *device {
	t.Helper()
	device := buildDevice(t, vlt)
	enrollmentToken := enrollDevice(t, baseURL, token, authorizing.localDeviceID, device.localDeviceID)
	if err := device.sync.ConnectWithEnrollment(context.Background(), baseURL, token, enrollmentToken); err != nil {
		t.Fatalf("connect with enrollment: %v", err)
	}
	return device
}

func enrollDevice(t *testing.T, baseURL, bearer, authorizerDeviceID, targetDeviceID string) string {
	t.Helper()
	body, err := json.Marshal(struct {
		TargetDeviceID string `json:"target_device_id"`
	}{targetDeviceID})
	if err != nil {
		t.Fatalf("marshal enrollment request: %v", err)
	}
	target := strings.TrimRight(baseURL, "/") + "/v1/sync/devices/enrollments"
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create enrollment request: %v", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("X-DevKit-Device-ID", authorizerDeviceID)
	request.Header.Set("X-DevKit-Sync-Protocol", strconv.FormatUint(uint64(sync.SyncProtocolVersion), 10))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("enrollment request failed: %v", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read enrollment response: %v", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("enrollment request returned HTTP %d (body length %d)", response.StatusCode, len(payload))
	}
	var wire struct {
		EnrollmentToken string `json:"enrollment_token"`
		TargetDeviceID  string `json:"target_device_id"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("decode enrollment response: %v", err)
	}
	if wire.EnrollmentToken == "" {
		t.Fatal("enrollment response missing token")
	}
	if wire.TargetDeviceID != targetDeviceID {
		t.Fatalf("enrollment target device = %q, want %q", wire.TargetDeviceID, targetDeviceID)
	}
	return wire.EnrollmentToken
}

func (d *device) createNote(t *testing.T, title, body string) string {
	t.Helper()
	meta, _ := json.Marshal(struct {
		Title string `json:"title"`
	}{title})
	record, err := d.items.Create("note", "note-body", meta, []byte(body))
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	return record.ID
}

func (d *device) createDBProfile(t *testing.T, name string) string {
	t.Helper()
	meta, _ := json.Marshal(struct {
		Name     string `json:"name"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Database string `json:"database"`
		Username string `json:"username"`
		SSLMode  string `json:"ssl_mode"`
	}{
		Name:     name,
		Host:     "db.internal",
		Port:     5432,
		Database: "app",
		Username: "devkit",
		SSLMode:  "verify-full",
	})
	record, err := d.items.Create("db-profile", "db-password", meta, []byte("db-secret"))
	if err != nil {
		t.Fatalf("create db profile: %v", err)
	}
	return record.ID
}

func (d *device) createSSHProfile(t *testing.T, name string) string {
	t.Helper()
	meta, _ := json.Marshal(struct {
		Name               string `json:"name"`
		Host               string `json:"host"`
		Port               int    `json:"port"`
		Username           string `json:"username"`
		AuthMethod         string `json:"auth_method"`
		HostKeyFingerprint string `json:"host_key_fingerprint"`
	}{
		Name:               name,
		Host:               "ssh.internal",
		Port:               22,
		Username:           "devkit",
		AuthMethod:         "key",
		HostKeyFingerprint: "SHA256:test",
	})
	record, err := d.items.Create("ssh-profile", "ssh-secret", meta, []byte("ssh-secret"))
	if err != nil {
		t.Fatalf("create ssh profile: %v", err)
	}
	return record.ID
}

func (d *device) editNote(t *testing.T, id, body string) {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	edited, err := d.items.Reseal(record, "note-body", []byte(body))
	if err != nil {
		t.Fatalf("reseal note: %v", err)
	}
	if err := d.repo.Put(edited); err != nil {
		t.Fatalf("put note: %v", err)
	}
}

func (d *device) body(t *testing.T, id string) string {
	t.Helper()
	record, err := d.items.Get(id)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	plain, err := d.items.OpenPayload(record, "note-body")
	if err != nil {
		t.Fatalf("open note: %v", err)
	}
	return string(plain)
}

func (d *device) syncNow(t *testing.T) syncclient.Report {
	t.Helper()
	report, err := d.sync.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}
	return report
}

func (d *device) account(t *testing.T) string {
	t.Helper()
	account, bound, err := d.remote.BoundAccountID(context.Background())
	if err != nil || !bound {
		t.Fatalf("bound account: bound=%v err=%v", bound, err)
	}
	return account
}
