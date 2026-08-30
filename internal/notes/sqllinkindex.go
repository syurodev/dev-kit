package notes

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"devkit/internal/storage"
)

// SQLLinkIndex stores note references in a plaintext table outside the vault.
type SQLLinkIndex struct {
	db *sql.DB
}

func NewSQLLinkIndex(db *sql.DB) *SQLLinkIndex {
	return &SQLLinkIndex{db: db}
}

// ReplaceLinks makes the stored set for one note match what its body says now.
// Delete-then-insert rather than a diff: a note links to a handful of others,
// and the whole thing is one transaction either way.
func (i *SQLLinkIndex) ReplaceLinks(fromNoteID string, toNoteIDs []string) error {
	ctx := context.Background()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE from_note_id = ?", storage.TableNoteLinks),
		fromNoteID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, toID := range toNoteIDs {
		// A note linking to itself is noise in its own backlink list.
		if toID == fromNoteID {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"INSERT OR REPLACE INTO %s (from_note_id, to_note_id, updated_at) VALUES (?, ?, ?)",
			storage.TableNoteLinks), fromNoteID, toID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (i *SQLLinkIndex) ListBacklinks(toNoteID string) ([]string, error) {
	rows, err := i.db.QueryContext(context.Background(), fmt.Sprintf(
		"SELECT from_note_id FROM %s WHERE to_note_id = ? ORDER BY updated_at DESC",
		storage.TableNoteLinks), toNoteID)
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

// DeleteLinksFor clears both directions, so purging a note leaves no row
// pointing at an id that no longer exists.
func (i *SQLLinkIndex) DeleteLinksFor(noteID string) error {
	_, err := i.db.ExecContext(context.Background(), fmt.Sprintf(
		"DELETE FROM %s WHERE from_note_id = ? OR to_note_id = ?",
		storage.TableNoteLinks), noteID, noteID)
	return err
}
