package agenthub

import (
	"context"
	"database/sql"
	"time"
)

const (
	tableAgentSessions = "agent_sessions"
	tableAgentTurns    = "agent_turns"
	storeTimeLayout    = time.RFC3339Nano
)

// Store persists hub-owned agent sessions and turn records.
type Store interface {
	InsertSession(ctx context.Context, s AgentSession) error
	UpdateSession(ctx context.Context, s AgentSession) error
	GetSession(ctx context.Context, id string) (AgentSession, error)
	ListSessions(ctx context.Context) ([]AgentSession, error)
	InsertTurn(ctx context.Context, t TurnRecord) error
	ListTurns(ctx context.Context, sessionID string) ([]TurnRecord, error)
}

// SQLStore implements Store against SQLite tables created by storage migration 13.
type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

func formatStoreTime(t time.Time) string {
	return t.UTC().Format(storeTimeLayout)
}

func parseStoreTime(value string) (time.Time, error) {
	return time.Parse(storeTimeLayout, value)
}

func nullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func stringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

const sessionColumns = `id, agent_type, project_path, status, parent_session_id, depth, title, acp_session_id, created_at, updated_at`

func scanSession(scan func(dest ...any) error) (AgentSession, error) {
	var s AgentSession
	var parent, title sql.NullString
	var createdAt, updatedAt string
	var agentType, status string
	if err := scan(
		&s.ID, &agentType, &s.ProjectPath, &status, &parent, &s.Depth, &title, &s.ACPSessionID, &createdAt, &updatedAt,
	); err != nil {
		return AgentSession{}, err
	}
	s.AgentType = AgentType(agentType)
	s.Status = SessionStatus(status)
	s.ParentSessionID = stringPtr(parent)
	s.Title = stringPtr(title)
	var err error
	if s.CreatedAt, err = parseStoreTime(createdAt); err != nil {
		return AgentSession{}, err
	}
	if s.UpdatedAt, err = parseStoreTime(updatedAt); err != nil {
		return AgentSession{}, err
	}
	return s, nil
}

func (st *SQLStore) InsertSession(ctx context.Context, s AgentSession) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO `+tableAgentSessions+` (
			id, agent_type, project_path, status, parent_session_id, depth, title, acp_session_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, string(s.AgentType), s.ProjectPath, string(s.Status),
		nullString(s.ParentSessionID), s.Depth, nullString(s.Title), s.ACPSessionID,
		formatStoreTime(s.CreatedAt), formatStoreTime(s.UpdatedAt),
	)
	return err
}

func (st *SQLStore) UpdateSession(ctx context.Context, s AgentSession) error {
	result, err := st.db.ExecContext(ctx, `
		UPDATE `+tableAgentSessions+` SET
			agent_type = ?, project_path = ?, status = ?, parent_session_id = ?,
			depth = ?, title = ?, acp_session_id = ?, updated_at = ?
		WHERE id = ?`,
		string(s.AgentType), s.ProjectPath, string(s.Status), nullString(s.ParentSessionID),
		s.Depth, nullString(s.Title), s.ACPSessionID, formatStoreTime(s.UpdatedAt), s.ID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (st *SQLStore) GetSession(ctx context.Context, id string) (AgentSession, error) {
	row := st.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM `+tableAgentSessions+` WHERE id = ?`, id,
	)
	s, err := scanSession(row.Scan)
	if err == sql.ErrNoRows {
		return AgentSession{}, ErrSessionNotFound
	}
	return s, err
}

func (st *SQLStore) ListSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM `+tableAgentSessions+` ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AgentSession, 0)
	for rows.Next() {
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *SQLStore) InsertTurn(ctx context.Context, t TurnRecord) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO `+tableAgentTurns+` (id, session_id, stop_reason, usage_json, raw_meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.SessionID, nullEmptyString(t.StopReason), nullRawJSON(t.UsageJSON), nullRawJSON(t.RawMeta),
		formatStoreTime(t.CreatedAt),
	)
	return err
}

func nullEmptyString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullRawJSON(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

func (st *SQLStore) ListTurns(ctx context.Context, sessionID string) ([]TurnRecord, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT id, session_id, stop_reason, usage_json, raw_meta, created_at
		FROM `+tableAgentTurns+`
		WHERE session_id = ?
		ORDER BY created_at ASC, id ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TurnRecord, 0)
	for rows.Next() {
		var t TurnRecord
		var stopReason, usageJSON, rawMeta sql.NullString
		var createdAt string
		if err := rows.Scan(&t.ID, &t.SessionID, &stopReason, &usageJSON, &rawMeta, &createdAt); err != nil {
			return nil, err
		}
		if stopReason.Valid {
			t.StopReason = stopReason.String
		}
		if usageJSON.Valid {
			t.UsageJSON = []byte(usageJSON.String)
		}
		if rawMeta.Valid {
			t.RawMeta = []byte(rawMeta.String)
		}
		t.CreatedAt, err = parseStoreTime(createdAt)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
