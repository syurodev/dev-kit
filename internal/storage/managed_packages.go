package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"devkit/internal/agentwork"
)

func (l *ManagedLedger) InsertContextPackage(ctx context.Context, record agentwork.StoredContextPackage) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("managed package database is nil")
	}
	if err := record.ContextPackage.ValidateMetadata(); err != nil {
		return ErrManagedInvalid
	}
	if err := validateSealedPayload(record.SealedBody, record.BodyHash); err != nil {
		return err
	}
	if len(record.SealedBody) > agentwork.ManagedSealedEnvelopeMaxBytes || record.ItemRevision < 1 || record.AttemptRevision < 1 {
		return ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled, paused int
	var groupEnded sql.NullString
	var groupSchema int
	err = tx.QueryRowContext(ctx, `SELECT enabled,paused,ended_at,schema_version FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&enabled, &paused, &groupEnded, &groupSchema)
	if err != nil {
		return managedReadErr(err)
	}
	if groupSchema != agentwork.ManagedSchemaVersion || groupEnded.Valid || enabled != 1 || paused != 0 {
		return agentwork.ErrManagedPayloadStale
	}
	var itemRevision, itemSchema int64
	var itemTaskID, itemRole, itemCurrentAttempt, itemRequirements, itemTombstone string
	var itemEnded sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT revision,schema_version,task_id,role,COALESCE(current_attempt_id,''),requirements_revision,tombstone_reason,ended_at FROM agent_work_items WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.WorkItemID, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&itemRevision, &itemSchema, &itemTaskID, &itemRole, &itemCurrentAttempt, &itemRequirements, &itemTombstone, &itemEnded)
	if err != nil {
		return managedReadErr(err)
	}
	var attemptGeneration, attemptRevision, attemptSchema int64
	var attemptRole, attemptState, attemptRequirements string
	err = tx.QueryRowContext(ctx, `SELECT generation,revision,schema_version,role,state,requirements_revision FROM agent_work_attempts WHERE id=? AND work_item_id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.AttemptID, record.WorkItemID, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&attemptGeneration, &attemptRevision, &attemptSchema, &attemptRole, &attemptState, &attemptRequirements)
	if err != nil {
		return managedReadErr(err)
	}
	if itemRevision != record.ItemRevision || attemptRevision != record.AttemptRevision || itemSchema != agentwork.ManagedSchemaVersion || attemptSchema != agentwork.ManagedSchemaVersion || itemEnded.Valid || itemTombstone != "" || itemTaskID != record.TaskID || itemRole != record.Role || attemptRole != record.Role || itemCurrentAttempt != record.AttemptID || attemptGeneration != record.Generation || attemptState != "reserved" || itemRequirements != record.RequirementsRevision || attemptRequirements != record.RequirementsRevision {
		return agentwork.ErrManagedPayloadStale
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_work_packages
		(id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,generation,item_revision,attempt_revision,requirements_revision,role,schema_version,body_hash,body_size,sealed_body,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.WorkspaceID, record.BoardID, record.ParentTaskID, record.GroupID, record.WorkItemID, record.TaskID, record.AttemptID, record.Generation, record.ItemRevision, record.AttemptRevision, record.RequirementsRevision, record.Role, record.SchemaVersion, record.BodyHash, record.BodyBytes, record.SealedBody, stamp(record.CreatedAt))
	if err != nil {
		return mapManagedWrite(err)
	}
	return tx.Commit()
}

func (l *ManagedLedger) GetContextPackage(ctx context.Context, id string) (agentwork.StoredContextPackage, error) {
	if l == nil || l.db == nil || id == "" {
		return agentwork.StoredContextPackage{}, ErrManagedInvalid
	}
	var size int64
	err := l.db.QueryRowContext(ctx, `SELECT length(sealed_body) FROM agent_work_packages WHERE id=?`, id).Scan(&size)
	if err != nil {
		return agentwork.StoredContextPackage{}, managedReadErr(err)
	}
	if size < 1 || size > agentwork.ManagedSealedEnvelopeMaxBytes {
		return agentwork.StoredContextPackage{}, ErrManagedInvalid
	}
	var record agentwork.StoredContextPackage
	var created string
	err = l.db.QueryRowContext(ctx, `SELECT id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,generation,requirements_revision,role,schema_version,body_hash,body_size,created_at,sealed_body,item_revision,attempt_revision FROM agent_work_packages WHERE id=?`, id).Scan(
		&record.ID, &record.WorkspaceID, &record.BoardID, &record.ParentTaskID, &record.GroupID, &record.WorkItemID, &record.TaskID, &record.AttemptID, &record.Generation, &record.RequirementsRevision, &record.Role, &record.SchemaVersion, &record.BodyHash, &record.BodyBytes, &created, &record.SealedBody, &record.ItemRevision, &record.AttemptRevision)
	if err != nil {
		return record, managedReadErr(err)
	}
	record.CreatedAt, _ = parseStamp(created)
	if err = record.ContextPackage.ValidateMetadata(); err != nil || len(record.SealedBody) != int(size) || validateSealedPayload(record.SealedBody, record.BodyHash) != nil {
		return agentwork.StoredContextPackage{}, ErrManagedInvalid
	}
	return record, nil
}

func (l *ManagedLedger) InsertArtifact(ctx context.Context, record agentwork.StoredArtifact) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("managed artifact database is nil")
	}
	if err := record.ArtifactMetadata.Validate(); err != nil {
		return ErrManagedInvalid
	}
	if err := validateSealedPayload(record.SealedBody, record.BodyHash); err != nil {
		return err
	}
	if len(record.SealedBody) > agentwork.ManagedSealedEnvelopeMaxBytes || record.ItemRevision < 1 || record.AttemptRevision < 1 {
		return ErrManagedInvalid
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupEnded sql.NullString
	var groupSchema int
	err = tx.QueryRowContext(ctx, `SELECT ended_at,schema_version FROM agent_work_groups WHERE id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&groupEnded, &groupSchema)
	if err != nil {
		return managedReadErr(err)
	}
	if groupEnded.Valid || groupSchema != agentwork.ManagedSchemaVersion {
		return agentwork.ErrManagedPayloadStale
	}
	var itemRevision, itemSchema int64
	var itemTaskID, itemRole, itemCurrentAttempt, itemRequirements, itemTombstone string
	var itemEnded sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT revision,schema_version,task_id,role,COALESCE(current_attempt_id,''),requirements_revision,tombstone_reason,ended_at FROM agent_work_items WHERE id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.WorkItemID, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&itemRevision, &itemSchema, &itemTaskID, &itemRole, &itemCurrentAttempt, &itemRequirements, &itemTombstone, &itemEnded)
	if err != nil {
		return managedReadErr(err)
	}
	var attemptGeneration, attemptRevision, attemptSchema int64
	var attemptRole, attemptState, attemptRequirements, sessionID string
	err = tx.QueryRowContext(ctx, `SELECT generation,revision,schema_version,role,state,requirements_revision,session_id FROM agent_work_attempts WHERE id=? AND work_item_id=? AND group_id=? AND workspace_id=? AND board_id=? AND parent_task_id=?`, record.AttemptID, record.WorkItemID, record.GroupID, record.WorkspaceID, record.BoardID, record.ParentTaskID).Scan(&attemptGeneration, &attemptRevision, &attemptSchema, &attemptRole, &attemptState, &attemptRequirements, &sessionID)
	if err != nil {
		return managedReadErr(err)
	}
	if itemRevision != record.ItemRevision || attemptRevision != record.AttemptRevision || itemSchema != agentwork.ManagedSchemaVersion || attemptSchema != agentwork.ManagedSchemaVersion || itemEnded.Valid || itemTombstone != "" || itemTaskID != record.TaskID || itemRole != record.Role || attemptRole != record.Role || itemCurrentAttempt != record.AttemptID || attemptGeneration != record.Generation || attemptState != "running" || attemptRequirements != record.RequirementsRevision || itemRequirements != record.RequirementsRevision || sessionID != record.ProducerSessionID {
		return agentwork.ErrManagedPayloadStale
	}
	var leaseGeneration int64
	var expiresAt string
	err = tx.QueryRowContext(ctx, `SELECT generation,expires_at FROM agent_work_leases
		WHERE attempt_id=? AND attempt_generation=? AND session_id=? AND workspace_id=? AND board_id=? AND parent_task_id=? AND task_id=? AND group_id=? AND work_item_id=? AND released_at IS NULL AND schema_version=?`,
		record.AttemptID, record.Generation, record.ProducerSessionID, record.WorkspaceID, record.BoardID, record.ParentTaskID, record.TaskID, record.GroupID, record.WorkItemID, agentwork.ManagedWriterLeaseSchemaVersion).Scan(&leaseGeneration, &expiresAt)
	if err != nil {
		return agentwork.ErrManagedPayloadStale
	}
	expiry, parseErr := parseStamp(expiresAt)
	if parseErr != nil || leaseGeneration != record.LeaseGeneration || !expiry.After(l.now().UTC()) {
		return agentwork.ErrManagedPayloadStale
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_work_artifacts
		(id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,generation,lease_generation,item_revision,attempt_revision,requirements_revision,kind,provenance,producer_session_id,role,schema_version,body_hash,body_size,sealed_body,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.WorkspaceID, record.BoardID, record.ParentTaskID, record.GroupID, record.WorkItemID, record.TaskID, record.AttemptID, record.Generation, record.LeaseGeneration, record.ItemRevision, record.AttemptRevision, record.RequirementsRevision, record.Kind, record.Provenance, record.ProducerSessionID, record.Role, record.SchemaVersion, record.BodyHash, record.BodyBytes, record.SealedBody, stamp(record.CreatedAt))
	if err != nil {
		return mapManagedWrite(err)
	}
	return tx.Commit()
}

func (l *ManagedLedger) GetArtifact(ctx context.Context, id string) (agentwork.StoredArtifact, error) {
	if l == nil || l.db == nil || id == "" {
		return agentwork.StoredArtifact{}, ErrManagedInvalid
	}
	var size int64
	err := l.db.QueryRowContext(ctx, `SELECT length(sealed_body) FROM agent_work_artifacts WHERE id=?`, id).Scan(&size)
	if err != nil {
		return agentwork.StoredArtifact{}, managedReadErr(err)
	}
	if size < 1 || size > agentwork.ManagedSealedEnvelopeMaxBytes {
		return agentwork.StoredArtifact{}, ErrManagedInvalid
	}
	var record agentwork.StoredArtifact
	var created string
	err = l.db.QueryRowContext(ctx, `SELECT id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,generation,lease_generation,requirements_revision,kind,provenance,producer_session_id,role,schema_version,body_hash,body_size,created_at,sealed_body,item_revision,attempt_revision FROM agent_work_artifacts WHERE id=?`, id).Scan(
		&record.ID, &record.WorkspaceID, &record.BoardID, &record.ParentTaskID, &record.GroupID, &record.WorkItemID, &record.TaskID, &record.AttemptID, &record.Generation, &record.LeaseGeneration, &record.RequirementsRevision, &record.Kind, &record.Provenance, &record.ProducerSessionID, &record.Role, &record.SchemaVersion, &record.BodyHash, &record.BodyBytes, &created, &record.SealedBody, &record.ItemRevision, &record.AttemptRevision)
	if err != nil {
		return record, managedReadErr(err)
	}
	record.CreatedAt, _ = parseStamp(created)
	if err = record.ArtifactMetadata.Validate(); err != nil || len(record.SealedBody) != int(size) || validateSealedPayload(record.SealedBody, record.BodyHash) != nil {
		return agentwork.StoredArtifact{}, ErrManagedInvalid
	}
	return record, nil
}

func (l *ManagedLedger) ListArtifacts(ctx context.Context, workItemID, after string, limit int) (agentwork.ArtifactPage, error) {
	if l == nil || l.db == nil || workItemID == "" || limit < 1 || limit > agentwork.ManagedArtifactPageMax {
		return agentwork.ArtifactPage{}, ErrManagedInvalid
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,generation,lease_generation,requirements_revision,kind,provenance,producer_session_id,role,schema_version,body_hash,body_size,created_at FROM agent_work_artifacts WHERE work_item_id=? AND id>? ORDER BY id LIMIT ?`, workItemID, after, limit+1)
	if err != nil {
		return agentwork.ArtifactPage{}, err
	}
	defer rows.Close()
	page := agentwork.ArtifactPage{Items: make([]agentwork.ArtifactMetadata, 0, limit)}
	for rows.Next() {
		metadata, scanErr := scanArtifactMetadata(rows)
		if scanErr != nil {
			return page, scanErr
		}
		page.Items = append(page.Items, metadata)
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

type payloadScanner interface{ Scan(...any) error }

func scanArtifactMetadata(row payloadScanner) (agentwork.ArtifactMetadata, error) {
	var metadata agentwork.ArtifactMetadata
	var created string
	err := row.Scan(&metadata.ID, &metadata.WorkspaceID, &metadata.BoardID, &metadata.ParentTaskID, &metadata.GroupID, &metadata.WorkItemID, &metadata.TaskID, &metadata.AttemptID, &metadata.Generation, &metadata.LeaseGeneration, &metadata.RequirementsRevision, &metadata.Kind, &metadata.Provenance, &metadata.ProducerSessionID, &metadata.Role, &metadata.SchemaVersion, &metadata.BodyHash, &metadata.BodyBytes, &created)
	if err != nil {
		return metadata, err
	}
	metadata.CreatedAt, _ = parseStamp(created)
	if err = metadata.Validate(); err != nil {
		return agentwork.ArtifactMetadata{}, ErrManagedInvalid
	}
	return metadata, nil
}

func validateSealedPayload(sealed []byte, bodyHash string) error {
	if len(sealed) == 0 || len(sealed) > agentwork.ManagedSealedEnvelopeMaxBytes || !strings.HasPrefix(bodyHash, "sha256:") {
		return ErrManagedInvalid
	}
	hash := sha256.Sum256(sealed)
	if bodyHash != "sha256:"+hex.EncodeToString(hash[:]) {
		return ErrManagedInvalid
	}
	return nil
}

var _ agentwork.ManagedPayloadStore = (*ManagedLedger)(nil)
