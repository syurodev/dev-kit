package agentwork

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// Binding states reported by CheckBinding.
const (
	HarnessStateCurrent = "current"
	HarnessStateStale   = "stale"
	HarnessStateMissing = "missing"
)

// HarnessBindingStatus is a report; producing it changes nothing. It carries
// ids, a state, a phase and a fixed code only.
type HarnessBindingStatus struct {
	AttemptID, Phase, State, Code string
	// RebuildRequired is true when the bound configuration no longer matches
	// and the supported remedy is a new attempt, package and binding. Nothing
	// here performs that rebuild or overwrites a stored binding.
	RebuildRequired bool
}

// ManagedHarnessService binds an attempt to the harness configuration it was
// meant to run with and the one its child runs with. The binding is metadata
// only (ids, revisions, digests, group names), so it needs no vault. The
// service never sends, queues, enables a group or changes a model or account.
type ManagedHarnessService struct {
	store ManagedHarnessStore
	host  HarnessHost
	newID func() (string, error)
	now   func() time.Time
}

func NewManagedHarnessService(store ManagedHarnessStore, host HarnessHost) *ManagedHarnessService {
	return &ManagedHarnessService{store: store, host: host, newID: newManagedID, now: time.Now}
}

// hostFailure turns anything the host returns into a refusal. A refusal the
// host chose passes through; everything else is "unavailable" and never a
// default, and the cause is not carried (it may name a path or a value).
func hostFailure(err error) error {
	var r *HarnessRefusal
	if errors.As(err, &r) {
		return r
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return refusal(HarnessUnavailable)
}

func (s *ManagedHarnessService) hostConfigured() error {
	if s == nil || s.store == nil || s.host == nil {
		return refusal(HarnessNotConfigured)
	}
	return nil
}

type attemptContext struct {
	attempt Attempt
	item    WorkItem
	group   GroupState
}

func (c attemptContext) scope() PackageScope {
	return PackageScope{
		WorkspaceID: c.attempt.WorkspaceID, BoardID: c.attempt.BoardID, ParentTaskID: c.attempt.ParentTaskID,
		GroupID: c.attempt.GroupID, WorkItemID: c.attempt.WorkItemID, TaskID: c.item.TaskID, AttemptID: c.attempt.ID,
		Generation: c.attempt.Generation, ItemRevision: c.item.Revision, AttemptRevision: c.attempt.Revision,
		RequirementsRevision: c.attempt.RequirementsRevision,
	}
}

// load reads the attempt and requires it to be the item's current attempt in
// an open group. Store errors are returned as they are.
func (s *ManagedHarnessService) load(ctx context.Context, attemptID string) (attemptContext, error) {
	var c attemptContext
	if attemptID == "" {
		return c, ErrManagedPayloadScope
	}
	attempt, err := s.store.GetManagedAttempt(ctx, attemptID)
	if err != nil {
		return c, err
	}
	item, err := s.store.GetWorkItem(ctx, attempt.WorkItemID)
	if err != nil {
		return c, err
	}
	current, err := s.store.GetCurrentManagedAttempt(ctx, item.ID)
	if err != nil {
		return c, err
	}
	if current.ID != attempt.ID || item.CurrentAttemptID != attempt.ID {
		return c, ErrManagedPayloadStale
	}
	group, err := s.store.GetWorkGroup(ctx, attempt.GroupID)
	if err != nil {
		return c, err
	}
	if attempt.SchemaVersion != ManagedSchemaVersion || item.SchemaVersion != ManagedSchemaVersion || group.SchemaVersion != ManagedSchemaVersion {
		return c, refusal(HarnessSchemaUnsupported)
	}
	if item.WorkspaceID != attempt.WorkspaceID || item.GroupID != attempt.GroupID || group.ID != attempt.GroupID || group.WorkspaceID != attempt.WorkspaceID {
		return c, ErrManagedPayloadScope
	}
	return attemptContext{attempt: attempt, item: item, group: group}, nil
}

func (c attemptContext) open() error {
	if !c.group.Enabled || c.group.Paused || c.group.EndedAt != nil || c.item.EndedAt != nil || c.item.TombstoneReason != "" {
		return ErrHarnessPolicyClosed
	}
	return nil
}

func (s *ManagedHarnessService) requiredGroups(ctx context.Context, role string) ([]string, error) {
	groups, err := s.host.RequiredGroups(ctx, role)
	if err != nil {
		return nil, hostFailure(err)
	}
	groups = normalizeGroups(groups)
	if len(groups) == 0 {
		// A role with no requirement would make the check vacuous.
		return nil, refusal(HarnessUnavailable)
	}
	return groups, nil
}

func (s *ManagedHarnessService) preflightIntended(ctx context.Context, c attemptContext, child IntendedChild) (HarnessResolution, []string, error) {
	if child.Vendor == "" || child.Role == "" || child.Role != c.attempt.Role {
		return HarnessResolution{}, nil, refusal(HarnessChildScope)
	}
	required, err := s.requiredGroups(ctx, c.attempt.Role)
	if err != nil {
		return HarnessResolution{}, nil, err
	}
	res, err := s.host.ResolveIntended(ctx, c.scope(), child)
	if err != nil {
		return HarnessResolution{}, nil, hostFailure(err)
	}
	res.Groups = normalizeGroups(res.Groups)
	if err = checkResolution(HarnessPhaseIntended, res, required, nil); err != nil {
		return HarnessResolution{}, nil, err
	}
	return res, required, nil
}

// PreflightIntended resolves the configuration a fresh child of this role and
// vendor would get (workspace and vendor layers only, never an override) and
// checks the role's required groups. Read only; call it before the child
// exists. A missing group returns a configuration-needed refusal and enables
// nothing.
func (s *ManagedHarnessService) PreflightIntended(ctx context.Context, attemptID string, child IntendedChild) (HarnessResolution, error) {
	if err := s.hostConfigured(); err != nil {
		return HarnessResolution{}, err
	}
	c, err := s.load(ctx, attemptID)
	if err != nil {
		return HarnessResolution{}, err
	}
	if c.attempt.State != "reserved" {
		return HarnessResolution{}, ErrHarnessAttemptState
	}
	if err = c.open(); err != nil {
		return HarnessResolution{}, err
	}
	res, _, err := s.preflightIntended(ctx, c, child)
	return res, err
}

func (s *ManagedHarnessService) newBinding(c attemptContext, phase, packageID string, required []string, facts HarnessFacts) (HarnessBinding, error) {
	id, err := s.newID()
	if err != nil {
		return HarnessBinding{}, err
	}
	b := HarnessBinding{
		ID: id, SchemaVersion: HarnessBindingSchemaVersion, Phase: phase,
		WorkspaceID: c.attempt.WorkspaceID, BoardID: c.attempt.BoardID, ParentTaskID: c.attempt.ParentTaskID,
		GroupID: c.attempt.GroupID, WorkItemID: c.attempt.WorkItemID, TaskID: c.item.TaskID, AttemptID: c.attempt.ID,
		PackageID: packageID, Role: c.attempt.Role, Required: append([]string(nil), required...), Facts: facts,
		CreatedAt: s.now().UTC(),
	}
	if phase == HarnessPhaseActual {
		b.SessionID = c.attempt.SessionID
	}
	return b, nil
}

// BindIntended runs the preflight and records the intended binding for the
// attempt that owns the package. It does not touch the package row.
func (s *ManagedHarnessService) BindIntended(ctx context.Context, packageID string, child IntendedChild, expectedAttemptRevision int64) (HarnessBinding, error) {
	if err := s.hostConfigured(); err != nil {
		return HarnessBinding{}, err
	}
	if packageID == "" || expectedAttemptRevision < 1 {
		return HarnessBinding{}, ErrManagedPayloadScope
	}
	pkg, err := s.store.GetContextPackage(ctx, packageID)
	if err != nil {
		return HarnessBinding{}, err
	}
	c, err := s.load(ctx, pkg.AttemptID)
	if err != nil {
		return HarnessBinding{}, err
	}
	if pkg.WorkItemID != c.attempt.WorkItemID || pkg.GroupID != c.attempt.GroupID || pkg.WorkspaceID != c.attempt.WorkspaceID || pkg.Role != c.attempt.Role {
		return HarnessBinding{}, ErrManagedPayloadScope
	}
	if c.attempt.Revision != expectedAttemptRevision {
		return HarnessBinding{}, ErrStaleRevision
	}
	if c.attempt.State != "reserved" {
		return HarnessBinding{}, ErrHarnessAttemptState
	}
	if err = c.open(); err != nil {
		return HarnessBinding{}, err
	}
	res, required, err := s.preflightIntended(ctx, c, child)
	if err != nil {
		return HarnessBinding{}, err
	}
	b, err := s.newBinding(c, HarnessPhaseIntended, pkg.ID, required, res.Facts)
	if err != nil {
		return HarnessBinding{}, err
	}
	b.Facts.SessionRevision = nil
	if err = s.store.InsertHarnessBinding(ctx, b, expectedAttemptRevision); err != nil {
		return HarnessBinding{}, err
	}
	return b, nil
}

// bindingFits reports whether a stored binding still describes this attempt.
func bindingFits(b HarnessBinding, c attemptContext, phase string) bool {
	return b.Phase == phase && b.AttemptID == c.attempt.ID && b.WorkItemID == c.attempt.WorkItemID && b.GroupID == c.attempt.GroupID &&
		b.WorkspaceID == c.attempt.WorkspaceID && b.BoardID == c.attempt.BoardID && b.ParentTaskID == c.attempt.ParentTaskID &&
		b.TaskID == c.item.TaskID && b.Role == c.attempt.Role &&
		(phase != HarnessPhaseActual || (b.SessionID != "" && b.SessionID == c.attempt.SessionID))
}

// storedBinding reads one phase. An absent row is harness_binding_missing; an
// unknown schema or a row that no longer fits the attempt is refused rather
// than trusted.
func (s *ManagedHarnessService) storedBinding(ctx context.Context, c attemptContext, phase string) (HarnessBinding, error) {
	b, err := s.store.GetHarnessBinding(ctx, c.attempt.ID, phase)
	if errors.Is(err, ErrManagedRecordNotFound) {
		return HarnessBinding{}, refusal(HarnessBindingMissing)
	}
	if err != nil {
		return HarnessBinding{}, err
	}
	if b.SchemaVersion != HarnessBindingSchemaVersion || b.Facts.SourceSchema != HarnessSourceSchemaSupported {
		return HarnessBinding{}, refusal(HarnessSchemaUnsupported)
	}
	if !bindingFits(b, c, phase) {
		return HarnessBinding{}, refusal(HarnessBindingStale)
	}
	return b, nil
}

func childStateBindable(state string) bool {
	return state == "session_created" || state == "task_assigned"
}

// BindActual checks the child that really exists against the intended
// binding and records the actual one. It runs only while the attempt is
// session_created or task_assigned, needs the session to be bound to the
// attempt already, and never changes what is stored for an earlier phase.
func (s *ManagedHarnessService) BindActual(ctx context.Context, attemptID string, expectedAttemptRevision int64) (HarnessBinding, error) {
	if err := s.hostConfigured(); err != nil {
		return HarnessBinding{}, err
	}
	if expectedAttemptRevision < 1 {
		return HarnessBinding{}, ErrManagedPayloadScope
	}
	c, err := s.load(ctx, attemptID)
	if err != nil {
		return HarnessBinding{}, err
	}
	if !childStateBindable(c.attempt.State) {
		return HarnessBinding{}, ErrHarnessAttemptState
	}
	if c.attempt.Revision != expectedAttemptRevision {
		return HarnessBinding{}, ErrStaleRevision
	}
	if err = c.open(); err != nil {
		return HarnessBinding{}, err
	}
	if c.attempt.SessionID == "" {
		return HarnessBinding{}, refusal(HarnessChildUnbound)
	}
	intended, err := s.storedBinding(ctx, c, HarnessPhaseIntended)
	if err != nil {
		return HarnessBinding{}, err
	}
	res, err := s.host.ResolveActual(ctx, c.attempt)
	if err != nil {
		return HarnessBinding{}, hostFailure(err)
	}
	res.Groups = normalizeGroups(res.Groups)
	if err = checkResolution(HarnessPhaseActual, res, intended.Required, &intended.Facts); err != nil {
		return HarnessBinding{}, err
	}
	b, err := s.newBinding(c, HarnessPhaseActual, intended.PackageID, intended.Required, res.Facts)
	if err != nil {
		return HarnessBinding{}, err
	}
	if err = s.store.InsertHarnessBinding(ctx, b, expectedAttemptRevision); err != nil {
		return HarnessBinding{}, err
	}
	return b, nil
}

// VerifyForEnqueue re-resolves the child and requires it to equal the stored
// actual binding with every required group still served. It is read only: the
// dispatcher calls it at the side-effect boundary, together with its own
// admission barrier, and it never sends or queues anything.
func (s *ManagedHarnessService) VerifyForEnqueue(ctx context.Context, attemptID string) error {
	if err := s.hostConfigured(); err != nil {
		return err
	}
	c, err := s.load(ctx, attemptID)
	if err != nil {
		return err
	}
	if !childStateBindable(c.attempt.State) {
		return ErrHarnessAttemptState
	}
	if err = c.open(); err != nil {
		return err
	}
	if c.attempt.SessionID == "" {
		return refusal(HarnessChildUnbound)
	}
	actual, err := s.storedBinding(ctx, c, HarnessPhaseActual)
	if err != nil {
		return err
	}
	res, err := s.host.ResolveActual(ctx, c.attempt)
	if err != nil {
		return hostFailure(err)
	}
	res.Groups = normalizeGroups(res.Groups)
	return checkResolution(HarnessPhaseActual, res, actual.Required, &actual.Facts)
}

// rebuildRequired is true for the refusals that mean the bound configuration is no longer
// the one in effect; the others are transient or about the child itself.
func rebuildRequired(code string) bool {
	switch code {
	case HarnessConfigChanged, HarnessRequiredGroupOff, HarnessOverridePresent, HarnessBindingStale, HarnessSchemaUnsupported:
		return true
	}
	return false
}

// CheckBinding reports whether the stored binding still matches what the host
// resolves now, without mutating anything. The actual binding is checked once
// it exists, the intended one before. A host failure is an error, not a state.
func (s *ManagedHarnessService) CheckBinding(ctx context.Context, attemptID string) (HarnessBindingStatus, error) {
	if err := s.hostConfigured(); err != nil {
		return HarnessBindingStatus{}, err
	}
	c, err := s.load(ctx, attemptID)
	if err != nil {
		return HarnessBindingStatus{}, err
	}
	status := HarnessBindingStatus{AttemptID: attemptID}
	phase := HarnessPhaseActual
	binding, err := s.storedBinding(ctx, c, phase)
	if bindingMissing(err) {
		phase = HarnessPhaseIntended
		binding, err = s.storedBinding(ctx, c, phase)
	}
	status.Phase = phase
	if bindingMissing(err) {
		status.Phase, status.State, status.Code = "", HarnessStateMissing, HarnessBindingMissing
		return status, nil
	}
	if err != nil {
		var r *HarnessRefusal
		if errors.As(err, &r) {
			return stale(status, r.Code), nil
		}
		return status, err
	}
	required, err := s.requiredGroups(ctx, c.attempt.Role)
	if err != nil {
		return status, err
	}
	if !reflect.DeepEqual(required, normalizeGroups(binding.Required)) {
		return stale(status, HarnessBindingStale), nil
	}
	var res HarnessResolution
	if phase == HarnessPhaseActual {
		res, err = s.host.ResolveActual(ctx, c.attempt)
	} else {
		res, err = s.host.ResolveIntended(ctx, c.scope(), IntendedChild{Vendor: binding.Facts.Vendor, Role: c.attempt.Role})
	}
	if err != nil {
		err = hostFailure(err)
		var r *HarnessRefusal
		if errors.As(err, &r) && r.Code != HarnessUnavailable && r.Code != HarnessNotConfigured {
			return stale(status, r.Code), nil
		}
		return status, err
	}
	res.Groups = normalizeGroups(res.Groups)
	if err = checkResolution(phase, res, binding.Required, &binding.Facts); err != nil {
		var r *HarnessRefusal
		if errors.As(err, &r) {
			return stale(status, r.Code), nil
		}
		return status, err
	}
	status.State = HarnessStateCurrent
	return status, nil
}

func bindingMissing(err error) bool {
	var r *HarnessRefusal
	return errors.As(err, &r) && r.Code == HarnessBindingMissing
}

func stale(status HarnessBindingStatus, code string) HarnessBindingStatus {
	status.State, status.Code, status.RebuildRequired = HarnessStateStale, code, rebuildRequired(code)
	return status
}
