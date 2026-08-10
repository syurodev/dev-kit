package bridge

import (
	"context"
	"errors"

	"devkit/internal/syncclient"
)

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
		if recordID(id) == invalidOpaqueID {
			return nil, errors.New("invalid record id")
		}
		if s.container.SyncConflict == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		detail, err := s.container.SyncConflict.Detail(ctx, id)
		if err != nil {
			return nil, err
		}
		return SyncConflictDetailDTO{
			RecordID:       detail.RecordID,
			RecordType:     detail.RecordType,
			TheirsCaptured: detail.TheirsCaptured,
			Mergeable:      detail.Mergeable,
			OursText:       detail.OursText,
			TheirsText:     detail.TheirsText,
		}, nil
	})
	if err != nil {
		return SyncConflictDetailDTO{}, err
	}
	return result.(SyncConflictDetailDTO), nil
}

func (s *AppService) SyncResolveConflict(id, strategy, mergedText string) error {
	_, err := s.call(context.Background(), "builtin.sync", "sync:manage", "sync:resolve", map[string]any{
		// Only the non-secret strategy label is audited, never merged content.
		"action":   "resolve",
		"strategy": safeStrategy(strategy),
	}, func(ctx context.Context) (any, error) {
		if recordID(id) == invalidOpaqueID {
			return nil, errors.New("invalid record id")
		}
		if s.container.SyncConflict == nil {
			return nil, syncclient.ErrInvalidDependencies
		}
		return nil, s.container.SyncConflict.Resolve(ctx, id, strategy, mergedText)
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
