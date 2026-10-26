package agentwork

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ManagedPackageSchemaVersion  = 1
	ManagedArtifactSchemaVersion = 1
	ManagedPackageKeyID          = "team-package"
	ManagedArtifactKeyID         = "team-artifact"
	ManagedPayloadKindPackage    = "context_package"
	ManagedPayloadKindArtifact   = "artifact"

	// These are implementation bounds, not approved product quotas or TEAM1
	// policy. A later payload phase can revise them with an explicit decision.
	ManagedPackageBodyMaxBytes    = 64 * 1024
	ManagedArtifactBodyMaxBytes   = 1_000_000
	ManagedPackageReferenceMax    = 100
	ManagedArtifactRangeMaxBytes  = 64 * 1024
	ManagedArtifactPageMax        = 100
	ManagedSealedEnvelopeMaxBytes = 1_500_000 // JSON/base64 envelope for <=1 MB plaintext.
)

var (
	ErrManagedPayloadLocked = errors.New("managed payload unavailable while vault is locked")
	ErrManagedPayloadScope  = errors.New("managed payload scope mismatch")
	ErrManagedPayloadStale  = errors.New("managed payload attempt is no longer current")
	ErrManagedPayloadLimit  = errors.New("managed payload exceeds implementation bounds")
)

// PayloadBinding is the complete authenticated identity for one encrypted
// body. The app codec maps it to Vault SealV2/OpenWithAD associated data.
// RecordType includes every field except RecordID and KeyID, so changing any
// scope, version, or payload kind makes authentication fail.
type PayloadBinding struct {
	RecordID             string
	WorkspaceID          string
	BoardID              string
	ParentTaskID         string
	GroupID              string
	WorkItemID           string
	TaskID               string
	AttemptID            string
	Generation           int64
	LeaseGeneration      int64
	RequirementsRevision string
	ArtifactKind         string
	ArtifactRole         string
	ArtifactProvenance   string
	ProducerSessionID    string
	SchemaVersion        int
	Kind                 string
}

func (b PayloadBinding) KeyID() (string, error) {
	switch b.Kind {
	case ManagedPayloadKindPackage:
		return ManagedPackageKeyID, nil
	case ManagedPayloadKindArtifact:
		return ManagedArtifactKeyID, nil
	default:
		return "", ErrManagedPayloadScope
	}
}

func (b PayloadBinding) RecordType() (string, error) {
	if b.RecordID == "" || b.WorkspaceID == "" || b.BoardID == "" || b.ParentTaskID == "" || b.GroupID == "" || b.WorkItemID == "" || b.TaskID == "" || b.AttemptID == "" || b.Generation < 1 || b.LeaseGeneration < 0 || b.RequirementsRevision == "" || b.SchemaVersion < 1 || (b.Kind == ManagedPayloadKindPackage && b.LeaseGeneration != 0) || (b.Kind == ManagedPayloadKindArtifact && b.LeaseGeneration < 1) {
		return "", ErrManagedPayloadScope
	}
	if b.Kind == ManagedPayloadKindPackage && (b.ArtifactKind != "" || b.ArtifactRole != "" || b.ArtifactProvenance != "" || b.ProducerSessionID != "") {
		return "", ErrManagedPayloadScope
	}
	if b.Kind == ManagedPayloadKindArtifact && (!validArtifactKind(b.ArtifactKind) || !validManagedArtifactRole(b.ArtifactRole) || b.ArtifactProvenance != "agent_claim" || b.ProducerSessionID == "") {
		return "", ErrManagedPayloadScope
	}
	if _, err := b.KeyID(); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Purpose              string `json:"purpose"`
		WorkspaceID          string `json:"workspaceId"`
		BoardID              string `json:"boardId"`
		ParentTaskID         string `json:"parentTaskId"`
		GroupID              string `json:"groupId"`
		WorkItemID           string `json:"workItemId"`
		TaskID               string `json:"taskId"`
		AttemptID            string `json:"attemptId"`
		Generation           int64  `json:"generation"`
		LeaseGeneration      int64  `json:"leaseGeneration"`
		RequirementsRevision string `json:"requirementsRevision"`
		ArtifactKind         string `json:"artifactKind,omitempty"`
		ArtifactRole         string `json:"artifactRole,omitempty"`
		ArtifactProvenance   string `json:"artifactProvenance,omitempty"`
		ProducerSessionID    string `json:"producerSessionId,omitempty"`
		SchemaVersion        int    `json:"schemaVersion"`
		Kind                 string `json:"kind"`
	}{"devkit-managed-payload-v1", b.WorkspaceID, b.BoardID, b.ParentTaskID, b.GroupID, b.WorkItemID, b.TaskID, b.AttemptID, b.Generation, b.LeaseGeneration, b.RequirementsRevision, b.ArtifactKind, b.ArtifactRole, b.ArtifactProvenance, b.ProducerSessionID, b.SchemaVersion, b.Kind})
	if err != nil {
		return "", err
	}
	return "devkit-managed-payload:" + base64.RawURLEncoding.EncodeToString(canonical), nil
}

// PayloadCodec is implemented by the app adapter around the unlocked vault.
// It accepts no caller-built crypto AD; the service constructs PayloadBinding.
type PayloadCodec interface {
	IsLocked() bool
	Seal(context.Context, PayloadBinding, []byte) ([]byte, error)
	Open(context.Context, PayloadBinding, []byte) ([]byte, error)
}

type PackageScope struct {
	WorkspaceID, BoardID, ParentTaskID, GroupID, WorkItemID, TaskID   string
	AttemptID                                                         string
	ArtifactKind, ArtifactRole, ArtifactProvenance, ProducerSessionID string
	Generation                                                        int64
	LeaseGeneration                                                   int64
	ItemRevision, AttemptRevision                                     int64
	RequirementsRevision                                              string
}

// PackageSource is returned only by the trusted task/role adapter. It has no
// transcript, comments, memory bodies, or arbitrary path fields by design.
type PackageSource struct {
	Requirements RequirementsSnapshot
	Role         string
	RoleContract string
	Output       PackageOutputContract
	Decisions    []PackageDecision
	References   []PackageReference
}

type PackageOutputContract struct {
	AllowedOutcomes []string `json:"allowedOutcomes"`
	EvidenceKinds   []string `json:"evidenceKinds"`
	EscalateOn      []string `json:"escalateOn,omitempty"`
}

type PackageDecision struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// PackageReference contains only an opaque record identifier and revision
// proof. It deliberately has no path, URL, body, or caller-provided scope.
type PackageReference struct {
	Kind        string `json:"kind"`
	RecordID    string `json:"recordId"`
	Revision    string `json:"revision"`
	Digest      string `json:"digest,omitempty"`
	WorkspaceID string `json:"workspaceId"`
	GroupID     string `json:"groupId,omitempty"`
}

type PackageHost interface {
	BuildPackageSource(context.Context, PackageScope) (PackageSource, error)
	CurrentRequirements(context.Context, PackageScope) (RequirementsSnapshot, error)
	AuthorizePackageBuild(context.Context, PackageScope) error
	AuthorizePackageRead(context.Context, PackageScope) error
	AuthorizeArtifactWrite(context.Context, PackageScope) error
	CurrentArtifactLeaseGeneration(context.Context, PackageScope) (int64, error)
	AuthorizeArtifactRead(context.Context, ArtifactMetadata) error
	AuthorizeArtifactList(context.Context, PackageScope) error
	// ResolvePackageReference returns the reference's current canonical form for
	// the work item's scope, or ErrManagedReferenceMissing, ...Foreign or
	// ...Unsupported. It never trusts the revision it was given.
	ResolvePackageReference(context.Context, PackageScope, PackageReference) (PackageReference, error)
}

type ContextPackage struct {
	ID, WorkspaceID, BoardID, ParentTaskID, GroupID, WorkItemID, TaskID string
	AttemptID, Role, RequirementsRevision                               string
	Generation                                                          int64
	SchemaVersion                                                       int
	BodyHash                                                            string
	BodyBytes                                                           int64
	CreatedAt                                                           time.Time
	Requirements                                                        RequirementsSnapshot  `json:"requirements"`
	RoleContract                                                        string                `json:"roleContract"`
	Output                                                              PackageOutputContract `json:"output"`
	Decisions                                                           []PackageDecision     `json:"decisions"`
	References                                                          []PackageReference    `json:"references"`
}

type StoredContextPackage struct {
	ContextPackage
	ItemRevision, AttemptRevision int64
	SealedBody                    []byte
}

type ArtifactMetadata struct {
	ID, WorkspaceID, BoardID, ParentTaskID, GroupID, WorkItemID, TaskID string
	AttemptID, Kind, Provenance, ProducerSessionID, Role                string
	RequirementsRevision                                                string
	Generation                                                          int64
	LeaseGeneration                                                     int64
	SchemaVersion                                                       int
	BodyHash                                                            string
	BodyBytes                                                           int64
	CreatedAt                                                           time.Time
}

type StoredArtifact struct {
	ArtifactMetadata
	ItemRevision, AttemptRevision int64
	SealedBody                    []byte
}

type ArtifactPage struct {
	Items []ArtifactMetadata
	Next  string
}

// PackageHost is the only source of task text, role contracts, output rules,
// selected references and caller authorization. Store methods independently
// fence writes against the item's current attempt and digest.
type ManagedPayloadStore interface {
	GetWorkGroup(context.Context, string) (GroupState, error)
	GetWorkItem(context.Context, string) (WorkItem, error)
	GetManagedAttempt(context.Context, string) (Attempt, error)
	GetCurrentManagedAttempt(context.Context, string) (Attempt, error)
	InsertContextPackage(context.Context, StoredContextPackage) error
	GetContextPackage(context.Context, string) (StoredContextPackage, error)
	InsertArtifact(context.Context, StoredArtifact) error
	GetArtifact(context.Context, string) (StoredArtifact, error)
	ListArtifacts(context.Context, string, string, int) (ArtifactPage, error)
}

type ManagedPayloadService struct {
	store  ManagedPayloadStore
	host   PackageHost
	codec  PayloadCodec
	newID  func() (string, error)
	limits PayloadLimits
}

type PayloadLimits struct {
	PackageBodyBytes   int
	ArtifactBodyBytes  int
	PackageReferences  int
	ArtifactRangeBytes int
	ArtifactPageItems  int
}

func DefaultPayloadLimits() PayloadLimits {
	return PayloadLimits{
		PackageBodyBytes: ManagedPackageBodyMaxBytes, ArtifactBodyBytes: ManagedArtifactBodyMaxBytes,
		PackageReferences: ManagedPackageReferenceMax, ArtifactRangeBytes: ManagedArtifactRangeMaxBytes,
		ArtifactPageItems: ManagedArtifactPageMax,
	}
}

func NewManagedPayloadService(store ManagedPayloadStore, host PackageHost, codec PayloadCodec) *ManagedPayloadService {
	return &ManagedPayloadService{store: store, host: host, codec: codec, newID: newManagedID, limits: DefaultPayloadLimits()}
}

func NewManagedPayloadServiceWithLimits(store ManagedPayloadStore, host PackageHost, codec PayloadCodec, limits PayloadLimits) (*ManagedPayloadService, error) {
	defaults := DefaultPayloadLimits()
	if limits.PackageBodyBytes < 1 || limits.PackageBodyBytes > defaults.PackageBodyBytes ||
		limits.ArtifactBodyBytes < 1 || limits.ArtifactBodyBytes > defaults.ArtifactBodyBytes ||
		limits.PackageReferences < 1 || limits.PackageReferences > defaults.PackageReferences ||
		limits.ArtifactRangeBytes < 1 || limits.ArtifactRangeBytes > defaults.ArtifactRangeBytes ||
		limits.ArtifactPageItems < 1 || limits.ArtifactPageItems > defaults.ArtifactPageItems {
		return nil, ErrManagedPayloadLimit
	}
	return &ManagedPayloadService{store: store, host: host, codec: codec, newID: newManagedID, limits: limits}, nil
}

// BuildContextPackage reads current trusted task inputs through the host,
// seals the complete package, and inserts it once for the reserved attempt.
func (s *ManagedPayloadService) BuildContextPackage(ctx context.Context, attemptID string) (ContextPackage, error) {
	if err := s.available(ctx); err != nil {
		return ContextPackage{}, err
	}
	a, item, scope, err := s.currentScope(ctx, attemptID)
	if err != nil {
		return ContextPackage{}, err
	}
	group, err := s.store.GetWorkGroup(ctx, item.GroupID)
	if err != nil {
		return ContextPackage{}, err
	}
	if a.State != "reserved" || !group.Enabled || group.Paused || group.EndedAt != nil || item.EndedAt != nil || strings.TrimSpace(item.TombstoneReason) != "" {
		return ContextPackage{}, ErrManagedPayloadStale
	}
	if err = s.host.AuthorizePackageBuild(ctx, scope); err != nil {
		return ContextPackage{}, err
	}
	source, err := s.host.BuildPackageSource(ctx, scope)
	if err != nil {
		return ContextPackage{}, err
	}
	if err = validatePackageSource(scope, source, s.limits); err != nil {
		return ContextPackage{}, err
	}
	source.References, err = canonicalReferences(source.References)
	if err != nil {
		return ContextPackage{}, err
	}
	if err = s.resolveReferences(ctx, scope, source.References); err != nil {
		return ContextPackage{}, err
	}
	digest, err := RequirementsDigest(source.Requirements)
	if err != nil {
		return ContextPackage{}, err
	}
	if digest != scope.RequirementsRevision || source.Role != item.Role || source.Role != a.Role {
		return ContextPackage{}, ErrManagedPayloadStale
	}
	id, err := s.newID()
	if err != nil {
		return ContextPackage{}, err
	}
	packageBody := ContextPackage{
		ID: id, WorkspaceID: scope.WorkspaceID, BoardID: scope.BoardID, ParentTaskID: scope.ParentTaskID,
		GroupID: scope.GroupID, WorkItemID: scope.WorkItemID, TaskID: scope.TaskID, AttemptID: scope.AttemptID,
		Role: source.Role, RequirementsRevision: digest, Generation: scope.Generation,
		SchemaVersion: ManagedPackageSchemaVersion, Requirements: source.Requirements,
		RoleContract: source.RoleContract, Output: source.Output,
		Decisions: append([]PackageDecision(nil), source.Decisions...), References: append([]PackageReference(nil), source.References...),
	}
	plain, err := json.Marshal(packageBody)
	if err != nil {
		return ContextPackage{}, err
	}
	if len(plain) == 0 || len(plain) > s.limits.PackageBodyBytes {
		return ContextPackage{}, ErrManagedPayloadLimit
	}
	binding := packageBinding(scope, id)
	sealed, err := s.codec.Seal(ctx, binding, plain)
	if err != nil {
		return ContextPackage{}, err
	}
	if len(sealed) == 0 || len(sealed) > ManagedSealedEnvelopeMaxBytes {
		return ContextPackage{}, ErrManagedPayloadLimit
	}
	meta := packageBody
	meta.Requirements = RequirementsSnapshot{}
	meta.RoleContract = ""
	meta.Output = PackageOutputContract{}
	meta.Decisions = nil
	meta.References = nil
	meta.BodyHash = sealedBodyHash(sealed)
	meta.BodyBytes = int64(len(plain))
	meta.CreatedAt = time.Now().UTC()
	if err = s.store.InsertContextPackage(ctx, StoredContextPackage{ContextPackage: meta, ItemRevision: scope.ItemRevision, AttemptRevision: scope.AttemptRevision, SealedBody: sealed}); err != nil {
		return ContextPackage{}, err
	}
	packageBody.BodyHash, packageBody.BodyBytes, packageBody.CreatedAt = meta.BodyHash, meta.BodyBytes, meta.CreatedAt
	return packageBody, nil
}

func (s *ManagedPayloadService) ReadContextPackage(ctx context.Context, id string) (ContextPackage, error) {
	if err := s.available(ctx); err != nil {
		return ContextPackage{}, err
	}
	record, err := s.store.GetContextPackage(ctx, id)
	if err != nil {
		return ContextPackage{}, err
	}
	scope := PackageScope{WorkspaceID: record.WorkspaceID, BoardID: record.BoardID, ParentTaskID: record.ParentTaskID, GroupID: record.GroupID, WorkItemID: record.WorkItemID, TaskID: record.TaskID, AttemptID: record.AttemptID, Generation: record.Generation, RequirementsRevision: record.RequirementsRevision}
	if err = s.host.AuthorizePackageRead(ctx, scope); err != nil {
		return ContextPackage{}, err
	}
	if sealedBodyHash(record.SealedBody) != record.BodyHash {
		return ContextPackage{}, ErrManagedPayloadScope
	}
	plain, err := s.codec.Open(ctx, packageBinding(scope, record.ID), record.SealedBody)
	if err != nil {
		return ContextPackage{}, err
	}
	if len(plain) == 0 || len(plain) > s.limits.PackageBodyBytes {
		return ContextPackage{}, ErrManagedPayloadLimit
	}
	var body ContextPackage
	if err = json.Unmarshal(plain, &body); err != nil {
		return ContextPackage{}, ErrManagedPayloadScope
	}
	if body.ID != record.ID || body.WorkspaceID != record.WorkspaceID || body.BoardID != record.BoardID || body.ParentTaskID != record.ParentTaskID || body.GroupID != record.GroupID || body.WorkItemID != record.WorkItemID || body.TaskID != record.TaskID || body.AttemptID != record.AttemptID || body.Role != record.Role || body.SchemaVersion != record.SchemaVersion || body.Generation != record.Generation || body.RequirementsRevision != record.RequirementsRevision || int64(len(plain)) != record.BodyBytes {
		return ContextPackage{}, ErrManagedPayloadScope
	}
	digest, digestErr := RequirementsDigest(body.Requirements)
	if digestErr != nil || digest != record.RequirementsRevision {
		return ContextPackage{}, ErrManagedPayloadScope
	}
	body.BodyHash, body.BodyBytes, body.CreatedAt = record.BodyHash, record.BodyBytes, record.CreatedAt
	return body, nil
}

// WriteArtifact stores caller bytes as an agent claim. There is intentionally
// no API parameter for host verification or a filesystem path.
func (s *ManagedPayloadService) WriteArtifact(ctx context.Context, attemptID, kind string, body []byte) (ArtifactMetadata, error) {
	if err := s.available(ctx); err != nil {
		return ArtifactMetadata{}, err
	}
	if !validArtifactKind(kind) || len(body) == 0 || len(body) > s.limits.ArtifactBodyBytes {
		return ArtifactMetadata{}, ErrManagedPayloadLimit
	}
	a, item, scope, err := s.currentScope(ctx, attemptID)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	if a.State != "running" || item.EndedAt != nil || strings.TrimSpace(item.TombstoneReason) != "" || a.RequirementsRevision != item.RequirementsRevision {
		return ArtifactMetadata{}, ErrManagedPayloadStale
	}
	currentRequirements, err := s.host.CurrentRequirements(ctx, scope)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	currentDigest, err := RequirementsDigest(currentRequirements)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	if currentDigest != scope.RequirementsRevision {
		return ArtifactMetadata{}, ErrManagedPayloadStale
	}
	if err = s.host.AuthorizeArtifactWrite(ctx, scope); err != nil {
		return ArtifactMetadata{}, err
	}
	leaseGeneration, err := s.host.CurrentArtifactLeaseGeneration(ctx, scope)
	if err != nil || leaseGeneration < 1 {
		if err != nil {
			return ArtifactMetadata{}, err
		}
		return ArtifactMetadata{}, ErrManagedPayloadStale
	}
	scope.LeaseGeneration = leaseGeneration
	id, err := s.newID()
	if err != nil {
		return ArtifactMetadata{}, err
	}
	meta := ArtifactMetadata{
		ID: id, WorkspaceID: scope.WorkspaceID, BoardID: scope.BoardID, ParentTaskID: scope.ParentTaskID,
		GroupID: scope.GroupID, WorkItemID: scope.WorkItemID, TaskID: scope.TaskID, AttemptID: scope.AttemptID,
		Kind: kind, Provenance: "agent_claim", ProducerSessionID: a.SessionID, Role: a.Role,
		RequirementsRevision: scope.RequirementsRevision, Generation: scope.Generation, LeaseGeneration: leaseGeneration,
		SchemaVersion: ManagedArtifactSchemaVersion, BodyHash: "", BodyBytes: int64(len(body)), CreatedAt: time.Now().UTC(),
	}
	scope.ArtifactKind, scope.ArtifactRole = meta.Kind, meta.Role
	scope.ArtifactProvenance, scope.ProducerSessionID = meta.Provenance, meta.ProducerSessionID
	sealed, err := s.codec.Seal(ctx, artifactBinding(scope, id), body)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	if len(sealed) == 0 || len(sealed) > ManagedSealedEnvelopeMaxBytes {
		return ArtifactMetadata{}, ErrManagedPayloadLimit
	}
	meta.BodyHash = sealedBodyHash(sealed)
	if err = s.store.InsertArtifact(ctx, StoredArtifact{ArtifactMetadata: meta, ItemRevision: scope.ItemRevision, AttemptRevision: scope.AttemptRevision, SealedBody: sealed}); err != nil {
		return ArtifactMetadata{}, err
	}
	return meta, nil
}

func (s *ManagedPayloadService) ReadArtifactRange(ctx context.Context, id string, offset int64, limit int) ([]byte, int64, error) {
	if err := s.available(ctx); err != nil {
		return nil, offset, err
	}
	if offset < 0 || limit < 1 || limit > s.limits.ArtifactRangeBytes {
		return nil, offset, ErrManagedPayloadLimit
	}
	record, err := s.store.GetArtifact(ctx, id)
	if err != nil {
		return nil, offset, err
	}
	if err = s.host.AuthorizeArtifactRead(ctx, record.ArtifactMetadata); err != nil {
		return nil, offset, err
	}
	if sealedBodyHash(record.SealedBody) != record.BodyHash {
		return nil, offset, ErrManagedPayloadScope
	}
	scope := PackageScope{WorkspaceID: record.WorkspaceID, BoardID: record.BoardID, ParentTaskID: record.ParentTaskID, GroupID: record.GroupID, WorkItemID: record.WorkItemID, TaskID: record.TaskID, AttemptID: record.AttemptID, Generation: record.Generation, LeaseGeneration: record.LeaseGeneration, RequirementsRevision: record.RequirementsRevision, ArtifactKind: record.Kind, ArtifactRole: record.Role, ArtifactProvenance: record.Provenance, ProducerSessionID: record.ProducerSessionID}
	plain, err := s.codec.Open(ctx, artifactBinding(scope, record.ID), record.SealedBody)
	if err != nil {
		return nil, offset, err
	}
	if int64(len(plain)) != record.BodyBytes || len(plain) > s.limits.ArtifactBodyBytes {
		return nil, offset, ErrManagedPayloadScope
	}
	if offset > int64(len(plain)) {
		return nil, offset, ErrManagedPayloadLimit
	}
	end := offset + int64(limit)
	if end > int64(len(plain)) {
		end = int64(len(plain))
	}
	return append([]byte(nil), plain[offset:end]...), end, nil
}

func (s *ManagedPayloadService) ListArtifacts(ctx context.Context, workItemID, after string, limit int) (ArtifactPage, error) {
	if err := s.available(ctx); err != nil {
		return ArtifactPage{}, err
	}
	if workItemID == "" || limit < 1 || limit > s.limits.ArtifactPageItems {
		return ArtifactPage{}, ErrManagedPayloadLimit
	}
	item, err := s.store.GetWorkItem(ctx, workItemID)
	if err != nil {
		return ArtifactPage{}, err
	}
	group, err := s.store.GetWorkGroup(ctx, item.GroupID)
	if err != nil {
		return ArtifactPage{}, err
	}
	scope := PackageScope{WorkspaceID: item.WorkspaceID, BoardID: item.BoardID, ParentTaskID: item.ParentTaskID, GroupID: item.GroupID, WorkItemID: item.ID, TaskID: item.TaskID}
	if group.WorkspaceID != scope.WorkspaceID || group.BoardID != scope.BoardID || group.ParentTaskID != scope.ParentTaskID {
		return ArtifactPage{}, ErrManagedPayloadScope
	}
	// Authorize the caller against the selected work item before loading any
	// artifact metadata. The host derives principal identity from ctx.
	if err = s.host.AuthorizeArtifactList(ctx, scope); err != nil {
		return ArtifactPage{}, err
	}
	return s.store.ListArtifacts(ctx, workItemID, after, limit)
}

func (s *ManagedPayloadService) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.store == nil || s.host == nil || s.codec == nil {
		return ErrManagedHostUnavailable
	}
	if s.codec.IsLocked() {
		return ErrManagedPayloadLocked
	}
	return nil
}

func (s *ManagedPayloadService) currentScope(ctx context.Context, attemptID string) (Attempt, WorkItem, PackageScope, error) {
	if strings.TrimSpace(attemptID) == "" {
		return Attempt{}, WorkItem{}, PackageScope{}, ErrManagedPayloadScope
	}
	a, err := s.store.GetManagedAttempt(ctx, attemptID)
	if err != nil {
		return Attempt{}, WorkItem{}, PackageScope{}, err
	}
	item, err := s.store.GetWorkItem(ctx, a.WorkItemID)
	if err != nil {
		return Attempt{}, WorkItem{}, PackageScope{}, err
	}
	current, err := s.store.GetCurrentManagedAttempt(ctx, item.ID)
	if err != nil {
		return Attempt{}, WorkItem{}, PackageScope{}, err
	}
	group, err := s.store.GetWorkGroup(ctx, item.GroupID)
	if err != nil {
		return Attempt{}, WorkItem{}, PackageScope{}, err
	}
	if current.ID != a.ID || current.Generation != a.Generation || current.Revision != a.Revision || current.State != a.State || item.CurrentAttemptID != a.ID || item.RequirementsRevision != a.RequirementsRevision || group.EndedAt != nil || group.WorkspaceID != item.WorkspaceID || group.BoardID != item.BoardID || group.ParentTaskID != item.ParentTaskID {
		return Attempt{}, WorkItem{}, PackageScope{}, ErrManagedPayloadStale
	}
	return a, item, PackageScope{
		WorkspaceID: item.WorkspaceID, BoardID: item.BoardID, ParentTaskID: item.ParentTaskID,
		GroupID: group.ID, WorkItemID: item.ID, TaskID: item.TaskID, AttemptID: a.ID,
		Generation: a.Generation, ItemRevision: item.Revision, AttemptRevision: a.Revision,
		RequirementsRevision: item.RequirementsRevision,
	}, nil
}

func validatePackageSource(scope PackageScope, source PackageSource, limits PayloadLimits) error {
	if source.Requirements.WorkspaceID != scope.WorkspaceID || source.Requirements.BoardID != scope.BoardID || source.Requirements.TaskID != scope.TaskID || source.Role == "" || source.RoleContract == "" {
		return ErrManagedPayloadScope
	}
	if len(source.References) > limits.PackageReferences {
		return ErrManagedPayloadLimit
	}
	for _, ref := range source.References {
		if !validReference(scope, ref) || !validReferenceDigest(ref) {
			return ErrManagedPayloadScope
		}
	}
	for _, decision := range source.Decisions {
		if decision.Kind != "requirement" && decision.Kind != "proposal" && decision.Kind != "assumption" {
			return ErrManagedPayloadScope
		}
	}
	return nil
}

func validReference(scope PackageScope, ref PackageReference) bool {
	if ref.RecordID == "" || strings.Contains(ref.RecordID, "/") || strings.Contains(ref.RecordID, "\\") || strings.Contains(ref.RecordID, "..") || strings.ContainsRune(ref.RecordID, '\x00') || len(ref.RecordID) > 128 {
		return false
	}
	if ref.WorkspaceID != scope.WorkspaceID {
		return false
	}
	switch ref.Kind {
	case "task", "document", "managed_artifact":
		if ref.Revision == "" {
			return false
		}
		if ref.Kind == "managed_artifact" {
			return ref.GroupID == scope.GroupID
		}
		return ref.GroupID == "" || ref.GroupID == scope.GroupID
	default:
		return false
	}
}

func validArtifactKind(kind string) bool {
	switch kind {
	case "file", "patch", "test_output", "image", "other":
		return true
	default:
		return false
	}
}

func packageBinding(scope PackageScope, id string) PayloadBinding {
	return PayloadBinding{RecordID: id, WorkspaceID: scope.WorkspaceID, BoardID: scope.BoardID, ParentTaskID: scope.ParentTaskID, GroupID: scope.GroupID, WorkItemID: scope.WorkItemID, TaskID: scope.TaskID, AttemptID: scope.AttemptID, Generation: scope.Generation, RequirementsRevision: scope.RequirementsRevision, SchemaVersion: ManagedPackageSchemaVersion, Kind: ManagedPayloadKindPackage}
}

func artifactBinding(scope PackageScope, id string) PayloadBinding {
	return PayloadBinding{RecordID: id, WorkspaceID: scope.WorkspaceID, BoardID: scope.BoardID, ParentTaskID: scope.ParentTaskID, GroupID: scope.GroupID, WorkItemID: scope.WorkItemID, TaskID: scope.TaskID, AttemptID: scope.AttemptID, Generation: scope.Generation, LeaseGeneration: scope.LeaseGeneration, RequirementsRevision: scope.RequirementsRevision, ArtifactKind: scope.ArtifactKind, ArtifactRole: scope.ArtifactRole, ArtifactProvenance: scope.ArtifactProvenance, ProducerSessionID: scope.ProducerSessionID, SchemaVersion: ManagedArtifactSchemaVersion, Kind: ManagedPayloadKindArtifact}
}

func validManagedArtifactRole(role string) bool {
	switch role {
	case "coordinator", "implementer", "reviewer", "integrator":
		return true
	default:
		return false
	}
}

func sealedBodyHash(sealed []byte) string {
	hash := sha256.Sum256(sealed)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func (m ContextPackage) ValidateMetadata() error {
	if err := ValidateScope(m.WorkspaceID, m.BoardID, m.ParentTaskID, m.TaskID); err != nil {
		return err
	}
	if m.ID == "" || m.GroupID == "" || m.WorkItemID == "" || m.AttemptID == "" || m.Role == "" || m.Generation < 1 || m.SchemaVersion != ManagedPackageSchemaVersion || m.RequirementsRevision == "" || m.BodyBytes < 1 || m.BodyBytes > ManagedPackageBodyMaxBytes || !validHash(m.BodyHash) {
		return fmt.Errorf("invalid managed context package metadata")
	}
	return nil
}

func (m ArtifactMetadata) Validate() error {
	if err := ValidateScope(m.WorkspaceID, m.BoardID, m.ParentTaskID, m.TaskID); err != nil {
		return err
	}
	if m.ID == "" || m.GroupID == "" || m.WorkItemID == "" || m.AttemptID == "" || m.Generation < 1 || m.LeaseGeneration < 1 || m.SchemaVersion != ManagedArtifactSchemaVersion || m.RequirementsRevision == "" || m.BodyBytes < 1 || m.BodyBytes > ManagedArtifactBodyMaxBytes || m.Provenance != "agent_claim" || m.ProducerSessionID == "" || m.Role == "" || !validArtifactKind(m.Kind) || !validHash(m.BodyHash) {
		return fmt.Errorf("invalid managed artifact metadata")
	}
	return nil
}

func validHash(hash string) bool {
	if !strings.HasPrefix(hash, "sha256:") || len(hash) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(hash, "sha256:"))
	return err == nil
}
