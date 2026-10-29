package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"devkit/internal/agentwork"
)

func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func validHarnessBinding(b agentwork.HarnessBinding) bool {
	if b.ID == "" || b.SchemaVersion != agentwork.HarnessBindingSchemaVersion || b.Facts.SourceSchema != agentwork.HarnessSourceSchemaSupported {
		return false
	}
	if b.WorkspaceID == "" || b.BoardID == "" || b.ParentTaskID == "" || b.GroupID == "" || b.WorkItemID == "" || b.TaskID == "" || b.AttemptID == "" || b.PackageID == "" || !validManagedRole(b.Role) {
		return false
	}
	switch b.Phase {
	case agentwork.HarnessPhaseIntended:
		if b.SessionID != "" {
			return false
		}
	case agentwork.HarnessPhaseActual:
		if b.SessionID == "" {
			return false
		}
	default:
		return false
	}
	f := b.Facts
	if f.Vendor == "" || f.WorkspaceID == "" || f.VendorRevision < 0 || f.Fingerprint == "" || f.ExecutableDigest == "" || len(f.Fingerprint) > 200 || len(f.ExecutableDigest) > 200 {
		return false
	}
	// An absent revision is nil, never zero.
	if (f.WorkspaceRevision != nil && *f.WorkspaceRevision < 1) || (f.SessionRevision != nil && *f.SessionRevision < 1) {
		return false
	}
	return len(b.Required) > 0
}

// InsertHarnessBinding appends one binding. In one transaction it re-reads and
// fences on the attempt revision, the item's current-attempt pointer, the
// attempt state for the phase, an enabled unpaused group, a live item and the
// package belonging to that attempt. A second row for the same attempt and
// phase is ErrHarnessBindingExists and never replaces the first.
func (l *ManagedLedger) InsertHarnessBinding(ctx context.Context, b agentwork.HarnessBinding, expectedAttemptRevision int64) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("managed harness database is nil")
	}
	if expectedAttemptRevision < 1 || !validHarnessBinding(b) {
		return ErrManagedInvalid
	}
	required, err := json.Marshal(b.Required)
	if err != nil || len(required) > 1024 {
		return ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled, paused, groupSchema int
	var groupEnded sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT enabled,paused,ended_at,schema_version FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID).Scan(&enabled, &paused, &groupEnded, &groupSchema); err != nil {
		return scopeReadErr(err)
	}
	if groupSchema != agentwork.ManagedSchemaVersion {
		return ErrManagedInvalid
	}
	var itemSchema int
	var itemTaskID, itemRole, itemCurrent, itemTombstone string
	var itemEnded sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,task_id,role,COALESCE(current_attempt_id,''),tombstone_reason,ended_at FROM agent_work_items WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, b.WorkItemID, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID).Scan(&itemSchema, &itemTaskID, &itemRole, &itemCurrent, &itemTombstone, &itemEnded); err != nil {
		return scopeReadErr(err)
	}
	var attemptSchema int
	var attemptRevision int64
	var attemptRole, attemptState, attemptSession string
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,revision,role,state,session_id FROM agent_work_attempts WHERE id=? AND work_item_id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, b.AttemptID, b.WorkItemID, b.GroupID, b.WorkspaceID, b.BoardID, b.ParentTaskID).Scan(&attemptSchema, &attemptRevision, &attemptRole, &attemptState, &attemptSession); err != nil {
		return scopeReadErr(err)
	}
	if itemSchema != agentwork.ManagedSchemaVersion || attemptSchema != agentwork.ManagedSchemaVersion {
		return ErrManagedInvalid
	}
	if itemTaskID != b.TaskID || itemRole != b.Role || attemptRole != b.Role {
		return ErrManagedScope
	}
	if itemCurrent != b.AttemptID {
		return agentwork.ErrManagedPayloadStale
	}
	if attemptRevision != expectedAttemptRevision {
		return ErrManagedConflict
	}
	if enabled != 1 || paused != 0 || groupEnded.Valid || itemEnded.Valid || itemTombstone != "" {
		return agentwork.ErrHarnessPolicyClosed
	}
	switch b.Phase {
	case agentwork.HarnessPhaseIntended:
		if attemptState != "reserved" {
			return agentwork.ErrHarnessAttemptState
		}
	case agentwork.HarnessPhaseActual:
		if attemptState != "session_created" && attemptState != "task_assigned" {
			return agentwork.ErrHarnessAttemptState
		}
		if attemptSession == "" || attemptSession != b.SessionID {
			return ErrManagedScope
		}
		var intendedPackage string
		if err = tx.QueryRowContext(ctx, `SELECT package_id FROM agent_work_harness_bindings WHERE attempt_id=? AND phase='intended'`, b.AttemptID).Scan(&intendedPackage); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrManagedScope
			}
			return err
		}
		if intendedPackage != b.PackageID {
			return ErrManagedScope
		}
	}
	var one int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM agent_work_packages WHERE id=? AND attempt_id=? AND work_item_id=? AND group_id=? AND workspace_id=? AND task_id=?`, b.PackageID, b.AttemptID, b.WorkItemID, b.GroupID, b.WorkspaceID, b.TaskID).Scan(&one); err != nil {
		return scopeReadErr(err)
	}
	var session any
	if b.SessionID != "" {
		session = b.SessionID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_work_harness_bindings
		(id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,package_id,phase,binding_schema,source_schema,role,session_id,vendor,harness_workspace_id,vendor_revision,workspace_revision,session_revision,fingerprint,executable_digest,facts_complete,is_default,required_groups,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.WorkspaceID, b.BoardID, b.ParentTaskID, b.GroupID, b.WorkItemID, b.TaskID, b.AttemptID, b.PackageID, b.Phase, b.SchemaVersion, b.Facts.SourceSchema, b.Role, session,
		b.Facts.Vendor, b.Facts.WorkspaceID, b.Facts.VendorRevision, nullableInt(b.Facts.WorkspaceRevision), nullableInt(b.Facts.SessionRevision), b.Facts.Fingerprint, b.Facts.ExecutableDigest,
		boolInt(b.Facts.FactsComplete), boolInt(b.Facts.Default), string(required), stamp(b.CreatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "agent_work_harness_bindings.attempt_id") {
			return agentwork.ErrHarnessBindingExists
		}
		return mapManagedWrite(err)
	}
	return tx.Commit()
}

// scopeReadErr reports a row that does not exist in the binding's own scope as
// a scope mismatch, so a foreign or invented id is never a plain not-found.
func scopeReadErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrManagedScope
	}
	return err
}

// GetHarnessBinding reads one phase. A missing row is ErrManagedNotFound and an
// unknown binding or source schema, or an unreadable group list, fails closed.
func (l *ManagedLedger) GetHarnessBinding(ctx context.Context, attemptID, phase string) (agentwork.HarnessBinding, error) {
	if l == nil || l.db == nil || attemptID == "" || (phase != agentwork.HarnessPhaseIntended && phase != agentwork.HarnessPhaseActual) {
		return agentwork.HarnessBinding{}, ErrManagedInvalid
	}
	var b agentwork.HarnessBinding
	var session sql.NullString
	var workspaceRev, sessionRev sql.NullInt64
	var complete, isDefault int
	var required, created string
	err := l.db.QueryRowContext(ctx, `SELECT id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,package_id,phase,binding_schema,source_schema,role,session_id,vendor,harness_workspace_id,vendor_revision,workspace_revision,session_revision,fingerprint,executable_digest,facts_complete,is_default,required_groups,created_at
		FROM agent_work_harness_bindings WHERE attempt_id=? AND phase=?`, attemptID, phase).Scan(
		&b.ID, &b.WorkspaceID, &b.BoardID, &b.ParentTaskID, &b.GroupID, &b.WorkItemID, &b.TaskID, &b.AttemptID, &b.PackageID, &b.Phase, &b.SchemaVersion, &b.Facts.SourceSchema, &b.Role, &session,
		&b.Facts.Vendor, &b.Facts.WorkspaceID, &b.Facts.VendorRevision, &workspaceRev, &sessionRev, &b.Facts.Fingerprint, &b.Facts.ExecutableDigest, &complete, &isDefault, &required, &created)
	if err != nil {
		return agentwork.HarnessBinding{}, managedReadErr(err)
	}
	if b.SchemaVersion != agentwork.HarnessBindingSchemaVersion || b.Facts.SourceSchema != agentwork.HarnessSourceSchemaSupported {
		return agentwork.HarnessBinding{}, ErrManagedInvalid
	}
	if err = json.Unmarshal([]byte(required), &b.Required); err != nil {
		return agentwork.HarnessBinding{}, ErrManagedInvalid
	}
	if session.Valid {
		b.SessionID = session.String
	}
	if workspaceRev.Valid {
		v := workspaceRev.Int64
		b.Facts.WorkspaceRevision = &v
	}
	if sessionRev.Valid {
		v := sessionRev.Int64
		b.Facts.SessionRevision = &v
	}
	b.Facts.FactsComplete, b.Facts.Default = complete == 1, isDefault == 1
	b.CreatedAt, _ = parseStamp(created)
	if !validHarnessBinding(b) {
		return agentwork.HarnessBinding{}, ErrManagedInvalid
	}
	return b, nil
}
