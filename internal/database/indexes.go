package database

import (
	"context"
	"errors"
)

// ErrEngineHasNoIndexes means the driver cannot report indexes. The ERD falls
// back to treating every relation as one-to-many rather than showing nothing.
var ErrEngineHasNoIndexes = errors.New("this engine does not report indexes")

// IndexInfo is one index as the engine reports it.
//
// Columns is ordered and complete or the index is not reported at all: a
// unique index on (a, b) says nothing about a on its own, and half of an index
// read as the whole thing would claim a one-to-one that does not exist.
type IndexInfo struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
}

// IndexLister is optional, alongside SessionInspector and QueryPlanner rather
// than on Conn: an engine that cannot answer should say so, not fake it.
//
// It is what separates one-to-one from one-to-many. Whether orders.customer_id
// carries a unique index is the only thing that distinguishes them, and no
// amount of reading column names will substitute for it.
type IndexLister interface {
	ListIndexes(ctx context.Context, schema string) ([]IndexInfo, error)
}

// ListIndexes reports every index in a schema in one pass, like
// ListForeignKeys: the ERD is the only caller and it wants the whole schema.
func (s *Service) ListIndexes(ctx context.Context, id, schema string) ([]IndexInfo, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	lister, ok := conn.(IndexLister)
	if !ok {
		return nil, ErrEngineHasNoIndexes
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return lister.ListIndexes(ctx, schema)
}

// appendIndexColumn folds one row of the (index, column) join into the index it
// belongs to. Both drivers order by index then column position, so a row either
// extends the index being built or starts a new one.
func appendIndexColumn(out []IndexInfo, row IndexInfo, column string) []IndexInfo {
	if n := len(out); n > 0 && out[n-1].Table == row.Table && out[n-1].Name == row.Name {
		out[n-1].Columns = append(out[n-1].Columns, column)
		return out
	}
	row.Columns = []string{column}
	return append(out, row)
}

// dropIndexes removes indexes the driver decided it could not read completely.
// The null column that disqualifies an index can arrive after some of its
// columns have already been folded in, so the removal happens at the end.
func dropIndexes(out []IndexInfo, dropped map[string]bool) []IndexInfo {
	if len(dropped) == 0 {
		return out
	}
	kept := out[:0]
	for _, idx := range out {
		if !dropped[idx.Table+"\x00"+idx.Name] {
			kept = append(kept, idx)
		}
	}
	return kept
}
