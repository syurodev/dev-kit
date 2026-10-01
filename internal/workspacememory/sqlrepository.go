package workspacememory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

// Database diagnostics can contain persisted content; keep them inside this layer.
var errStorage = errors.New("workspace memory storage unavailable")

func safeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errStorage
}
func (r *SQLRepository) WorkspaceExists(ctx context.Context, ws string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM workspaces WHERE id=?`, ws).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return n == 1, safeError(ctx, err)
}
func (r *SQLRepository) Get(ctx context.Context, ws, id string) (Entry, error) {
	var data string
	err := r.db.QueryRowContext(ctx, `SELECT data FROM workspace_memory WHERE workspace_id=? AND id=?`, ws, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, safeError(ctx, err)
	}
	var e Entry
	if json.Unmarshal([]byte(data), &e) != nil {
		return Entry{}, errStorage
	}
	return e, nil
}
func (r *SQLRepository) List(ctx context.Context, ws, q string, limit, offset int) (Page, error) {
	// instr treats search text literally, including SQL wildcard characters.
	rows, err := r.db.QueryContext(ctx, `SELECT json_remove(data,'$.body') FROM workspace_memory WHERE workspace_id=? AND deleted=0 AND (?='' OR instr(lower(title||char(10)||summary||char(10)||body),lower(?))>0) ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, ws, q, q, limit+1, offset)
	if err != nil {
		return Page{}, safeError(ctx, err)
	}
	defer rows.Close()
	p := Page{Entries: make([]Summary, 0)}
	for rows.Next() {
		var b []byte
		var summary Summary
		if err = rows.Scan(&b); err != nil {
			return Page{}, safeError(ctx, err)
		}
		if json.Unmarshal(b, &summary) != nil {
			return Page{}, errStorage
		}
		p.Entries = append(p.Entries, summary)
	}
	if err = rows.Err(); err != nil {
		return Page{}, safeError(ctx, err)
	}
	if len(p.Entries) > limit {
		p.HasMore = true
		p.Entries = p.Entries[:limit]
	}
	return p, nil
}
func (r *SQLRepository) Write(ctx context.Context, e Entry, expected int64, op string) error {
	data, err := json.Marshal(e)
	if err != nil {
		return errStorage
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM workspaces WHERE id=?`, e.WorkspaceID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceNotFound
	}
	if err != nil {
		return safeError(ctx, err)
	}
	if expected == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_memory(id,workspace_id,revision,deleted,title,summary,body,updated_at,data)VALUES(?,?,?,?,?,?,?,?,?)`, e.ID, e.WorkspaceID, e.Revision, e.Deleted, e.Title, e.Summary, e.Body, e.UpdatedAt, string(data))
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE workspace_memory SET revision=?,deleted=?,title=?,summary=?,body=?,updated_at=?,data=? WHERE workspace_id=? AND id=? AND revision=?`, e.Revision, e.Deleted, e.Title, e.Summary, e.Body, e.UpdatedAt, string(data), e.WorkspaceID, e.ID, expected)
		if err == nil {
			n, nerr := result.RowsAffected()
			if nerr != nil {
				return safeError(ctx, nerr)
			}
			if n == 0 {
				var revision int64
				find := tx.QueryRowContext(ctx, `SELECT revision FROM workspace_memory WHERE workspace_id=? AND id=?`, e.WorkspaceID, e.ID).Scan(&revision)
				if errors.Is(find, sql.ErrNoRows) {
					return ErrNotFound
				}
				if find != nil {
					return safeError(ctx, find)
				}
				return ErrConflict
			}
		}
	}
	if err != nil {
		return safeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_memory_revisions(entry_id,revision,operation,data)VALUES(?,?,?,?)`, e.ID, e.Revision, op, string(data))
	if err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
}
func (r *SQLRepository) History(ctx context.Context, ws, id string, limit, offset int) ([]Revision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT h.data,h.operation FROM workspace_memory_revisions h JOIN workspace_memory e ON e.id=h.entry_id WHERE e.workspace_id=? AND e.id=? ORDER BY h.revision DESC LIMIT ? OFFSET ?`, ws, id, limit, offset)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer rows.Close()
	out := make([]Revision, 0)
	for rows.Next() {
		var b []byte
		var rev Revision
		if err = rows.Scan(&b, &rev.Operation); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(b, &rev.Entry) != nil {
			return nil, errStorage
		}
		out = append(out, rev)
	}
	return out, safeError(ctx, rows.Err())
}

func (r *SQLRepository) GetRevision(ctx context.Context, ws, id string, target int64) (Revision, error) {
	var data []byte
	var rev Revision
	err := r.db.QueryRowContext(ctx, `SELECT h.data,h.operation FROM workspace_memory_revisions h JOIN workspace_memory e ON e.id=h.entry_id WHERE e.workspace_id=? AND e.id=? AND h.revision=?`, ws, id, target).Scan(&data, &rev.Operation)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, safeError(ctx, err)
	}
	if json.Unmarshal(data, &rev.Entry) != nil {
		return Revision{}, errStorage
	}
	return rev, nil
}
