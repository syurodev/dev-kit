package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"devkit/internal/harness"
)

// maxHarnessBody bounds one stored document, like AgentCustomization.
const maxHarnessBody = 64 * 1024

// HarnessStore persists the workspace harness profile (W) and the session
// override (S). It is the SQL half of the contract in harness/store.go: it
// validates shape, scope and compare-and-swap, nothing about meaning. Only the
// harness service and composition code call it; edits go through that service's
// validation, never straight to Apply.
type HarnessStore struct {
	db  *sql.DB
	now func() time.Time
	// betweenWrites runs after W and before S inside Apply's transaction. Tests
	// use it to inject a failure; production leaves it nil.
	betweenWrites func() error
}

func NewHarnessStore(db *sql.DB) *HarnessStore {
	return &HarnessStore{db: db, now: func() time.Time { return time.Now().UTC() }}
}

type harnessRow struct {
	workspaceID string
	schema      int
	revision    int64
	deleted     bool
	body        string
}

type harnessQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// GetProfile returns W, or a nil profile when there is none. The stamp keeps
// counting through deletes, so "absent" still carries a token.
func (s *HarnessStore) GetProfile(ctx context.Context, workspaceID string) (*harness.Profile, harness.Stamp, error) {
	row, found, err := readHarnessRow(ctx, s.db, true, workspaceID)
	if err != nil || !found {
		return nil, harness.Stamp{}, err
	}
	return decodeProfile(row)
}

// GetOverride returns S for a session of workspaceID. S written for another
// workspace is a scope error: it is neither shown nor transferred.
func (s *HarnessStore) GetOverride(ctx context.Context, sessionID, workspaceID string) (*harness.Override, harness.Stamp, error) {
	row, found, err := readHarnessRow(ctx, s.db, false, sessionID)
	if err != nil || !found {
		return nil, harness.Stamp{}, err
	}
	if !row.deleted && row.workspaceID != workspaceID {
		return nil, harness.Stamp{}, harness.ErrStoreScope
	}
	return decodeOverride(row)
}

func readHarnessRow(ctx context.Context, q harnessQuerier, profile bool, key string) (harnessRow, bool, error) {
	query := `SELECT '',schema_version,revision,deleted,body FROM workspace_harness_profiles WHERE workspace_id=?`
	if !profile {
		query = `SELECT workspace_id,schema_version,revision,deleted,body FROM session_harness_overrides WHERE session_id=?`
	}
	var r harnessRow
	var deleted int
	err := q.QueryRowContext(ctx, query, key).Scan(&r.workspaceID, &r.schema, &r.revision, &deleted, &r.body)
	if errors.Is(err, sql.ErrNoRows) {
		return harnessRow{}, false, nil
	}
	if err != nil {
		return harnessRow{}, false, err
	}
	r.deleted = deleted == 1
	return r, true, nil
}

func decodeStrict(body string, into any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil || dec.More() {
		return harness.ErrStoreCorrupt
	}
	return nil
}

func decodeProfile(r harnessRow) (*harness.Profile, harness.Stamp, error) {
	if r.deleted {
		return nil, harness.Stamp{Revision: r.revision}, nil
	}
	var p harness.Profile
	if r.schema != harness.SchemaVersion || decodeStrict(r.body, &p) != nil || p.Schema != r.schema {
		return nil, harness.Stamp{}, harness.ErrStoreCorrupt
	}
	p.Revision = r.revision
	return &p, harness.Stamp{Revision: r.revision, Present: true}, nil
}

func decodeOverride(r harnessRow) (*harness.Override, harness.Stamp, error) {
	if r.deleted {
		return nil, harness.Stamp{Revision: r.revision}, nil
	}
	var o harness.Override
	if r.schema != harness.SchemaVersion || decodeStrict(r.body, &o) != nil || o.Schema != r.schema {
		return nil, harness.Stamp{}, harness.ErrStoreCorrupt
	}
	o.Revision = r.revision
	return &o, harness.Stamp{Revision: r.revision, Present: true}, nil
}

// Apply commits a write to W, S or both in one transaction, or changes nothing.
// A record whose stamp differs from Expect fails with *harness.ConflictError;
// a missing or foreign workspace or session fails with harness.ErrStoreScope.
func (s *HarnessStore) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	if s == nil || s.db == nil || (w.Profile == nil && w.Override == nil) ||
		w.WorkspaceID == "" || (w.Override != nil && w.SessionID == "") {
		return harness.Applied{}, harness.ErrStoreScope
	}
	profileBody, err := encodeProfile(w.Profile)
	if err != nil {
		return harness.Applied{}, err
	}
	overrideBody, err := encodeOverride(w.Override)
	if err != nil {
		return harness.Applied{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return harness.Applied{}, err
	}
	defer tx.Rollback()

	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM workspaces WHERE id=?`, w.WorkspaceID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return harness.Applied{}, harness.ErrStoreScope
		}
		return harness.Applied{}, err
	}
	if w.SessionID != "" {
		var sessionWorkspace sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM agent_sessions WHERE id=?`, w.SessionID).Scan(&sessionWorkspace)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!sessionWorkspace.Valid || sessionWorkspace.String != w.WorkspaceID)) {
			return harness.Applied{}, harness.ErrStoreScope
		}
		if err != nil {
			return harness.Applied{}, err
		}
	}

	now := s.now().Format(time.RFC3339Nano)
	var applied harness.Applied
	if w.Profile != nil {
		applied.Profile, err = writeHarnessRow(ctx, tx, harnessWrite{
			profile: true, key: w.WorkspaceID, workspaceID: w.WorkspaceID, target: "profile",
			expect: w.Profile.Expect, body: profileBody, remove: w.Profile.Value == nil, now: now,
		})
		if err != nil {
			return harness.Applied{}, err
		}
	}
	if s.betweenWrites != nil {
		if err := s.betweenWrites(); err != nil {
			return harness.Applied{}, err
		}
	}
	if w.Override != nil {
		applied.Override, err = writeHarnessRow(ctx, tx, harnessWrite{
			key: w.SessionID, workspaceID: w.WorkspaceID, target: "override",
			expect: w.Override.Expect, body: overrideBody, remove: w.Override.Value == nil, now: now,
		})
		if err != nil {
			return harness.Applied{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return harness.Applied{}, err
	}
	return applied, nil
}

func encodeProfile(w *harness.ProfileWrite) (string, error) {
	if w == nil || w.Value == nil {
		return "", nil
	}
	p := *w.Value
	if p.Schema != harness.SchemaVersion {
		return "", harness.ErrStoreCorrupt
	}
	p.Revision = 0 // the column owns the revision
	return encodeHarnessBody(p)
}

func encodeOverride(w *harness.OverrideWrite) (string, error) {
	if w == nil || w.Value == nil {
		return "", nil
	}
	o := *w.Value
	if o.Schema != harness.SchemaVersion {
		return "", harness.ErrStoreCorrupt
	}
	o.Revision = 0
	if o.Groups != nil {
		o.Groups = slices.Compact(slices.Sorted(slices.Values(o.Groups)))
		if o.Groups == nil {
			o.Groups = []string{}
		}
	}
	return encodeHarnessBody(o)
}

func encodeHarnessBody(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxHarnessBody {
		return "", harness.ErrStoreCorrupt
	}
	return string(b), nil
}

type harnessWrite struct {
	profile                  bool
	key, workspaceID, target string
	expect                   harness.Stamp
	body, now                string
	remove                   bool
}

// writeHarnessRow is one compare-and-swap. The revision is expect+1 whether the
// write creates, replaces or deletes, so no sequence of writes repeats a token.
func writeHarnessRow(ctx context.Context, tx *sql.Tx, w harnessWrite) (harness.Stamp, error) {
	cur, found, err := readHarnessRow(ctx, tx, w.profile, w.key)
	if err != nil {
		return harness.Stamp{}, err
	}
	var curRevision int64
	if found {
		curRevision = cur.revision
	}
	if curRevision != w.expect.Revision {
		return harness.Stamp{}, &harness.ConflictError{Target: w.target}
	}
	if w.remove && (!found || cur.deleted) {
		// Deleting what is not there changes nothing and keeps its token.
		return harness.Stamp{Revision: curRevision}, nil
	}
	next := curRevision + 1
	deleted := 0
	if w.remove {
		deleted = 1
	}
	switch {
	case !found && w.profile:
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_harness_profiles(workspace_id,schema_version,revision,deleted,body,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			w.key, harness.SchemaVersion, next, deleted, w.body, w.now, w.now)
	case !found:
		_, err = tx.ExecContext(ctx, `INSERT INTO session_harness_overrides(session_id,workspace_id,schema_version,revision,deleted,body,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			w.key, w.workspaceID, harness.SchemaVersion, next, deleted, w.body, w.now, w.now)
	default:
		var res sql.Result
		if w.profile {
			res, err = tx.ExecContext(ctx, `UPDATE workspace_harness_profiles SET schema_version=?,revision=?,deleted=?,body=?,updated_at=? WHERE workspace_id=? AND revision=?`,
				harness.SchemaVersion, next, deleted, w.body, w.now, w.key, curRevision)
		} else {
			res, err = tx.ExecContext(ctx, `UPDATE session_harness_overrides SET workspace_id=?,schema_version=?,revision=?,deleted=?,body=?,updated_at=? WHERE session_id=? AND revision=?`,
				w.workspaceID, harness.SchemaVersion, next, deleted, w.body, w.now, w.key, curRevision)
		}
		if err == nil {
			if n, _ := res.RowsAffected(); n != 1 {
				return harness.Stamp{}, &harness.ConflictError{Target: w.target}
			}
		}
	}
	if err != nil {
		return harness.Stamp{}, err
	}
	return harness.Stamp{Revision: next, Present: !w.remove}, nil
}
