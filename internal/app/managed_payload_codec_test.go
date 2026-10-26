package app

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agentwork"
	"devkit/internal/vault"
)

func TestManagedVaultPayloadCodecAuthenticatesFullScope(t *testing.T) {
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("managed-payload-test"); err != nil {
		t.Fatal(err)
	}
	codec := managedVaultPayloadCodec{vault: v}
	binding := agentwork.PayloadBinding{RecordID: "artifact-1", WorkspaceID: "ws-a", BoardID: "board-a", ParentTaskID: "parent-a", GroupID: "group-a", WorkItemID: "item-a", TaskID: "task-a", AttemptID: "attempt-a", Generation: 1, LeaseGeneration: 1, RequirementsRevision: "sha256:v2:abc", SchemaVersion: agentwork.ManagedArtifactSchemaVersion, Kind: agentwork.ManagedPayloadKindArtifact}
	sealed, err := codec.Seal(context.Background(), binding, []byte("claim"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := codec.Open(context.Background(), binding, sealed)
	if err != nil || string(plain) != "claim" {
		t.Fatalf("open = %q, %v", plain, err)
	}

	tampered := binding
	tampered.WorkspaceID = "ws-b"
	if _, err := codec.Open(context.Background(), tampered, sealed); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("cross-workspace open error = %v", err)
	}
	tampered = binding
	tampered.AttemptID = "attempt-b"
	if _, err := codec.Open(context.Background(), tampered, sealed); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("cross-attempt open error = %v", err)
	}

	v.Lock()
	if _, err := codec.Open(context.Background(), binding, sealed); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked open error = %v", err)
	}
}
