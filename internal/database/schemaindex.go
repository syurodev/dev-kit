package database

import (
	"context"
	"errors"
	"log"
	"time"
)

// ErrSchemaCacheUnavailable means the service was built without a schema cache.
var ErrSchemaCacheUnavailable = errors.New("schema cache is not available")

// SetSchemaCache wires the local index. Without it, completion falls back to
// asking the database every time.
func (s *Service) SetSchemaCache(repo SchemaCacheRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schemaCache = repo
}

func (s *Service) schemaCacheRepo() SchemaCacheRepository {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.schemaCache
}

// IndexSchema walks the whole catalogue once and stores it. This is the only
// place that pays for a full introspect; everything afterwards reads the cache.
func (s *Service) IndexSchema(ctx context.Context, id string) (SchemaSnapshot, error) {
	repo := s.schemaCacheRepo()
	if repo == nil {
		return SchemaSnapshot{}, ErrSchemaCacheUnavailable
	}
	conn, err := s.connect(ctx, id)
	if err != nil {
		return SchemaSnapshot{}, err
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()

	schemas, err := conn.ListSchemas(ctx)
	if err != nil {
		return SchemaSnapshot{}, err
	}
	namespace := make(SchemaNamespace, len(schemas))
	for _, schema := range schemas {
		tables, err := conn.ListTables(ctx, schema)
		if err != nil {
			return SchemaSnapshot{}, err
		}
		entry := make(map[string][]string, len(tables))
		for _, table := range tables {
			columns, err := conn.ListColumns(ctx, schema, table.Name)
			if err != nil {
				return SchemaSnapshot{}, err
			}
			names := make([]string, 0, len(columns))
			for _, column := range columns {
				names = append(names, column.Name)
			}
			entry[table.Name] = names
		}
		namespace[schema] = entry
	}

	snapshot := SchemaSnapshot{Namespace: namespace, IndexedAt: time.Now().UTC()}
	if err := repo.Put(id, snapshot); err != nil {
		return SchemaSnapshot{}, err
	}
	return snapshot, nil
}

// GetSchemaCache reads the stored index. An empty cache indexes once rather
// than returning nothing — the first console open should not need a manual step.
func (s *Service) GetSchemaCache(ctx context.Context, id string) (SchemaSnapshot, error) {
	repo := s.schemaCacheRepo()
	if repo == nil {
		return SchemaSnapshot{}, ErrSchemaCacheUnavailable
	}
	snapshot, found, err := repo.Get(id)
	if err != nil {
		return SchemaSnapshot{}, err
	}
	if found {
		return snapshot, nil
	}
	return s.IndexSchema(ctx, id)
}

// refreshSchemaAfterDDL re-indexes in the background when a statement looked
// like DDL. Best effort on purpose: the query already succeeded, and a stale
// suggestion is a smaller harm than blocking on a second full introspect.
func (s *Service) refreshSchemaAfterDDL(id, sql string) {
	if !looksLikeDDL(sql) || s.schemaCacheRepo() == nil {
		return
	}
	go func() {
		if _, err := s.IndexSchema(context.Background(), id); err != nil {
			log.Printf("db schema cache: refresh after DDL: %v", err)
		}
	}()
}
