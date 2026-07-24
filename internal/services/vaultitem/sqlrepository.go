package vaultitem

import (
	"context"
	"database/sql"
	"encoding/json"
)

// SQLRepository persists items in the vault_items table (id, type, data). The
// data column holds the JSON-encoded Record; type is duplicated into its own
// column for indexed ListByType. Payloads inside data are encrypted envelopes.
type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) scanRows(rows *sql.Rows) ([]Record, error) {
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *SQLRepository) List() ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM vault_items")
	if err != nil {
		return nil, err
	}
	return r.scanRows(rows)
}

func (r *SQLRepository) ListByType(itemType string) ([]Record, error) {
	rows, err := r.db.QueryContext(context.Background(), "SELECT data FROM vault_items WHERE type = ?", itemType)
	if err != nil {
		return nil, err
	}
	return r.scanRows(rows)
}

func (r *SQLRepository) Get(id string) (Record, error) {
	var data []byte
	err := r.db.QueryRowContext(context.Background(), "SELECT data FROM vault_items WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (r *SQLRepository) Put(record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		"INSERT INTO vault_items (id, type, data) VALUES (?, ?, ?) "+
			"ON CONFLICT(id) DO UPDATE SET type = excluded.type, data = excluded.data",
		record.ID, record.Type, data,
	)
	return err
}

func (r *SQLRepository) Delete(id string) error {
	_, err := r.db.ExecContext(context.Background(), "DELETE FROM vault_items WHERE id = ?", id)
	return err
}
