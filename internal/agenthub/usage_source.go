package agenthub

import (
	"context"
	"database/sql"
	"errors"
)

// ErrUsageUnavailable: the store cannot list what turns spent. Every shipped
// store can; this is for a custom one.
var ErrUsageUnavailable = errors.New("agenthub.usageUnavailable")

// TurnUsageSource is the part of a Store that lists what a vendor's turns spent.
// It is optional, like SessionLoader: the Service asks for it and reports
// ErrUsageUnavailable when a store has none.
type TurnUsageSource interface {
	TurnUsageRows(ctx context.Context, vendor AgentType) ([]TurnUsageRow, error)
}

var _ TurnUsageSource = (*SQLStore)(nil)

// TurnUsageRows lists every turn of a vendor's sessions, oldest first, with the
// session's workspace. It reads no message and no title.
func (st *SQLStore) TurnUsageRows(ctx context.Context, vendor AgentType) ([]TurnUsageRow, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT t.id, t.session_id, COALESCE(s.workspace_id, ''), t.created_at, t.usage_json, t.raw_meta
		FROM `+tableAgentTurns+` t
		JOIN `+tableAgentSessions+` s ON s.id = t.session_id
		WHERE s.agent_type = ?
		ORDER BY t.created_at ASC, t.id ASC`, string(vendor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TurnUsageRow, 0)
	for rows.Next() {
		var r TurnUsageRow
		var usageJSON, rawMeta sql.NullString
		var createdAt string
		if err := rows.Scan(&r.TurnID, &r.SessionID, &r.WorkspaceID, &createdAt, &usageJSON, &rawMeta); err != nil {
			return nil, err
		}
		if usageJSON.Valid {
			r.UsageJSON = []byte(usageJSON.String)
		}
		if rawMeta.Valid {
			r.RawMeta = []byte(rawMeta.String)
		}
		if r.CreatedAt, err = parseStoreTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageReport reports what a vendor's turns spent, in tokens. Window only, like
// every Settings read of the hub's own data.
func (s *Service) UsageReport(ctx context.Context, vendor AgentType) (UsageReport, error) {
	if err := requireWindow(ctx); err != nil {
		return UsageReport{}, err
	}
	adapter, ok := s.reg.Get(vendor)
	if !ok {
		return UsageReport{}, ErrVendorUnknown
	}
	source, ok := s.store.(TurnUsageSource)
	if !ok {
		return UsageReport{}, ErrUsageUnavailable
	}
	rows, err := source.TurnUsageRows(ctx, vendor)
	if err != nil {
		return UsageReport{}, err
	}
	return BuildUsageReport(vendor, adapter, rows), nil
}
