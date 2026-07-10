package ssh

import (
	"context"
	"encoding/json"

	"changeme/internal/storage"
)

// SQLRepository persists SSH profiles in SQLite (one JSON row each). Secrets are
// encrypted envelopes, so rows never contain plaintext passwords or keys.
type SQLRepository struct {
	kv *storage.KV
}

func NewSQLRepository(kv *storage.KV) *SQLRepository {
	return &SQLRepository{kv: kv}
}

func (r *SQLRepository) List() ([]ProfileRecord, error) {
	blobs, err := r.kv.List(context.Background())
	if err != nil {
		return nil, err
	}
	records := make([]ProfileRecord, 0, len(blobs))
	for _, blob := range blobs {
		var record ProfileRecord
		if err := json.Unmarshal(blob, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *SQLRepository) Get(id string) (ProfileRecord, error) {
	data, ok, err := r.kv.Get(context.Background(), id)
	if err != nil {
		return ProfileRecord{}, err
	}
	if !ok {
		return ProfileRecord{}, ErrNotFound
	}
	var record ProfileRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ProfileRecord{}, err
	}
	return record, nil
}

func (r *SQLRepository) Put(record ProfileRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.kv.Put(context.Background(), record.ID, data)
}

func (r *SQLRepository) Delete(id string) error {
	return r.kv.Delete(context.Background(), id)
}
