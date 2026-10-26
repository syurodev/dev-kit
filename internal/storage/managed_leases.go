package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"devkit/internal/agentwork"
)

var (
	ErrManagedLeaseNotFound = errors.New("managed writer lease not found")
	ErrManagedLeaseConflict = errors.New("managed writer lease conflict")
)

type ManagedWriterLease = agentwork.ManagedWriterLease
type ManagedWriterBinding = agentwork.ManagedWriterBinding
type ManagedWriterQuiescence = agentwork.ManagedWriterQuiescence
type managedLeaseScanner interface{ Scan(...any) error }

const managedLeaseSelect = `SELECT id,schema_version,workspace_id,board_id,parent_task_id,task_id,group_id,work_item_id,attempt_id,attempt_generation,session_id,resource_key,generation,revision,heartbeat_at,expires_at,created_at,released_at,release_reason FROM agent_work_leases`

func (l *ManagedLedger) GetManagedAttempt(ctx context.Context, id string) (ManagedWorkAttempt, error) {
	a, err := scanManagedAttempt(l.db.QueryRowContext(ctx, `SELECT id,work_item_id,group_id,workspace_id,board_id,parent_task_id,session_id,role,state,dispatch_state,task_revision,requirements_revision,generation,revision,created_at,updated_at,schema_version FROM agent_work_attempts WHERE id=?`, id))
	if err != nil {
		return ManagedWorkAttempt{}, managedReadErr(err)
	}
	return a, nil
}

func (l *ManagedLedger) GetCurrentManagedAttempt(ctx context.Context, workItemID string) (ManagedWorkAttempt, error) {
	var a ManagedWorkAttempt
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	a, err = scanManagedAttempt(tx.QueryRowContext(ctx, `SELECT a.id,a.work_item_id,a.group_id,a.workspace_id,a.board_id,a.parent_task_id,a.session_id,a.role,a.state,a.dispatch_state,a.task_revision,a.requirements_revision,a.generation,a.revision,a.created_at,a.updated_at,a.schema_version FROM agent_work_attempts a JOIN agent_work_items i ON i.id=a.work_item_id WHERE i.id=? AND i.current_attempt_id=a.id`, workItemID))
	if err != nil {
		return ManagedWorkAttempt{}, managedReadErr(err)
	}
	if err = tx.Commit(); err != nil {
		return ManagedWorkAttempt{}, err
	}
	return a, nil
}

// ManagedSessionAssociation is privileged local metadata lookup used by the
// invoke/steer guard to classify old managed sessions even after release.
func (l *ManagedLedger) ManagedSessionAssociation(ctx context.Context, sessionID string) (ManagedWorkAttempt, bool, error) {
	if sessionID == "" {
		return ManagedWorkAttempt{}, false, ErrManagedInvalid
	}
	var unknown int
	if err := l.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_work_leases WHERE session_id=? AND schema_version<>?`, sessionID, agentwork.ManagedWriterLeaseSchemaVersion).Scan(&unknown); err != nil {
		return ManagedWorkAttempt{}, false, err
	}
	if unknown > 0 {
		return ManagedWorkAttempt{}, false, ErrManagedInvalid
	}
	a, err := scanManagedAttempt(l.db.QueryRowContext(ctx, `SELECT a.id,a.work_item_id,a.group_id,a.workspace_id,a.board_id,a.parent_task_id,a.session_id,a.role,a.state,a.dispatch_state,a.task_revision,a.requirements_revision,a.generation,a.revision,a.created_at,a.updated_at,a.schema_version FROM agent_work_attempts a LEFT JOIN agent_work_items i ON i.id=a.work_item_id WHERE a.session_id=? OR a.id IN (SELECT attempt_id FROM agent_work_leases WHERE session_id=?) ORDER BY CASE WHEN i.current_attempt_id=a.id THEN 0 ELSE 1 END,a.updated_at DESC LIMIT 1`, sessionID, sessionID))
	if err == sql.ErrNoRows {
		return ManagedWorkAttempt{}, false, nil
	}
	if err != nil {
		return ManagedWorkAttempt{}, false, err
	}
	return a, true, nil
}
func (l *ManagedLedger) GetManagedWorkItem(ctx context.Context, id string) (ManagedWorkItem, error) {
	return l.GetWorkItem(ctx, id)
}
func (l *ManagedLedger) GetManagedWorkGroup(ctx context.Context, id string) (ManagedWorkGroup, error) {
	return l.GetWorkGroup(ctx, id)
}

func (l *ManagedLedger) CreateManagedWriterLease(ctx context.Context, lease ManagedWriterLease, expectedAttemptRevision int64) (ManagedWriterLease, error) {
	if l == nil || l.db == nil || lease.ID == "" || expectedAttemptRevision < 1 || !validManagedLeaseBinding(lease.Binding) || lease.SchemaVersion != agentwork.ManagedWriterLeaseSchemaVersion || lease.Revision != 1 || lease.AttemptGeneration < 1 || lease.HeartbeatAt.IsZero() || !lease.ExpiresAt.After(lease.HeartbeatAt) {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	defer tx.Rollback()
	if err = validateManagedLeaseScopeTx(ctx, tx, lease.Binding, lease.AttemptGeneration, expectedAttemptRevision, true, false); err != nil {
		return ManagedWriterLease{}, err
	}
	var generation int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM agent_work_leases WHERE resource_key=?`, lease.Binding.ResourceKey).Scan(&generation); err != nil {
		return ManagedWriterLease{}, err
	}
	lease.Generation = generation
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_work_leases(id,schema_version,workspace_id,board_id,parent_task_id,task_id,group_id,work_item_id,attempt_id,attempt_generation,session_id,resource_key,generation,revision,heartbeat_at,expires_at,created_at,released_at,release_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,NULL,'')`, lease.ID, lease.SchemaVersion, lease.Binding.WorkspaceID, lease.Binding.BoardID, lease.Binding.ParentTaskID, lease.Binding.TaskID, lease.Binding.GroupID, lease.Binding.WorkItemID, lease.Binding.AttemptID, lease.AttemptGeneration, lease.Binding.SessionID, lease.Binding.ResourceKey, lease.Generation, stamp(lease.HeartbeatAt), stamp(lease.ExpiresAt), stamp(lease.CreatedAt))
	if err != nil {
		return ManagedWriterLease{}, mapManagedLeaseWrite(err)
	}
	if err = tx.Commit(); err != nil {
		return ManagedWriterLease{}, err
	}
	return lease, nil
}

func (l *ManagedLedger) GetManagedWriterLease(ctx context.Context, id string) (ManagedWriterLease, error) {
	lease, err := scanManagedWriterLease(l.db.QueryRowContext(ctx, managedLeaseSelect+` WHERE id=?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return ManagedWriterLease{}, ErrManagedLeaseNotFound
		}
		return ManagedWriterLease{}, err
	}
	return lease, nil
}

// GetActiveManagedWriterLease returns only the active lease for this exact
// host-derived binding. Expiration is parsed and compared in Go at RFC3339Nano
// precision; SQLite text or Julian-date comparisons are deliberately avoided.
func (l *ManagedLedger) GetActiveManagedWriterLease(ctx context.Context, binding ManagedWriterBinding, now time.Time) (ManagedWriterLease, error) {
	if l == nil || l.db == nil || !validManagedLeaseBinding(binding) || now.IsZero() {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ManagedWriterLease{}, err
	}
	defer tx.Rollback()
	lease, err := scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE attempt_id=? AND released_at IS NULL`, binding.AttemptID))
	if err != nil {
		return ManagedWriterLease{}, managedLeaseReadErr(err)
	}
	if lease.Binding != binding || lease.AttemptGeneration < 1 || lease.Generation < 1 {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseScope
	}
	if err = validateManagedLeaseScopeTx(ctx, tx, binding, lease.AttemptGeneration, 0, true, false); err != nil {
		return ManagedWriterLease{}, err
	}
	if !lease.ExpiresAt.After(now) {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseExpired
	}
	if err = tx.Commit(); err != nil {
		return ManagedWriterLease{}, err
	}
	return lease, nil
}

func (l *ManagedLedger) HeartbeatManagedWriterLease(ctx context.Context, id string, generation int64, expectedRevision int64, binding ManagedWriterBinding, now, expires time.Time) (ManagedWriterLease, error) {
	if generation < 1 || expectedRevision < 1 || !validManagedLeaseBinding(binding) || !expires.After(now) {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	defer tx.Rollback()
	lease, err := scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE id=?`, id))
	if err != nil {
		return ManagedWriterLease{}, managedLeaseReadErr(err)
	}
	if lease.Generation != generation || lease.Revision != expectedRevision || lease.ReleasedAt != nil || lease.Binding != binding {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseStale
	}
	if err = validateManagedLeaseScopeTx(ctx, tx, binding, lease.AttemptGeneration, 0, false, false); err != nil {
		return ManagedWriterLease{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_leases SET heartbeat_at=?,expires_at=?,revision=revision+1 WHERE id=? AND generation=? AND revision=? AND released_at IS NULL AND schema_version=?`, stamp(now), stamp(expires), id, generation, expectedRevision, agentwork.ManagedWriterLeaseSchemaVersion)
	if err != nil {
		return ManagedWriterLease{}, mapManagedLeaseWrite(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseStale
	}
	lease, err = scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE id=?`, id))
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if err = tx.Commit(); err != nil {
		return ManagedWriterLease{}, err
	}
	return lease, nil
}

func (l *ManagedLedger) ReleaseManagedWriterLease(ctx context.Context, id string, generation int64, expectedRevision int64, proof ManagedWriterQuiescence, now time.Time) (ManagedWriterLease, error) {
	if generation < 1 || expectedRevision < 1 || !proof.Known || !proof.Quiescent || !proof.Isolated || proof.CheckedAt.IsZero() {
		return ManagedWriterLease{}, agentwork.ErrManagedQuiescenceUnknown
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	defer tx.Rollback()
	lease, err := scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE id=?`, id))
	if err != nil {
		return ManagedWriterLease{}, managedLeaseReadErr(err)
	}
	if lease.Generation != generation || lease.Revision != expectedRevision || lease.ReleasedAt != nil || lease.Binding != proof.Binding || lease.Generation != proof.Generation {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseStale
	}
	if err = validateManagedLeaseScopeTx(ctx, tx, lease.Binding, lease.AttemptGeneration, 0, false, true); err != nil {
		return ManagedWriterLease{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_work_leases SET released_at=?,release_reason='quiescent_isolated',revision=revision+1 WHERE id=? AND generation=? AND revision=? AND released_at IS NULL AND schema_version=?`, stamp(now), id, generation, expectedRevision, agentwork.ManagedWriterLeaseSchemaVersion)
	if err != nil {
		return ManagedWriterLease{}, mapManagedLeaseWrite(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ManagedWriterLease{}, agentwork.ErrManagedLeaseStale
	}
	res, err = tx.ExecContext(ctx, `UPDATE agent_work_items SET current_attempt_id=NULL,state='blocked',revision=revision+1,updated_at=? WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=? AND current_attempt_id=? AND schema_version=?`, stamp(now), lease.Binding.WorkItemID, lease.Binding.GroupID, lease.Binding.WorkspaceID, lease.Binding.BoardID, lease.Binding.ParentTaskID, lease.Binding.AttemptID, agentwork.ManagedSchemaVersion)
	if err != nil {
		return ManagedWriterLease{}, err
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		return ManagedWriterLease{}, ErrManagedConflict
	}
	lease, err = scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE id=?`, id))
	if err != nil {
		return ManagedWriterLease{}, err
	}
	if err = tx.Commit(); err != nil {
		return ManagedWriterLease{}, err
	}
	return lease, nil
}

func (l *ManagedLedger) AuthorizeManagedWriter(ctx context.Context, binding ManagedWriterBinding, now time.Time) error {
	if l == nil || l.db == nil || !validManagedLeaseBinding(binding) || now.IsZero() {
		return ErrManagedScope
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lease, err := scanManagedWriterLease(tx.QueryRowContext(ctx, managedLeaseSelect+` WHERE session_id=? AND resource_key=? AND released_at IS NULL`, binding.SessionID, binding.ResourceKey))
	if err != nil {
		if err == sql.ErrNoRows {
			return agentwork.ErrManagedLeaseRequired
		}
		return err
	}
	if lease.Binding != binding {
		return ErrManagedScope
	}
	if lease.ExpiresAt.Before(now) || lease.ExpiresAt.Equal(now) {
		return agentwork.ErrManagedLeaseExpired
	}
	if err = validateManagedLeaseScopeTx(ctx, tx, binding, lease.AttemptGeneration, 0, true, false); err != nil {
		return err
	}
	return tx.Commit()
}

func validateManagedLeaseScopeTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, b ManagedWriterBinding, attemptGeneration, expectedRevision int64, requireEnabled, allowStaleTaskRevision bool) error {
	var a ManagedWorkAttempt
	a, err := scanManagedAttempt(q.QueryRowContext(ctx, `SELECT a.id,a.work_item_id,a.group_id,a.workspace_id,a.board_id,a.parent_task_id,a.session_id,a.role,a.state,a.dispatch_state,a.task_revision,a.requirements_revision,a.generation,a.revision,a.created_at,a.updated_at,a.schema_version FROM agent_work_attempts a JOIN agent_work_items i ON i.id=a.work_item_id WHERE a.id=? AND i.id=? AND i.group_id=? AND i.workspace_id=? AND i.board_id=? AND i.parent_task_id=? AND i.current_attempt_id=a.id AND a.schema_version=? AND i.schema_version=?`, b.AttemptID, b.WorkItemID, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID, agentwork.ManagedSchemaVersion, agentwork.ManagedSchemaVersion))
	if err != nil {
		return managedReadErr(err)
	}
	if a.Generation != attemptGeneration || expectedRevision > 0 && a.Revision != expectedRevision || a.WorkItemID != b.WorkItemID || a.GroupID != b.GroupID || a.WorkspaceID != b.WorkspaceID || a.BoardID != b.BoardID || a.ParentTaskID != b.ParentTaskID || a.SessionID != "" && a.SessionID != b.SessionID || a.State != "session_created" && a.State != "task_assigned" && a.State != "prompt_enqueued" && a.State != "running" {
		return ErrManagedScope
	}
	if !allowStaleTaskRevision {
		var itemTaskRevision int64
		var itemRequirementsRevision string
		if err = q.QueryRowContext(ctx, `SELECT task_revision,requirements_revision FROM agent_work_items WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=? AND current_attempt_id=?`, b.WorkItemID, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID, b.AttemptID).Scan(&itemTaskRevision, &itemRequirementsRevision); err != nil {
			return managedReadErr(err)
		}
		if itemTaskRevision != a.TaskRevision || itemRequirementsRevision != a.RequirementsRevision {
			return agentwork.ErrManagedLeaseStale
		}
	}
	var groupSchema, enabled, paused int
	var groupEnded sql.NullString
	if err = q.QueryRowContext(ctx, `SELECT schema_version,enabled,paused,ended_at FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID).Scan(&groupSchema, &enabled, &paused, &groupEnded); err != nil {
		return managedReadErr(err)
	}
	if groupSchema != agentwork.ManagedSchemaVersion {
		return ErrManagedInvalid
	}
	if requireEnabled && (enabled != 1 || paused != 0 || groupEnded.Valid) {
		return ErrManagedPolicyClosed
	}
	var itemSchema int
	var itemEnded, tombstone string
	if err = q.QueryRowContext(ctx, `SELECT schema_version,COALESCE(ended_at,''),tombstone_reason FROM agent_work_items WHERE id=? AND group_id=? AND current_attempt_id=?`, b.WorkItemID, b.GroupID, b.AttemptID).Scan(&itemSchema, &itemEnded, &tombstone); err != nil {
		return managedReadErr(err)
	}
	if itemSchema != agentwork.ManagedSchemaVersion {
		return ErrManagedInvalid
	}
	if requireEnabled && (itemEnded != "" || tombstone != "") {
		return ErrManagedScope
	}
	return nil
}

func scanManagedWriterLease(s managedLeaseScanner) (ManagedWriterLease, error) {
	var lease ManagedWriterLease
	var hb, expires, created string
	var released sql.NullString
	err := s.Scan(&lease.ID, &lease.SchemaVersion, &lease.Binding.WorkspaceID, &lease.Binding.BoardID, &lease.Binding.ParentTaskID, &lease.Binding.TaskID, &lease.Binding.GroupID, &lease.Binding.WorkItemID, &lease.Binding.AttemptID, &lease.AttemptGeneration, &lease.Binding.SessionID, &lease.Binding.ResourceKey, &lease.Generation, &lease.Revision, &hb, &expires, &created, &released, &lease.ReleaseReason)
	if err != nil {
		return lease, err
	}
	if lease.SchemaVersion != agentwork.ManagedWriterLeaseSchemaVersion {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	var parseErr error
	lease.HeartbeatAt, parseErr = parseStamp(hb)
	if parseErr != nil {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	lease.ExpiresAt, parseErr = parseStamp(expires)
	if parseErr != nil || !lease.ExpiresAt.After(lease.HeartbeatAt) {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	lease.CreatedAt, parseErr = parseStamp(created)
	if parseErr != nil {
		return ManagedWriterLease{}, ErrManagedInvalid
	}
	if released.Valid {
		t, parseErr := parseStamp(released.String)
		if parseErr != nil {
			return ManagedWriterLease{}, ErrManagedInvalid
		}
		lease.ReleasedAt = &t
	}
	return lease, nil
}
func validManagedLeaseBinding(b ManagedWriterBinding) bool {
	return b.WorkspaceID != "" && b.BoardID != "" && b.ParentTaskID != "" && b.TaskID != "" && b.GroupID != "" && b.WorkItemID != "" && b.AttemptID != "" && b.SessionID != "" && b.ResourceKey != ""
}
func managedLeaseReadErr(err error) error {
	if err == sql.ErrNoRows {
		return ErrManagedLeaseNotFound
	}
	return err
}
func mapManagedLeaseWrite(err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "unique constraint failed") {
		return ErrManagedLeaseConflict
	}
	return err
}
