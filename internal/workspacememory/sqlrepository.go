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
	expression, empty := searchExpression(ws, q)
	if empty {
		return Page{Entries: make([]Summary, 0)}, nil
	}
	dense, err := r.denseSearch(ctx, expression)
	if err != nil {
		return Page{}, err
	}
	statement, args := summaryQuery(ws, BrowseInput{Query: q}, expression, pageCursor{}, limit+1, offset, dense)
	entries, err := r.readSummaries(ctx, statement, args...)
	if err != nil {
		return Page{}, err
	}
	p := Page{Entries: entries}
	if len(entries) > limit {
		p.HasMore = true
		p.Entries = entries[:limit]
	}
	return p, nil
}
func entrySummary(e Entry) Summary {
	return Summary{ID: e.ID, WorkspaceID: e.WorkspaceID, Kind: e.Kind, Title: e.Title, Summary: e.Summary, Revision: e.Revision, Deleted: e.Deleted, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Source: e.Source}
}
func revisionSummary(e Entry, op string) RevisionSummary {
	return RevisionSummary{Revision: e.Revision, Operation: op, Title: e.Title, Summary: e.Summary, UpdatedAt: e.UpdatedAt, Deleted: e.Deleted, Source: e.Source}
}

func (r *SQLRepository) Write(ctx context.Context, e Entry, expected int64, op string) error {
	data, err := json.Marshal(e)
	if err != nil {
		return errStorage
	}
	summaryData, _ := json.Marshal(entrySummary(e))
	revisionData, _ := json.Marshal(revisionSummary(e, op))
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
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_memory(id,workspace_id,revision,deleted,title,summary,body,updated_at,data,kind,summary_data)VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.WorkspaceID, e.Revision, e.Deleted, e.Title, e.Summary, e.Body, e.UpdatedAt, string(data), e.Kind, string(summaryData))
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE workspace_memory SET revision=?,deleted=?,title=?,summary=?,body=?,updated_at=?,data=?,kind=?,summary_data=? WHERE workspace_id=? AND id=? AND revision=?`, e.Revision, e.Deleted, e.Title, e.Summary, e.Body, e.UpdatedAt, string(data), e.Kind, string(summaryData), e.WorkspaceID, e.ID, expected)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_memory_revisions(entry_id,revision,operation,data,summary_data)VALUES(?,?,?,?,?)`, e.ID, e.Revision, op, string(data), string(revisionData))
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
