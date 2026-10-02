package workspacememory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// summaryQuery sorts and limits FTS candidate IDs before loading summary JSON.
// Sparse matches start from FTS postings; ordinary browsing uses the workspace
// ordering index. Search sorting contains only ID/timestamp keys, never bodies.
func summaryQuery(ws string, in BrowseInput, expression string, cursor pageCursor, limit, offset int, dense bool) (string, []any) {
	statement := `SELECT m.summary_data FROM workspace_memory m WHERE m.workspace_id=? AND m.deleted=?`
	args := []any{ws, in.Deleted}
	if expression != "" && !dense {
		statement = `SELECT m.rowid AS entry_rowid,m.updated_at,m.id FROM workspace_memory_fts CROSS JOIN workspace_memory m WHERE m.rowid=workspace_memory_fts.rowid AND workspace_memory_fts MATCH ? AND m.workspace_id=? AND m.deleted=?`
		args = []any{expression, ws, in.Deleted}
	}
	if in.Kind != "" {
		statement += ` AND m.kind=?`
		args = append(args, in.Kind)
	}
	if cursor.ID != "" {
		statement += ` AND (m.updated_at,m.id)<(?,?)`
		args = append(args, cursor.UpdatedAt, cursor.ID)
	}
	if expression != "" && dense {
		statement += ` AND m.rowid IN (SELECT rowid FROM workspace_memory_fts WHERE workspace_memory_fts MATCH ?)`
		args = append(args, expression)
	}
	statement += ` ORDER BY m.updated_at DESC,m.id DESC LIMIT ?`
	args = append(args, limit)
	if offset >= 0 {
		statement += ` OFFSET ?`
		args = append(args, offset)
	}
	if expression != "" && !dense {
		statement = `SELECT result.summary_data FROM (` + statement + `) page JOIN workspace_memory result ON result.rowid=page.entry_rowid ORDER BY page.updated_at DESC,page.id DESC`
	}
	return statement, args
}

// Probe only a bounded prefix of the postings. Sparse searches benefit from
// direct FTS-to-rowid lookups; dense searches preserve the workspace ordering
// index and avoid sorting all candidate table rows. Both paths share semantics.
func (r *SQLRepository) denseSearch(ctx context.Context, expression string) (bool, error) {
	if expression == "" {
		return false, nil
	}
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT rowid FROM workspace_memory_fts WHERE workspace_memory_fts MATCH ? LIMIT 129)`, expression).Scan(&count)
	return count > 128, safeError(ctx, err)
}
func (r *SQLRepository) readSummaries(ctx context.Context, statement string, args ...any) ([]Summary, error) {
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer rows.Close()
	entries := make([]Summary, 0)
	for rows.Next() {
		var data []byte
		var summary Summary
		if err = rows.Scan(&data); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(data, &summary) != nil {
			return nil, errStorage
		}
		entries = append(entries, summary)
	}
	return entries, safeError(ctx, rows.Err())
}
func (r *SQLRepository) Browse(ctx context.Context, ws string, in BrowseInput) (BrowsePage, error) {
	binding := browseBinding(ws, in)
	cursor, err := decodeCursor(in.Cursor, binding, false)
	if err != nil {
		return BrowsePage{}, err
	}
	expression, empty := searchExpression(ws, in.Query)
	if empty {
		return BrowsePage{Entries: make([]Summary, 0)}, nil
	}
	dense, err := r.denseSearch(ctx, expression)
	if err != nil {
		return BrowsePage{}, err
	}
	statement, args := summaryQuery(ws, in, expression, cursor, in.Limit+1, -1, dense)
	entries, err := r.readSummaries(ctx, statement, args...)
	if err != nil {
		return BrowsePage{}, err
	}
	p := BrowsePage{Entries: entries}
	if len(entries) > in.Limit {
		p.HasMore = true
		p.Entries = entries[:in.Limit]
		last := p.Entries[len(p.Entries)-1]
		p.NextCursor = encodeCursor(pageCursor{Binding: binding, UpdatedAt: last.UpdatedAt, ID: last.ID})
	}
	return p, nil
}
func (r *SQLRepository) HistoryPage(ctx context.Context, ws, id, text string, limit int) (HistoryPage, error) {
	binding := cursorBinding("history", ws, id)
	cursor, err := decodeCursor(text, binding, true)
	if err != nil {
		return HistoryPage{}, err
	}
	var n int
	err = r.db.QueryRowContext(ctx, `SELECT 1 FROM workspace_memory WHERE workspace_id=? AND id=?`, ws, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return HistoryPage{}, ErrNotFound
	}
	if err != nil {
		return HistoryPage{}, safeError(ctx, err)
	}
	statement := `SELECT summary_data FROM workspace_memory_revisions WHERE entry_id=?`
	args := []any{id}
	if cursor.Revision > 0 {
		statement += ` AND revision<?`
		args = append(args, cursor.Revision)
	}
	statement += ` ORDER BY revision DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return HistoryPage{}, safeError(ctx, err)
	}
	defer rows.Close()
	p := HistoryPage{Entries: make([]RevisionSummary, 0)}
	for rows.Next() {
		var data []byte
		var summary RevisionSummary
		if err = rows.Scan(&data); err != nil {
			return HistoryPage{}, safeError(ctx, err)
		}
		if json.Unmarshal(data, &summary) != nil {
			return HistoryPage{}, errStorage
		}
		p.Entries = append(p.Entries, summary)
	}
	if err = rows.Err(); err != nil {
		return HistoryPage{}, safeError(ctx, err)
	}
	if len(p.Entries) > limit {
		p.HasMore = true
		p.Entries = p.Entries[:limit]
		p.NextCursor = encodeCursor(pageCursor{Binding: binding, Revision: p.Entries[len(p.Entries)-1].Revision})
	}
	return p, nil
}
