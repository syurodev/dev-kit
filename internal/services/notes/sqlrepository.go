package notes

import (
	"context"
	"encoding/json"

	"changeme/internal/storage"
)

// SQLRepository persists notes in SQLite (one JSON row per note). Bodies are
// encrypted envelopes, so the row never contains plaintext note content.
type SQLRepository struct {
	kv *storage.KV
}

func NewSQLRepository(kv *storage.KV) *SQLRepository {
	return &SQLRepository{kv: kv}
}

func (r *SQLRepository) List() ([]Record, error) {
	blobs, err := r.kv.List(context.Background())
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(blobs))
	for _, blob := range blobs {
		var record Record
		if err := json.Unmarshal(blob, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *SQLRepository) Get(id string) (Record, error) {
	data, ok, err := r.kv.Get(context.Background(), id)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, ErrNotFound
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
	return r.kv.Put(context.Background(), record.ID, data)
}

func (r *SQLRepository) Delete(id string) error {
	return r.kv.Delete(context.Background(), id)
}
