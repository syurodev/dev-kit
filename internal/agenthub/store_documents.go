package agenthub

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

const documentColumns = `id, session_id, kind, name, mime, size, sha256, pages, text_layer, chrome, created_at`

var _ DocumentStore = (*SQLStore)(nil)

func scanDocument(row interface{ Scan(...any) error }) (Document, error) {
	var d Document
	var created string
	if err := row.Scan(&d.ID, &d.SessionID, &d.Kind, &d.Name, &d.MIME, &d.Size, &d.SHA256, &d.Pages, &d.TextLayer, &d.Chrome, &created); err != nil {
		return Document{}, err
	}
	t, err := parseStoreTime(created)
	if err != nil {
		return Document{}, err
	}
	d.CreatedAt = t
	return d, nil
}

func (st *SQLStore) InsertDocument(ctx context.Context, d Document) error {
	ordinal, _ := strconv.Atoi(strings.TrimPrefix(d.ID, "doc"))
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO `+tableAgentDocuments+` (session_id, id, ordinal, kind, name, mime, size, sha256, pages, text_layer, chrome, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.SessionID, d.ID, ordinal, d.Kind, d.Name, d.MIME, d.Size, d.SHA256, d.Pages, d.TextLayer, d.Chrome, formatStoreTime(d.CreatedAt))
	return err
}

// GetDocument is scoped by the session in the query itself: a document of
// another session is not found, the same as one that does not exist.
func (st *SQLStore) GetDocument(ctx context.Context, sessionID, id string) (Document, error) {
	d, err := scanDocument(st.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM `+tableAgentDocuments+` WHERE session_id = ? AND id = ?`, sessionID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	return d, err
}

func (st *SQLStore) FindDocumentBySHA(ctx context.Context, sessionID, sha string) (Document, bool, error) {
	d, err := scanDocument(st.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM `+tableAgentDocuments+` WHERE session_id = ? AND sha256 = ?`, sessionID, sha))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, false, nil
	}
	return d, err == nil, err
}

func (st *SQLStore) ListDocuments(ctx context.Context, sessionID string) ([]Document, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+documentColumns+` FROM `+tableAgentDocuments+` WHERE session_id = ? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (st *SQLStore) DocumentUsage(ctx context.Context, sessionID string) (count int, bytes int64, ever int, err error) {
	err = st.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size), 0), COALESCE(MAX(ordinal), 0) FROM `+tableAgentDocuments+` WHERE session_id = ?`, sessionID).
		Scan(&count, &bytes, &ever)
	return count, bytes, ever, err
}

func (st *SQLStore) DocumentSessionIDs(ctx context.Context) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT DISTINCT session_id FROM `+tableAgentDocuments)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
