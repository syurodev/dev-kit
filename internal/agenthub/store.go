package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	tableAgentSessions    = "agent_sessions"
	tableAgentTurns       = "agent_turns"
	tableAgentTranscript  = "agent_transcript"
	tableAgentCheckpoints = "agent_checkpoints"
	storeTimeLayout       = time.RFC3339Nano
)

// Store persists hub-owned agent sessions and turn records.
type Store interface {
	InsertSession(ctx context.Context, s AgentSession) error
	UpdateSession(ctx context.Context, s AgentSession) error
	UpdateSessionStatus(ctx context.Context, id string, status SessionStatus, at time.Time) error
	UpdateSessionTitle(ctx context.Context, id, title string, at time.Time) error
	// UpdateSessionACPID points a session at another vendor session — the
	// fork a rewind made. Its own writer, like status and title.
	UpdateSessionACPID(ctx context.Context, id, acpSessionID string, at time.Time) error
	AdoptSessionTitle(ctx context.Context, id, title string, at time.Time) error
	GetSession(ctx context.Context, id string) (AgentSession, error)
	ListSessions(ctx context.Context) ([]AgentSession, error)
	InsertTurn(ctx context.Context, t TurnRecord) error
	ListTurns(ctx context.Context, sessionID string) ([]TurnRecord, error)
	DeleteSessions(ctx context.Context, ids []string) error
	TranscriptStore
	CheckpointStore
	RewindStore
	// ListInterruptedRewinds finds the safety rows of rewinds that never
	// finished, so a restart can offer to put the files back.
	ListInterruptedRewinds(ctx context.Context) ([]CheckpointRecord, error)
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

// UpdateSessionStatus writes the status alone. Status and title have separate
// writers — a turn ending, the user renaming, the agent naming its session —
// and a whole-row write from any of them put back whatever the others had
// changed since it read the row.
func (st *SQLStore) UpdateSessionStatus(ctx context.Context, id string, status SessionStatus, at time.Time) error {
	return st.updateSessionColumn(ctx, `status = ?`, id, string(status), at)
}

// UpdateSessionTitle writes the title alone, for the same reason.
func (st *SQLStore) UpdateSessionTitle(ctx context.Context, id, title string, at time.Time) error {
	return st.updateSessionColumn(ctx, `title = ?`, id, title, at)
}

// UpdateSessionACPID writes the vendor session id alone, for the same reason.
func (st *SQLStore) UpdateSessionACPID(ctx context.Context, id, acpSessionID string, at time.Time) error {
	return st.updateSessionColumn(ctx, `acp_session_id = ?`, id, acpSessionID, at)
}

// AdoptSessionTitle sets the title only while the session has none, in the same
// statement that checks, so an agent-announced title cannot overwrite a name
// the user gave between the check and the write. A session that already has a
// title is not an error.
func (st *SQLStore) AdoptSessionTitle(ctx context.Context, id, title string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `
		UPDATE `+tableAgentSessions+` SET title = ?, updated_at = ?
		WHERE id = ? AND (title IS NULL OR TRIM(title) = '')`,
		title, formatStoreTime(at), id,
	)
	return err
}

func (st *SQLStore) updateSessionColumn(ctx context.Context, set, id string, value any, at time.Time) error {
	result, err := st.db.ExecContext(ctx,
		`UPDATE `+tableAgentSessions+` SET `+set+`, updated_at = ? WHERE id = ?`,
		value, formatStoreTime(at), id,
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
		INSERT INTO `+tableAgentTurns+` (id, session_id, prompt_message_id, stop_reason, usage_json, raw_meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.SessionID, t.PromptMessageID, nullEmptyString(t.StopReason), nullRawJSON(t.UsageJSON), nullRawJSON(t.RawMeta),
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
		SELECT id, session_id, prompt_message_id, stop_reason, usage_json, raw_meta, created_at
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
		if err := rows.Scan(&t.ID, &t.SessionID, &t.PromptMessageID, &stopReason, &usageJSON, &rawMeta, &createdAt); err != nil {
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

// FindPromptEntry returns the transcript row of one prompt, by the message id
// DevKit gave its echo. Rewind and restore name a turn by that id: it is the
// one key the live stream, a reloaded page, the turn row and the checkpoint
// all share.
func (st *SQLStore) FindPromptEntry(ctx context.Context, sessionID, messageID string) (TranscriptEntry, bool, error) {
	if messageID == "" {
		return TranscriptEntry{}, false, nil
	}
	row := st.db.QueryRowContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND kind = ? AND json_extract(payload, '$.messageId') = ?
		ORDER BY seq ASC
		LIMIT 1`, sessionID, string(UpdateUserMessageChunk), messageID)
	return scanOptionalTranscript(row)
}

// scanTranscriptRows drains a transcript query into entries.
func scanTranscriptRows(rows *sql.Rows) ([]TranscriptEntry, error) {
	out := []TranscriptEntry{}
	for rows.Next() {
		entry, err := scanTranscript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// ForkCandidates returns the branch points a rewind may use, newest first.
//
// The filter is in SQL rather than in Go so the LIMIT counts rows that qualify:
// a session whose last hundred assistant messages all came from subagents would
// otherwise return a hundred rows for the caller to throw away. The message id
// and the origin live in the payload because they are the vendor's, not
// columns of ours.
func (st *SQLStore) ForkCandidates(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]TranscriptEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND seq < ? AND kind = ?
		  AND COALESCE(json_extract(payload, '$.messageId'), '') != ''
		  AND COALESCE(json_extract(payload, '$.origin'), '') = ''
		ORDER BY seq DESC
		LIMIT ?`, sessionID, beforeSeq, string(UpdateAgentMessageChunk), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTranscriptRows(rows)
}

// PromptsFrom returns the user messages at or after fromSeq, oldest first —
// the prompts a cut at fromSeq would remove.
func (st *SQLStore) PromptsFrom(ctx context.Context, sessionID string, fromSeq int64) ([]TranscriptEntry, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND seq >= ? AND kind = ?
		ORDER BY seq ASC`, sessionID, fromSeq, string(UpdateUserMessageChunk))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTranscriptRows(rows)
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

// PruneClosedSessions permanently deletes sessions closed before closedBefore
// — row, turns and transcript — and reports how many went.
//
// Nothing else ever deletes from agent_transcript unasked: it is
// append-plus-upsert, and a long agent session writes a row per chunk.
//
// Whole sessions, never rows: the first cut was row-wise on created_at and
// sawed the head off any conversation old enough to span the cutoff. And only
// closed ones: the second rule keyed on updated_at, which opening or reading a
// session never touches, so a session left idle for the window lost its whole
// transcript while its row stayed resumable — reopened, it looked empty and
// still answered. A closed session is one the user ended, and it goes with
// its row, so nothing is left looking alive and empty.
//
// A closed session with a descendant that is kept stays too: children point
// at their parent by id, and a parent pruned from under a live child would
// reshape the tree in the sidebar.
func (st *SQLStore) PruneClosedSessions(ctx context.Context, closedBefore time.Time) (int, error) {
	sessions, err := st.ListSessions(ctx)
	if err != nil {
		return 0, err
	}
	children := map[string][]AgentSession{}
	for _, s := range sessions {
		if s.ParentSessionID != nil {
			children[*s.ParentSessionID] = append(children[*s.ParentSessionID], s)
		}
	}
	var prunable func(AgentSession) bool
	prunable = func(s AgentSession) bool {
		if s.Status != SessionStatusClosed || !s.UpdatedAt.Before(closedBefore) {
			return false
		}
		for _, child := range children[s.ID] {
			if !prunable(child) {
				return false
			}
		}
		return true
	}
	var ids []string
	for _, s := range sessions {
		if prunable(s) {
			ids = append(ids, s.ID)
		}
	}
	if err := st.DeleteSessions(ctx, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// DeleteSessions removes the given sessions along with their turns,
// transcripts and checkpoint rows, permanently. (A checkpoint's snapshot is in
// the shadow repository; without its row, maintenance collects it.)
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

	for _, table := range []string{tableAgentTranscript, tableAgentTurns, tableAgentCheckpoints} {
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
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+transcriptColumns+`
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

// Page size bounds for transcript reads. The default is what a session open
// asks for; the ceiling is what any single call may return regardless of what
// the caller asked.
const (
	DefaultTranscriptPageSize = 200
	MaxTranscriptPageSize     = 500
)

// ListTranscriptPage returns the newest `limit` entries older than beforeSeq
// (0 = no bound), oldest-first, plus whether older rows exist above the page.
//
// A page that starts mid-turn is snapped back to the turn's user_message_chunk
// row when one exists above it, so a prepend never lands between a prompt and
// its reply — the loader coalesces consecutive chunk rows, and a cut inside a
// run would render one bubble as two. The snap is bounded to one extra
// page-worth: a turn longer than that keeps the page's original mid-turn cut,
// visibly, because cutting again would leave the skipped rows unreachable.
func (st *SQLStore) ListTranscriptPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) (TranscriptPage, error) {
	if limit <= 0 {
		limit = DefaultTranscriptPageSize
	}

	newestFirst, hasMore, err := st.transcriptWindow(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		return TranscriptPage{}, err
	}
	entries := reverseTranscript(newestFirst)

	// Snap to the turn start: only worth it when the page is full (a partial
	// page already reaches the top of the window) and older rows exist.
	if hasMore && len(entries) == limit && entries[0].Kind != UpdateUserMessageChunk {
		older, snapped, err := st.prependPartialTurn(ctx, sessionID, entries[0].Seq, limit)
		if err != nil {
			return TranscriptPage{}, err
		}
		if snapped {
			entries = append(older, entries...)
			// The boundary row is part of the turn; re-check what is above it.
			hasMore, err = st.hasOlderThan(ctx, sessionID, entries[0].Seq)
			if err != nil {
				return TranscriptPage{}, err
			}
		}
	}

	return TranscriptPage{Entries: entries, HasMore: hasMore}, nil
}

// transcriptWindow reads the newest `limit` rows strictly below beforeSeq,
// newest-first. Its own function on purpose: the pool holds one connection,
// so the result set must be closed (by returning from here) before the snap
// queries below it can run — querying with it still open waits forever.
func (st *SQLStore) transcriptWindow(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]TranscriptEntry, bool, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND (? <= 0 OR seq < ?)
		ORDER BY seq DESC
		LIMIT ?`, sessionID, beforeSeq, beforeSeq, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	newestFirst := make([]TranscriptEntry, 0, limit)
	hasMore := false
	for rows.Next() {
		e, err := scanTranscript(rows)
		if err != nil {
			return nil, false, err
		}
		if len(newestFirst) == limit {
			hasMore = true
			break
		}
		newestFirst = append(newestFirst, e)
	}
	return newestFirst, hasMore, rows.Err()
}

// prependPartialTurn returns the rows from the previous user_message_chunk
// (inclusive) up to oldestSeq (exclusive) — the head of the turn a page cut
// into. Bounded to `limit` rows; returns snapped=false when no turn start
// exists above, or when the bound would cut the turn again — a second cut
// would leave the skipped rows unreachable (every later walk keys off the
// page's new oldest seq), so the page keeps its original mid-turn cut instead:
// visible, but nothing is silently unreachable.
func (st *SQLStore) prependPartialTurn(ctx context.Context, sessionID string, oldestSeq int64, limit int) ([]TranscriptEntry, bool, error) {
	var boundary sql.NullInt64
	if err := st.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND kind = ? AND seq < ?`,
		sessionID, string(UpdateUserMessageChunk), oldestSeq,
	).Scan(&boundary); err != nil {
		return nil, false, err
	}
	if !boundary.Valid {
		return nil, false, nil
	}

	rows, err := st.db.QueryContext(ctx,
		`SELECT `+transcriptColumns+`
		FROM `+tableAgentTranscript+`
		WHERE session_id = ? AND seq >= ? AND seq < ?
		ORDER BY seq ASC
		LIMIT ?`, sessionID, boundary.Int64, oldestSeq, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := []TranscriptEntry{}
	for rows.Next() {
		e, err := scanTranscript(rows)
		if err != nil {
			return nil, false, err
		}
		if len(out) == limit {
			// The bound cut the turn: the rows near oldestSeq would be
			// skipped by every later walk. Keep the original cut instead.
			return nil, false, nil
		}
		out = append(out, e)
	}
	return out, true, rows.Err()
}

func (st *SQLStore) hasOlderThan(ctx context.Context, sessionID string, seq int64) (bool, error) {
	var exists bool
	err := st.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM `+tableAgentTranscript+` WHERE session_id = ? AND seq < ?)`,
		sessionID, seq).Scan(&exists)
	return exists, err
}

func reverseTranscript(in []TranscriptEntry) []TranscriptEntry {
	out := make([]TranscriptEntry, len(in))
	for i, e := range in {
		out[len(in)-1-i] = e
	}
	return out
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
