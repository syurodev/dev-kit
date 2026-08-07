package environments

import (
	"context"
	"database/sql"
)

// SQLRepository persists tags in the env_tags table (id, name,
// requires_confirmation, is_builtin, locked). Storage migration m12
// (internal/storage) creates the table and seeds the three builtin rows;
// this repository never seeds — same split as internal/vaultitem.SQLRepository.
type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func scanTag(scan func(dest ...any) error) (Tag, error) {
	var tag Tag
	var requiresConfirmation, isBuiltin, locked int
	if err := scan(&tag.ID, &tag.Name, &requiresConfirmation, &isBuiltin, &locked); err != nil {
		return Tag{}, err
	}
	tag.RequiresConfirmation = requiresConfirmation != 0
	tag.IsBuiltin = isBuiltin != 0
	tag.Locked = locked != 0
	return tag, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

const selectTagColumns = "SELECT id, name, requires_confirmation, is_builtin, locked FROM env_tags"

const listTagsQuery = selectTagColumns + " ORDER BY name"

func (r *SQLRepository) List() ([]Tag, error) {
	rows, err := r.db.QueryContext(context.Background(), listTagsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]Tag, 0)
	for rows.Next() {
		tag, err := scanTag(rows.Scan)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (r *SQLRepository) Get(name string) (Tag, error) {
	row := r.db.QueryRowContext(context.Background(), selectTagColumns+" WHERE name = ?", name)
	tag, err := scanTag(row.Scan)
	if err == sql.ErrNoRows {
		return Tag{}, ErrTagNotFound
	}
	return tag, err
}

func (r *SQLRepository) GetByID(id string) (Tag, error) {
	row := r.db.QueryRowContext(context.Background(), selectTagColumns+" WHERE id = ?", id)
	tag, err := scanTag(row.Scan)
	if err == sql.ErrNoRows {
		return Tag{}, ErrTagNotFound
	}
	return tag, err
}

func (r *SQLRepository) Create(tag Tag) error {
	_, err := r.db.ExecContext(context.Background(),
		"INSERT INTO env_tags (id, name, requires_confirmation, is_builtin, locked) VALUES (?, ?, ?, ?, ?)",
		tag.ID, tag.Name, boolToInt(tag.RequiresConfirmation), boolToInt(tag.IsBuiltin), boolToInt(tag.Locked))
	return err
}

func (r *SQLRepository) Update(tag Tag) error {
	result, err := r.db.ExecContext(context.Background(),
		"UPDATE env_tags SET requires_confirmation = ? WHERE id = ?",
		boolToInt(tag.RequiresConfirmation), tag.ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTagNotFound
	}
	return nil
}

func (r *SQLRepository) Delete(id string) error {
	_, err := r.db.ExecContext(context.Background(), "DELETE FROM env_tags WHERE id = ?", id)
	return err
}
