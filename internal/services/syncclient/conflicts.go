package syncclient

import "context"

// Conflicts lists locally recorded push conflicts (non-secret metadata only).
// It reads the outbox and does not require an active connection.
func (s *Service) Conflicts(ctx context.Context) ([]ConflictInfo, error) {
	if s == nil {
		return nil, ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	changes, err := s.deps.Outbox.Conflicts()
	if err != nil {
		return nil, ErrConflictsFailed
	}
	infos := make([]ConflictInfo, 0, len(changes))
	for _, change := range changes {
		if change.Conflict == nil {
			continue
		}
		infos = append(infos, ConflictInfo{
			RecordID:        change.Conflict.RecordID,
			RecordType:      change.Conflict.RecordType,
			LocalChangeKey:  change.Conflict.LocalChangeKey,
			RemoteChangeKey: change.Conflict.RemoteChangeKey,
			DetectedAt:      change.Conflict.DetectedAt,
		})
	}
	return infos, nil
}
