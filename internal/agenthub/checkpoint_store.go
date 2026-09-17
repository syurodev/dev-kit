// internal/agenthub/checkpoint_store.go
package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// What a checkpoint is, once it has been written down.
//
// The engine in internal/checkpoint knows about trees and paths; this is the
// part that knows which session and which turn a tree belongs to. The two are
// deliberately separate — the engine is a leaf that runs git against a
// directory, and everything about sessions lives here.

// CheckpointKind says why a checkpoint exists.
type CheckpointKind string

const (
	// CheckpointTurn is the pair of trees around one prompt.
	CheckpointTurn CheckpointKind = "turn"
	// CheckpointRestoreSafety is the state of the project immediately before a
	// restore, so the restore itself can be undone.
	CheckpointRestoreSafety CheckpointKind = "restore_safety"
	// CheckpointRewindSafety is the same for a rewind, and doubles as its
	// recovery log: a row still pending after a crash says the rewind was
	// interrupted part-way.
	CheckpointRewindSafety CheckpointKind = "rewind_safety"
)

// CheckpointStatus is where a checkpoint got to.
type CheckpointStatus string

const (
	CheckpointPending CheckpointStatus = "pending"
	CheckpointDone    CheckpointStatus = "done"
	CheckpointFailed  CheckpointStatus = "failed"
)

// Checkpoint outcomes. A checkpoint that is "done" can still be incomplete, and
// a restore has to know which half it is missing before it offers anything.
const (
	// CheckpointOutcomeOK: both trees were taken.
	CheckpointOutcomeOK = "ok"
	// CheckpointOutcomeBeforeMissing: the opening snapshot did not finish in
	// time. The turn ran anyway — a checkpoint must never delay a prompt — but
	// there is nothing to restore to.
	CheckpointOutcomeBeforeMissing = "before_missing"
	// CheckpointOutcomeAfterMissing: the closing snapshot failed, so what the
	// turn changed is unknown.
	CheckpointOutcomeAfterMissing = "after_missing"
	// CheckpointOutcomeUnavailable: the project could not be snapshotted at all
	// (too large, no git, an unreadable directory). Detail carries the reason.
	CheckpointOutcomeUnavailable = "unavailable"
)

// ChangedSource is how much DevKit could work out about who wrote a file.
//
// Honest rather than confident: the jail lets several sessions share a project
// and a Bash script writes files no tool call names, so "the agent did it" is a
// claim that can only be made where there is evidence for it.
type ChangedSource string

const (
	// SourceAgent: the vendor's own report named the file, or a tool call in
	// this turn did.
	SourceAgent ChangedSource = "agent"
	// SourceChild: the file falls inside a sub-session's own checkpoint, taken
	// while it ran nested in this turn.
	SourceChild ChangedSource = "child"
	// SourceUnattributed: something changed it during the turn and nothing says
	// what. A Bash script, an MCP server, an async task — or another session,
	// which is what Overlap is for.
	SourceUnattributed ChangedSource = "unattributed"
)

// ChangedFile is one path a checkpoint's two trees disagree about.
type ChangedFile struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary"`
	// Source is the attribution above.
	Source ChangedSource `json:"source"`
	// SourceSession is the sub-session's id when Source is SourceChild.
	SourceSession string `json:"sourceSession,omitempty"`
}

// CheckpointRecord is one stored checkpoint.
type CheckpointRecord struct {
	ID              string           `json:"id"`
	SessionID       string           `json:"sessionId"`
	ProjectKey      string           `json:"projectKey"`
	PromptMessageID string           `json:"promptMessageId"`
	Kind            CheckpointKind   `json:"kind"`
	BeforeTree      string           `json:"beforeTree"`
	AfterTree       string           `json:"afterTree"`
	Status          CheckpointStatus `json:"status"`
	Outcome         string           `json:"outcome"`
	// Changed is the classified diff between the two trees.
	Changed []ChangedFile `json:"changed,omitempty"`
	// Skipped is what neither tree holds, so a restore knows never to touch it.
	Skipped json.RawMessage `json:"skipped,omitempty"`
	// AgentReport is the vendor's own list of files it changed, as it sent it.
	AgentReport json.RawMessage `json:"agentReport,omitempty"`
	// Overlap names the other sessions that were running in this project at the
	// same time — the honest half of an unattributed file.
	Overlap json.RawMessage `json:"overlap,omitempty"`
	// Detail is per-kind extra: a reason for an unavailable checkpoint, the
	// fork's state for a rewind.
	Detail     json.RawMessage `json:"detail,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

// CheckpointStore is the checkpoint half of Store, split out the way
// TranscriptStore is: a fake that only cares about checkpoints can implement
// this one interface.
type CheckpointStore interface {
	InsertCheckpoint(ctx context.Context, c CheckpointRecord) error
	// UpdateCheckpoint writes every field but id, session and created_at.
	UpdateCheckpoint(ctx context.Context, c CheckpointRecord) error
	GetCheckpoint(ctx context.Context, id string) (CheckpointRecord, error)
	// ListCheckpoints returns one session's checkpoints, oldest first.
	ListCheckpoints(ctx context.Context, sessionID string) ([]CheckpointRecord, error)
	// ListAllCheckpoints is what maintenance prunes against.
	ListAllCheckpoints(ctx context.Context) ([]CheckpointRecord, error)
	DeleteCheckpoints(ctx context.Context, ids []string) error
}

const checkpointColumns = `id, session_id, project_key, prompt_message_id, kind, before_tree, after_tree,
	status, outcome, changed, skipped, agent_report, overlap, detail, created_at, finished_at`

func (st *SQLStore) InsertCheckpoint(ctx context.Context, c CheckpointRecord) error {
	changed, err := marshalChanged(c.Changed)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx, `
		INSERT INTO `+tableAgentCheckpoints+` (`+checkpointColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.SessionID, c.ProjectKey, c.PromptMessageID, string(c.Kind), c.BeforeTree, c.AfterTree,
		string(c.Status), c.Outcome, changed, nullRawJSON(c.Skipped), nullRawJSON(c.AgentReport),
		nullRawJSON(c.Overlap), nullRawJSON(c.Detail),
		formatStoreTime(c.CreatedAt), nullTime(c.FinishedAt),
	)
	return err
}

// UpdateCheckpoint settles a row that was inserted pending. Everything a turn
// learns — the closing tree, the diff, the vendor's report, who else was
// running — arrives after the row exists, because the row has to exist before
// the turn starts for its id to be the report's requestId.
func (st *SQLStore) UpdateCheckpoint(ctx context.Context, c CheckpointRecord) error {
	changed, err := marshalChanged(c.Changed)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx, `
		UPDATE `+tableAgentCheckpoints+`
		SET project_key = ?, prompt_message_id = ?, kind = ?, before_tree = ?, after_tree = ?,
		    status = ?, outcome = ?, changed = ?, skipped = ?, agent_report = ?, overlap = ?,
		    detail = ?, finished_at = ?
		WHERE id = ?`,
		c.ProjectKey, c.PromptMessageID, string(c.Kind), c.BeforeTree, c.AfterTree,
		string(c.Status), c.Outcome, changed, nullRawJSON(c.Skipped), nullRawJSON(c.AgentReport),
		nullRawJSON(c.Overlap), nullRawJSON(c.Detail), nullTime(c.FinishedAt), c.ID,
	)
	return err
}

func (st *SQLStore) GetCheckpoint(ctx context.Context, id string) (CheckpointRecord, error) {
	row := st.db.QueryRowContext(ctx, `
		SELECT `+checkpointColumns+`
		FROM `+tableAgentCheckpoints+`
		WHERE id = ?`, id)
	c, err := scanCheckpoint(row)
	if err == sql.ErrNoRows {
		return CheckpointRecord{}, ErrCheckpointNotFound
	}
	return c, err
}

func (st *SQLStore) ListCheckpoints(ctx context.Context, sessionID string) ([]CheckpointRecord, error) {
	return st.queryCheckpoints(ctx, `
		SELECT `+checkpointColumns+`
		FROM `+tableAgentCheckpoints+`
		WHERE session_id = ?
		ORDER BY created_at ASC, id ASC`, sessionID)
}

func (st *SQLStore) ListAllCheckpoints(ctx context.Context) ([]CheckpointRecord, error) {
	return st.queryCheckpoints(ctx, `
		SELECT `+checkpointColumns+`
		FROM `+tableAgentCheckpoints+`
		ORDER BY created_at ASC, id ASC`)
}

func (st *SQLStore) queryCheckpoints(ctx context.Context, query string, args ...any) ([]CheckpointRecord, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CheckpointRecord, 0)
	for rows.Next() {
		c, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (st *SQLStore) DeleteCheckpoints(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// One statement per id, matching DeleteSessions: SQLite is opened with a
	// single connection, and a hand-built IN list is a place for an injection
	// bug to hide for the sake of a round trip that costs nothing here.
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableAgentCheckpoints+` WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// scanner is what QueryRow and Rows have in common.
type scanner interface{ Scan(dest ...any) error }

func scanCheckpoint(row scanner) (CheckpointRecord, error) {
	var (
		c                                         CheckpointRecord
		kind, status                              string
		changed, skipped, report, overlap, detail sql.NullString
		createdAt                                 string
		finishedAt                                sql.NullString
	)
	if err := row.Scan(&c.ID, &c.SessionID, &c.ProjectKey, &c.PromptMessageID, &kind,
		&c.BeforeTree, &c.AfterTree, &status, &c.Outcome, &changed, &skipped, &report,
		&overlap, &detail, &createdAt, &finishedAt); err != nil {
		return CheckpointRecord{}, err
	}
	c.Kind, c.Status = CheckpointKind(kind), CheckpointStatus(status)
	if changed.Valid && changed.String != "" {
		if err := json.Unmarshal([]byte(changed.String), &c.Changed); err != nil {
			return CheckpointRecord{}, err
		}
	}
	c.Skipped = rawJSON(skipped)
	c.AgentReport = rawJSON(report)
	c.Overlap = rawJSON(overlap)
	c.Detail = rawJSON(detail)
	parsed, err := parseStoreTime(createdAt)
	if err != nil {
		return CheckpointRecord{}, err
	}
	c.CreatedAt = parsed
	if finishedAt.Valid && finishedAt.String != "" {
		at, err := parseStoreTime(finishedAt.String)
		if err != nil {
			return CheckpointRecord{}, err
		}
		c.FinishedAt = &at
	}
	return c, nil
}

func marshalChanged(changed []ChangedFile) (sql.NullString, error) {
	if len(changed) == 0 {
		return sql.NullString{}, nil
	}
	raw, err := json.Marshal(changed)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(raw), Valid: true}, nil
}

func rawJSON(ns sql.NullString) json.RawMessage {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return json.RawMessage(ns.String)
}

func nullTime(at *time.Time) sql.NullString {
	if at == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatStoreTime(*at), Valid: true}
}
