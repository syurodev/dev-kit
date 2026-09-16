// internal/codegraph/store.go
package codegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	end_line INTEGER NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	params TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS edges (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	kind TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
CREATE TABLE IF NOT EXISTS endpoints (
	id TEXT PRIMARY KEY,
	http_method TEXT NOT NULL,
	path TEXT NOT NULL,
	entry_node TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS file_dependencies (
	file_path TEXT NOT NULL,
	depends_on_file_path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_file_deps_depends_on ON file_dependencies(depends_on_file_path);
CREATE TABLE IF NOT EXISTS field_accesses (
	class_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	field_name TEXT NOT NULL,
	kind TEXT NOT NULL,
	line INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_field_accesses_lookup ON field_accesses(class_id, field_name);
CREATE TABLE IF NOT EXISTS call_sites (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	line INTEGER NOT NULL,
	unresolved TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_call_sites_from ON call_sites(from_id);
CREATE TABLE IF NOT EXISTS plan_node_comments (
	plan_node_id TEXT NOT NULL,
	comment_id   TEXT NOT NULL PRIMARY KEY,
	author       TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	session_id   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_node_comments ON plan_node_comments(plan_node_id, created_at);
CREATE TABLE IF NOT EXISTS plan_meta (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	description TEXT NOT NULL,
	state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_nodes (
	plan_node_id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	description TEXT NOT NULL,
	fqn TEXT NOT NULL,
	kind TEXT NOT NULL,
	ordinal INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_edges (
	from_plan_node_id TEXT NOT NULL,
	to_plan_node_id TEXT NOT NULL,
	condition TEXT NOT NULL
);

-- Which project this index file describes.
--
-- The file is already per project by naming convention (see ProjectDBPath),
-- but a convention is not something the code can check. Recording it here is
-- what lets an open reuse an index only when it is THIS project's — a store
-- holding rows is not the same claim as a store holding the right rows, and
-- acting on the weaker one reuses one project's index for another.
--
-- An index written before this table existed simply has no row, which reads as
-- "unknown" and costs one full re-index, once.
CREATE TABLE IF NOT EXISTS index_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// indexedProjectKey is the index_meta row holding the project root.
const indexedProjectKey = "project_root"

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
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: migrate schema: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func migrateSchema(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(nodes)")
	if err != nil {
		return fmt.Errorf("codegraph: inspect nodes table: %w", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("codegraph: scan table_info: %w", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("codegraph: table_info rows: %w", err)
	}

	if !cols["summary"] {
		if _, err := db.Exec("ALTER TABLE nodes ADD COLUMN summary TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("codegraph: add summary column: %w", err)
		}
	}
	if !cols["params"] {
		if _, err := db.Exec("ALTER TABLE nodes ADD COLUMN params TEXT NOT NULL DEFAULT '[]'"); err != nil {
			return fmt.Errorf("codegraph: add params column: %w", err)
		}
	}
	return nil
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

	for _, table := range []string{"nodes", "edges", "endpoints", "file_dependencies", "field_accesses", "call_sites"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
	for _, n := range result.Nodes {
		paramsJSON, err := json.Marshal(n.Params)
		if err != nil {
			return fmt.Errorf("codegraph: marshal params for node %q: %w", n.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nodes (id, label, kind, layer, file_path, line, end_line, summary, params) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Label, n.Kind, n.Layer, n.FilePath, n.Line, n.EndLine, n.Summary, string(paramsJSON)); err != nil {
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
	for _, fd := range result.FileDependencies {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO file_dependencies (file_path, depends_on_file_path) VALUES (?, ?)`,
			fd.FilePath, fd.DependsOnFilePath); err != nil {
			return fmt.Errorf("codegraph: insert file dependency %q->%q: %w", fd.FilePath, fd.DependsOnFilePath, err)
		}
	}
	for _, fa := range result.FieldAccesses {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO field_accesses (class_id, node_id, field_name, kind, line) VALUES (?, ?, ?, ?, ?)`,
			fa.ClassID, fa.NodeID, fa.FieldName, fa.Kind, fa.Line); err != nil {
			return fmt.Errorf("codegraph: insert field access %q.%q: %w", fa.ClassID, fa.FieldName, err)
		}
	}
	for _, c := range result.CallSites {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO call_sites (from_id, to_id, line, unresolved) VALUES (?, ?, ?, ?)`,
			c.From, c.To, c.Line, c.Unresolved); err != nil {
			return fmt.Errorf("codegraph: insert call site %q->%q: %w", c.From, c.To, err)
		}
	}
	// Commit the revision atomically with the graph, including empty indexes.
	if _, err := tx.ExecContext(ctx, `INSERT INTO index_meta(key, value) VALUES ('version', '1')
        ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`); err != nil {
		return fmt.Errorf("codegraph: increment index version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit replace: %w", err)
	}
	return nil
}

func (s *sqliteStore) IndexVersion(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM index_meta WHERE key = 'version'`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func (s *sqliteStore) HasIndex(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("codegraph: HasIndex: %w", err)
	}
	return count > 0, nil
}

// IndexedProject returns the project root this index was built from, or ""
// when the index predates the record or was never written.
func (s *sqliteStore) IndexedProject(ctx context.Context) (string, error) {
	var root string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM index_meta WHERE key = ?`, indexedProjectKey).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("codegraph: IndexedProject: %w", err)
	}
	return root, nil
}

// SetIndexedProject records which project the index now describes. Called
// after every successful ReplaceIndex, so the two cannot drift.
func (s *sqliteStore) SetIndexedProject(ctx context.Context, root string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO index_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		indexedProjectKey, root)
	if err != nil {
		return fmt.Errorf("codegraph: SetIndexedProject: %w", err)
	}
	return nil
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
	var paramsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes WHERE id = ?`, nodeID,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine, &n.Summary, &paramsJSON)
	if err == sql.ErrNoRows {
		return Node{}, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("codegraph: GetNode: %w", err)
	}
	if err := json.Unmarshal([]byte(paramsJSON), &n.Params); err != nil {
		return Node{}, fmt.Errorf("codegraph: unmarshal params for node %q: %w", nodeID, err)
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

// GetInEdges returns every edge pointing at nodeID — the reverse of
// GetOutEdges. Used by GetNeighbors, which explores both directions around a
// node (unlike walkFlow's directed-only call-chain walk).
func (s *sqliteStore) GetInEdges(ctx context.Context, nodeID string) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind FROM edges WHERE to_id = ? ORDER BY from_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetInEdges: %w", err)
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

// GetDependents returns every file that directly depends on filePath (i.e.
// filePath appears as their depends_on_file_path) — one hop, not transitive.
// GetBlastRadius (Service) walks this repeatedly for the transitive closure.
func (s *sqliteStore) GetDependents(ctx context.Context, filePath string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT file_path FROM file_dependencies WHERE depends_on_file_path = ? ORDER BY file_path`,
		filePath)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetDependents: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("codegraph: scan dependent: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetFieldAccesses(ctx context.Context, classID, fieldName string) ([]FieldAccess, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT class_id, node_id, field_name, kind, line FROM field_accesses WHERE class_id = ? AND field_name = ? ORDER BY line`,
		classID, fieldName)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetFieldAccesses: %w", err)
	}
	defer rows.Close()

	var out []FieldAccess
	for rows.Next() {
		var fa FieldAccess
		if err := rows.Scan(&fa.ClassID, &fa.NodeID, &fa.FieldName, &fa.Kind, &fa.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan field access: %w", err)
		}
		out = append(out, fa)
	}
	return out, rows.Err()
}

// ListFields returns every field reachable from one node, for a field picker.
//
// nodeID may be either a class or a method and the caller does not have to
// know which: field_accesses stores both the declaring class and the accessing
// method, so one OR matches whichever it is. A class yields all of its fields;
// a method yields only the fields it itself touches.
func (s *sqliteStore) ListFields(ctx context.Context, nodeID string) ([]FieldRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT class_id,
		       field_name,
		       SUM(CASE WHEN kind = 'read' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN kind = 'write' THEN 1 ELSE 0 END)
		  FROM field_accesses
		 WHERE class_id = ? OR node_id = ?
		 GROUP BY class_id, field_name
		 ORDER BY field_name`, nodeID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListFields: %w", err)
	}
	defer rows.Close()

	var out []FieldRef
	for rows.Next() {
		var f FieldRef
		if err := rows.Scan(&f.ClassID, &f.FieldName, &f.Reads, &f.Writes); err != nil {
			return nil, fmt.Errorf("codegraph: scan field ref: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetCallSites returns every call expression in one method, in source order.
// Repeats are preserved: a method that calls the same target twice has two
// rows, and losing one would put the trace's events in the wrong order.
func (s *sqliteStore) GetCallSites(ctx context.Context, fromID string) ([]CallSite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, line, unresolved FROM call_sites WHERE from_id = ? ORDER BY line`,
		fromID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetCallSites: %w", err)
	}
	defer rows.Close()

	var out []CallSite
	for rows.Next() {
		var c CallSite
		if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved); err != nil {
			return nil, fmt.Errorf("codegraph: scan call site: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FindNodeAtLine returns the narrowest node in filePath whose [line, end_line]
// range contains line — e.g. a method nested inside a class both technically
// contain the line, but the method (the smaller range) is the useful answer
// for "what am I looking at". Returns ok=false, not an error, when nothing
// in the index covers that line (unindexed file, or line past EOF).
func (s *sqliteStore) FindNodeAtLine(ctx context.Context, filePath string, line int) (Node, bool, error) {
	var n Node
	var paramsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes
		 WHERE file_path = ? AND ? BETWEEN line AND end_line
		 ORDER BY (end_line - line) ASC LIMIT 1`,
		filePath, line,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine, &n.Summary, &paramsJSON)
	if err == sql.ErrNoRows {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, fmt.Errorf("codegraph: FindNodeAtLine: %w", err)
	}
	if err := json.Unmarshal([]byte(paramsJSON), &n.Params); err != nil {
		return Node{}, false, fmt.Errorf("codegraph: unmarshal params for node %q: %w", n.ID, err)
	}
	return n, true, nil
}
