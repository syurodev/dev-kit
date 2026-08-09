package sync

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

// TestBackendContractE2E is opt-in because it needs the Java backend, its
// PostgreSQL database, and a gateway-compatible signed identity token. The
// backend test suite supplies these values when it runs the cross-repo gate.
func TestBackendContractE2E(t *testing.T) {
	baseURL := os.Getenv("DEVKIT_BACKEND_E2E_URL")
	token := os.Getenv("DEVKIT_BACKEND_E2E_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("backend contract environment is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	authenticator, err := NewHTTPAuthenticator(baseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionA, err := authenticator.Authenticate(ctx, token, "contract-device-a")
	if err != nil {
		t.Fatalf("authenticate device A: %v", err)
	}
	providerA := &staticSessionProvider{session: sessionA}
	transportA, err := NewHTTPTransport(baseURL, providerA, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Use the real replication-envelope builder so pull is checked against the
	// nested crypto-envelope validation performed by the desktop client.
	vault := testVault{key: bytes.Repeat([]byte{0x42}, 32)}
	create := backendOperation(t, vault, sessionA, "contract-record", "note", "create-1", 1, OperationCreate)
	secret := backendOperation(t, vault, sessionA, "contract-secret", "ssh", "create-ssh", 1, OperationCreate)
	result, err := transportA.Push(ctx, []PushOperation{create, secret})
	if err != nil || len(result.SentIdempotencyKeys) != 2 {
		t.Fatalf("initial push = %+v, %v", result, err)
	}
	result, err = transportA.Push(ctx, []PushOperation{create})
	if err != nil || len(result.SentIdempotencyKeys) != 1 {
		t.Fatalf("idempotent replay = %+v, %v", result, err)
	}

	pulledA, err := transportA.Pull(ctx, Cursor{}, 100)
	if err != nil || len(pulledA.Operations) != 2 || pulledA.NextCursor.Token == "" {
		t.Fatalf("device A pull = %+v, %v", pulledA, err)
	}

	sessionB, err := authenticator.Authenticate(ctx, token, "contract-device-b")
	if err != nil {
		t.Fatalf("authenticate device B: %v", err)
	}
	providerB := &staticSessionProvider{session: sessionB}
	transportB, err := NewHTTPTransport(baseURL, providerB, nil)
	if err != nil {
		t.Fatal(err)
	}
	stale := backendOperation(t, vault, sessionB, "contract-record", "note", "stale-1", 1, OperationUpdate)
	conflicted, err := transportB.Push(ctx, []PushOperation{stale})
	if err != nil || len(conflicted.Conflicts) != 1 ||
		conflicted.Conflicts[0].LocalChangeKey != stale.IdempotencyKey {
		t.Fatalf("stale push = %+v, %v", conflicted, err)
	}

	pulledB, err := transportB.Pull(ctx, Cursor{}, 100)
	if err != nil || len(pulledB.Operations) != 2 {
		t.Fatalf("device B pull = %+v, %v", pulledB, err)
	}
}

func backendOperation(
	t *testing.T,
	vault Vault,
	session AuthSession,
	recordID string,
	recordType string,
	operationID string,
	version uint32,
	operation Operation,
) PushOperation {
	t.Helper()
	context := SyncContext{
		AccountID:       session.AccountID(),
		DeviceID:        session.DeviceID(),
		ProtocolVersion: SyncProtocolVersion,
		OperationID:     operationID,
		EntityVersion:   version,
	}
	envelope, err := SealPayload(
		vault, context, recordID, recordType, "contract-key", 1, operation, []byte("encrypted contract payload"))
	if err != nil {
		t.Fatal(err)
	}
	return PushOperation{
		IdempotencyKey: "idem-" + operationID,
		Operation:      operation,
		Envelope:       envelope,
	}
}
