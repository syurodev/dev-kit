package agenthub

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"devkit/internal/harness"
)

// HarnessAppliedStore keeps, per session, the reference of the harness
// configuration its runtime was last opened with. It is an optional capability
// of a Store: without it the applied record lives in memory only and a restart
// forgets it. A reference holds ids, revisions, a fingerprint and an executable
// digest; never a definition, path, command or environment value.
type HarnessAppliedStore interface {
	GetHarnessApplied(ctx context.Context, sessionID string) (ref harness.SnapshotRef, enforced, found bool, err error)
	SetHarnessApplied(ctx context.Context, sessionID string, ref harness.SnapshotRef, enforced bool, at time.Time) error
	DeleteHarnessApplied(ctx context.Context, sessionID string) error
}

var _ HarnessAppliedStore = (*SQLStore)(nil)

var errHarnessAppliedUnreadable = errors.New("stored harness reference is unreadable")

func (st *SQLStore) GetHarnessApplied(ctx context.Context, sessionID string) (harness.SnapshotRef, bool, bool, error) {
	var schema, enforced int
	var raw string
	err := st.db.QueryRowContext(ctx, `SELECT schema_version,ref,enforced FROM agent_session_harness_applied WHERE session_id=?`, sessionID).Scan(&schema, &raw, &enforced)
	if errors.Is(err, sql.ErrNoRows) {
		return harness.SnapshotRef{}, false, false, nil
	}
	if err != nil {
		return harness.SnapshotRef{}, false, false, err
	}
	var ref harness.SnapshotRef
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var extra json.RawMessage
	if schema != harness.SnapshotSchema || dec.Decode(&ref) != nil || !errors.Is(dec.Decode(&extra), io.EOF) || ref.Schema != schema || ref.SessionID != sessionID {
		return harness.SnapshotRef{}, false, false, errHarnessAppliedUnreadable
	}
	return ref, enforced == 1, true, nil
}

func (st *SQLStore) SetHarnessApplied(ctx context.Context, sessionID string, ref harness.SnapshotRef, enforced bool, at time.Time) error {
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	flag := 0
	if enforced {
		flag = 1
	}
	_, err = st.db.ExecContext(ctx, `INSERT INTO agent_session_harness_applied(session_id,schema_version,ref,enforced,applied_at) VALUES(?,?,?,?,?)
 ON CONFLICT(session_id) DO UPDATE SET schema_version=excluded.schema_version,ref=excluded.ref,enforced=excluded.enforced,applied_at=excluded.applied_at`,
		sessionID, harness.SnapshotSchema, string(raw), flag, formatStoreTime(at))
	return err
}

func (st *SQLStore) DeleteHarnessApplied(ctx context.Context, sessionID string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM agent_session_harness_applied WHERE session_id=?`, sessionID)
	return err
}
