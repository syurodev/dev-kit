package agentwork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrManagedHostUnavailable = errors.New("managed-work host is unavailable")
	ErrManagedIneligible      = errors.New("managed-work group is not eligible")
	ErrManagedTaskInvalid     = errors.New("managed-work task is unavailable or outside its group")
)

type ManagedParentSnapshot struct {
	WorkspaceID, BoardID, ParentTaskID, CoordinatorID string
	Revision                                          int64
	StatusID                                          string
	Deleted, Archived, Terminal                       bool
	Statuses                                          []BoardStatus
}

type ManagedTaskSnapshot struct {
	WorkspaceID, BoardID, ParentTaskID, TaskID string
	Revision                                   int64
	StatusID, StatusCategory                   string
	Deleted, Archived, Terminal, WontDo        bool
	Requirements                               RequirementsSnapshot
}

// ManagedHost is the narrow adapter boundary. The adapter resolves identity,
// caller scope, board state, current requirements and vault availability.
type ManagedHost interface {
	AuthorizeManagedHuman(context.Context) error
	CurrentManagedWorkspace(context.Context) (string, error)
	ResolveManagedParent(context.Context, string) (ManagedParentSnapshot, error)
	ResolveManagedParentForGroup(context.Context, GroupState) (ManagedParentSnapshot, error)
	LoadManagedTask(context.Context, GroupState, string) (ManagedTaskSnapshot, error)
	ValidateManagedEligibility(context.Context, ManagedParentSnapshot, StatusMap) error
}

// ManagedStore stores metadata only; requirement bodies remain in host memory.
type ManagedStore interface {
	CreateWorkGroup(context.Context, GroupState) (GroupState, error)
	GetWorkGroup(context.Context, string) (GroupState, error)
	UpdateGroupConfiguration(context.Context, string, int64, int64, StatusMap, bool, bool) (GroupState, error)
	ListWorkGroups(context.Context, string, string, int) (GroupPage, error)
	EndWorkGroup(context.Context, string, int64, string) (GroupState, error)
	CreateWorkItem(context.Context, WorkItem) (WorkItem, error)
	GetWorkItem(context.Context, string) (WorkItem, error)
	UpdateWorkItemHostState(context.Context, string, int64, int64, string, string, string, string) (WorkItem, error)
	GroupContainsTask(context.Context, string, string, string) (bool, error)
	ManagedTaskMembership(context.Context, string, string) (bool, error)
}

type ManagedService struct {
	host  ManagedHost
	store ManagedStore
}

func NewManagedService(host ManagedHost, store ManagedStore) *ManagedService {
	return &ManagedService{host: host, store: store}
}

func (s *ManagedService) CreateGroup(ctx context.Context, parentTaskID string, statusMap StatusMap) (GroupState, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupState{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupState{}, err
	}
	p, err := s.host.ResolveManagedParent(ctx, parentTaskID)
	if err != nil {
		return GroupState{}, err
	}
	if err = validManagedParent(p); err != nil {
		return GroupState{}, err
	}
	if err = statusMap.Validate(p.Statuses); err != nil {
		return GroupState{}, err
	}
	id, err := newManagedID()
	if err != nil {
		return GroupState{}, err
	}
	g, err := s.store.CreateWorkGroup(ctx, GroupState{ID: id, WorkspaceID: p.WorkspaceID, BoardID: p.BoardID, ParentTaskID: p.ParentTaskID, CoordinatorID: p.CoordinatorID, PolicyRevision: 1, SchemaVersion: ManagedSchemaVersion, StatusMap: statusMap, Enabled: false, Paused: true})
	if err != nil {
		return GroupState{}, err
	}
	return g, nil
}

func (s *ManagedService) ConfigureGroup(ctx context.Context, id string, expectedRevision, policyRevision int64, statusMap StatusMap, enabled, paused bool) (GroupState, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupState{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupState{}, err
	}
	g, err := s.scopedGroup(ctx, id)
	if err != nil {
		return GroupState{}, err
	}
	if g.EndedAt != nil {
		return GroupState{}, ErrManagedIneligible
	}
	p, err := s.host.ResolveManagedParentForGroup(ctx, g)
	if err != nil {
		return GroupState{}, err
	}
	if err = validManagedParent(p); err != nil {
		return GroupState{}, err
	}
	if p.WorkspaceID != g.WorkspaceID || p.BoardID != g.BoardID || p.ParentTaskID != g.ParentTaskID || p.CoordinatorID != g.CoordinatorID {
		return GroupState{}, ErrManagedTaskInvalid
	}
	if err = statusMap.Validate(p.Statuses); err != nil {
		return GroupState{}, err
	}
	if enabled {
		if err = s.host.ValidateManagedEligibility(ctx, p, statusMap); err != nil {
			return GroupState{}, fmt.Errorf("%w: %v", ErrManagedIneligible, err)
		}
	}
	return s.store.UpdateGroupConfiguration(ctx, id, expectedRevision, policyRevision, statusMap, enabled, paused)
}

func (s *ManagedService) GetGroup(ctx context.Context, id string) (GroupState, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupState{}, ErrManagedHostUnavailable
	}
	return s.scopedGroup(ctx, id)
}
func (s *ManagedService) ListGroups(ctx context.Context, after string, limit int) (GroupPage, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupPage{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupPage{}, err
	}
	ws, err := s.host.CurrentManagedWorkspace(ctx)
	if err != nil {
		return GroupPage{}, err
	}
	if strings.TrimSpace(ws) == "" {
		return GroupPage{}, ErrManagedHostUnavailable
	}
	page, err := s.store.ListWorkGroups(ctx, ws, after, limit)
	if err != nil {
		return GroupPage{}, err
	}
	return page, nil
}
func (s *ManagedService) EndGroup(ctx context.Context, id string, expectedRevision int64, reason string) (GroupState, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupState{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupState{}, err
	}
	_, err := s.scopedGroup(ctx, id)
	if err != nil {
		return GroupState{}, err
	}
	ended, err := s.store.EndWorkGroup(ctx, id, expectedRevision, reason)
	if err != nil {
		return GroupState{}, err
	}
	return ended, nil
}

// ReconcileGroup marks a group ended when its host-authorized parent task is
// deleted, archived or terminal. Missing host evidence remains an error.
func (s *ManagedService) ReconcileGroup(ctx context.Context, id string) (GroupState, error) {
	if s == nil || s.host == nil || s.store == nil {
		return GroupState{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupState{}, err
	}
	g, err := s.scopedGroup(ctx, id)
	if err != nil {
		return GroupState{}, err
	}
	if g.EndedAt != nil {
		return g, nil
	}
	p, err := s.resolveParentForGroup(ctx, g)
	if err != nil {
		return GroupState{}, err
	}
	if !p.Deleted && !p.Archived && !p.Terminal {
		if err = g.StatusMap.Validate(p.Statuses); err != nil {
			return GroupState{}, err
		}
		return g, nil
	}
	reason := TombstoneParentTerminal
	if p.Deleted {
		reason = TombstoneParentDeleted
	} else if p.Archived {
		reason = TombstoneParentArchived
	}
	ended, err := s.store.EndWorkGroup(ctx, g.ID, g.Revision, reason)
	if err != nil {
		return GroupState{}, err
	}
	return ended, nil
}

func (s *ManagedService) TrackWorkItem(ctx context.Context, groupID, taskID, role string) (WorkItem, error) {
	if s == nil || s.host == nil || s.store == nil {
		return WorkItem{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return WorkItem{}, err
	}
	g, err := s.scopedGroup(ctx, groupID)
	if err != nil {
		return WorkItem{}, err
	}
	if g.EndedAt != nil {
		return WorkItem{}, ErrManagedIneligible
	}
	p, err := s.resolveParentForGroup(ctx, g)
	if err != nil {
		return WorkItem{}, err
	}
	if err = validManagedParent(p); err != nil {
		return WorkItem{}, err
	}
	if err = g.StatusMap.Validate(p.Statuses); err != nil {
		return WorkItem{}, err
	}
	t, err := s.host.LoadManagedTask(ctx, g, taskID)
	if err != nil {
		return WorkItem{}, err
	}
	if err = validateTaskScope(g, t); err != nil {
		return WorkItem{}, err
	}
	_, ok := stateForStatus(g.StatusMap, t.StatusID, t.WontDo)
	if !ok || t.Deleted || t.Archived || t.Terminal {
		return WorkItem{}, ErrManagedTaskInvalid
	}
	req := t.Requirements
	req.WorkspaceID, req.BoardID, req.TaskID = t.WorkspaceID, t.BoardID, t.TaskID
	revision, err := RequirementsDigest(req)
	if err != nil {
		return WorkItem{}, err
	}
	id, err := newManagedID()
	if err != nil {
		return WorkItem{}, err
	}
	item, err := s.store.CreateWorkItem(ctx, WorkItem{ID: id, GroupID: g.ID, WorkspaceID: g.WorkspaceID, BoardID: g.BoardID, ParentTaskID: g.ParentTaskID, TaskID: t.TaskID, Role: role, TaskRevision: 1, RequirementsRevision: revision, HostStatusID: t.StatusID, State: "planned"})
	if err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (s *ManagedService) RefreshWorkItem(ctx context.Context, id string) (WorkItem, error) {
	return s.refresh(ctx, id)
}
func (s *ManagedService) ReconcileWorkItem(ctx context.Context, id string) (WorkItem, error) {
	return s.refresh(ctx, id)
}

func (s *ManagedService) refresh(ctx context.Context, id string) (WorkItem, error) {
	if s == nil || s.host == nil || s.store == nil {
		return WorkItem{}, ErrManagedHostUnavailable
	}
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return WorkItem{}, err
	}
	i, g, err := s.scopedWorkItem(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if i.TombstoneReason != "" {
		return i, ErrManagedIneligible
	}
	if g.EndedAt != nil {
		return WorkItem{}, ErrManagedIneligible
	}
	p, parentErr := s.resolveParentForGroup(ctx, g)
	if parentErr != nil {
		return WorkItem{}, parentErr
	}
	if p.Deleted || p.Archived || p.Terminal {
		reason := TombstoneParentTerminal
		if p.Deleted {
			reason = TombstoneParentDeleted
		} else if p.Archived {
			reason = TombstoneParentArchived
		}
		if _, err = s.store.EndWorkGroup(ctx, g.ID, g.Revision, reason); err != nil {
			return WorkItem{}, err
		}
		item, err := s.store.GetWorkItem(ctx, i.ID)
		if err != nil {
			return WorkItem{}, err
		}
		return item, nil
	}
	if err = g.StatusMap.Validate(p.Statuses); err != nil {
		return WorkItem{}, err
	}
	t, err := s.host.LoadManagedTask(ctx, g, i.TaskID)
	if err != nil {
		return WorkItem{}, err
	}
	if err = validateTaskScope(g, t); err != nil {
		return WorkItem{}, err
	}
	reason := ""
	switch {
	case t.Deleted:
		reason = "deleted"
	case t.Archived:
		reason = "archived"
	case t.Terminal:
		reason = "terminal"
	default:
		if _, ok := stateForStatus(g.StatusMap, t.StatusID, t.WontDo); !ok {
			return WorkItem{}, ErrManagedTaskInvalid
		}
	}
	reqRev := i.RequirementsRevision
	if reason == "" {
		req := t.Requirements
		req.WorkspaceID, req.BoardID, req.TaskID = t.WorkspaceID, t.BoardID, t.TaskID
		reqRev, err = RequirementsDigest(req)
		if err != nil {
			return WorkItem{}, err
		}
	}
	state := i.State
	taskRevision := i.TaskRevision
	if reason != "" {
		state = "ended"
	} else if reqRev != i.RequirementsRevision {
		taskRevision++
		state = "stale"
	}
	updated, err := s.store.UpdateWorkItemHostState(ctx, i.ID, i.Revision, taskRevision, reqRev, firstNonEmpty(t.StatusID, i.HostStatusID), state, reason)
	if err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

func (s *ManagedService) GroupContainsTask(ctx context.Context, workspaceID, boardID, taskID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrManagedHostUnavailable
	}
	return s.store.GroupContainsTask(ctx, workspaceID, boardID, taskID)
}

// ManagedTaskMembership is an internal fail-closed guard for terminal board
// transitions. It intentionally includes tombstones and returns no scope data.
func (s *ManagedService) ManagedTaskMembership(ctx context.Context, boardID, taskID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrManagedHostUnavailable
	}
	return s.store.ManagedTaskMembership(ctx, boardID, taskID)
}

func (s *ManagedService) resolveParentForGroup(ctx context.Context, g GroupState) (ManagedParentSnapshot, error) {
	p, err := s.host.ResolveManagedParentForGroup(ctx, g)
	if err != nil {
		return p, err
	}
	if p.WorkspaceID == "" || p.BoardID == "" || p.ParentTaskID == "" || p.CoordinatorID == "" || p.WorkspaceID != g.WorkspaceID || p.BoardID != g.BoardID || p.ParentTaskID != g.ParentTaskID || p.CoordinatorID != g.CoordinatorID {
		return p, ErrManagedTaskInvalid
	}
	return p, nil
}

func (s *ManagedService) scopedGroup(ctx context.Context, id string) (GroupState, error) {
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return GroupState{}, err
	}
	ws, err := s.host.CurrentManagedWorkspace(ctx)
	if err != nil {
		return GroupState{}, err
	}
	g, err := s.store.GetWorkGroup(ctx, id)
	if err != nil {
		return GroupState{}, err
	}
	if g.WorkspaceID != ws {
		return GroupState{}, ErrManagedTaskInvalid
	}
	return g, nil
}

func (s *ManagedService) scopedWorkItem(ctx context.Context, id string) (WorkItem, GroupState, error) {
	if err := s.host.AuthorizeManagedHuman(ctx); err != nil {
		return WorkItem{}, GroupState{}, err
	}
	ws, err := s.host.CurrentManagedWorkspace(ctx)
	if err != nil {
		return WorkItem{}, GroupState{}, err
	}
	i, err := s.store.GetWorkItem(ctx, id)
	if err != nil {
		return WorkItem{}, GroupState{}, err
	}
	g, err := s.store.GetWorkGroup(ctx, i.GroupID)
	if err != nil {
		return WorkItem{}, GroupState{}, err
	}
	if g.WorkspaceID != ws {
		return WorkItem{}, GroupState{}, ErrManagedTaskInvalid
	}
	return i, g, nil
}
func validManagedParent(p ManagedParentSnapshot) error {
	if p.WorkspaceID == "" || p.BoardID == "" || p.ParentTaskID == "" || p.CoordinatorID == "" || p.Deleted || p.Archived || p.Terminal {
		return ErrManagedTaskInvalid
	}
	return nil
}
func validateTaskScope(g GroupState, t ManagedTaskSnapshot) error {
	if t.WorkspaceID != g.WorkspaceID || t.BoardID != g.BoardID || t.ParentTaskID != g.ParentTaskID || t.TaskID == "" || t.TaskID == g.ParentTaskID {
		return ErrManagedTaskInvalid
	}
	return nil
}
func stateForStatus(m StatusMap, id string, wontDo bool) (string, bool) {
	if id == "" || wontDo {
		return "", false
	}
	switch id {
	case m.Ready:
		return "planned", true
	case m.Running:
		return "running", true
	case m.AwaitingReview:
		return "awaiting_review", true
	case m.Accepted:
		return "accepted", true
	default:
		return "", false
	}
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func newManagedID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
