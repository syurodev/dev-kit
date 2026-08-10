package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"devkit/internal/crypto"
	"devkit/internal/storage"
	replicationsync "devkit/internal/sync"
	"devkit/internal/syncstore"
	"devkit/internal/vaultitem"
)

const (
	testToken     = "secret-access-token"
	testAccountID = "server-account"
	testDeviceID  = "stable-local-device"
	testKeyID     = "vault-item"
)

type testVault struct{ key []byte }

func (v testVault) SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error) {
	return crypto.SealV2(ad, v.key, plaintext)
}

func (v testVault) OpenWithAD(envelope crypto.Envelope, ad crypto.AssociatedData) ([]byte, error) {
	return crypto.OpenWithAD(v.key, envelope, ad)
}

type fakeDeviceStore struct{ id string }

func (s fakeDeviceStore) EnsureLocalDevice(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.id, nil
}

type fakeCursorStore struct{ cursor replicationsync.Cursor }

func (s *fakeCursorStore) GetCursor(ctx context.Context, _ string) (replicationsync.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return replicationsync.Cursor{}, err
	}
	return s.cursor, nil
}

type fakeRemoteStore struct {
	mu         stdsync.Mutex
	accountID  string
	cursorName string
	nextCursor string
	mutations  []syncstore.RemoteMutation
}

func (s *fakeRemoteStore) ValidateAuthenticatedAccount(ctx context.Context, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountID != "" && s.accountID != accountID {
		return syncstore.ErrSyncAccountMismatch
	}
	return nil
}

func (s *fakeRemoteStore) BindAuthenticatedAccount(ctx context.Context, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountID != "" && s.accountID != accountID {
		return syncstore.ErrSyncAccountMismatch
	}
	s.accountID = accountID
	return nil
}

func (s *fakeRemoteStore) ApplyRemoteBatch(ctx context.Context, accountID, cursorName, nextCursor string, mutations []syncstore.RemoteMutation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountID = accountID
	s.cursorName = cursorName
	s.nextCursor = nextCursor
	s.mutations = make([]syncstore.RemoteMutation, len(mutations))
	copy(s.mutations, mutations)
	for i := range s.mutations {
		s.mutations[i].RecordJSON = append([]byte(nil), mutations[i].RecordJSON...)
	}
	return nil
}

type fakeTransport struct {
	mu          stdsync.Mutex
	pushed      []replicationsync.PushOperation
	pullResult  replicationsync.PullResult
	pullStarted chan struct{}
	pullRelease chan struct{}
	startOnce   stdsync.Once
	pullErr     error
}

func (t *fakeTransport) Push(ctx context.Context, operations []replicationsync.PushOperation) (replicationsync.PushResult, error) {
	if err := ctx.Err(); err != nil {
		return replicationsync.PushResult{}, err
	}
	t.mu.Lock()
	t.pushed = append([]replicationsync.PushOperation(nil), operations...)
	t.mu.Unlock()
	result := replicationsync.PushResult{SentIdempotencyKeys: make([]string, 0, len(operations))}
	for _, operation := range operations {
		result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, operation.IdempotencyKey)
	}
	return result, nil
}

func (t *fakeTransport) Pull(ctx context.Context, _ replicationsync.Cursor, _ int) (replicationsync.PullResult, error) {
	if t.pullStarted != nil {
		t.startOnce.Do(func() { close(t.pullStarted) })
	}
	if t.pullRelease != nil {
		select {
		case <-ctx.Done():
			return replicationsync.PullResult{}, ctx.Err()
		case <-t.pullRelease:
		}
	}
	if t.pullErr != nil {
		return replicationsync.PullResult{}, t.pullErr
	}
	return t.pullResult, nil
}

func TestConnectUsesServerAuthoritativeAccountAndDoesNotExposeToken(t *testing.T) {
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{})
	defer server.Close()

	status := service.Status()
	if !status.Connected || status.AccountID != testAccountID || status.DeviceID != testDeviceID {
		t.Fatalf("unexpected status: %+v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if bytes.Contains(encoded, []byte(testToken)) || bytes.Contains(encoded, []byte("token")) {
		t.Fatalf("status exposed token data: %s", encoded)
	}
}

func TestConnectErrorDoesNotExposeTokenOrRawResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server leaked "+testToken, http.StatusInternalServerError)
	}))
	defer server.Close()

	service := newTestService(t, &fakeTransport{})
	err := service.Connect(context.Background(), server.URL, testToken)
	if !errors.Is(err, ErrConnectServiceUnavailable) {
		t.Fatalf("Connect error = %v, want ErrConnectServiceUnavailable", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "server leaked") {
		t.Fatalf("Connect exposed sensitive upstream error: %v", err)
	}
}

func TestConnectClassifiesRejectedAuthenticationWithoutExposingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "token diagnostic must stay private", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := newTestService(t, &fakeTransport{})
	err := service.Connect(context.Background(), server.URL, testToken)
	if !errors.Is(err, ErrConnectAuthenticationRejected) {
		t.Fatalf("Connect error = %v, want ErrConnectAuthenticationRejected", err)
	}
	if strings.Contains(err.Error(), "diagnostic") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("Connect exposed upstream detail: %v", err)
	}
}

func TestConnectWithEnrollmentSendsEnrollmentHeader(t *testing.T) {
	const enrollmentToken = "enroll-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync/session" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("X-DevKit-Enrollment-Token"); got != enrollmentToken {
			http.Error(w, "missing enrollment header", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": testAccountID,
			"device_id":  testDeviceID,
			"expires_at": time.Now().UTC().Add(time.Hour),
		})
	}))
	defer server.Close()

	service := newTestService(t, &fakeTransport{})
	if err := service.ConnectWithEnrollment(context.Background(), server.URL, testToken, enrollmentToken); err != nil {
		t.Fatalf("ConnectWithEnrollment: %v", err)
	}
	status := service.Status()
	if !status.Connected || status.AccountID != testAccountID || status.DeviceID != testDeviceID {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSyncNowPushesPullsDecryptsAndApplies(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	createSnapshot := noteSnapshot(t, vault, "note-1", "Remote")
	create := sealedOperation(t, vault, "remote-create", "note-1", "note", replicationsync.OperationCreate, 1, createSnapshot)
	remove := sealedOperation(t, vault, "remote-delete", "db-1", "db-profile", replicationsync.OperationDelete, 2, []byte(`{"record_id":"db-1","record_type":"db-profile","deleted":true}`))
	transport := &fakeTransport{pullResult: replicationsync.PullResult{
		Operations: []replicationsync.PushOperation{create, remove},
		NextCursor: replicationsync.Cursor{Token: "cursor-next"},
	}}
	service, server, remote, outbox := newConnectedTestService(t, transport)
	defer server.Close()
	service.deps.Vault = vault

	localEnvelope := sealEnvelope(t, vault, "local-op", "local-1", "note", replicationsync.OperationCreate, 1, []byte(`{"id":"local-1","type":"note"}`), testDeviceID)
	serialized, err := json.Marshal(localEnvelope)
	if err != nil {
		t.Fatalf("marshal local envelope: %v", err)
	}
	localContext := replicationsync.SyncContext{
		AccountID: testAccountID, DeviceID: testDeviceID, ProtocolVersion: replicationsync.SyncProtocolVersion,
		OperationID: "local-op", EntityVersion: 1,
	}
	if err := outbox.Enqueue(replicationsync.NewChange("local-key", "local-1", "note", replicationsync.OperationCreate, serialized, 1, localContext, testKeyID, time.Now().UTC())); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	report, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if report.PushSent != 1 || report.Pulled != 2 || report.Applied != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if remote.accountID != testAccountID || remote.cursorName != defaultCursorName || remote.nextCursor != "cursor-next" {
		t.Fatalf("unexpected apply metadata: %+v", remote)
	}
	if len(remote.mutations) != 2 {
		t.Fatalf("unexpected create mutation: %+v", remote.mutations)
	}
	var normalized struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		Metadata  json.RawMessage `json:"metadata"`
		SyncState string          `json:"sync_state"`
	}
	if err := json.Unmarshal(remote.mutations[0].RecordJSON, &normalized); err != nil ||
		normalized.ID != "note-1" || normalized.Type != "note" || normalized.SyncState != "synced" || string(normalized.Metadata) != `{"title":"Remote"}` {
		t.Fatalf("remote snapshot was not normalized: %s", remote.mutations[0].RecordJSON)
	}
	if remote.mutations[1].Operation != replicationsync.OperationDelete || len(remote.mutations[1].RecordJSON) != 0 {
		t.Fatalf("delete mutation must contain no record JSON: %+v", remote.mutations[1])
	}
}

func TestHTTPClientPullAppliesAndAdvancesCursorAtomicallyInSQLite(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	vault := testVault{key: bytes.Repeat([]byte{0x61}, 32)}
	snapshot := noteSnapshot(t, vault, "remote-note", "Remote")
	operation := sealedOperation(t, vault, "remote-http-op", "remote-note", "note", replicationsync.OperationCreate, 3, snapshot)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deviceID := request.Header.Get("X-DevKit-Device-ID")
		if request.Header.Get("Authorization") != "Bearer "+testToken || deviceID == "" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v1/sync/session":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"account_id": testAccountID,
				"device_id":  deviceID,
				"expires_at": time.Now().UTC().Add(time.Hour),
			})
		case "/v1/sync/pull":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"account_id":       testAccountID,
				"device_id":        deviceID,
				"protocol_version": replicationsync.SyncProtocolVersion,
				"operations":       []replicationsync.PushOperation{operation},
				"next_cursor":      "sqlite-cursor-1",
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service, err := New(Dependencies{
		DeviceIdentity: syncstore.NewSQLDeviceIdentityStore(db),
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         syncstore.NewSQLRemoteStore(db),
		Outbox:         syncstore.NewSQLOutbox(db),
		Vault:          vault,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Connect(context.Background(), server.URL, testToken); err != nil {
		t.Fatal(err)
	}
	var preSyncBindingCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_account_binding").Scan(&preSyncBindingCount); err != nil || preSyncBindingCount != 0 {
		t.Fatalf("Connect claimed account before SyncNow: count=%d err=%v", preSyncBindingCount, err)
	}
	report, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pulled != 1 || report.Applied != 1 {
		t.Fatalf("report = %+v", report)
	}

	var recordJSON []byte
	if err := db.QueryRow("SELECT data FROM vault_items WHERE id = ?", "remote-note").Scan(&recordJSON); err != nil {
		t.Fatal(err)
	}
	var record struct {
		SyncState string `json:"sync_state"`
	}
	if err := json.Unmarshal(recordJSON, &record); err != nil || record.SyncState != "synced" {
		t.Fatalf("stored remote record = %s, err = %v", recordJSON, err)
	}
	var cursor string
	if err := db.QueryRow("SELECT cursor FROM sync_cursors WHERE name = ?", defaultCursorName).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if cursor != "sqlite-cursor-1" {
		t.Fatalf("cursor = %q", cursor)
	}
	var account string
	if err := db.QueryRow("SELECT account_id FROM sync_account_binding WHERE id = 'local'").Scan(&account); err != nil {
		t.Fatal(err)
	}
	if account != testAccountID {
		t.Fatalf("bound account = %q", account)
	}
}

func TestSyncNowMaterializesLocalIntentBeforePush(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	vault := testVault{key: bytes.Repeat([]byte{0x62}, 32)}
	items := vaultitem.NewStore(vaultitem.NewSQLMutationRepository(db), vault)
	record, err := items.Create("note", "note-body", json.RawMessage(`{"title":"Local"}`), []byte("local body"))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": testAccountID,
			"device_id":  testDeviceID,
			"expires_at": time.Now().UTC().Add(time.Hour),
		})
	}))
	defer server.Close()
	transport := &fakeTransport{}
	outbox := syncstore.NewSQLOutbox(db)
	service, err := NewWithFactories(Dependencies{
		DeviceIdentity: fakeDeviceStore{id: testDeviceID},
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         syncstore.NewSQLRemoteStore(db),
		LocalMutations: syncstore.NewSQLLocalMutationStore(db),
		Outbox:         outbox,
		Vault:          vault,
	}, Factories{
		Authenticator: func(baseURL string, client *http.Client) (replicationsync.Authenticator, error) {
			return replicationsync.NewHTTPAuthenticator(baseURL, client)
		},
		Transport: func(string, replicationsync.SessionProvider, *http.Client) (replicationsync.PullTransport, error) {
			return transport, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Connect(context.Background(), server.URL, testToken); err != nil {
		t.Fatal(err)
	}
	report, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.PushSent != 1 {
		t.Fatalf("report = %+v", report)
	}
	transport.mu.Lock()
	pushed := append([]replicationsync.PushOperation(nil), transport.pushed...)
	transport.mu.Unlock()
	if len(pushed) != 1 || pushed[0].Envelope.RecordID != record.ID || pushed[0].Envelope.EntityVersion != 1 {
		t.Fatalf("pushed operations = %+v", pushed)
	}
	snapshot, err := replicationsync.OpenPayload(vault, pushed[0].Envelope, "vault-item")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(snapshot)
	if !bytes.Contains(snapshot, []byte(`"title":"Local"`)) || bytes.Contains(snapshot, []byte("sync_state")) {
		t.Fatalf("materialized snapshot = %s", snapshot)
	}
	var intentCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_local_mutations").Scan(&intentCount); err != nil || intentCount != 0 {
		t.Fatalf("remaining intents = %d, err = %v", intentCount, err)
	}
	var accountVersion int
	if err := db.QueryRow(`SELECT entity_version FROM sync_entity_versions
		WHERE account_id = ? AND record_id = ?`, testAccountID, record.ID).Scan(&accountVersion); err != nil {
		t.Fatal(err)
	}
	if accountVersion != 1 {
		t.Fatalf("account entity version = %d", accountVersion)
	}
	stored, err := items.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SyncState != "synced" {
		t.Fatalf("stored sync state = %q, want synced", stored.SyncState)
	}
}

func TestNormalizeSnapshotRejectsMalformedRemoteRecords(t *testing.T) {
	outer := replicationsync.ReplicationEnvelope{RecordID: "note-1", RecordType: "note", KeyVersion: 1}
	tests := []struct {
		name     string
		snapshot string
	}{
		{
			name:     "source sync state is local only",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1,"sync_state":"local"}`,
		},
		{
			name:     "malformed timestamp",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"created_at":"not-a-time","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "updated before created",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"created_at":"2026-08-01T12:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "key version mismatch",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":2}`,
		},
		{
			name:     "invalid payload envelope",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"payload":{"version":2,"algorithm":"xchacha20-poly1305"},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "invalid metadata JSON shape",
			snapshot: `{"id":"note-1","type":"note","created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "metadata is an array",
			snapshot: `{"id":"note-1","type":"note","metadata":[],"payload":{"version":2,"algorithm":"xchacha20-poly1305","key_id":"note-body","nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "required module metadata is missing",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"payload":{"version":2,"algorithm":"xchacha20-poly1305","key_id":"note-body","nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "encrypted module payload is missing",
			snapshot: `{"id":"note-1","type":"note","metadata":{"title":"Remote"},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "module payload key is wrong",
			snapshot: `{"id":"note-1","type":"note","metadata":{"title":"Remote"},"payload":{"version":2,"algorithm":"xchacha20-poly1305","key_id":"db-password","nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`,
		},
		{
			name:     "more than one JSON value",
			snapshot: `{"id":"note-1","type":"note","metadata":{},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1} {}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizeSnapshot([]byte(test.snapshot), outer); !errors.Is(err, ErrInvalidRemoteData) {
				t.Fatalf("normalizeSnapshot error = %v, want ErrInvalidRemoteData", err)
			}
		})
	}
}

func TestNormalizeSnapshotRejectsUnsupportedModuleType(t *testing.T) {
	outer := replicationsync.ReplicationEnvelope{RecordID: "custom-1", RecordType: "custom", KeyVersion: 1}
	snapshot := []byte(`{"id":"custom-1","type":"custom","metadata":{},"payload":{"version":2,"algorithm":"xchacha20-poly1305","key_id":"custom-secret","nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-08-01T11:00:00Z","key_version":1}`)

	if _, _, err := normalizeSnapshot(snapshot, outer); !errors.Is(err, ErrInvalidRemoteData) {
		t.Fatalf("normalizeSnapshot error = %v, want ErrInvalidRemoteData", err)
	}
}

func TestSyncNowRejectsTamperedTombstone(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x31}, 32)}
	operation := sealedOperation(t, vault, "delete-op", "note-1", "note", replicationsync.OperationDelete, 1,
		[]byte(`{"record_id":"other-note","record_type":"note","deleted":true}`))
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{pullResult: replicationsync.PullResult{Operations: []replicationsync.PushOperation{operation}}})
	defer server.Close()
	service.deps.Vault = vault

	_, err := service.SyncNow(context.Background())
	if !errors.Is(err, ErrInvalidRemoteData) {
		t.Fatalf("SyncNow error = %v, want ErrInvalidRemoteData", err)
	}
}

func TestSyncNowRejectsTamperedNestedKeyEnvelope(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x52}, 32)}
	operation := sealedOperation(t, vault, "create-op", "note-1", "note", replicationsync.OperationCreate, 1, []byte(`{"id":"note-1","type":"note"}`))
	var nested crypto.Envelope
	if err := json.Unmarshal(operation.Envelope.Ciphertext, &nested); err != nil {
		t.Fatalf("unmarshal nested envelope: %v", err)
	}
	nested.KeyID = "attacker-key"
	operation.Envelope.Ciphertext, _ = json.Marshal(nested)
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{pullResult: replicationsync.PullResult{Operations: []replicationsync.PushOperation{operation}}})
	defer server.Close()
	service.deps.Vault = vault

	_, err := service.SyncNow(context.Background())
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("SyncNow error = %v, want ErrDecryptFailed", err)
	}
}

func TestSyncNowRejectsMalformedNestedEnvelopeWithoutPanic(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x53}, 32)}
	operation := sealedOperation(t, vault, "create-op", "note-1", "note", replicationsync.OperationCreate, 1, []byte(`{"id":"note-1","type":"note"}`))
	var nested crypto.Envelope
	if err := json.Unmarshal(operation.Envelope.Ciphertext, &nested); err != nil {
		t.Fatal(err)
	}
	nested.Nonce = []byte{1}
	operation.Envelope.Ciphertext, _ = json.Marshal(nested)
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{pullResult: replicationsync.PullResult{Operations: []replicationsync.PushOperation{operation}}})
	defer server.Close()
	service.deps.Vault = vault

	if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrInvalidRemoteData) {
		t.Fatalf("SyncNow error = %v, want ErrInvalidRemoteData", err)
	}
}

func TestSyncNowAuthenticatesInnerPayloadBeforeApply(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x55}, 32)}
	copied, err := vault.SealV2(crypto.AssociatedData{RecordType: "note", RecordID: "source-note", KeyID: "note-body"}, []byte("source body"))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := vault.SealV2(crypto.AssociatedData{RecordType: "note", RecordID: "target-note", KeyID: "note-body"}, []byte("target body"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := valid
	tampered.Ciphertext = append([]byte(nil), valid.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xff

	for _, test := range []struct {
		name    string
		payload crypto.Envelope
	}{
		{name: "copied from another record", payload: copied},
		{name: "invalid authentication tag", payload: tampered},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := noteSnapshotWithEnvelope(t, "target-note", "Remote", test.payload)
			operation := sealedOperation(t, vault, "inner-auth-op", "target-note", "note", replicationsync.OperationCreate, 1, snapshot)
			service, server, remote, _ := newConnectedTestService(t, &fakeTransport{pullResult: replicationsync.PullResult{Operations: []replicationsync.PushOperation{operation}}})
			defer server.Close()
			service.deps.Vault = vault

			if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrDecryptFailed) {
				t.Fatalf("SyncNow error = %v, want ErrDecryptFailed", err)
			}
			if len(remote.mutations) != 0 {
				t.Fatalf("invalid inner payload reached ApplyRemoteBatch: %+v", remote.mutations)
			}
		})
	}
}

func TestSyncNowRejectsUnsupportedTombstoneType(t *testing.T) {
	vault := testVault{key: bytes.Repeat([]byte{0x54}, 32)}
	operation := sealedOperation(t, vault, "delete-op", "custom-1", "custom", replicationsync.OperationDelete, 1,
		[]byte(`{"record_id":"custom-1","record_type":"custom","deleted":true}`))
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{pullResult: replicationsync.PullResult{Operations: []replicationsync.PushOperation{operation}}})
	defer server.Close()
	service.deps.Vault = vault

	if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrInvalidRemoteData) {
		t.Fatalf("SyncNow error = %v, want ErrInvalidRemoteData", err)
	}
}

func TestDisconnectClearsRuntimeSession(t *testing.T) {
	service, server, _, _ := newConnectedTestService(t, &fakeTransport{})
	defer server.Close()
	service.Disconnect()
	if status := service.Status(); status.Connected || status.Endpoint != "" || status.AccountID != "" || status.DeviceID != "" {
		t.Fatalf("status after disconnect: %+v", status)
	}
	if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("SyncNow error = %v, want ErrNotConnected", err)
	}
}

func TestSyncNowClearsRuntimeSessionAfterAuthenticationFailure(t *testing.T) {
	transport := &fakeTransport{pullErr: replicationsync.ErrAuthentication}
	service, server, _, _ := newConnectedTestService(t, transport)
	defer server.Close()

	if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrPullFailed) {
		t.Fatalf("SyncNow error = %v, want ErrPullFailed", err)
	}
	if status := service.Status(); status.Connected {
		t.Fatalf("status after auth rejection = %+v", status)
	}
}

func TestStatusClearsExpiredRuntimeSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": testAccountID,
			"device_id":  testDeviceID,
			"expires_at": time.Now().UTC().Add(200 * time.Millisecond),
		})
	}))
	defer server.Close()
	service := newTestService(t, &fakeTransport{})
	if err := service.Connect(context.Background(), server.URL, testToken); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for service.Status().Connected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if status := service.Status(); status.Connected {
		t.Fatalf("expired status = %+v", status)
	}
}

func TestConcurrentSyncNowReturnsErrSyncInProgress(t *testing.T) {
	transport := &fakeTransport{pullStarted: make(chan struct{}), pullRelease: make(chan struct{})}
	service, server, _, _ := newConnectedTestService(t, transport)
	defer server.Close()
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.SyncNow(context.Background())
		firstDone <- err
	}()
	<-transport.pullStarted
	if _, err := service.SyncNow(context.Background()); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("concurrent SyncNow error = %v, want ErrSyncInProgress", err)
	}
	close(transport.pullRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("first SyncNow: %v", err)
	}
}

func newConnectedTestService(t *testing.T, transport *fakeTransport) (*Service, *httptest.Server, *fakeRemoteStore, *replicationsync.MemoryOutbox) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync/session" || r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("X-DevKit-Device-ID") != testDeviceID {
			http.Error(w, "invalid request", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": testAccountID,
			"device_id":  testDeviceID,
			"expires_at": time.Now().UTC().Add(time.Hour),
		})
	}))
	service := newTestService(t, transport)
	if err := service.Connect(context.Background(), server.URL, testToken); err != nil {
		server.Close()
		t.Fatalf("Connect: %v", err)
	}
	return service, server, service.deps.Remote.(*fakeRemoteStore), service.deps.Outbox.(*replicationsync.MemoryOutbox)
}

func newTestService(t *testing.T, transport *fakeTransport) *Service {
	t.Helper()
	remote := &fakeRemoteStore{}
	outbox := replicationsync.NewMemoryOutbox()
	service, err := NewWithFactories(Dependencies{
		DeviceIdentity: fakeDeviceStore{id: testDeviceID},
		Cursors:        &fakeCursorStore{cursor: replicationsync.Cursor{Token: "cursor-current"}},
		Remote:         remote,
		Outbox:         outbox,
		Vault:          testVault{key: bytes.Repeat([]byte{0x42}, 32)},
	}, Factories{
		Authenticator: func(baseURL string, client *http.Client) (replicationsync.Authenticator, error) {
			return replicationsync.NewHTTPAuthenticator(baseURL, client)
		},
		Transport: func(_ string, sessions replicationsync.SessionProvider, _ *http.Client) (replicationsync.PullTransport, error) {
			if session, err := sessions.Session(context.Background()); err != nil || session.AccountID() != testAccountID {
				return nil, errors.New("missing authenticated server account")
			}
			return transport, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithFactories: %v", err)
	}
	return service
}

func TestConflictsListsOutboxConflicts(t *testing.T) {
	outbox := replicationsync.NewMemoryOutbox()
	change := replicationsync.NewChange(
		"key-1", "rec-1", "note", replicationsync.OperationUpdate, []byte("cipher"), 1,
		replicationsync.SyncContext{
			AccountID: testAccountID, DeviceID: testDeviceID,
			ProtocolVersion: replicationsync.SyncProtocolVersion, OperationID: "op-1", EntityVersion: 2,
		},
		testKeyID, time.Now().UTC())
	if err := outbox.Enqueue(change); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	conflict := replicationsync.Conflict{
		ID: "conflict-1", RecordID: "rec-1", RecordType: "note",
		LocalChangeKey: "key-1", RemoteChangeKey: "key-remote", DetectedAt: time.Now().UTC(),
	}
	if err := outbox.MarkConflict("key-1", conflict); err != nil {
		t.Fatalf("MarkConflict: %v", err)
	}

	service, err := NewWithFactories(Dependencies{
		DeviceIdentity: fakeDeviceStore{id: testDeviceID},
		Cursors:        &fakeCursorStore{},
		Remote:         &fakeRemoteStore{},
		Outbox:         outbox,
		Vault:          testVault{key: bytes.Repeat([]byte{0x42}, 32)},
	}, Factories{
		Authenticator: func(baseURL string, client *http.Client) (replicationsync.Authenticator, error) {
			return replicationsync.NewHTTPAuthenticator(baseURL, client)
		},
		Transport: func(_ string, sessions replicationsync.SessionProvider, _ *http.Client) (replicationsync.PullTransport, error) {
			return &fakeTransport{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithFactories: %v", err)
	}

	conflicts, err := service.Conflicts(context.Background())
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("len(conflicts) = %d, want 1", len(conflicts))
	}
	got := conflicts[0]
	if got.RecordID != "rec-1" || got.RecordType != "note" || got.LocalChangeKey != "key-1" || got.RemoteChangeKey != "key-remote" {
		t.Fatalf("conflict = %#v", got)
	}
}

func sealedOperation(t *testing.T, vault testVault, operationID, recordID, recordType string, operation replicationsync.Operation, entityVersion uint32, plaintext []byte) replicationsync.PushOperation {
	t.Helper()
	envelope := sealEnvelope(t, vault, operationID, recordID, recordType, operation, entityVersion, plaintext, "remote-device")
	return replicationsync.PushOperation{IdempotencyKey: "key-" + operationID, Operation: operation, Envelope: envelope}
}

func sealEnvelope(t *testing.T, vault testVault, operationID, recordID, recordType string, operation replicationsync.Operation, entityVersion uint32, plaintext []byte, deviceID string) replicationsync.ReplicationEnvelope {
	t.Helper()
	envelope, err := replicationsync.SealPayload(vault, replicationsync.SyncContext{
		AccountID: testAccountID, DeviceID: deviceID, ProtocolVersion: replicationsync.SyncProtocolVersion,
		OperationID: operationID, EntityVersion: entityVersion,
	}, recordID, recordType, testKeyID, 1, operation, plaintext)
	if err != nil {
		t.Fatalf("SealPayload: %v", err)
	}
	return envelope
}

func noteSnapshot(t *testing.T, vault testVault, recordID, title string) []byte {
	t.Helper()
	payload, err := vault.SealV2(crypto.AssociatedData{RecordType: "note", RecordID: recordID, KeyID: "note-body"}, []byte("encrypted note body"))
	if err != nil {
		t.Fatal(err)
	}
	return noteSnapshotWithEnvelope(t, recordID, title, payload)
}

func noteSnapshotWithEnvelope(t *testing.T, recordID, title string, payload crypto.Envelope) []byte {
	t.Helper()
	encoded, err := json.Marshal(snapshot{
		ID:         recordID,
		Type:       "note",
		Metadata:   json.RawMessage(`{"title":` + strconv.Quote(title) + `}`),
		Payload:    &payload,
		CreatedAt:  time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
