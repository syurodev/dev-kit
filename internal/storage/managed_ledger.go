package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"devkit/internal/agentwork"
)

const ManagedLedgerPageMax = 100

var (
	ErrManagedNotFound     = errors.New("managed ledger record not found")
	ErrManagedConflict     = errors.New("managed ledger revision conflict")
	ErrManagedScope        = errors.New("managed ledger scope mismatch")
	ErrManagedInvalid      = errors.New("invalid managed ledger record")
	ErrManagedPolicyClosed = errors.New("managed work group is disabled or paused")
)

type ManagedWorkGroup = agentwork.GroupState
type ManagedWorkItem = agentwork.WorkItem
type ManagedWorkAttempt = agentwork.Attempt

type ManagedPage[T any] struct {
	Items []T
	Next  string
}

// ManagedLedger is a device-local coordination metadata store. It does not
// contain prompt, result, evidence, transcript or private path bodies.
type ManagedLedger struct {
	db  *sql.DB
	now func() time.Time
}

const managedGroupSelect = `SELECT id,workspace_id,board_id,parent_task_id,coordinator_id,enabled,paused,policy_revision,revision,created_at,updated_at,schema_version,status_map_json,ended_at,tombstone_reason FROM agent_work_groups`
const managedItemSelect = `SELECT i.id,i.group_id,i.workspace_id,i.board_id,i.parent_task_id,i.task_id,i.role,i.state,i.task_revision,i.requirements_revision,i.revision,COALESCE(i.current_attempt_id,''),i.created_at,i.updated_at,i.host_status_id,i.ended_at,i.tombstone_reason,i.schema_version FROM agent_work_items i`

func NewManagedLedger(db *sql.DB) *ManagedLedger { return NewManagedLedgerWithClock(db, time.Now) }

// NewManagedLedgerWithClock injects time for deterministic persistence and
// recovery tests. The caller must have applied storage.Migrate.
func NewManagedLedgerWithClock(db *sql.DB, now func() time.Time) *ManagedLedger {
	if now == nil {
		now = time.Now
	}
	return &ManagedLedger{db: db, now: now}
}

func (l *ManagedLedger) CreateWorkGroup(ctx context.Context, g ManagedWorkGroup) (ManagedWorkGroup, error) {
	if l == nil || l.db == nil {
		return g, fmt.Errorf("managed ledger database is nil")
	}
	if g.ID == "" || g.WorkspaceID == "" || g.BoardID == "" || g.ParentTaskID == "" || g.CoordinatorID == "" || g.PolicyRevision < 1 {
		return g, ErrManagedInvalid
	}
	if g.SchemaVersion == 0 {
		g.SchemaVersion = agentwork.ManagedSchemaVersion
	}
	if g.SchemaVersion != agentwork.ManagedSchemaVersion {
		return g, ErrManagedInvalid
	}
	statusJSON, err := json.Marshal(g.StatusMap)
	if err != nil {
		return g, ErrManagedInvalid
	}
	now := l.now().UTC()
	g.Enabled, g.Paused, g.Revision = false, true, 1
	g.CreatedAt, g.UpdatedAt = now, now
	res, err := l.db.ExecContext(ctx, `INSERT INTO agent_work_groups
		(id,workspace_id,board_id,parent_task_id,coordinator_id,enabled,paused,policy_revision,revision,created_at,updated_at,schema_version,status_map_json)
		SELECT ?,?,?,?,?,0,1,?,1,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM agent_work_groups WHERE workspace_id=? AND board_id=? AND parent_task_id=? AND ended_at IS NULL)`, g.ID, g.WorkspaceID, g.BoardID, g.ParentTaskID, g.CoordinatorID, g.PolicyRevision, stamp(now), stamp(now), g.SchemaVersion, string(statusJSON), g.WorkspaceID, g.BoardID, g.ParentTaskID)
	if err != nil {
		return g, mapManagedWrite(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWorkGroup{}, ErrManagedConflict
	}
	return g, nil
}

func (l *ManagedLedger) GetWorkGroup(ctx context.Context, id string) (ManagedWorkGroup, error) {
	g, err := scanManagedGroup(l.db.QueryRowContext(ctx, managedGroupSelect+` WHERE id=?`, id))
	if err != nil {
		return g, managedReadErr(err)
	}
	return g, nil
}

// UpdateGroupPolicy performs compare-and-swap on the group row. Enabling is
// explicit; callers cannot accidentally unpause a group by omitting a field.
func (l *ManagedLedger) UpdateGroupPolicy(ctx context.Context, id string, expectedRevision, policyRevision int64, enabled, paused bool) (ManagedWorkGroup, error) {
	if expectedRevision < 1 || policyRevision < 1 {
		return ManagedWorkGroup{}, ErrManagedInvalid
	}
	now := l.now().UTC()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkGroup{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_groups SET enabled=?,paused=?,policy_revision=?,revision=revision+1,updated_at=?
		WHERE id=? AND revision=? AND policy_revision<? AND ended_at IS NULL AND schema_version=?`, bit(enabled), bit(paused), policyRevision, stamp(now), id, expectedRevision, policyRevision, agentwork.ManagedSchemaVersion)
	if err != nil {
		return ManagedWorkGroup{}, mapManagedWrite(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM agent_work_groups WHERE id=?`, id).Scan(&revision)
		if err == sql.ErrNoRows {
			return ManagedWorkGroup{}, ErrManagedNotFound
		}
		if err != nil {
			return ManagedWorkGroup{}, err
		}
		return ManagedWorkGroup{}, ErrManagedConflict
	}
	g, err := scanManagedGroup(tx.QueryRowContext(ctx, managedGroupSelect+` WHERE id=?`, id))
	if err != nil {
		return g, err
	}
	if err := tx.Commit(); err != nil {
		return g, err
	}
	return g, nil
}

func (l *ManagedLedger) UpdateGroupConfiguration(ctx context.Context, id string, expectedRevision, policyRevision int64, statusMap agentwork.StatusMap, enabled, paused bool) (ManagedWorkGroup, error) {
	if expectedRevision < 1 || policyRevision < 1 {
		return ManagedWorkGroup{}, ErrManagedInvalid
	}
	encoded, err := json.Marshal(statusMap)
	if err != nil {
		return ManagedWorkGroup{}, ErrManagedInvalid
	}
	now := l.now().UTC()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkGroup{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_groups SET enabled=?,paused=?,policy_revision=?,status_map_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND policy_revision<? AND ended_at IS NULL AND schema_version=?`, bit(enabled), bit(paused), policyRevision, string(encoded), stamp(now), id, expectedRevision, policyRevision, agentwork.ManagedSchemaVersion)
	if err != nil {
		return ManagedWorkGroup{}, mapManagedWrite(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var rev int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM agent_work_groups WHERE id=?`, id).Scan(&rev)
		if err == sql.ErrNoRows {
			return ManagedWorkGroup{}, ErrManagedNotFound
		}
		if err != nil {
			return ManagedWorkGroup{}, err
		}
		return ManagedWorkGroup{}, ErrManagedConflict
	}
	g, err := scanManagedGroup(tx.QueryRowContext(ctx, managedGroupSelect+` WHERE id=?`, id))
	if err != nil {
		return g, err
	}
	if err = tx.Commit(); err != nil {
		return g, err
	}
	return g, nil
}

func (l *ManagedLedger) ListWorkGroups(ctx context.Context, workspaceID, after string, limit int) (agentwork.GroupPage, error) {
	if workspaceID == "" || limit < 1 || limit > ManagedLedgerPageMax {
		return agentwork.GroupPage{}, ErrManagedInvalid
	}
	rows, err := l.db.QueryContext(ctx, managedGroupSelect+` WHERE workspace_id=? AND id>? ORDER BY id LIMIT ?`, workspaceID, after, limit+1)
	if err != nil {
		return agentwork.GroupPage{}, err
	}
	defer rows.Close()
	p := agentwork.GroupPage{Items: make([]ManagedWorkGroup, 0, limit)}
	for rows.Next() {
		g, e := scanManagedGroup(rows)
		if e != nil {
			return p, e
		}
		p.Items = append(p.Items, g)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		p.Next = p.Items[len(p.Items)-1].ID
	}
	return p, nil
}

// EndWorkGroup records a fixed reason code without deleting history. Items
// with a current attempt remain blocked and keep their unique task slot.
func (l *ManagedLedger) EndWorkGroup(ctx context.Context, id string, expectedRevision int64, reason string) (ManagedWorkGroup, error) {
	if expectedRevision < 1 || !validManagedTombstoneReason(reason) {
		return ManagedWorkGroup{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkGroup{}, err
	}
	defer tx.Rollback()
	now := l.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_groups SET enabled=0,paused=1,ended_at=?,tombstone_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND ended_at IS NULL AND schema_version=?`, stamp(now), reason, stamp(now), id, expectedRevision, agentwork.ManagedSchemaVersion)
	if err != nil {
		return ManagedWorkGroup{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWorkGroup{}, ErrManagedConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_work_items SET ended_at=CASE WHEN current_attempt_id IS NULL THEN ? ELSE ended_at END,state=CASE WHEN current_attempt_id IS NULL THEN state ELSE 'blocked' END,tombstone_reason=CASE WHEN tombstone_reason='' THEN ? ELSE tombstone_reason END,revision=revision+1,updated_at=? WHERE group_id=? AND ended_at IS NULL`, stamp(now), reason, stamp(now), id); err != nil {
		return ManagedWorkGroup{}, err
	}
	g, err := scanManagedGroup(tx.QueryRowContext(ctx, managedGroupSelect+` WHERE id=?`, id))
	if err != nil {
		return g, err
	}
	if err = tx.Commit(); err != nil {
		return g, err
	}
	return g, nil
}

func (l *ManagedLedger) UpdateWorkItemHostState(ctx context.Context, id string, expectedRevision, taskRevision int64, reqRevision, statusID, state, tombstoneReason string) (ManagedWorkItem, error) {
	if expectedRevision < 1 || taskRevision < 1 || strings.TrimSpace(reqRevision) == "" || statusID == "" || state == "" {
		return ManagedWorkItem{}, ErrManagedInvalid
	}
	if tombstoneReason != "" && !validManagedTombstoneReason(tombstoneReason) {
		return ManagedWorkItem{}, ErrManagedInvalid
	}
	if !validManagedItemState(state) {
		return ManagedWorkItem{}, ErrManagedInvalid
	}
	now := l.now().UTC()
	var ended any
	if tombstoneReason != "" {
		ended = stamp(now)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkItem{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_items SET task_revision=?,requirements_revision=?,host_status_id=?,state=CASE WHEN ?<>'' AND current_attempt_id IS NOT NULL THEN 'blocked' ELSE ? END,ended_at=CASE WHEN current_attempt_id IS NULL THEN COALESCE(ended_at,?) ELSE ended_at END,tombstone_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND ended_at IS NULL AND tombstone_reason='' AND schema_version=?`, taskRevision, reqRevision, statusID, tombstoneReason, state, ended, tombstoneReason, stamp(now), id, expectedRevision, agentwork.ManagedSchemaVersion)
	if err != nil {
		return ManagedWorkItem{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWorkItem{}, ErrManagedConflict
	}
	item, err := scanManagedItem(tx.QueryRowContext(ctx, managedItemSelect+` WHERE i.id=?`, id))
	if err != nil {
		return ManagedWorkItem{}, managedReadErr(err)
	}
	if err = tx.Commit(); err != nil {
		return ManagedWorkItem{}, err
	}
	return item, nil
}

func (l *ManagedLedger) GroupContainsTask(ctx context.Context, workspaceID, boardID, taskID string) (bool, error) {
	if workspaceID == "" || boardID == "" || taskID == "" {
		return false, ErrManagedInvalid
	}
	var n int
	err := l.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_work_items WHERE workspace_id=? AND board_id=? AND task_id=? AND ended_at IS NULL`, workspaceID, boardID, taskID).Scan(&n)
	return n > 0, err
}

func (l *ManagedLedger) ManagedTaskMembership(ctx context.Context, boardID, taskID string) (bool, error) {
	if boardID == "" || taskID == "" {
		return false, ErrManagedInvalid
	}
	var unsupported int
	if err := l.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM agent_work_items WHERE board_id=? AND task_id=? AND schema_version<>?) +
		(SELECT count(*) FROM agent_work_groups WHERE board_id=? AND parent_task_id=? AND schema_version<>?)`, boardID, taskID, agentwork.ManagedSchemaVersion, boardID, taskID, agentwork.ManagedSchemaVersion).Scan(&unsupported); err != nil {
		return false, err
	}
	if unsupported > 0 {
		return false, ErrManagedInvalid
	}
	var count int
	err := l.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM agent_work_items WHERE board_id=? AND task_id=?) +
		(SELECT count(*) FROM agent_work_groups WHERE board_id=? AND parent_task_id=?)`, boardID, taskID, boardID, taskID).Scan(&count)
	return count > 0, err
}

func (l *ManagedLedger) CreateWorkItem(ctx context.Context, item ManagedWorkItem) (ManagedWorkItem, error) {
	if item.ID == "" || item.GroupID == "" || agentwork.ValidateScope(item.WorkspaceID, item.BoardID, item.ParentTaskID, item.TaskID) != nil || item.TaskRevision < 1 || strings.TrimSpace(item.RequirementsRevision) == "" || !validManagedRole(item.Role) {
		return item, ErrManagedInvalid
	}
	item.SchemaVersion = agentwork.ManagedSchemaVersion
	var groupSchema int
	var workspaceID, boardID, parentTaskID string
	var groupEnded sql.NullString
	err := l.db.QueryRowContext(ctx, `SELECT workspace_id,board_id,parent_task_id,schema_version,ended_at FROM agent_work_groups WHERE id=?`, item.GroupID).Scan(&workspaceID, &boardID, &parentTaskID, &groupSchema, &groupEnded)
	if err != nil {
		return item, managedReadErr(err)
	}
	if workspaceID != item.WorkspaceID || boardID != item.BoardID || parentTaskID != item.ParentTaskID {
		return item, ErrManagedScope
	}
	if groupSchema != agentwork.ManagedSchemaVersion {
		return item, ErrManagedInvalid
	}
	if groupEnded.Valid {
		return ManagedWorkItem{}, ErrManagedPolicyClosed
	}
	now := l.now().UTC()
	item.State, item.Revision = "planned", 1
	item.CurrentAttemptID = ""
	item.CreatedAt, item.UpdatedAt = now, now
	res, err := l.db.ExecContext(ctx, `INSERT INTO agent_work_items
		(id,group_id,workspace_id,board_id,parent_task_id,task_id,task_revision,requirements_revision,role,state,current_attempt_id,revision,created_at,updated_at,host_status_id,schema_version)
		SELECT ?,?,?,?,?,?,?,?,?,'planned',NULL,1,?,?,?,? WHERE EXISTS(SELECT 1 FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=? AND ended_at IS NULL AND schema_version=?) AND NOT EXISTS(SELECT 1 FROM agent_work_items WHERE task_id=? AND ended_at IS NULL)`, item.ID, item.GroupID, item.WorkspaceID, item.BoardID, item.ParentTaskID, item.TaskID, item.TaskRevision, item.RequirementsRevision, item.Role, stamp(now), stamp(now), item.HostStatusID, item.SchemaVersion, item.GroupID, item.WorkspaceID, item.BoardID, item.ParentTaskID, agentwork.ManagedSchemaVersion, item.TaskID)
	if err != nil {
		return item, mapManagedWrite(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWorkItem{}, ErrManagedConflict
	}
	return item, nil
}

func (l *ManagedLedger) GetWorkItem(ctx context.Context, id string) (ManagedWorkItem, error) {
	i, err := scanManagedItem(l.db.QueryRowContext(ctx, managedItemSelect+` WHERE i.id=?`, id))
	if err != nil {
		return i, managedReadErr(err)
	}
	return i, nil
}

// RefreshWorkItemRequirements CAS-updates the host-derived task revision and
// opaque requirements digest. Existing attempts retain their original
// revisions and therefore cannot apply a result after this changes.
func (l *ManagedLedger) RefreshWorkItemRequirements(ctx context.Context, id string, expectedRevision, taskRevision int64, requirementsRevision string) (ManagedWorkItem, error) {
	if expectedRevision < 1 || taskRevision < 1 || strings.TrimSpace(requirementsRevision) == "" {
		return ManagedWorkItem{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkItem{}, err
	}
	defer tx.Rollback()
	now := l.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_items SET task_revision=?,requirements_revision=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND schema_version=? AND (task_revision<>? OR requirements_revision<>?)`, taskRevision, requirementsRevision, stamp(now), id, expectedRevision, agentwork.ManagedSchemaVersion, taskRevision, requirementsRevision)
	if err != nil {
		return ManagedWorkItem{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		item, readErr := scanManagedItem(tx.QueryRowContext(ctx, managedItemSelect+` WHERE i.id=?`, id))
		if readErr != nil {
			return item, managedReadErr(readErr)
		}
		if item.Revision != expectedRevision {
			return item, ErrManagedConflict
		}
		if item.TaskRevision != taskRevision || item.RequirementsRevision != requirementsRevision {
			return item, ErrManagedConflict
		}
		if err = tx.Commit(); err != nil {
			return item, err
		}
		return item, nil
	}
	item, err := scanManagedItem(tx.QueryRowContext(ctx, managedItemSelect+` WHERE i.id=?`, id))
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}

// BeginAttempt atomically claims the current item revision and inserts a new
// attempt. The exact task and requirements revisions must still match.
func (l *ManagedLedger) BeginAttempt(ctx context.Context, attempt ManagedWorkAttempt, expectedItemRevision int64) (ManagedWorkAttempt, error) {
	if attempt.ID == "" || attempt.WorkItemID == "" || expectedItemRevision < 1 {
		return attempt, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return attempt, err
	}
	defer tx.Rollback()
	var scope ManagedWorkItem
	err = tx.QueryRowContext(ctx, `SELECT id,group_id,workspace_id,board_id,parent_task_id,task_id,role,state,task_revision,requirements_revision,revision,COALESCE(current_attempt_id,''),schema_version FROM agent_work_items WHERE id=?`, attempt.WorkItemID).
		Scan(&scope.ID, &scope.GroupID, &scope.WorkspaceID, &scope.BoardID, &scope.ParentTaskID, &scope.TaskID, &scope.Role, &scope.State, &scope.TaskRevision, &scope.RequirementsRevision, &scope.Revision, &scope.CurrentAttemptID, &scope.SchemaVersion)
	if err != nil {
		return attempt, managedReadErr(err)
	}
	if scope.SchemaVersion != agentwork.ManagedSchemaVersion {
		return attempt, ErrManagedInvalid
	}
	if scope.Revision != expectedItemRevision {
		return attempt, ErrManagedConflict
	}
	if scope.CurrentAttemptID != "" {
		return attempt, ErrManagedConflict
	}
	var enabled, paused, groupSchema int
	if err = tx.QueryRowContext(ctx, `SELECT enabled,paused,schema_version FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, scope.GroupID, scope.WorkspaceID, scope.BoardID, scope.ParentTaskID).Scan(&enabled, &paused, &groupSchema); err != nil {
		return attempt, managedReadErr(err)
	}
	if groupSchema != agentwork.ManagedSchemaVersion {
		return attempt, ErrManagedInvalid
	}
	if enabled != 1 || paused != 0 {
		return attempt, ErrManagedPolicyClosed
	}
	if attempt.TaskRevision != scope.TaskRevision || attempt.RequirementsRevision != scope.RequirementsRevision {
		return attempt, ErrManagedScope
	}
	if attempt.Role == "" {
		attempt.Role = scope.Role
	}
	if attempt.Role != scope.Role {
		return attempt, ErrManagedScope
	}
	var generation int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(generation),0)+1 FROM agent_work_attempts WHERE work_item_id=?", scope.ID).Scan(&generation); err != nil {
		return attempt, err
	}
	now := l.now().UTC()
	attempt.GroupID, attempt.WorkspaceID, attempt.BoardID, attempt.ParentTaskID = scope.GroupID, scope.WorkspaceID, scope.BoardID, scope.ParentTaskID
	attempt.Generation, attempt.Revision, attempt.State, attempt.DispatchState, attempt.SchemaVersion = generation, 1, "reserved", "reserved", agentwork.ManagedSchemaVersion
	attempt.CreatedAt, attempt.UpdatedAt = now, now
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_work_attempts(id,work_item_id,group_id,workspace_id,board_id,parent_task_id,session_id,role,task_revision,requirements_revision,generation,revision,state,dispatch_state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,1,'reserved','reserved',?,?)`, attempt.ID, attempt.WorkItemID, attempt.GroupID, attempt.WorkspaceID, attempt.BoardID, attempt.ParentTaskID, attempt.SessionID, attempt.Role, attempt.TaskRevision, attempt.RequirementsRevision, generation, stamp(now), stamp(now))
	if err != nil {
		return attempt, mapManagedWrite(err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_items SET current_attempt_id=?,state='dispatching',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND schema_version=? AND current_attempt_id IS NULL AND task_revision=? AND requirements_revision=?`, attempt.ID, stamp(now), scope.ID, expectedItemRevision, agentwork.ManagedSchemaVersion, attempt.TaskRevision, attempt.RequirementsRevision)
	if err != nil {
		return attempt, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return attempt, ErrManagedConflict
	}
	if err = tx.Commit(); err != nil {
		return attempt, err
	}
	return attempt, nil
}

// TransitionAttempt is fenced by attempt revision, current item pointer, scope
// and task requirements revision. It never marks a task accepted or closed.
func (l *ManagedLedger) TransitionAttempt(ctx context.Context, id string, expectedRevision int64, to string) (ManagedWorkAttempt, error) {
	if expectedRevision < 1 || to == "accepted" || to == "closed" || to == "done" {
		return ManagedWorkAttempt{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWorkAttempt{}, err
	}
	defer tx.Rollback()
	a, err := scanManagedAttempt(tx.QueryRowContext(ctx, `SELECT id,work_item_id,group_id,workspace_id,board_id,parent_task_id,session_id,role,state,dispatch_state,task_revision,requirements_revision,generation,revision,created_at,updated_at,schema_version FROM agent_work_attempts WHERE id=?`, id))
	if err != nil {
		return a, managedReadErr(err)
	}
	if a.Revision != expectedRevision {
		return a, ErrManagedConflict
	}
	if err = agentwork.CanTransitionAttempt(a.State, to); err != nil {
		return a, fmt.Errorf("%w: %v", ErrManagedInvalid, err)
	}
	var current string
	var taskRev int64
	var reqRev string
	var itemSchema int
	var tombstoneReason string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(current_attempt_id,''),task_revision,requirements_revision,schema_version,tombstone_reason FROM agent_work_items WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, a.WorkItemID, a.GroupID, a.WorkspaceID, a.BoardID, a.ParentTaskID).Scan(&current, &taskRev, &reqRev, &itemSchema, &tombstoneReason)
	if err != nil {
		return a, managedReadErr(err)
	}
	if itemSchema != agentwork.ManagedSchemaVersion {
		return a, ErrManagedInvalid
	}
	if tombstoneReason != "" {
		return a, ErrManagedScope
	}
	var groupSchema int
	var groupEnded sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,ended_at FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, a.GroupID, a.WorkspaceID, a.BoardID, a.ParentTaskID).Scan(&groupSchema, &groupEnded); err != nil {
		return a, managedReadErr(err)
	}
	if groupSchema != agentwork.ManagedSchemaVersion {
		return a, ErrManagedInvalid
	}
	if groupEnded.Valid {
		return a, ErrManagedScope
	}
	if current != a.ID || taskRev != a.TaskRevision || reqRev != a.RequirementsRevision {
		return a, ErrManagedScope
	}
	dispatchState := to
	if to == "interrupted_unknown" {
		dispatchState = "ambiguous"
	}
	now := l.now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_attempts SET state=?,dispatch_state=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND schema_version=?`, to, dispatchState, stamp(now), id, expectedRevision, agentwork.ManagedSchemaVersion)
	if err != nil {
		return a, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return a, ErrManagedConflict
	}
	itemState := to
	if to == "interrupted_unknown" {
		itemState = "blocked"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_work_items SET state=?,revision=revision+1,updated_at=? WHERE id=? AND current_attempt_id=?`, itemState, stamp(now), a.WorkItemID, a.ID); err != nil {
		return a, err
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	a.State, a.DispatchState, a.Revision, a.UpdatedAt = to, dispatchState, a.Revision+1, now
	return a, nil
}

// MarkAttemptAmbiguous records an explicitly selected dispatch as uncertain
// after the host has reconciled its live session/turn. It cannot scan or
// interrupt unrelated live attempts and never queues, launches or resends.
func (l *ManagedLedger) MarkAttemptAmbiguous(ctx context.Context, id string, expectedRevision int64) (ManagedWorkAttempt, error) {
	if expectedRevision < 1 {
		return ManagedWorkAttempt{}, ErrManagedInvalid
	}
	a, err := scanManagedAttempt(l.db.QueryRowContext(ctx, `SELECT id,work_item_id,group_id,workspace_id,board_id,parent_task_id,session_id,role,state,dispatch_state,task_revision,requirements_revision,generation,revision,created_at,updated_at,schema_version FROM agent_work_attempts WHERE id=?`, id))
	if err != nil {
		return a, managedReadErr(err)
	}
	if a.Revision != expectedRevision {
		return a, ErrManagedConflict
	}
	if a.State != "prompt_enqueued" && a.State != "running" {
		return a, ErrManagedInvalid
	}
	return l.TransitionAttempt(ctx, id, expectedRevision, "interrupted_unknown")
}

func (l *ManagedLedger) ListWorkItems(ctx context.Context, groupID, after string, limit int) (ManagedPage[ManagedWorkItem], error) {
	if limit < 1 || limit > ManagedLedgerPageMax {
		return ManagedPage[ManagedWorkItem]{}, ErrManagedInvalid
	}
	rows, err := l.db.QueryContext(ctx, managedItemSelect+` WHERE i.group_id=? AND i.id>? ORDER BY i.id LIMIT ?`, groupID, after, limit+1)
	if err != nil {
		return ManagedPage[ManagedWorkItem]{}, err
	}
	defer rows.Close()
	page := ManagedPage[ManagedWorkItem]{Items: make([]ManagedWorkItem, 0, limit)}
	for rows.Next() {
		i, scanErr := scanManagedItem(rows)
		if scanErr != nil {
			return page, scanErr
		}
		page.Items = append(page.Items, i)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func (l *ManagedLedger) ListAttempts(ctx context.Context, workItemID, after string, limit int) (ManagedPage[ManagedWorkAttempt], error) {
	if limit < 1 || limit > ManagedLedgerPageMax {
		return ManagedPage[ManagedWorkAttempt]{}, ErrManagedInvalid
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id,work_item_id,group_id,workspace_id,board_id,parent_task_id,session_id,role,state,dispatch_state,task_revision,requirements_revision,generation,revision,created_at,updated_at,schema_version FROM agent_work_attempts WHERE work_item_id=? AND id>? ORDER BY id LIMIT ?`, workItemID, after, limit+1)
	if err != nil {
		return ManagedPage[ManagedWorkAttempt]{}, err
	}
	defer rows.Close()
	p := ManagedPage[ManagedWorkAttempt]{Items: make([]ManagedWorkAttempt, 0, limit)}
	for rows.Next() {
		a, e := scanManagedAttempt(rows)
		if e != nil {
			return p, e
		}
		p.Items = append(p.Items, a)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		p.Next = p.Items[len(p.Items)-1].ID
	}
	return p, nil
}

type managedScanner interface{ Scan(...any) error }

func scanManagedItem(s managedScanner) (ManagedWorkItem, error) {
	var i ManagedWorkItem
	var c, u string
	var ended sql.NullString
	err := s.Scan(&i.ID, &i.GroupID, &i.WorkspaceID, &i.BoardID, &i.ParentTaskID, &i.TaskID, &i.Role, &i.State, &i.TaskRevision, &i.RequirementsRevision, &i.Revision, &i.CurrentAttemptID, &c, &u, &i.HostStatusID, &ended, &i.TombstoneReason, &i.SchemaVersion)
	if err == nil {
		if i.SchemaVersion != agentwork.ManagedSchemaVersion {
			return i, ErrManagedInvalid
		}
		if i.SchemaVersion != agentwork.ManagedSchemaVersion {
			return i, ErrManagedInvalid
		}
		i.CreatedAt, _ = parseStamp(c)
		i.UpdatedAt, _ = parseStamp(u)
		if ended.Valid {
			t, _ := parseStamp(ended.String)
			i.EndedAt = &t
		}
	}
	return i, err
}

func scanManagedAttempt(s managedScanner) (ManagedWorkAttempt, error) {
	var a ManagedWorkAttempt
	var c, u string
	err := s.Scan(&a.ID, &a.WorkItemID, &a.GroupID, &a.WorkspaceID, &a.BoardID, &a.ParentTaskID, &a.SessionID, &a.Role, &a.State, &a.DispatchState, &a.TaskRevision, &a.RequirementsRevision, &a.Generation, &a.Revision, &c, &u, &a.SchemaVersion)
	if err == nil && a.SchemaVersion != agentwork.ManagedSchemaVersion {
		return a, ErrManagedInvalid
	}
	a.CreatedAt, _ = parseStamp(c)
	a.UpdatedAt, _ = parseStamp(u)
	return a, err
}
func scanManagedGroup(s managedScanner) (ManagedWorkGroup, error) {
	var g ManagedWorkGroup
	var enabled, paused int
	var created, updated, statusMap string
	var ended sql.NullString
	err := s.Scan(&g.ID, &g.WorkspaceID, &g.BoardID, &g.ParentTaskID, &g.CoordinatorID, &enabled, &paused, &g.PolicyRevision, &g.Revision, &created, &updated, &g.SchemaVersion, &statusMap, &ended, &g.TombstoneReason)
	if err == nil {
		if g.SchemaVersion != agentwork.ManagedSchemaVersion {
			return g, ErrManagedInvalid
		}
		g.Enabled, g.Paused = enabled == 1, paused == 1
		g.CreatedAt, _ = parseStamp(created)
		g.UpdatedAt, _ = parseStamp(updated)
		_ = json.Unmarshal([]byte(statusMap), &g.StatusMap)
		if ended.Valid {
			t, _ := parseStamp(ended.String)
			g.EndedAt = &t
		}
	}
	return g, err
}
func managedReadErr(err error) error {
	if err == sql.ErrNoRows {
		return ErrManagedNotFound
	}
	return err
}
func mapManagedWrite(err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "foreign key constraint failed") {
		return ErrManagedScope
	}
	if strings.Contains(s, "unique constraint failed") {
		return ErrManagedConflict
	}
	return err
}
func validManagedRole(s string) bool {
	return s == "coordinator" || s == "implementer" || s == "reviewer" || s == "integrator"
}
func validManagedTombstoneReason(s string) bool {
	return s == agentwork.TombstoneUserEnded || s == agentwork.TombstoneOperatorStopped || s == agentwork.TombstoneParentDeleted || s == agentwork.TombstoneParentArchived || s == agentwork.TombstoneParentTerminal || s == agentwork.TombstoneDeleted || s == agentwork.TombstoneArchived || s == agentwork.TombstoneTerminal
}
func validManagedItemState(s string) bool {
	switch s {
	case "planned", "dispatching", "reserved", "session_created", "task_assigned", "prompt_enqueued", "running", "awaiting_review", "blocked", "stale", "failed", "cancelled", "interrupted_unknown", "ended":
		return true
	default:
		return false
	}
}
func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}
func stamp(t time.Time) string               { return t.UTC().Format(time.RFC3339Nano) }
func parseStamp(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
