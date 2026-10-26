// internal/agenthub/rewind_store.go
package agenthub

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"devkit/internal/harness"
)

// The one write a rewind makes.
//
// Everything a rewind changes in the database lands together or not at all. The
// pieces do not make sense apart: a session pointed at a forked vendor session
// while its transcript still holds the messages that fork does not have would
// answer the next prompt out of a conversation the user cannot see, and a
// transcript cut without the turn rows to match would leave costs and
// checkpoints attributed to messages that are gone.

// RewindSpec is the whole cut, as one statement of intent.
type RewindSpec struct {
	SessionID string
	// ACPSessionID is the forked vendor session. Empty leaves the column
	// alone, which is what "conversation untouched" means.
	ACPSessionID string
	// CutSeq is the first transcript row to drop. Rows below it stay.
	CutSeq int64
	// DroppedPromptMessageIDs are the turns being cut, whose turn rows and
	// checkpoints go with them.
	DroppedPromptMessageIDs []string
	// SafetyCheckpointID, when set, is marked done in the same transaction:
	// the row exists to say a rewind was interrupted, so it stops saying that
	// at the moment the rewind stops being interruptible.
	SafetyCheckpointID string
	At                 time.Time
	// The fork's applied reference commits with the conversation cut, never in
	// a later best-effort write. Nil removes the reference for an unconfigured
	// launch; a failed write leaves the entire old conversation/reference intact.
	HarnessRef      *harness.SnapshotRef
	HarnessEnforced bool
}

// RewindStore is the transactional half of a rewind.
type RewindStore interface {
	RewindSession(ctx context.Context, spec RewindSpec) error
}

// rewindKeptStateKinds are the session-level rows a cut keeps.
//
// The mode, the model options and the command list describe the session, not
// the conversation: they are true of the forked session too, and losing them
// would leave the picker empty until the agent happened to republish. Usage is
// not kept — it is the running cost of messages that no longer exist.
var rewindKeptStateKinds = []UpdateKind{UpdateCurrentMode, UpdateConfigOptions, UpdateAvailableCommands}

// RewindSession applies the cut in one transaction.
func (st *SQLStore) RewindSession(ctx context.Context, spec RewindSpec) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if spec.ACPSessionID != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+tableAgentSessions+` SET acp_session_id = ?, updated_at = ? WHERE id = ?`,
			spec.ACPSessionID, formatStoreTime(spec.At), spec.SessionID); err != nil {
			return err
		}
	}

	// The state rows that survive, in the order they were in. They are read
	// before the delete because the delete is what frees their sequence
	// numbers for reuse.
	kept, err := keptStateRows(ctx, tx, spec.SessionID, spec.CutSeq)
	if err != nil {
		return err
	}

	query := `DELETE FROM ` + tableAgentTranscript + ` WHERE session_id = ? AND seq >= ?`
	args := []any{spec.SessionID, spec.CutSeq}
	for _, kind := range rewindKeptStateKinds {
		query += ` AND kind != ?`
		args = append(args, string(kind))
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	// Renumber the survivors from the cut. A unique index covers
	// (session_id, seq), and a kept row sitting at seq 400 above a transcript
	// that now ends at 12 would make the next append collide with it —
	// AppendTranscript takes max(seq)+1, which would be 401.
	for i, row := range kept {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+tableAgentTranscript+` SET seq = ? WHERE id = ?`,
			spec.CutSeq+int64(i), row.id); err != nil {
			return fmt.Errorf("renumber transcript row %s: %w", row.id, err)
		}
	}

	for _, promptID := range spec.DroppedPromptMessageIDs {
		if promptID == "" {
			continue
		}
		for _, table := range []string{tableAgentTurns, tableAgentCheckpoints} {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM `+table+` WHERE session_id = ? AND prompt_message_id = ?`,
				spec.SessionID, promptID); err != nil {
				return err
			}
		}
	}

	if spec.SafetyCheckpointID != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+tableAgentCheckpoints+` SET status = ?, finished_at = ? WHERE id = ?`,
			string(CheckpointDone), formatStoreTime(spec.At), spec.SafetyCheckpointID); err != nil {
			return err
		}
	}
	if spec.HarnessRef != nil {
		if err := setHarnessApplied(ctx, tx, spec.SessionID, *spec.HarnessRef, spec.HarnessEnforced, spec.At); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `DELETE FROM agent_session_harness_applied WHERE session_id=?`, spec.SessionID); err != nil {
		return err
	}
	return tx.Commit()
}

type keptRow struct {
	id  string
	seq int64
}

func keptStateRows(ctx context.Context, tx *sql.Tx, sessionID string, cutSeq int64) ([]keptRow, error) {
	query := `SELECT id, seq FROM ` + tableAgentTranscript + ` WHERE session_id = ? AND seq >= ? AND kind IN (`
	args := []any{sessionID, cutSeq}
	for i, kind := range rewindKeptStateKinds {
		if i > 0 {
			query += `, `
		}
		query += `?`
		args = append(args, string(kind))
	}
	query += `) ORDER BY seq ASC`

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []keptRow
	for rows.Next() {
		var row keptRow
		if err := rows.Scan(&row.id, &row.seq); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListInterruptedRewinds returns the safety rows of rewinds that never
// finished. A row still pending at startup is a rewind whose process died
// between taking the snapshot and committing the cut: the files may be
// half-restored, and only the user can say whether to put them back.
func (st *SQLStore) ListInterruptedRewinds(ctx context.Context) ([]CheckpointRecord, error) {
	return st.queryCheckpoints(ctx, `
		SELECT `+checkpointColumns+`
		FROM `+tableAgentCheckpoints+`
		WHERE kind = ? AND status = ?
		ORDER BY created_at ASC`, string(CheckpointRewindSafety), string(CheckpointPending))
}
