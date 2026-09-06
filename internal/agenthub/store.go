package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	tableAgentSessions   = "agent_sessions"
	tableAgentTurns      = "agent_turns"
	tableAgentTranscript = "agent_transcript"
	storeTimeLayout      = time.RFC3339Nano
)

// Store persists hub-owned agent sessions and turn records.
type Store interface {
	InsertSession(ctx context.Context, s AgentSession) error
	UpdateSession(ctx context.Context, s AgentSession) error
	GetSession(ctx context.Context, id string) (AgentSession, error)
	ListSessions(ctx context.Context) ([]AgentSession, error)
	InsertTurn(ctx context.Context, t TurnRecord) error
	ListTurns(ctx context.Context, sessionID string) ([]TurnRecord, error)
	DeleteSessions(ctx context.Context, ids []string) error
	TranscriptStore
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

// --- transcript ---

const transcriptColumns = `id, session_id, seq, kind, tool_call_id, text, payload, created_at, updated_at`

// AppendTranscript writes a new row and assigns its sequence number in the same
// statement. Computing seq in Go would need a read-then-write, and two updates
// landing between the read and the write would collide on the unique index.
func (st *SQLStore) AppendTranscript(ctx context.Context, e TranscriptEntry) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO `+tableAgentTranscript+` (`+transcriptColumns+`)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM `+tableAgentTranscript+` WHERE session_id = ?), ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SessionID, e.SessionID, string(e.Kind), e.ToolCallID, e.Text,
		nullRawJSON(e.Payload), formatStoreTime(e.CreatedAt), formatStoreTime(e.UpdatedAt),
	)
	return err
}

// UpdateTranscript rewrites the mutable part of a row: the accumulated text of
// a chunk run, or the merged state of a tool call. Kind and seq never change.
func (st *SQLStore) UpdateTranscript(ctx context.Context, e TranscriptEntry) error {
	res, err := st.db.ExecContext(ctx, `
		UPDATE `+tableAgentTranscript+`
		SET text = ?, payload = ?, updated_at = ?
		WHERE id = ?`,
		e.Text, nullRawJSON(e.Payload), formatStoreTime(e.UpdatedAt), e.ID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTranscriptEntryNotFound
	}
	return nil
}

func (st *SQLStore) LastTranscriptEntry(ctx context.Context, sessionID string) (TranscriptEntry, bool, error) {
	row := st.db.QueryRowContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ?
		ORDER BY seq DESC
		LIMIT 1`, sessionID)
	return scanOptionalTranscript(row)
}

func (st *SQLStore) FindTranscriptToolCall(ctx context.Context, sessionID, toolCallID string) (TranscriptEntry, bool, error) {
	if toolCallID == "" {
		return TranscriptEntry{}, false, nil
	}
	row := st.db.QueryRowContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND tool_call_id = ?
		ORDER BY seq DESC
		LIMIT 1`, sessionID, toolCallID)
	return scanOptionalTranscript(row)
}

// FindTranscriptKind returns the session-level row for one kind, if any. Used
// to upsert usage / commands / mode instead of appending them.
func (st *SQLStore) FindTranscriptKind(ctx context.Context, sessionID string, kind UpdateKind) (TranscriptEntry, bool, error) {
	row := st.db.QueryRowContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND kind = ?
		ORDER BY seq ASC
		LIMIT 1`, sessionID, string(kind))
	return scanOptionalTranscript(row)
}

// PruneTranscriptsBefore deletes transcript rows created before cutoff and
// reports how many went.
//
// Nothing ever deleted from this table: it is append-plus-upsert only, no
// session delete cascades into it, and a single long-running agent session
// writes a row per chunk. The table was unbounded by construction.
//
// Deliberately time-based rather than "keep the last N rows per session":
// trimming inside a session would make a transcript silently lose its middle,
// and a reload would then show the user a different conversation than the one
// they watched arrive.
func (st *SQLStore) PruneTranscriptsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM `+tableAgentTranscript+` WHERE created_at < ?`,
		formatStoreTime(cutoff),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteSessions removes the given sessions along with their turns and
// transcripts, permanently.
//
// Children first, then the sessions themselves: agent_turns and
// agent_transcript both declare a foreign key onto agent_sessions and the
// connection runs with foreign_keys=ON, so deleting a session that still has
// either would fail rather than orphan them. There is no ON DELETE CASCADE on
// those tables — migrations 13 and 20 predate anything that deleted a session
// — so the order is the cascade.
//
// One transaction, because a half-deleted session is worse than either
// outcome: rows whose parent is gone are unreachable through every read path
// this package has, so they would simply consume space forever.
func (st *SQLStore) DeleteSessions(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{tableAgentTranscript, tableAgentTurns} {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM `+table+` WHERE session_id = ?`, id); err != nil {
				return err
			}
		}
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM `+tableAgentSessions+` WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (st *SQLStore) ListTranscript(ctx context.Context, sessionID string) ([]TranscriptEntry, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ?
		ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TranscriptEntry{}
	for rows.Next() {
		e, err := scanTranscript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type transcriptScanner interface {
	Scan(dest ...any) error
}

func scanTranscript(sc transcriptScanner) (TranscriptEntry, error) {
	var (
		e                    TranscriptEntry
		kind                 string
		payload              sql.NullString
		createdAt, updatedAt string
	)
	if err := sc.Scan(&e.ID, &e.SessionID, &e.Seq, &kind, &e.ToolCallID, &e.Text, &payload, &createdAt, &updatedAt); err != nil {
		return TranscriptEntry{}, err
	}
	e.Kind = UpdateKind(kind)
	if payload.Valid {
		e.Payload = json.RawMessage(payload.String)
	}
	var err error
	if e.CreatedAt, err = parseStoreTime(createdAt); err != nil {
		return TranscriptEntry{}, err
	}
	if e.UpdatedAt, err = parseStoreTime(updatedAt); err != nil {
		return TranscriptEntry{}, err
	}
	return e, nil
}

func scanOptionalTranscript(row *sql.Row) (TranscriptEntry, bool, error) {
	e, err := scanTranscript(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TranscriptEntry{}, false, nil
	}
	if err != nil {
		return TranscriptEntry{}, false, err
	}
	return e, true, nil
}
