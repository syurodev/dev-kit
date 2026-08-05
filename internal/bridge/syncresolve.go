package bridge

import (
	"context"
	"errors"
	"time"

	"changeme/internal/services/syncclient"
	"changeme/internal/services/vaultitem"
	"changeme/internal/storage"
	replicationsync "changeme/internal/sync"
)

// noteBodyKeyID mirrors notes.bodyKeyID. Edit-merge is notes-only, so the
// bridge re-seals the merged body under this inner key id.
const noteBodyKeyID = "note-body"

// syncSnapshotKeyID mirrors vaultitem's localSnapshotKeyID: the outer key id a
// materialised push uses, which a resolution mutation must reuse.
const syncSnapshotKeyID = "vault-item"

var errSyncNotLinked = errors.New("sync account is not linked")

// SyncConflictDetailDTO carries the on-demand decrypted note text for a
// conflict so the UI can show ours vs theirs. For non-note (secret) types no
// text is returned; the UI offers only Use ours / Use theirs.
type SyncConflictDetailDTO struct {
	RecordID       string `json:"record_id"`
	RecordType     string `json:"record_type"`
	TheirsCaptured bool   `json:"theirs_captured"`
	Mergeable      bool   `json:"mergeable"`
	OursText       string `json:"ours_text"`
	TheirsText     string `json:"theirs_text"`
}

func (s *AppService) SyncConflictDetail(id string) (SyncConflictDetailDTO, error) {
	result, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:conflicts", map[string]any{
		"action": "detail",
	}, func(ctx context.Context) (any, error) {
		return s.conflictDetail(ctx, id)
	})
	if err != nil {
		return SyncConflictDetailDTO{}, err
	}
	return result.(SyncConflictDetailDTO), nil
}

func (s *AppService) conflictDetail(ctx context.Context, id string) (SyncConflictDetailDTO, error) {
	if recordID(id) == invalidOpaqueID {
		return SyncConflictDetailDTO{}, errors.New("invalid record id")
	}
	if s.container.ConflictStore == nil || s.container.AccountReader == nil || s.container.Items == nil {
		return SyncConflictDetailDTO{}, syncclient.ErrInvalidDependencies
	}
	account, bound, err := s.container.AccountReader.BoundAccountID(ctx)
	if err != nil {
		return SyncConflictDetailDTO{}, err
	}
	if !bound {
		return SyncConflictDetailDTO{}, errSyncNotLinked
	}
	ours, err := s.container.Items.Get(id)
	if err != nil {
		return SyncConflictDetailDTO{}, err
	}
	dto := SyncConflictDetailDTO{RecordID: id, RecordType: ours.Type}
	conflict, found, err := s.container.ConflictStore.Get(ctx, account, id)
	if err != nil {
		return SyncConflictDetailDTO{}, err
	}
	dto.TheirsCaptured = found
	if ours.Type == "note" {
		dto.Mergeable = true
		oursBody, err := s.container.Items.OpenPayload(ours, noteBodyKeyID)
		if err != nil {
			return SyncConflictDetailDTO{}, err
		}
		dto.OursText = string(oursBody)
		if found {
			theirsRecord, err := vaultitem.DecodeRecord(conflict.TheirsSnapshot)
			if err != nil {
				return SyncConflictDetailDTO{}, err
			}
			theirsBody, err := s.container.Items.OpenPayload(theirsRecord, noteBodyKeyID)
			if err != nil {
				return SyncConflictDetailDTO{}, err
			}
			dto.TheirsText = string(theirsBody)
		}
	}
	return dto, nil
}

func (s *AppService) SyncResolveConflict(id, strategy, mergedText string) error {
	_, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:resolve", map[string]any{
		// Only the non-secret strategy label is audited, never merged content.
		"action":   "resolve",
		"strategy": safeStrategy(strategy),
	}, func(ctx context.Context) (any, error) {
		return nil, s.resolveConflict(ctx, id, strategy, mergedText)
	})
	return err
}

func safeStrategy(strategy string) string {
	switch strategy {
	case "ours", "theirs", "merge":
		return strategy
	default:
		return invalidOpaqueID
	}
}

func (s *AppService) resolveConflict(ctx context.Context, id, strategy, mergedText string) error {
	if recordID(id) == invalidOpaqueID {
		return errors.New("invalid record id")
	}
	if s.container.ConflictResolver == nil || s.container.ConflictStore == nil ||
		s.container.AccountReader == nil || s.container.Items == nil {
		return syncclient.ErrInvalidDependencies
	}
	account, bound, err := s.container.AccountReader.BoundAccountID(ctx)
	if err != nil {
		return err
	}
	if !bound {
		return errSyncNotLinked
	}

	switch strategy {
	case "theirs":
		return s.container.ConflictResolver.ResolveUseTheirs(ctx, account, id)
	case "ours":
		ours, err := s.container.Items.Get(id)
		if err != nil {
			return err
		}
		return s.resolveUseLocal(ctx, account, id, ours)
	case "merge":
		ours, err := s.container.Items.Get(id)
		if err != nil {
			return err
		}
		if ours.Type != "note" {
			return errors.New("merge is only supported for notes")
		}
		merged, err := s.container.Items.Reseal(ours, noteBodyKeyID, []byte(mergedText))
		if err != nil {
			return err
		}
		return s.resolveUseLocal(ctx, account, id, merged)
	default:
		return errors.New("unknown resolution strategy")
	}
}

func (s *AppService) resolveUseLocal(ctx context.Context, account, id string, record vaultitem.Record) error {
	recordJSON, snapshot, err := vaultitem.MarshalForResolution(record)
	if err != nil {
		return err
	}
	operationID, err := replicationsync.NewOperationID()
	if err != nil {
		return err
	}
	return s.container.ConflictResolver.ResolveUseLocal(ctx, account, id, storage.LocalResolution{
		OperationID: operationID,
		Operation:   replicationsync.OperationUpdate,
		KeyVersion:  uint32(record.KeyVersion),
		KeyID:       syncSnapshotKeyID,
		RecordJSON:  recordJSON,
		Snapshot:    snapshot,
		CreatedAt:   time.Now().UTC(),
	})
}
