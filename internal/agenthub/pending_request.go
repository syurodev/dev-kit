package agenthub

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	PendingRequestPermission  = "permission"
	PendingRequestElicitation = "elicitation"
	PendingRequestPending     = "pending"
	PendingRequestInterrupted = "interrupted"
)

var ErrPendingRequestNotFound = errors.New("agenthub: pending request not found")

// PendingRequestRecord contains only lifecycle metadata. It deliberately has
// no place for request params, question text, answers, or credentials.
type PendingRequestRecord struct {
	ID        string
	SessionID string
	Kind      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// PendingRequestResolution is a terminal lifecycle update, never response data.
type PendingRequestResolution struct {
	ID         string
	Reason     string
	ResolvedAt time.Time
}

// PendingRequestSummary is the sanitized session-level recovery view.
type PendingRequestSummary struct {
	SessionID   string
	Permissions int
	Questions   int
}

// PendingRequestStore is optional so in-memory/test Store implementations need
// not implement durable request recovery. Production SQLStore implements it.
type PendingRequestStore interface {
	RecordPendingRequest(context.Context, PendingRequestRecord) error
	ResolvePendingRequest(context.Context, PendingRequestResolution) error
	ListPendingRequestSummaries(context.Context) ([]PendingRequestSummary, error)
	MarkPendingRequestsInterrupted(context.Context, string, time.Time, string) error
	AcknowledgePendingRequestRecovery(context.Context, string) error
	PrunePendingRequests(context.Context, time.Time) error
}

func (st *SQLStore) RecordPendingRequest(ctx context.Context, r PendingRequestRecord) error {
	if r.ID == "" || r.SessionID == "" || (r.Kind != PendingRequestPermission && r.Kind != PendingRequestElicitation) {
		return errors.New("agenthub: invalid pending request metadata")
	}
	var expires any
	if !r.ExpiresAt.IsZero() {
		expires = formatStoreTime(r.ExpiresAt)
	}
	_, err := st.db.ExecContext(ctx, `INSERT INTO agent_pending_requests
		(id, session_id, kind, created_at, expires_at, resolution, reason)
		VALUES (?, ?, ?, ?, ?, 'pending', '')`, r.ID, r.SessionID, r.Kind, formatStoreTime(r.CreatedAt), expires)
	return err
}

func (st *SQLStore) ResolvePendingRequest(ctx context.Context, r PendingRequestResolution) error {
	res, err := st.db.ExecContext(ctx, `UPDATE agent_pending_requests
		SET resolution = 'resolved', reason = ?, resolved_at = ?
		WHERE id = ? AND resolution IN ('pending', 'resolved')`, r.Reason, formatStoreTime(r.ResolvedAt), r.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPendingRequestNotFound
	}
	return nil
}

// ListPendingRequestSummaries returns unresolved metadata grouped by session.
// It does not mutate rows: recovery acknowledges a session only after its stop,
// transcript notice, and idle status have all been persisted.
func (st *SQLStore) ListPendingRequestSummaries(ctx context.Context) ([]PendingRequestSummary, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT session_id, kind, COUNT(*)
		FROM agent_pending_requests WHERE resolution = 'pending' OR (resolution = 'interrupted' AND reason = 'app_restarted') GROUP BY session_id, kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bySession := map[string]*PendingRequestSummary{}
	for rows.Next() {
		var sessionID, kind string
		var count int
		if err := rows.Scan(&sessionID, &kind, &count); err != nil {
			return nil, err
		}
		summary := bySession[sessionID]
		if summary == nil {
			summary = &PendingRequestSummary{SessionID: sessionID}
			bySession[sessionID] = summary
		}
		switch kind {
		case PendingRequestPermission:
			summary.Permissions = count
		case PendingRequestElicitation:
			summary.Questions = count
		default:
			return nil, fmt.Errorf("agenthub: unknown pending request kind %q", kind)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]PendingRequestSummary, 0, len(bySession))
	for _, summary := range bySession {
		out = append(out, *summary)
	}
	return out, nil
}

// MarkPendingRequestsInterrupted permanently withdraws old IDs after startup
// has recorded the matching session's interruption and returned it to idle.
func (st *SQLStore) MarkPendingRequestsInterrupted(ctx context.Context, sessionID string, at time.Time, reason string) error {
	if reason == "" {
		reason = "app_restarted"
	}
	_, err := st.db.ExecContext(ctx, `UPDATE agent_pending_requests
		SET resolution = 'interrupted', reason = ?, resolved_at = ?
		WHERE session_id = ? AND (resolution = 'pending' OR (resolution = 'interrupted' AND reason = 'app_restarted'))`, reason, formatStoreTime(at), sessionID)
	return err
}

// AcknowledgePendingRequestRecovery forgets that an already-invalidated request
// still needs a Stop/notice retry after the session recovery has been persisted.
func (st *SQLStore) AcknowledgePendingRequestRecovery(ctx context.Context, sessionID string) error {
	_, err := st.db.ExecContext(ctx, `UPDATE agent_pending_requests SET reason = 'recovered'
		WHERE session_id = ? AND resolution = 'interrupted' AND reason = 'app_restarted'`, sessionID)
	return err
}

// PrunePendingRequests bounds lifecycle metadata retention without deleting
// active requests or interrupted records that recovery has not acknowledged.
func (st *SQLStore) PrunePendingRequests(ctx context.Context, before time.Time) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM agent_pending_requests
		WHERE resolution != 'pending' AND NOT (resolution = 'interrupted' AND reason = 'app_restarted')
		AND resolved_at < ?`, formatStoreTime(before))
	return err
}

var _ PendingRequestStore = (*SQLStore)(nil)

// HasInterruptedNotice queries the stable per-recovery marker without loading a
// session's full transcript. The payload contains only the local interruption
// update, never pending request data.
func (st *SQLStore) HasInterruptedNotice(ctx context.Context, sessionID, noticeID string) (bool, error) {
	var found int
	err := st.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM agent_transcript
		WHERE session_id = ? AND json_extract(payload, '$.failure.id') = ?
	)`, sessionID, noticeID).Scan(&found)
	return found != 0, err
}
