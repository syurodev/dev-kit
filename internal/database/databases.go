package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"devkit/internal/vaultitem"
)

// DatabaseLister is optional: PostgreSQL databases are connection boundaries,
// whereas MySQL databases already appear as schemas on the same connection.
type DatabaseLister interface {
	ListDatabases(context.Context) ([]string, error)
}

func (c *postgresConn) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := c.pool.Query(ctx, `SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate AND has_database_privilege(current_user, datname, 'CONNECT') ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
func (s *Service) ListDatabases(ctx context.Context, id string) ([]string, error) {
	conn, err := s.connect(ctx, id)
	if err != nil {
		return nil, err
	}
	lister, ok := conn.(DatabaseLister)
	if !ok {
		return nil, fmt.Errorf("database discovery is only available for PostgreSQL")
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	return lister.ListDatabases(ctx)
}

// OpenDatabase creates an encrypted metadata-only link to the server profile.
// Credentials and policy always resolve through the parent; pool/cache/tx keys
// remain the link's own stable ID, so no connection's database is mutated.
func (s *Service) OpenDatabase(ctx context.Context, parentID, name string) (ProfileSummary, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return ProfileSummary{}, errors.New("invalid database name")
	}
	parent, err := s.store.Get(parentID)
	if err != nil {
		return ProfileSummary{}, err
	}
	if parent.Type != itemType {
		return ProfileSummary{}, errors.New("invalid server profile")
	}
	meta, err := decodeMetadata(parent)
	if err != nil {
		return ProfileSummary{}, err
	}
	if meta.ParentProfileID != "" {
		return ProfileSummary{}, errors.New("choose the server connection to open a database")
	}
	if meta.Engine != "" && meta.Engine != EnginePostgres {
		return ProfileSummary{}, errors.New("database contexts are only available for PostgreSQL")
	}
	names, err := s.ListDatabases(ctx, parentID)
	if err != nil {
		return ProfileSummary{}, err
	}
	found := false
	for _, db := range names {
		if db == name {
			found = true
			break
		}
	}
	if !found {
		return ProfileSummary{}, errors.New("database is not available to this account")
	}
	defaultDB := meta.Database
	if defaultDB == "" {
		defaultDB = "postgres"
	}
	if defaultDB == name {
		return summaryFromRecord(parent)
	}
	s.mu.Lock()
	records, err := s.store.ListByType(itemType)
	if err != nil {
		s.mu.Unlock()
		return ProfileSummary{}, err
	}
	var linked vaultitem.Record
	for _, record := range records {
		candidate, err := decodeMetadata(record)
		if err != nil {
			s.mu.Unlock()
			return ProfileSummary{}, err
		}
		if candidate.ParentProfileID == parentID && candidate.Database == name {
			linked = record
			break
		}
	}
	if linked.ID == "" {
		raw, _ := json.Marshal(metadata{ParentProfileID: parentID, Database: name})
		linked, err = s.store.Create(itemType, passwordKeyID, raw, []byte{})
		if err != nil {
			s.mu.Unlock()
			return ProfileSummary{}, err
		}
	}
	s.mu.Unlock()
	target, err := s.target(linked.ID)
	if err != nil {
		return ProfileSummary{}, err
	}
	conn, err := s.pool(ctx, linked.ID, target)
	if err != nil {
		return ProfileSummary{}, err
	}
	pingCtx, cancel := queryContext(ctx)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		s.closeConnection(linked.ID)
		return ProfileSummary{}, err
	}
	return s.profileSummary(linked)
}
func (s *Service) profileSummary(record vaultitem.Record) (ProfileSummary, error) {
	meta, err := decodeMetadata(record)
	if err != nil {
		return ProfileSummary{}, err
	}
	if meta.ParentProfileID == "" {
		return summaryFromRecord(record)
	}
	parent, err := s.store.Get(meta.ParentProfileID)
	if err != nil {
		return ProfileSummary{}, err
	}
	if parent.Type != itemType {
		return ProfileSummary{}, errors.New("invalid parent profile")
	}
	root, err := decodeMetadata(parent)
	if err != nil {
		return ProfileSummary{}, err
	}
	if root.ParentProfileID != "" {
		return ProfileSummary{}, errors.New("nested database context is invalid")
	}
	summary, err := summaryFromRecord(parent)
	if err != nil {
		return ProfileSummary{}, err
	}
	summary.ID = record.ID
	summary.ParentProfileID = parent.ID
	summary.Database = meta.Database
	summary.Name += " / " + meta.Database
	return summary, nil
}
func (s *Service) databaseChildren(id string) ([]string, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, record := range records {
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		if meta.ParentProfileID == id {
			out = append(out, record.ID)
		}
	}
	return out, nil
}
