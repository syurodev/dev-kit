package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// GroupRequestStore is the ledger of child tool-group requests and the group
// locks. It is an optional capability of a Store: without it the service answers
// ErrGroupRequestUnavailable and no request can be made.
type GroupRequestStore interface {
	InsertGroupRequest(ctx context.Context, r GroupRequest) error
	GetGroupRequest(ctx context.Context, id string) (GroupRequest, bool, error)
	// ListGroupRequests returns requests of a child, newest first. openOnly keeps
	// the two pending states.
	ListGroupRequestsOfChild(ctx context.Context, childID string, openOnly bool) ([]GroupRequest, error)
	// ListOpenGroupRequestsOfParent returns the open requests whose recorded
	// parent is parentID, newest first.
	ListOpenGroupRequestsOfParent(ctx context.Context, parentID string) ([]GroupRequest, error)
	ListOpenUserGroupRequests(ctx context.Context) ([]GroupRequest, error)
	// UpdateGroupRequest replaces the mutable fields of r if the stored revision
	// is expect, and reports whether it did. A lost compare-and-swap is (false, nil).
	UpdateGroupRequest(ctx context.Context, r GroupRequest, expect int64) (bool, error)
	// InvalidateGroupRequests closes open requests with state invalidated.
	// childID closes every open request of that child; parentID closes the open
	// parent-route requests recorded under that parent; both empty closes every
	// open request. It returns how many rows changed.
	InvalidateGroupRequests(ctx context.Context, childID, parentID, outcome string, at time.Time) (int, error)

	GroupLocks(ctx context.Context, sessionID string) ([]string, error)
	SetGroupLock(ctx context.Context, sessionID, group string, locked bool, at time.Time) error
}

var _ GroupRequestStore = (*SQLStore)(nil)

const groupRequestColumns = `id,child_id,parent_id,groups,reason,route,state,revision,resolver,outcome,created_at,updated_at,resolved_at`

func scanGroupRequest(scan func(dest ...any) error) (GroupRequest, error) {
	var r GroupRequest
	var groups, route, state, created, updated, resolved string
	if err := scan(&r.ID, &r.ChildID, &r.ParentID, &groups, &r.Reason, &route, &state, &r.Revision, &r.Resolver, &r.Outcome, &created, &updated, &resolved); err != nil {
		return GroupRequest{}, err
	}
	if err := json.Unmarshal([]byte(groups), &r.Groups); err != nil {
		return GroupRequest{}, err
	}
	r.Route, r.State = GroupRequestRoute(route), GroupRequestState(state)
	var err error
	if r.CreatedAt, err = parseStoreTime(created); err != nil {
		return GroupRequest{}, err
	}
	if r.UpdatedAt, err = parseStoreTime(updated); err != nil {
		return GroupRequest{}, err
	}
	if resolved != "" {
		if r.ResolvedAt, err = parseStoreTime(resolved); err != nil {
			return GroupRequest{}, err
		}
	}
	return r, nil
}

func (st *SQLStore) InsertGroupRequest(ctx context.Context, r GroupRequest) error {
	groups, err := json.Marshal(r.Groups)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx, `INSERT INTO agent_group_requests(`+groupRequestColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ChildID, r.ParentID, string(groups), r.Reason, string(r.Route), string(r.State), r.Revision, r.Resolver, r.Outcome,
		formatStoreTime(r.CreatedAt), formatStoreTime(r.UpdatedAt), formatResolved(r.ResolvedAt))
	return err
}

func formatResolved(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatStoreTime(t)
}

func (st *SQLStore) GetGroupRequest(ctx context.Context, id string) (GroupRequest, bool, error) {
	r, err := scanGroupRequest(st.db.QueryRowContext(ctx, `SELECT `+groupRequestColumns+` FROM agent_group_requests WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return GroupRequest{}, false, nil
	}
	return r, err == nil, err
}

func (st *SQLStore) queryGroupRequests(ctx context.Context, where string, args ...any) ([]GroupRequest, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+groupRequestColumns+` FROM agent_group_requests WHERE `+where+` ORDER BY created_at DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupRequest
	for rows.Next() {
		r, err := scanGroupRequest(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const openStates = `state IN ('pending_parent','pending_user')`

func (st *SQLStore) ListGroupRequestsOfChild(ctx context.Context, childID string, openOnly bool) ([]GroupRequest, error) {
	if openOnly {
		return st.queryGroupRequests(ctx, `child_id=? AND `+openStates, childID)
	}
	return st.queryGroupRequests(ctx, `child_id=?`, childID)
}

func (st *SQLStore) ListOpenGroupRequestsOfParent(ctx context.Context, parentID string) ([]GroupRequest, error) {
	return st.queryGroupRequests(ctx, `parent_id=? AND `+openStates, parentID)
}

func (st *SQLStore) ListOpenUserGroupRequests(ctx context.Context) ([]GroupRequest, error) {
	return st.queryGroupRequests(ctx, `state='pending_user'`)
}

func (st *SQLStore) UpdateGroupRequest(ctx context.Context, r GroupRequest, expect int64) (bool, error) {
	res, err := st.db.ExecContext(ctx, `UPDATE agent_group_requests SET route=?,state=?,revision=?,resolver=?,outcome=?,updated_at=?,resolved_at=? WHERE id=? AND revision=?`,
		string(r.Route), string(r.State), r.Revision, r.Resolver, r.Outcome, formatStoreTime(r.UpdatedAt), formatResolved(r.ResolvedAt), r.ID, expect)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (st *SQLStore) InvalidateGroupRequests(ctx context.Context, childID, parentID, outcome string, at time.Time) (int, error) {
	where, args := openStates, []any{}
	switch {
	case childID != "":
		where, args = where+` AND child_id=?`, append(args, childID)
	case parentID != "":
		where, args = where+` AND parent_id=? AND state='pending_parent'`, append(args, parentID)
	}
	ts := formatStoreTime(at)
	res, err := st.db.ExecContext(ctx, `UPDATE agent_group_requests SET state='invalidated',revision=revision+1,resolver='system',outcome=?,updated_at=?,resolved_at=? WHERE `+where,
		append([]any{outcome, ts, ts}, args...)...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (st *SQLStore) GroupLocks(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT group_name FROM agent_session_group_locks WHERE session_id=? ORDER BY group_name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (st *SQLStore) SetGroupLock(ctx context.Context, sessionID, group string, locked bool, at time.Time) error {
	if !locked {
		_, err := st.db.ExecContext(ctx, `DELETE FROM agent_session_group_locks WHERE session_id=? AND group_name=?`, sessionID, group)
		return err
	}
	_, err := st.db.ExecContext(ctx, `INSERT INTO agent_session_group_locks(session_id,group_name,created_at) VALUES(?,?,?) ON CONFLICT(session_id,group_name) DO NOTHING`,
		sessionID, group, formatStoreTime(at))
	return err
}
