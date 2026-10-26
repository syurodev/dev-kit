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
	binding := agentwork.PayloadBinding{RecordID: "artifact-1", WorkspaceID: "ws-a", BoardID: "board-a", ParentTaskID: "parent-a", GroupID: "group-a", WorkItemID: "item-a", TaskID: "task-a", AttemptID: "attempt-a", Generation: 1, LeaseGeneration: 1, RequirementsRevision: "sha256:v2:abc", ArtifactKind: "patch", ArtifactRole: "implementer", ArtifactProvenance: "agent_claim", ProducerSessionID: "session-a", SchemaVersion: agentwork.ManagedArtifactSchemaVersion, Kind: agentwork.ManagedPayloadKindArtifact}
	sealed, err := codec.Seal(context.Background(), binding, []byte("claim"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := codec.Open(context.Background(), binding, sealed)
	if err != nil || string(plain) != "claim" {
		t.Fatalf("open = %q, %v", plain, err)
	}

	for name, tampered := range map[string]agentwork.PayloadBinding{
		"workspace": func() agentwork.PayloadBinding { b := binding; b.WorkspaceID = "ws-b"; return b }(),
		"attempt":   func() agentwork.PayloadBinding { b := binding; b.AttemptID = "attempt-b"; return b }(),
		"kind":      func() agentwork.PayloadBinding { b := binding; b.ArtifactKind = "file"; return b }(),
		"role":      func() agentwork.PayloadBinding { b := binding; b.ArtifactRole = "reviewer"; return b }(),
		"provenance": func() agentwork.PayloadBinding {
			b := binding
			b.ArtifactProvenance = "host_verified"
			return b
		}(),
		"producer session": func() agentwork.PayloadBinding {
			b := binding
			b.ProducerSessionID = "session-b"
			return b
		}(),
	} {
		if _, err := codec.Open(context.Background(), tampered, sealed); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
			t.Errorf("tampered %s open error = %v", name, err)
		}
	}

	v.Lock()
	if _, err := codec.Open(context.Background(), binding, sealed); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked open error = %v", err)
	}
}
