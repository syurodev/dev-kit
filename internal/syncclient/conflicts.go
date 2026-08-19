package syncclient

import "context"

// Conflicts lists captured sync conflicts (non-secret metadata only). It reads
// the captured "theirs" sides in sync_conflicts — the same source Detail and
// Resolve use — so every entry the UI lists is resolvable. It does not require
// an active connection; without a bound account the list is empty.
func (s *Service) Conflicts(ctx context.Context) ([]ConflictInfo, error) {
	if s == nil {
		return nil, ErrInvalidDependencies
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	account, bound, err := s.deps.Remote.BoundAccountID(ctx)
	if err != nil {
		return nil, ErrConflictsFailed
	}
	if !bound {
		return []ConflictInfo{}, nil
	}
	records, err := s.deps.Conflicts.List(ctx, account)
	if err != nil {
		return nil, ErrConflictsFailed
	}
	infos := make([]ConflictInfo, 0, len(records))
	for _, record := range records {
		infos = append(infos, ConflictInfo{
			RecordID:            record.RecordID,
			RecordType:          record.RecordType,
			RemoteOperation:     string(record.RemoteOperation),
			RemoteEntityVersion: record.RemoteEntityVersion,
			TheirsDeleted:       record.RemoteOperation == "delete",
			DetectedAt:          record.DetectedAt,
		})
	}
	return infos, nil
}
