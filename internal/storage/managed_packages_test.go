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
	// current overrides what a reference resolves to now (keyed kind/id), and
	// resolveErr what resolving it answers. Unlisted references resolve to
	// themselves.
	current    map[string]agentwork.PackageReference
	resolveErr map[string]error
	resolved   int
}

func (h *managedPackageHostFake) ResolvePackageReference(_ context.Context, _ agentwork.PackageScope, ref agentwork.PackageReference) (agentwork.PackageReference, error) {
	h.resolved++
	key := ref.Kind + "/" + ref.RecordID
	if err := h.resolveErr[key]; err != nil {
		return agentwork.PackageReference{}, err
	}
	if current, ok := h.current[key]; ok {
		return current, nil
	}
	return ref, nil
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

func TestManagedPackageReadRejectsTamperedPlaintextSizeMetadata(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	packageRecord, err := f.service.BuildContextPackage(ctx, f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.ExecContext(ctx, `UPDATE agent_work_packages SET body_size=body_size+1 WHERE id=?`, packageRecord.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ReadContextPackage(ctx, packageRecord.ID); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("package read with mismatched body size = %v", err)
	}
}

func TestManagedArtifactInsertRechecksGroupPolicyInTransaction(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	for _, state := range []string{"session_created", "task_assigned", "prompt_enqueued", "running"} {
		var err error
		f.attempt, err = f.ledger.TransitionAttempt(ctx, f.attempt.ID, f.attempt.Revision, state)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertActiveArtifactLease(t, f)
	group, err := f.ledger.GetWorkGroup(ctx, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ledger.UpdateGroupPolicy(ctx, group.ID, group.Revision, group.PolicyRevision+1, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.WriteArtifact(ctx, f.attempt.ID, "test_output", []byte("must not persist while paused")); !errors.Is(err, agentwork.ErrManagedPayloadStale) {
		t.Fatalf("artifact inserted into paused group = %v", err)
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
	wrongLease := agentwork.PayloadBinding{RecordID: storedArtifact.ID, WorkspaceID: storedArtifact.WorkspaceID, BoardID: storedArtifact.BoardID, ParentTaskID: storedArtifact.ParentTaskID, GroupID: storedArtifact.GroupID, WorkItemID: storedArtifact.WorkItemID, TaskID: storedArtifact.TaskID, AttemptID: storedArtifact.AttemptID, Generation: storedArtifact.Generation, LeaseGeneration: storedArtifact.LeaseGeneration + 1, RequirementsRevision: storedArtifact.RequirementsRevision, ArtifactKind: storedArtifact.Kind, ArtifactRole: storedArtifact.Role, ArtifactProvenance: storedArtifact.Provenance, ProducerSessionID: storedArtifact.ProducerSessionID, SchemaVersion: storedArtifact.SchemaVersion, Kind: agentwork.ManagedPayloadKindArtifact}
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

func taskRef(f *managedPackageFixture, id, digest string) agentwork.PackageReference {
	return agentwork.PackageReference{Kind: agentwork.ManagedReferenceTask, RecordID: id, Revision: digest, WorkspaceID: f.group.WorkspaceID, GroupID: f.group.ID}
}

func artifactRef(f *managedPackageFixture, id string) agentwork.PackageReference {
	return agentwork.PackageReference{Kind: agentwork.ManagedReferenceArtifact, RecordID: id, Revision: "v1.g1", Digest: "sha256:" + hex.EncodeToString(make([]byte, 32)), WorkspaceID: f.group.WorkspaceID, GroupID: f.group.ID}
}

func TestManagedPackageSealsCanonicalResolvedReferences(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	b, a := taskRef(f, "task-b", "sha256:v2:bb"), artifactRef(f, "artifact-a")
	f.host.source.References = []agentwork.PackageReference{b, a}
	built, err := f.service.BuildContextPackage(ctx, f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.host.resolved != 2 {
		t.Fatalf("host resolved %d references, want each checked once", f.host.resolved)
	}
	read, err := f.service.ReadContextPackage(ctx, built.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by kind then id regardless of the order the host listed them in.
	want := []agentwork.PackageReference{a, b}
	if len(read.References) != 2 || read.References[0] != want[0] || read.References[1] != want[1] {
		t.Fatalf("stored references = %+v, want %+v", read.References, want)
	}
}

func TestManagedPackageBuildRefusesAReferenceThatIsNotCurrent(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(f *managedPackageFixture, ref agentwork.PackageReference)
		want error
	}{
		{"revision changed", func(f *managedPackageFixture, ref agentwork.PackageReference) {
			changed := ref
			changed.Revision = "sha256:v2:other"
			f.host.current = map[string]agentwork.PackageReference{"task/task-b": changed}
		}, agentwork.ErrManagedReferenceStale},
		{"missing", func(f *managedPackageFixture, _ agentwork.PackageReference) {
			f.host.resolveErr = map[string]error{"task/task-b": agentwork.ErrManagedReferenceMissing}
		}, agentwork.ErrManagedReferenceMissing},
		{"foreign", func(f *managedPackageFixture, _ agentwork.PackageReference) {
			f.host.resolveErr = map[string]error{"task/task-b": agentwork.ErrManagedReferenceForeign}
		}, agentwork.ErrManagedReferenceForeign},
		{"unsupported kind", func(f *managedPackageFixture, _ agentwork.PackageReference) {
			f.host.resolveErr = map[string]error{"task/task-b": agentwork.ErrManagedReferenceUnsupported}
		}, agentwork.ErrManagedReferenceUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newManagedPackageFixture(t)
			ctx := context.Background()
			ref := taskRef(f, "task-b", "sha256:v2:bb")
			f.host.source.References = []agentwork.PackageReference{ref}
			tc.set(f, ref)
			if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, tc.want) {
				t.Fatalf("build = %v, want %v", err, tc.want)
			}
			var n int
			if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_work_packages`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("a package was stored for a refused build: rows=%d err=%v", n, err)
			}
			// The refusal does not consume the attempt: a corrected selection builds.
			f.host.current, f.host.resolveErr = nil, nil
			if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); err != nil {
				t.Fatalf("rebuild after correction: %v", err)
			}
		})
	}
}

func TestManagedPackageRejectsDuplicateAndUnboundArtifactReferences(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	dup := taskRef(f, "task-b", "sha256:v2:bb")
	other := dup
	other.Revision = "sha256:v2:cc"
	f.host.source.References = []agentwork.PackageReference{dup, other}
	if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("same record listed twice = %v", err)
	}
	unbound := artifactRef(f, "artifact-a")
	unbound.Digest = ""
	f.host.source.References = []agentwork.PackageReference{unbound}
	if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("artifact reference without a hash = %v", err)
	}
	foreign := taskRef(f, "task-b", "sha256:v2:bb")
	foreign.WorkspaceID = "another-workspace"
	f.host.source.References = []agentwork.PackageReference{foreign}
	if _, err := f.service.BuildContextPackage(ctx, f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadScope) {
		t.Fatalf("other-workspace reference = %v", err)
	}
}

func TestManagedPackageReferencesTooManyAreRefusedNotTruncated(t *testing.T) {
	f := newManagedPackageFixture(t)
	var refs []agentwork.PackageReference
	for i := 0; i <= agentwork.ManagedPackageReferenceMax; i++ {
		refs = append(refs, taskRef(f, "task-"+string(rune('a'+i%26))+string(rune('a'+i/26)), "sha256:v2:x"))
	}
	f.host.source.References = refs
	if _, err := f.service.BuildContextPackage(context.Background(), f.attempt.ID); !errors.Is(err, agentwork.ErrManagedPayloadLimit) {
		t.Fatalf("over the reference bound = %v", err)
	}
}

func TestCheckPackageReferencesReportsEachStateWithoutChangingThePackage(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	refs := []agentwork.PackageReference{
		taskRef(f, "task-current", "sha256:v2:a"), taskRef(f, "task-stale", "sha256:v2:b"),
		taskRef(f, "task-missing", "sha256:v2:c"), taskRef(f, "task-foreign", "sha256:v2:d"),
		taskRef(f, "task-unsupported", "sha256:v2:e"),
	}
	f.host.source.References = refs
	built, err := f.service.BuildContextPackage(ctx, f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := refs[1]
	stale.Revision = "sha256:v2:moved"
	f.host.current = map[string]agentwork.PackageReference{"task/task-stale": stale}
	f.host.resolveErr = map[string]error{
		"task/task-missing": agentwork.ErrManagedReferenceMissing, "task/task-foreign": agentwork.ErrManagedReferenceForeign,
		"task/task-unsupported": agentwork.ErrManagedReferenceUnsupported,
	}
	before, err := f.ledger.GetContextPackage(ctx, built.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.service.CheckPackageReferences(ctx, built.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, status := range got {
		states[status.RecordID] = status.State
	}
	want := map[string]string{"task-current": agentwork.ReferenceCurrent, "task-stale": agentwork.ReferenceStale, "task-missing": agentwork.ReferenceMissing, "task-foreign": agentwork.ReferenceForeign, "task-unsupported": agentwork.ReferenceUnsupported}
	if len(states) != len(want) {
		t.Fatalf("states = %v", states)
	}
	for id, state := range want {
		if states[id] != state {
			t.Errorf("%s = %q, want %q", id, states[id], state)
		}
	}
	after, err := f.ledger.GetContextPackage(ctx, built.ID)
	if err != nil || !bytes.Equal(before.SealedBody, after.SealedBody) || before.BodyHash != after.BodyHash {
		t.Fatalf("the stored package changed: %v", err)
	}
	// A host failure is an error, never a state.
	f.host.resolveErr = map[string]error{"task/task-current": errors.New("host down")}
	if _, err = f.service.CheckPackageReferences(ctx, built.ID); err == nil {
		t.Fatal("a host failure was reported as a reference state")
	}
}

func TestGetWorkItemByTaskIsGroupScoped(t *testing.T) {
	f := newManagedPackageFixture(t)
	ctx := context.Background()
	got, err := f.ledger.GetWorkItemByTask(ctx, f.group.ID, f.item.TaskID)
	if err != nil || got.ID != f.item.ID {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
	// Another group, an unknown task and empty ids never match; absence is the
	// shared sentinel a host can classify without importing storage.
	for _, c := range [][2]string{{"other-group", f.item.TaskID}, {f.group.ID, "no-such-task"}} {
		if _, err = f.ledger.GetWorkItemByTask(ctx, c[0], c[1]); !errors.Is(err, agentwork.ErrManagedRecordNotFound) {
			t.Fatalf("lookup %v = %v, want not found", c, err)
		}
	}
	if _, err = f.ledger.GetWorkItemByTask(ctx, "", f.item.TaskID); !errors.Is(err, storage.ErrManagedInvalid) {
		t.Fatalf("empty group = %v", err)
	}
}
