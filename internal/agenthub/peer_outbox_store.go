package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const (
	peerOutboxTable = "agent_peer_outbox"
	peerGuardTable  = "agent_peer_guard_state"
)

// PeerOutboxRecord is a durable peer-delivery item. Its body has the same
// local plaintext privacy as the corresponding peer transcript entry.
type PeerOutboxRecord struct {
	ID                     string        `json:"id"`
	From                   MessageSource `json:"from"`
	FromSession            string        `json:"fromSession"`
	To                     string        `json:"to"`
	FromWorkspace          string        `json:"fromWorkspace"`
	ToWorkspace            string        `json:"toWorkspace"`
	Relation               PeerRelation  `json:"relation"`
	Text                   string        `json:"text"`
	At                     time.Time     `json:"at"`
	UpdatedAt              time.Time     `json:"updatedAt"`
	Length                 int           `json:"length"`
	Limit                  int           `json:"limit"`
	Sibling                bool          `json:"sibling"`
	CrossWorkspaceApproved bool          `json:"crossWorkspaceApproved"`
	State                  string        `json:"state"`
	Code                   string        `json:"code,omitempty"`
}

// PeerGuardState stores the rate windows, exchange chains and reply lookup
// needed to keep peer protections in force after a restart.
type PeerGuardState struct {
	Chains  map[string]PeerChainState `json:"chains"`
	Windows map[string][]time.Time    `json:"windows"`
}

type PeerOutboxReference struct {
	ID          string
	FromSession string
	To          string
	From        MessageSource
}

type PeerOutboxStatus struct {
	ID    string
	State string
	Code  string
}

type PeerChainState struct {
	Hop  int       `json:"hop"`
	From string    `json:"from"`
	At   time.Time `json:"at"`
}

// PeerOutboxStore is optional to avoid expanding the session Store contract
// used by in-memory and test implementations.
type PeerOutboxStore interface {
	SavePeerOutbox(context.Context, PeerOutboxRecord) error
	TransitionPeerOutbox(context.Context, string, string, string, string, time.Time) (bool, error)
	ListPeerOutbox(context.Context) ([]PeerOutboxRecord, error)
	ListPeerOutboxStatuses(context.Context) ([]PeerOutboxStatus, error)
	ListRecentPeerReferences(context.Context) ([]PeerOutboxReference, error)
	SavePeerGuardState(context.Context, PeerGuardState, time.Time) error
	LoadPeerGuardState(context.Context) (PeerGuardState, error)
}

func (st *SQLStore) SavePeerOutbox(ctx context.Context, r PeerOutboxRecord) error {
	source, err := json.Marshal(r.From)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx, `INSERT INTO `+peerOutboxTable+`
		(message_id, from_session, to_session, from_workspace, to_workspace, relation, source_json, body, created_at, updated_at, state, code, hop, exchange_limit, sibling, cross_workspace_approved)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO NOTHING`, r.ID, r.FromSession, r.To, r.FromWorkspace, r.ToWorkspace, string(r.Relation), string(source), r.Text,
		formatStoreTime(r.At), formatStoreTime(r.UpdatedAt), r.State, r.Code, r.From.Hop, r.Limit, r.Sibling, r.CrossWorkspaceApproved)
	return err
}

func (st *SQLStore) TransitionPeerOutbox(ctx context.Context, id, from, to, code string, at time.Time) (bool, error) {
	result, err := st.db.ExecContext(ctx, `UPDATE `+peerOutboxTable+` SET state = ?, code = ?, updated_at = ? WHERE message_id = ? AND state = ?`, to, code, formatStoreTime(at), id, from)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if to == "started" || to == "steered" || to == "expired" || to == "refused" || to == "ambiguous" {
		_, pruneErr := st.db.ExecContext(ctx, `DELETE FROM `+peerOutboxTable+` WHERE message_id IN (
			SELECT message_id FROM `+peerOutboxTable+` WHERE state IN ('started','steered','expired','refused','ambiguous') ORDER BY updated_at DESC, message_id DESC LIMIT -1 OFFSET 2000
		)`)
		if pruneErr != nil {
			return n > 0, pruneErr
		}
	}
	return n > 0, nil
}

func (st *SQLStore) ListPeerOutbox(ctx context.Context) ([]PeerOutboxRecord, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT message_id, from_session, to_session, from_workspace, to_workspace, relation, source_json, body, created_at, updated_at, state, code, hop, exchange_limit, sibling, cross_workspace_approved FROM `+peerOutboxTable+` WHERE state IN ('queued','dispatching') ORDER BY created_at, message_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeerOutboxRecord
	for rows.Next() {
		var r PeerOutboxRecord
		var source, createdAt, updatedAt string
		var sibling, approved int
		var relation string
		if err := rows.Scan(&r.ID, &r.FromSession, &r.To, &r.FromWorkspace, &r.ToWorkspace, &relation, &source, &r.Text, &createdAt, &updatedAt, &r.State, &r.Code, &r.From.Hop, &r.Limit, &sibling, &approved); err != nil {
			return nil, err
		}
		r.Relation = PeerRelation(relation)
		if err := json.Unmarshal([]byte(source), &r.From); err != nil {
			return nil, err
		}
		if r.At, err = parseStoreTime(createdAt); err != nil {
			return nil, err
		}
		if r.UpdatedAt, err = parseStoreTime(updatedAt); err != nil {
			return nil, err
		}
		r.Length = len([]rune(r.Text))
		r.Sibling, r.CrossWorkspaceApproved = sibling != 0, approved != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (st *SQLStore) ListRecentPeerReferences(ctx context.Context) ([]PeerOutboxReference, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT message_id, from_session, to_session, source_json FROM `+peerOutboxTable+` WHERE from_session <> '' AND source_json LIKE '%"kind":"session"%' AND message_id IN (
		SELECT message_id FROM `+peerOutboxTable+` WHERE from_session <> '' AND source_json LIKE '%"kind":"session"%' ORDER BY updated_at DESC, message_id DESC LIMIT ?
	) ORDER BY updated_at ASC, message_id ASC`, peerRecordLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []PeerOutboxReference
	for rows.Next() {
		var ref PeerOutboxReference
		var raw string
		if err := rows.Scan(&ref.ID, &ref.FromSession, &ref.To, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &ref.From); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (st *SQLStore) ListPeerOutboxStatuses(ctx context.Context) ([]PeerOutboxStatus, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT message_id, state, code FROM `+peerOutboxTable+` WHERE state IN ('queued','dispatching') OR message_id IN (
		SELECT message_id FROM `+peerOutboxTable+` WHERE state IN ('started','steered','expired','refused','ambiguous') ORDER BY updated_at DESC, message_id DESC LIMIT 2000
	) ORDER BY updated_at DESC, message_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeerOutboxStatus
	for rows.Next() {
		var row PeerOutboxStatus
		if err := rows.Scan(&row.ID, &row.State, &row.Code); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (st *SQLStore) SavePeerGuardState(ctx context.Context, state PeerGuardState, at time.Time) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx, `INSERT INTO `+peerGuardTable+` (state_key, state_json, updated_at) VALUES ('peer', ?, ?) ON CONFLICT(state_key) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`, string(b), formatStoreTime(at))
	return err
}

func (st *SQLStore) LoadPeerGuardState(ctx context.Context) (PeerGuardState, error) {
	var raw string
	err := st.db.QueryRowContext(ctx, `SELECT state_json FROM `+peerGuardTable+` WHERE state_key = 'peer'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return PeerGuardState{Chains: map[string]PeerChainState{}, Windows: map[string][]time.Time{}}, nil
	}
	if err != nil {
		return PeerGuardState{}, err
	}
	var state PeerGuardState
	err = json.Unmarshal([]byte(raw), &state)
	if state.Chains == nil {
		state.Chains = map[string]PeerChainState{}
	}
	if state.Windows == nil {
		state.Windows = map[string][]time.Time{}
	}
	return state, err
}
