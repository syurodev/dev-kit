package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agentwork"
	"devkit/internal/crypto"
	"devkit/internal/storage"
	"devkit/internal/vault"
)

type managedPackageVaultCodec struct{ service *vault.Service }

func (c managedPackageVaultCodec) IsLocked() bool { return c.service == nil || c.service.IsLocked() }
func (c managedPackageVaultCodec) Seal(ctx context.Context, binding agentwork.PayloadBinding, plain []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ad, err := managedPackageAD(binding)
	if err != nil {
		return nil, err
	}
	envelope, err := c.service.SealV2(ad, plain)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}
func (c managedPackageVaultCodec) Open(ctx context.Context, binding agentwork.PayloadBinding, sealed []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var envelope crypto.Envelope
	if err := json.Unmarshal(sealed, &envelope); err != nil {
		return nil, err
	}
	ad, err := managedPackageAD(binding)
	if err != nil {
		return nil, err
	}
	return c.service.OpenWithAD(envelope, ad)
}

func managedPackageAD(binding agentwork.PayloadBinding) (crypto.AssociatedData, error) {
	recordType, err := binding.RecordType()
	if err != nil {
		return crypto.AssociatedData{}, err
	}
	keyID, err := binding.KeyID()
	if err != nil {
		return crypto.AssociatedData{}, err
	}
	return crypto.AssociatedData{RecordType: recordType, RecordID: binding.RecordID, KeyID: keyID}, nil
}

type managedPackageHostFake struct {
	source agentwork.PackageSource
	deny   error
}

func (h *managedPackageHostFake) BuildPackageSource(context.Context, agentwork.PackageScope) (agentwork.PackageSource, error) {
	return h.source, h.deny
}
func (h *managedPackageHostFake) CurrentRequirements(context.Context, agentwork.PackageScope) (agentwork.RequirementsSnapshot, error) {
	return h.source.Requirements, h.deny
}
func (h *managedPackageHostFake) AuthorizePackageBuild(context.Context, agentwork.PackageScope) error {
	return h.deny
}
func (h *managedPackageHostFake) AuthorizePackageRead(context.Context, agentwork.PackageScope) error {
	return h.deny
}
func (h *managedPackageHostFake) AuthorizeArtifactWrite(context.Context, agentwork.PackageScope) error {
	return h.deny
}
func (h *managedPackageHostFake) CurrentArtifactLeaseGeneration(context.Context, agentwork.PackageScope) (int64, error) {
	return 1, h.deny
}
func (h *managedPackageHostFake) AuthorizeArtifactRead(context.Context, agentwork.ArtifactMetadata) error {
	return h.deny
}
func (h *managedPackageHostFake) AuthorizeArtifactList(context.Context, agentwork.PackageScope) error {
	return h.deny
}

type managedPackageFixture struct {
	db      *sql.DB
	ledger  *storage.ManagedLedger
	group   agentwork.GroupState
	item    agentwork.WorkItem
	attempt agentwork.Attempt
	reqs    agentwork.RequirementsSnapshot
	codec   managedPackageVaultCodec
	service *agentwork.ManagedPayloadService
	host    *managedPackageHostFake
}

func newManagedPackageFixture(t *testing.T) *managedPackageFixture {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "managed-payloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	ledger := storage.NewManagedLedger(db)
	statusMap := agentwork.StatusMap{Ready: "todo", Running: "doing", AwaitingReview: "review", Accepted: "done"}
	group, err := ledger.CreateWorkGroup(ctx, agentwork.GroupState{ID: "payload-group", WorkspaceID: "payload-workspace", BoardID: "payload-board", ParentTaskID: "payload-parent", CoordinatorID: "coordinator-session", PolicyRevision: 1, SchemaVersion: agentwork.ManagedSchemaVersion, StatusMap: statusMap})
	if err != nil {
		t.Fatal(err)
	}
	group, err = ledger.UpdateGroupConfiguration(ctx, group.ID, group.Revision, 2, statusMap, true, false)
	if err != nil {
		t.Fatal(err)
	}
	reqs := agentwork.RequirementsSnapshot{WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, TaskID: "payload-task", Branch: "topic/payload", Description: "private-requirement-plaintext-marker", Scope: []string{"folder-id"}, Dependencies: []string{"dependency-task"}}
	reqRevision, err := agentwork.RequirementsDigest(reqs)
	if err != nil {
		t.Fatal(err)
	}
	item, err := ledger.CreateWorkItem(ctx, agentwork.WorkItem{ID: "payload-item", GroupID: group.ID, WorkspaceID: group.WorkspaceID, BoardID: group.BoardID, ParentTaskID: group.ParentTaskID, TaskID: reqs.TaskID, Role: "implementer", TaskRevision: 1, RequirementsRevision: reqRevision})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ledger.BeginAttempt(ctx, agentwork.Attempt{ID: "payload-attempt", WorkItemID: item.ID, SessionID: "worker-session", TaskRevision: item.TaskRevision, RequirementsRevision: item.RequirementsRevision}, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	vaultService := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err = vaultService.Setup("payload-test-master-password"); err != nil {
		t.Fatal(err)
	}
	codec := managedPackageVaultCodec{service: vaultService}
	host := &managedPackageHostFake{source: agentwork.PackageSource{Requirements: reqs, Role: "implementer", RoleContract: "Only implement the scoped task", Output: agentwork.PackageOutputContract{AllowedOutcomes: []string{"completed", "blocked"}, EvidenceKinds: []string{"tests"}}}}
	service := agentwork.NewManagedPayloadService(ledger, host, codec)
	return &managedPackageFixture{db: db, ledger: ledger, group: group, item: item, attempt: attempt, reqs: reqs, codec: codec, service: service, host: host}
}

func TestManagedPackageIsEncryptedImmutableAndBoundToFullScope(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	marker := "private-requirement-plaintext-marker"
	packageRecord, err := f.service.BuildContextPackage(ctx, f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packageRecord.Requirements.Description != marker {
		t.Fatalf("package content was not returned: %#v", packageRecord.Requirements)
	}
	var sealed []byte
	if err = f.db.QueryRowContext(ctx, `SELECT sealed_body FROM agent_work_packages WHERE id=?`, packageRecord.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(marker)) {
		t.Fatal("plaintext task requirements are present in the package row")
	}
	var plainRows int
	if err = f.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_work_packages WHERE instr(CAST(sealed_body AS TEXT), ?) > 0`, marker).Scan(&plainRows); err != nil {
		t.Fatal(err)
	}
	if plainRows != 0 {
		t.Fatal("plaintext task requirements are queryable in SQLite")
	}
	stored, err := f.ledger.GetContextPackage(ctx, packageRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := f.codec.Open(ctx, agentwork.PayloadBinding{RecordID: stored.ID, WorkspaceID: stored.WorkspaceID, BoardID: stored.BoardID, ParentTaskID: stored.ParentTaskID, GroupID: stored.GroupID, WorkItemID: stored.WorkItemID, TaskID: stored.TaskID, AttemptID: stored.AttemptID, Generation: stored.Generation, RequirementsRevision: stored.RequirementsRevision, SchemaVersion: stored.SchemaVersion, Kind: agentwork.ManagedPayloadKindPackage}, stored.SealedBody)
	if err != nil || !bytes.Contains(plain, []byte(marker)) {
		t.Fatalf("vault open failed or did not recover package text: err=%v", err)
	}
	wrongScope := agentwork.PayloadBinding{RecordID: stored.ID, WorkspaceID: "different-workspace", BoardID: stored.BoardID, ParentTaskID: stored.ParentTaskID, GroupID: stored.GroupID, WorkItemID: stored.WorkItemID, TaskID: stored.TaskID, AttemptID: stored.AttemptID, Generation: stored.Generation, RequirementsRevision: stored.RequirementsRevision, SchemaVersion: stored.SchemaVersion, Kind: agentwork.ManagedPayloadKindPackage}
	if _, err = f.codec.Open(ctx, wrongScope, stored.SealedBody); err == nil {
		t.Fatal("envelope opened under a different workspace binding")
	}
	if _, err = f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, storage.ErrManagedConflict) {
		t.Fatalf("second package for same attempt = %v, want duplicate conflict", err)
	}
}

func TestManagedPackageServiceReadsArtifactsInBoundedRangesAndHonorsLock(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	packageRecord, err := f.service.BuildContextPackage(ctx, f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"session_created", "task_assigned", "prompt_enqueued", "running"} {
		f.attempt, err = f.ledger.TransitionAttempt(ctx, f.attempt.ID, f.attempt.Revision, state)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertActiveArtifactLease(t, f)
	artifactBytes := bytes.Repeat([]byte("artifact-marker"), 1000)
	artifact, err := f.service.WriteArtifact(ctx, f.attempt.ID, "test_output", artifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err = f.db.QueryRowContext(ctx, `SELECT sealed_body FROM agent_work_artifacts WHERE id=?`, artifact.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("artifact-marker")) {
		t.Fatal("artifact plaintext is present in the database")
	}
	storedArtifact, err := f.ledger.GetArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	wrongLease := agentwork.PayloadBinding{RecordID: storedArtifact.ID, WorkspaceID: storedArtifact.WorkspaceID, BoardID: storedArtifact.BoardID, ParentTaskID: storedArtifact.ParentTaskID, GroupID: storedArtifact.GroupID, WorkItemID: storedArtifact.WorkItemID, TaskID: storedArtifact.TaskID, AttemptID: storedArtifact.AttemptID, Generation: storedArtifact.Generation, LeaseGeneration: storedArtifact.LeaseGeneration + 1, RequirementsRevision: storedArtifact.RequirementsRevision, SchemaVersion: storedArtifact.SchemaVersion, Kind: agentwork.ManagedPayloadKindArtifact}
	if _, err = f.codec.Open(ctx, wrongLease, storedArtifact.SealedBody); err == nil {
		t.Fatal("artifact opened under a different writer lease generation")
	}
	first, next, err := f.service.ReadArtifactRange(ctx, artifact.ID, 0, 4096)
	if err != nil || len(first) != 4096 || next != 4096 || !bytes.Equal(first, artifactBytes[:4096]) {
		t.Fatalf("first range len=%d next=%d err=%v", len(first), next, err)
	}
	second, next, err := f.service.ReadArtifactRange(ctx, artifact.ID, next, 4096)
	if err != nil || len(second) != 4096 || next != 8192 || !bytes.Equal(second, artifactBytes[4096:8192]) {
		t.Fatalf("second range len=%d next=%d err=%v", len(second), next, err)
	}
	if _, _, err = f.service.ReadArtifactRange(ctx, artifact.ID, 0, agentwork.ManagedArtifactRangeMaxBytes+1); !errors.Is(err, agentwork.ErrManagedPayloadLimit) {
		t.Fatalf("oversized read range = %v", err)
	}
	page, err := f.service.ListArtifacts(ctx, f.item.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Provenance != "agent_claim" {
		t.Fatalf("artifact metadata page = %#v, err=%v", page, err)
	}
	if _, err = f.service.WriteArtifact(ctx, f.attempt.ID, "host_verify", []byte("forged")); !errors.Is(err, agentwork.ErrManagedPayloadLimit) {
		t.Fatalf("caller-selected provenance kind = %v", err)
	}
	if _, err = f.db.ExecContext(ctx, `UPDATE agent_work_leases SET expires_at=? WHERE attempt_id=?`, time.Now().UTC().Add(-time.Nanosecond).Format(time.RFC3339Nano), f.attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.WriteArtifact(ctx, f.attempt.ID, "test_output", []byte("expired lease")); !errors.Is(err, agentwork.ErrManagedPayloadStale) {
		t.Fatalf("artifact write with expired lease = %v", err)
	}
	f.codec.service.Lock()
	if _, _, err = f.service.ReadArtifactRange(ctx, artifact.ID, 0, 10); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked artifact read = %v", err)
	}
	if _, err = f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadLocked) {
		t.Fatalf("locked package write = %v", err)
	}
	_ = packageRecord
}

func insertActiveArtifactLease(t *testing.T, f *managedPackageFixture) {
	t.Helper()
	now := time.Now().UTC()
	_, err := f.db.ExecContext(context.Background(), `INSERT INTO agent_work_leases
		(id,schema_version,workspace_id,board_id,parent_task_id,task_id,group_id,work_item_id,attempt_id,attempt_generation,session_id,resource_key,generation,revision,heartbeat_at,expires_at,created_at,released_at,release_reason)
		VALUES(?,1,?,?,?,?,?,?,?,?,?,?,1,1,?,?,?,NULL,'')`,
		"lease-"+f.attempt.ID, f.group.WorkspaceID, f.group.BoardID, f.group.ParentTaskID, f.item.TaskID, f.group.ID, f.item.ID, f.attempt.ID, f.attempt.Generation, f.attempt.SessionID, "resource-"+f.attempt.ID,
		now.Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagedPayloadRejectsStaleAttemptAndUnsafeReference(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	f.host.source.References = []agentwork.PackageReference{{Kind: "document", RecordID: "../outside", Revision: "rev-1", WorkspaceID: f.group.WorkspaceID}}
	if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("unsafe package reference = %v", err)
	}
	for _, state := range []string{"session_created", "task_assigned", "prompt_enqueued", "running"} {
		var err error
		f.attempt, err = f.ledger.TransitionAttempt(ctx, f.attempt.ID, f.attempt.Revision, state)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadStale) {
		t.Fatalf("build after dispatch started = %v", err)
	}
	// A digest change while the original attempt remains pinned fences writes.
	item, err := f.ledger.GetWorkItem(ctx, f.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed := sha256.Sum256([]byte("changed requirements"))
	newDigest := "sha256:" + hex.EncodeToString(changed[:])
	item, err = f.ledger.RefreshWorkItemRequirements(ctx, item.ID, item.Revision, item.TaskRevision+1, newDigest)
	if err != nil {
		t.Fatal(err)
	}
	sealed := []byte("ciphertext")
	sealedHash := sha256.Sum256(sealed)
	artifact := agentwork.StoredArtifact{ArtifactMetadata: agentwork.ArtifactMetadata{ID: "stale-artifact", WorkspaceID: f.group.WorkspaceID, BoardID: f.group.BoardID, ParentTaskID: f.group.ParentTaskID, GroupID: f.group.ID, WorkItemID: item.ID, TaskID: item.TaskID, AttemptID: f.attempt.ID, Kind: "test_output", Provenance: "agent_claim", ProducerSessionID: f.attempt.SessionID, Role: f.attempt.Role, RequirementsRevision: f.attempt.RequirementsRevision, Generation: f.attempt.Generation, LeaseGeneration: 1, SchemaVersion: agentwork.ManagedArtifactSchemaVersion, BodyHash: "sha256:" + hex.EncodeToString(sealedHash[:]), BodyBytes: 10, CreatedAt: time.Now()}, ItemRevision: item.Revision, AttemptRevision: f.attempt.Revision, SealedBody: sealed}
	if err = f.ledger.InsertArtifact(ctx, artifact); !errors.Is(err, agentwork.ErrManagedPayloadStale) {
		t.Fatalf("stale attempt artifact insert = %v", err)
	}
}
