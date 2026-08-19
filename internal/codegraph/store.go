// internal/codegraph/store.go
package codegraph

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	layer TEXT NOT NULL,
	file_path TEXT NOT NULL,
	line INTEGER NOT NULL,
	end_line INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS edges (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	kind TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE TABLE IF NOT EXISTS endpoints (
	id TEXT PRIMARY KEY,
	http_method TEXT NOT NULL,
	path TEXT NOT NULL,
	entry_node TEXT NOT NULL
);
`

// sqliteStore is a Store backed by one SQLite file per project. It is not
// safe for concurrent use by multiple goroutines without external locking —
// Service (Task 3) serializes access with a mutex.
type sqliteStore struct {
	db *sql.DB
}

func newSQLiteStore(path string) (*sqliteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("codegraph: open sqlite store: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: create schema: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) ReplaceIndex(ctx context.Context, result AnalysisResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codegraph: begin replace: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"nodes", "edges", "endpoints"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
	for _, n := range result.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nodes (id, label, kind, layer, file_path, line, end_line) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Label, n.Kind, n.Layer, n.FilePath, n.Line, n.EndLine); err != nil {
			return fmt.Errorf("codegraph: insert node %q: %w", n.ID, err)
		}
	}
	for _, e := range result.Edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO edges (from_id, to_id, kind) VALUES (?, ?, ?)`,
			e.From, e.To, e.Kind); err != nil {
			return fmt.Errorf("codegraph: insert edge %q->%q: %w", e.From, e.To, err)
		}
	}
	for _, ep := range result.Endpoints {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO endpoints (id, http_method, path, entry_node) VALUES (?, ?, ?, ?)`,
			ep.ID, ep.HTTPMethod, ep.Path, ep.EntryNode); err != nil {
			return fmt.Errorf("codegraph: insert endpoint %q: %w", ep.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit replace: %w", err)
	}
	return nil
}

func (s *sqliteStore) HasIndex(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("codegraph: HasIndex: %w", err)
	}
	return count > 0, nil
}

func (s *sqliteStore) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, http_method, path, entry_node FROM endpoints ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListEndpoints: %w", err)
	}
	defer rows.Close()

	var out []Endpoint
	for rows.Next() {
		var ep Endpoint
		if err := rows.Scan(&ep.ID, &ep.HTTPMethod, &ep.Path, &ep.EntryNode); err != nil {
			return nil, fmt.Errorf("codegraph: scan endpoint: %w", err)
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error) {
	var ep Endpoint
	err := s.db.QueryRowContext(ctx,
		`SELECT id, http_method, path, entry_node FROM endpoints WHERE id = ?`, endpointID,
	).Scan(&ep.ID, &ep.HTTPMethod, &ep.Path, &ep.EntryNode)
	if err == sql.ErrNoRows {
		return Endpoint{}, ErrNodeNotFound
	}
	if err != nil {
		return Endpoint{}, fmt.Errorf("codegraph: GetEndpoint: %w", err)
	}
	return ep, nil
}

func (s *sqliteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	var n Node
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line FROM nodes WHERE id = ?`, nodeID,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine)
	if err == sql.ErrNoRows {
		return Node{}, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("codegraph: GetNode: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind FROM edges WHERE from_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetOutEdges: %w", err)
	}
	defer rows.Close()

	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind); err != nil {
			return nil, fmt.Errorf("codegraph: scan edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, file_path, line FROM nodes WHERE label LIKE ? ORDER BY label`,
		"%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("codegraph: SearchSymbol: %w", err)
	}
	defer rows.Close()

	var out []SymbolMatch
	for rows.Next() {
		var m SymbolMatch
		if err := rows.Scan(&m.NodeID, &m.Label, &m.Kind, &m.FilePath, &m.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan symbol match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
