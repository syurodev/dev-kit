// internal/codegraph/store.go
package codegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	kind TEXT NOT NULL,
	guard TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file_path, line);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
CREATE TABLE IF NOT EXISTS config_keys (
	key TEXT NOT NULL,
	canon TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	value TEXT NOT NULL,
	redacted INTEGER NOT NULL,
	profile TEXT NOT NULL,
	env_vars TEXT NOT NULL,
	-- " A B " (space-padded) so one LIKE finds a variable: the names are canonical, with no spaces of their own.
	env_canon TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_config_keys_canon ON config_keys(canon);
CREATE TABLE IF NOT EXISTS config_usages (
	key TEXT NOT NULL,
	canon TEXT NOT NULL,
	kind TEXT NOT NULL,
	from_id TEXT NOT NULL,
	field TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	default_value TEXT NOT NULL,
	detail TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_config_usages_canon ON config_usages(canon);
CREATE INDEX IF NOT EXISTS idx_config_usages_kind ON config_usages(kind);
CREATE TABLE IF NOT EXISTS entity_tables (
	entity_id TEXT NOT NULL,
	entity_name TEXT NOT NULL,
	table_name TEXT NOT NULL,
	schema_name TEXT NOT NULL,
	kind TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	canon_table TEXT NOT NULL,
	canon_entity TEXT NOT NULL,
	canon_class TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entity_tables_table ON entity_tables(canon_table);
CREATE INDEX IF NOT EXISTS idx_entity_tables_entity ON entity_tables(canon_entity);
CREATE TABLE IF NOT EXISTS table_accesses (
	from_id TEXT NOT NULL,
	table_name TEXT NOT NULL,
	schema_name TEXT NOT NULL,
	canon TEXT NOT NULL,
	access TEXT NOT NULL,
	via TEXT NOT NULL,
	detail TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	col INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_table_accesses_canon ON table_accesses(canon);
CREATE TABLE IF NOT EXISTS outbound_calls (
	from_id TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	client TEXT NOT NULL,
	http_method TEXT NOT NULL,
	service_name TEXT NOT NULL,
	url TEXT NOT NULL,
	col INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_outbound_calls_from ON outbound_calls(from_id);
CREATE TABLE IF NOT EXISTS redis_accesses (
	from_id TEXT NOT NULL,
	key TEXT NOT NULL,
	access TEXT NOT NULL,
	command TEXT NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	col INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS kafka_accesses (
	from_id TEXT NOT NULL,
	topic TEXT NOT NULL,
	role TEXT NOT NULL,
	client TEXT NOT NULL,
	group_id TEXT NOT NULL,
	is_pattern INTEGER NOT NULL,
	file TEXT NOT NULL,
	line INTEGER NOT NULL,
	col INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS entry_points (
	id TEXT PRIMARY KEY,
	verb TEXT NOT NULL,
	target TEXT NOT NULL,
	entry_node TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT '',
	service TEXT NOT NULL DEFAULT '',
	owner TEXT NOT NULL DEFAULT '',
	grp TEXT NOT NULL DEFAULT '',
	ord INTEGER NOT NULL DEFAULT 0,
    route_id TEXT NOT NULL DEFAULT '',
    topic TEXT NOT NULL DEFAULT '',
    consumer_group TEXT NOT NULL DEFAULT ''
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
	unresolved TEXT NOT NULL,
	col INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL DEFAULT '',
	callee_key TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	result_use TEXT NOT NULL DEFAULT '',
	frames TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT '',
	target TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_call_sites_from ON call_sites(from_id);
CREATE INDEX IF NOT EXISTS idx_call_sites_to ON call_sites(to_id);
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

// indexedAnalyzerKey is the index_meta row naming the analyzer that wrote the
// index (analyzerIdentity).
const indexedAnalyzerKey = "analyzer"

// sqliteStore is a Store backed by one SQLite file per project. Safe for
// concurrent use (database/sql pools connections; see storePragmas). Service
// still serialises index writes itself — see Service.writeMu — so that the
// order they land in is the order they were requested in.
type sqliteStore struct {
	db *sql.DB

	// stats caches CallStats: it reads all of call_sites, and only
	// ReplaceIndex changes that. statsGen is bumped by each ReplaceIndex, so
	// a count read across one is not kept.
	statsMu  sync.Mutex
	stats    *CallStats
	statsGen uint64
}

// postMigrationIndexes run after migrateSchema, so an index may name a
// column a migration added. The step lookups read these tables by caller.
const postMigrationIndexes = `
CREATE INDEX IF NOT EXISTS idx_kafka_accesses_from ON kafka_accesses(from_id);
CREATE INDEX IF NOT EXISTS idx_redis_accesses_from ON redis_accesses(from_id);
CREATE INDEX IF NOT EXISTS idx_table_accesses_from ON table_accesses(from_id);
CREATE INDEX IF NOT EXISTS idx_field_accesses_node ON field_accesses(node_id, kind);
`

// storePragmas apply to every connection the pool opens (they are DSN
// parameters, not one-off statements, because database/sql may open more).
//
//   - WAL lets queries read the previous snapshot while a re-index rewrites
//     the whole index in one transaction, instead of blocking behind it.
//   - busy_timeout makes a writer that finds the file locked wait for it,
//     rather than failing the index outright with SQLITE_BUSY.
//   - synchronous=NORMAL is WAL's usual pairing: durable across app crashes,
//     and the index can always be re-derived from source after a power cut.
const storePragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

// maxStoreConns bounds the pool. A few readers can proceed alongside the one
// writer WAL allows; more would only contend for the same file.
const maxStoreConns = 4

func newSQLiteStore(path string) (*sqliteStore, error) {
	db, err := sql.Open("sqlite", path+storePragmas)
	if err != nil {
		return nil, fmt.Errorf("codegraph: open sqlite store: %w", err)
	}
	db.SetMaxOpenConns(maxStoreConns)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: create schema: %w", err)
	}
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: migrate schema: %w", err)
	}
	if _, err := db.Exec(postMigrationIndexes); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: create indexes: %w", err)
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
	if err := migrateEdgeGuard(db); err != nil {
		return err
	}
	if err := migrateCallSiteSteps(db); err != nil {
		return err
	}
	if err := migrateEndpointsTable(db); err != nil {
		return err
	}
	// Additive migration keeps old HTTP indexes readable. Analyzer identity
	// (jar + Go build) already invalidates reuse on a rebuild.
	for _, name := range []string{"route_id", "topic", "consumer_group"} {
		rows, err := db.Query("PRAGMA table_info(entry_points)")
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var col, typ string
			var dflt *string
			if err := rows.Scan(&cid, &col, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return err
			}
			if col == name {
				found = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			if _, err := db.Exec("ALTER TABLE entry_points ADD COLUMN " + name + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateEdgeGuard adds edges.guard to an index written before edges carried
// the condition they run under. Its rows read as unguarded, which is what the
// old index said; the next re-index fills it in.
func migrateEdgeGuard(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(edges)")
	if err != nil {
		return fmt.Errorf("codegraph: inspect edges table: %w", err)
	}
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("codegraph: scan table_info: %w", err)
		}
		has = has || name == "guard"
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("codegraph: table_info rows: %w", err)
	}
	rows.Close()
	if !has {
		if _, err := db.Exec("ALTER TABLE edges ADD COLUMN guard TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("codegraph: add edges.guard column: %w", err)
		}
	}
	return nil
}

// migrateCallSiteSteps adds the step columns to an index written before the
// flow outline. Its call sites read with no step — a flat outline — until the
// next re-index fills them in. The extractor tables gain the column of the
// call an access was seen at; an old row reads as 0, which GetSteps joins by
// line alone.
func migrateCallSiteSteps(db *sql.DB) error {
	if err := addMissingColumns(db, "call_sites", []tableColumn{
		{"col", "INTEGER NOT NULL DEFAULT 0"},
		{"text", "TEXT NOT NULL DEFAULT ''"},
		{"callee_key", "TEXT NOT NULL DEFAULT ''"},
		{"receiver", "TEXT NOT NULL DEFAULT ''"},
		{"result_use", "TEXT NOT NULL DEFAULT ''"},
		{"frames", "TEXT NOT NULL DEFAULT ''"},
		{"role", "TEXT NOT NULL DEFAULT ''"},
		{"target", "TEXT NOT NULL DEFAULT ''"},
	}); err != nil {
		return err
	}
	for _, table := range []string{"kafka_accesses", "redis_accesses", "table_accesses", "outbound_calls"} {
		if err := addMissingColumns(db, table, []tableColumn{{"col", "INTEGER NOT NULL DEFAULT 0"}}); err != nil {
			return err
		}
	}
	return nil
}

type tableColumn struct{ name, ddl string }

// addMissingColumns adds each column table does not have yet.
func addMissingColumns(db *sql.DB, table string, columns []tableColumn) error {
	have, err := tableColumns(db, table)
	if err != nil {
		return err
	}
	for _, c := range columns {
		if have[c.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + c.name + " " + c.ddl); err != nil {
			return fmt.Errorf("codegraph: add %s.%s column: %w", table, c.name, err)
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("codegraph: inspect %s table: %w", table, err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("codegraph: scan table_info: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("codegraph: table_info rows: %w", err)
	}
	return have, nil
}

// migrateEndpointsTable moves an index written when entry points were called
// endpoints (and were only HTTP routes) into entry_points. The rows are
// copied rather than dropped: a reopen serves the stored index while it
// re-indexes, and an empty list for those seconds would read as "this project
// has none". What they lack — kind, owner, group — completeEntryPoints fills
// on read.
func migrateEndpointsTable(db *sql.DB) error {
	var legacy int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'endpoints'`).Scan(&legacy); err != nil {
		return fmt.Errorf("codegraph: inspect endpoints table: %w", err)
	}
	if legacy == 0 {
		return nil
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO entry_points (id, verb, target, entry_node)
		SELECT id, http_method, path, entry_node FROM endpoints`); err != nil {
		return fmt.Errorf("codegraph: copy endpoints: %w", err)
	}
	if _, err := db.Exec(`DROP TABLE endpoints`); err != nil {
		return fmt.Errorf("codegraph: drop endpoints: %w", err)
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

	for _, table := range []string{"nodes", "edges", "entry_points", "file_dependencies", "field_accesses", "call_sites", "config_keys", "config_usages", "entity_tables", "table_accesses", "outbound_calls", "redis_accesses", "kafka_accesses"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}

	// One prepared statement per table, reused for every row: a large project
	// is hundreds of thousands of rows, and re-parsing the same INSERT for each
	// was most of the time the write took.
	insert := func(query string) (*sql.Stmt, error) {
		stmt, err := tx.PrepareContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("codegraph: prepare %q: %w", query, err)
		}
		return stmt, nil
	}

	// A duplicate node ID is an analyzer bug — two declarations it resolved to
	// the same signature — but it is one node, and failing the whole index
	// over it left the user with nothing at all. First-seen wins, the same rule
	// as entry points below; every edge that names the ID still points at a node
	// that exists, so the graph stays consistent.
	nodeStmt, err := insert(`INSERT INTO nodes (id, label, kind, layer, file_path, line, end_line, summary, params) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer nodeStmt.Close()
	seenNodes := make(map[string]bool, len(result.Nodes))
	for _, n := range result.Nodes {
		if seenNodes[n.ID] {
			continue
		}
		seenNodes[n.ID] = true
		paramsJSON, err := json.Marshal(n.Params)
		if err != nil {
			return fmt.Errorf("codegraph: marshal params for node %q: %w", n.ID, err)
		}
		if _, err := nodeStmt.ExecContext(ctx,
			n.ID, n.Label, n.Kind, n.Layer, n.FilePath, n.Line, n.EndLine, n.Summary, string(paramsJSON)); err != nil {
			return fmt.Errorf("codegraph: insert node %q: %w", n.ID, err)
		}
	}

	edgeStmt, err := insert(`INSERT INTO edges (from_id, to_id, kind, guard) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer edgeStmt.Close()
	for _, e := range result.Edges {
		if _, err := edgeStmt.ExecContext(ctx, e.From, e.To, e.Kind, e.Guard); err != nil {
			return fmt.Errorf("codegraph: insert edge %q->%q: %w", e.From, e.To, err)
		}
	}

	// An entry point's ID is its verb and target, the same string the UI
	// addresses it by — so two entry points that map to the identical route
	// collide on insert. That is not always an analyzer bug: a Feign-style
	// client interface declaring the same route its own controller serves,
	// or a content-negotiation overload (same path, different `produces`)
	// this best-effort extractor does not distinguish, both produce it on
	// an entirely valid project. First-seen wins, in the analyzer's own file
	// order — deterministic, and losing one entry from the list
	// beats losing the whole index over it.
	entryStmt, err := insert(`INSERT INTO entry_points (id, verb, target, entry_node, kind, service, owner, grp, ord, route_id, topic, consumer_group) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer entryStmt.Close()
	seenEntries := make(map[string]bool, len(result.EntryPoints))
	for _, ep := range result.EntryPoints {
		if seenEntries[ep.ID] {
			continue
		}
		seenEntries[ep.ID] = true
		if _, err := entryStmt.ExecContext(ctx, ep.ID, ep.Verb, ep.Target, ep.EntryNode, ep.Kind, ep.Service, ep.Owner, ep.Group, ep.Order, ep.RouteID, ep.Topic, ep.ConsumerGroup); err != nil {
			return fmt.Errorf("codegraph: insert entry point %q: %w", ep.ID, err)
		}
	}

	depStmt, err := insert(`INSERT INTO file_dependencies (file_path, depends_on_file_path) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer depStmt.Close()
	for _, fd := range result.FileDependencies {
		if _, err := depStmt.ExecContext(ctx, fd.FilePath, fd.DependsOnFilePath); err != nil {
			return fmt.Errorf("codegraph: insert file dependency %q->%q: %w", fd.FilePath, fd.DependsOnFilePath, err)
		}
	}

	accessStmt, err := insert(`INSERT INTO field_accesses (class_id, node_id, field_name, kind, line) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer accessStmt.Close()
	for _, fa := range result.FieldAccesses {
		if _, err := accessStmt.ExecContext(ctx, fa.ClassID, fa.NodeID, fa.FieldName, fa.Kind, fa.Line); err != nil {
			return fmt.Errorf("codegraph: insert field access %q.%q: %w", fa.ClassID, fa.FieldName, err)
		}
	}

	siteStmt, err := insert(`INSERT INTO call_sites (from_id, to_id, line, unresolved, col, text, callee_key, receiver, result_use, frames, role, target)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer siteStmt.Close()
	for _, c := range result.CallSites {
		frames := ""
		if len(c.Frames) > 0 {
			body, err := json.Marshal(c.Frames)
			if err != nil {
				return fmt.Errorf("codegraph: encode frames of %q: %w", c.From, err)
			}
			frames = string(body)
		}
		if _, err := siteStmt.ExecContext(ctx, c.From, c.To, c.Line, c.Unresolved, c.Col, c.Text, c.CalleeKey,
			c.Receiver, c.ResultUse, frames, c.Role, c.Target); err != nil {
			return fmt.Errorf("codegraph: insert call site %q->%q: %w", c.From, c.To, err)
		}
	}

	// Commit the revision atomically with the graph, including empty indexes.
	keyStmt, err := insert(`INSERT INTO config_keys (key, canon, file, line, value, redacted, profile, env_vars, env_canon) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer keyStmt.Close()
	for _, k := range result.ConfigKeys {
		envJSON, err := json.Marshal(k.EnvVars)
		if err != nil {
			return fmt.Errorf("codegraph: marshal env vars of %q: %w", k.Key, err)
		}
		envCanon := " "
		for _, name := range k.EnvVars {
			envCanon += CanonicalConfigKey(name) + " "
		}
		redacted := 0
		if k.Redacted {
			redacted = 1
		}
		if _, err := keyStmt.ExecContext(ctx, k.Key, CanonicalConfigKey(k.Key), k.File, k.Line, k.Value, redacted, k.Profile, string(envJSON), envCanon); err != nil {
			return fmt.Errorf("codegraph: insert config key %q: %w", k.Key, err)
		}
	}

	useStmt, err := insert(`INSERT INTO config_usages (key, canon, kind, from_id, field, file, line, default_value, detail) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer useStmt.Close()
	for _, u := range result.ConfigUsages {
		if _, err := useStmt.ExecContext(ctx, u.Key, CanonicalConfigKey(u.Key), u.Kind, u.From, u.Field, u.File, u.Line, u.DefaultValue, u.Detail); err != nil {
			return fmt.Errorf("codegraph: insert config use %q: %w", u.Key, err)
		}
	}

	entityStmt, err := insert(`INSERT INTO entity_tables (entity_id, entity_name, table_name, schema_name, kind, file, line, canon_table, canon_entity, canon_class) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer entityStmt.Close()
	for _, e := range result.EntityTables {
		if _, err := entityStmt.ExecContext(ctx, e.Entity, e.EntityName, e.Table, e.Schema, e.Kind, e.File, e.Line,
			CanonicalConfigKey(e.Table), CanonicalConfigKey(e.EntityName), CanonicalConfigKey(simpleNodeName(e.Entity))); err != nil {
			return fmt.Errorf("codegraph: insert entity table %q: %w", e.Table, err)
		}
	}

	tableAccessStmt, err := insert(`INSERT INTO table_accesses (from_id, table_name, schema_name, canon, access, via, detail, file, line, col) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer tableAccessStmt.Close()
	for _, a := range result.TableAccesses {
		if _, err := tableAccessStmt.ExecContext(ctx, a.From, a.Table, a.Schema, CanonicalConfigKey(a.Table), a.Access, a.Via, a.Detail, a.File, a.Line, a.Col); err != nil {
			return fmt.Errorf("codegraph: insert table access to %q: %w", a.Table, err)
		}
	}

	outboundStmt, err := insert(`INSERT INTO outbound_calls (from_id, file, line, client, http_method, service_name, url, col) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer outboundStmt.Close()
	for _, o := range result.OutboundCalls {
		if _, err := outboundStmt.ExecContext(ctx, o.From, o.File, o.Line, o.Client, o.HTTPMethod, o.ServiceName, o.URL, o.Col); err != nil {
			return fmt.Errorf("codegraph: insert outbound call %q: %w", o.URL, err)
		}
	}

	redisStmt, err := insert(`INSERT INTO redis_accesses (from_id, key, access, command, file, line, col) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer redisStmt.Close()
	for _, a := range result.RedisAccesses {
		if _, err := redisStmt.ExecContext(ctx, a.From, a.Key, a.Access, a.Command, a.File, a.Line, a.Col); err != nil {
			return fmt.Errorf("codegraph: insert redis access %q: %w", a.Key, err)
		}
	}

	kafkaStmt, err := insert(`INSERT INTO kafka_accesses (from_id, topic, role, client, group_id, is_pattern, file, line, col) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer kafkaStmt.Close()
	for _, a := range result.KafkaAccesses {
		pattern := 0
		if a.Pattern {
			pattern = 1
		}
		if _, err := kafkaStmt.ExecContext(ctx, a.From, a.Topic, a.Role, a.Client, a.GroupID, pattern, a.File, a.Line, a.Col); err != nil {
			return fmt.Errorf("codegraph: insert kafka access %q: %w", a.Topic, err)
		}
	}

	support, err := json.Marshal(entrySupport{Support: result.EntryPointSupport, Warnings: result.EntryPointWarnings})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO index_meta(key, value) VALUES ('entry_support', ?)`, string(support)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO index_meta(key, value) VALUES ('version', '1')
        ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`); err != nil {
		return fmt.Errorf("codegraph: increment index version: %w", err)
	}
	// When, for the sidebar's "Indexed 2m ago": written with the revision so
	// the two can never disagree.
	if _, err := tx.ExecContext(ctx, `INSERT INTO index_meta(key, value) VALUES ('indexed_at', ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return fmt.Errorf("codegraph: stamp index time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit replace: %w", err)
	}
	s.statsMu.Lock()
	s.stats = nil
	s.statsGen++
	s.statsMu.Unlock()
	return nil
}

// IndexedAt is when the stored index was last replaced, in Unix milliseconds;
// 0 for an index written before the stamp existed, or none at all.
func (s *sqliteStore) IndexedAt(ctx context.Context) (int64, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM index_meta WHERE key = 'indexed_at'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	at, _ := strconv.ParseInt(raw, 10, 64)
	return at, nil
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

// IndexedAnalyzer returns the identity of the analyzer that last wrote this
// index, or "" when unknown — an index written before the record existed, or
// by an analyzer that could not name itself.
func (s *sqliteStore) IndexedAnalyzer(ctx context.Context) (string, error) {
	var identity string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM index_meta WHERE key = ?`, indexedAnalyzerKey).Scan(&identity)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("codegraph: IndexedAnalyzer: %w", err)
	}
	return identity, nil
}

// SetIndexedAnalyzer records which analyzer wrote the index. Called after
// every successful write, open and re-index alike: a refresh by a newer
// analyzer rewrites the whole index, and the record must say so.
func (s *sqliteStore) SetIndexedAnalyzer(ctx context.Context, identity string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO index_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		indexedAnalyzerKey, identity)
	if err != nil {
		return fmt.Errorf("codegraph: SetIndexedAnalyzer: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListEntryPoints(ctx context.Context) ([]EntryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, verb, target, entry_node, kind, service, owner, grp, ord, route_id, topic, consumer_group FROM entry_points ORDER BY target`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListEntryPoints: %w", err)
	}
	defer rows.Close()

	var out []EntryPoint
	for rows.Next() {
		var ep EntryPoint
		if err := rows.Scan(&ep.ID, &ep.Verb, &ep.Target, &ep.EntryNode, &ep.Kind, &ep.Service, &ep.Owner, &ep.Group, &ep.Order, &ep.RouteID, &ep.Topic, &ep.ConsumerGroup); err != nil {
			return nil, fmt.Errorf("codegraph: scan entry point: %w", err)
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetEntryPoint(ctx context.Context, entryPointID string) (EntryPoint, error) {
	var ep EntryPoint
	err := s.db.QueryRowContext(ctx,
		`SELECT id, verb, target, entry_node, kind, service, owner, grp, ord, route_id, topic, consumer_group FROM entry_points WHERE id = ?`, entryPointID,
	).Scan(&ep.ID, &ep.Verb, &ep.Target, &ep.EntryNode, &ep.Kind, &ep.Service, &ep.Owner, &ep.Group, &ep.Order, &ep.RouteID, &ep.Topic, &ep.ConsumerGroup)
	if err == sql.ErrNoRows {
		return EntryPoint{}, ErrNodeNotFound
	}
	if err != nil {
		return EntryPoint{}, fmt.Errorf("codegraph: GetEntryPoint: %w", err)
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
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind, guard FROM edges WHERE from_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetOutEdges: %w", err)
	}
	defer rows.Close()

	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind, &e.Guard); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind, guard FROM edges WHERE to_id = ? ORDER BY from_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetInEdges: %w", err)
	}
	defer rows.Close()

	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind, &e.Guard); err != nil {
			return nil, fmt.Errorf("codegraph: scan edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// maxSymbolMatches caps SearchSymbol. A one-letter query matches most of a
// project; the picker shows a screenful, and the rest was serialised for
// nobody.
const maxSymbolMatches = 200

// escapeLike makes a user's query literal inside a LIKE pattern: "%" and "_"
// are wildcards there, so a search for "get_name" also matched "getXname",
// and "%" matched everything.
func escapeLike(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}

func (s *sqliteStore) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, layer, file_path, line FROM nodes WHERE label LIKE ? ESCAPE '\' ORDER BY label LIMIT ?`,
		"%"+escapeLike(query)+"%", maxSymbolMatches)
	if err != nil {
		return nil, fmt.Errorf("codegraph: SearchSymbol: %w", err)
	}
	defer rows.Close()

	var out []SymbolMatch
	for rows.Next() {
		var m SymbolMatch
		if err := rows.Scan(&m.NodeID, &m.Label, &m.Kind, &m.Layer, &m.FilePath, &m.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan symbol match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// symbolWhere is the WHERE clause of a SearchSymbols query without its
// cursor, and its arguments. LIKE is how the plain search matches, so the
// three modes stay case-insensitive the same way.
func symbolWhere(q SymbolQuery) (string, []any) {
	var conds []string
	var args []any
	esc := escapeLike(q.Query)
	switch q.Match {
	case MatchExact:
		conds = append(conds, `(label LIKE ? ESCAPE '\' OR label LIKE ? ESCAPE '\')`)
		args = append(args, esc, "%."+esc)
	case MatchPrefix:
		conds = append(conds, `(label LIKE ? ESCAPE '\' OR label LIKE ? ESCAPE '\')`)
		args = append(args, esc+"%", "%."+esc+"%")
	default:
		conds = append(conds, `label LIKE ? ESCAPE '\'`)
		args = append(args, "%"+esc+"%")
	}
	if q.Kind != "" {
		conds = append(conds, `kind = ?`)
		args = append(args, q.Kind)
	}
	if q.Layer != "" {
		conds = append(conds, `layer = ?`)
		args = append(args, q.Layer)
	}
	if q.PathPrefix != "" {
		conds = append(conds, `substr(file_path, 1, ?) = ?`)
		args = append(args, utf8.RuneCountInString(q.PathPrefix), q.PathPrefix)
	}
	return strings.Join(conds, " AND "), args
}

// SearchSymbols is SearchSymbol with filters and a keyset cursor over
// (label, id), so a page costs the same however deep it is.
func (s *sqliteStore) SearchSymbols(ctx context.Context, q SymbolQuery) (SymbolPage, error) {
	limit, err := q.normalize()
	if err != nil {
		return SymbolPage{}, err
	}
	binding := q.binding()
	cursor, err := decodePageCursor(q.Cursor, binding)
	if err != nil {
		return SymbolPage{}, err
	}
	where, args := symbolWhere(q)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE `+where, args...).Scan(&total); err != nil {
		return SymbolPage{}, fmt.Errorf("codegraph: SearchSymbols: %w", err)
	}

	statement := `SELECT id, label, kind, layer, file_path, line FROM nodes WHERE ` + where
	if cursor.After != "" {
		statement += ` AND (label > ? OR (label = ? AND id > ?))`
		args = append(args, cursor.Label, cursor.Label, cursor.After)
	}
	statement += ` ORDER BY label, id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return SymbolPage{}, fmt.Errorf("codegraph: SearchSymbols: %w", err)
	}
	defer rows.Close()

	page := SymbolPage{Result: []SymbolMatch{}, Total: total}
	for rows.Next() {
		var m SymbolMatch
		if err := rows.Scan(&m.NodeID, &m.Label, &m.Kind, &m.Layer, &m.FilePath, &m.Line); err != nil {
			return SymbolPage{}, fmt.Errorf("codegraph: scan symbol match: %w", err)
		}
		page.Result = append(page.Result, m)
	}
	if err := rows.Err(); err != nil {
		return SymbolPage{}, err
	}
	if len(page.Result) > limit {
		page.Result = page.Result[:limit]
		last := page.Result[limit-1]
		page.NextCursor = encodePageCursor(pageCursor{Binding: binding, After: last.NodeID, Label: last.Label})
	}
	return page, nil
}

// batchSize keeps an IN (...) list under SQLite's bound-parameter limit.
const batchSize = 500

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func stringArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// GetNodes fetches many nodes in a few queries, keyed by ID. IDs the index
// does not hold are simply absent from the map — a batch lookup is used for
// "whatever these edges reach", where a dangling reference is not an error.
func (s *sqliteStore) GetNodes(ctx context.Context, ids []string) (map[string]Node, error) {
	out := make(map[string]Node, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes WHERE id IN (`+placeholders(len(chunk))+`)`,
			stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: GetNodes: %w", err)
		}
		nodes, err := scanNodes(rows)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			out[n.ID] = n
		}
	}
	return out, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		var paramsJSON string
		if err := rows.Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine, &n.Summary, &paramsJSON); err != nil {
			return nil, fmt.Errorf("codegraph: scan node: %w", err)
		}
		if err := json.Unmarshal([]byte(paramsJSON), &n.Params); err != nil {
			return nil, fmt.Errorf("codegraph: unmarshal params for node %q: %w", n.ID, err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetAdjacentEdges returns every edge touching any of ids, in either
// direction, ordered so the same query always returns the same list. It is
// one level of GetNeighbors' walk in a few queries instead of two per node.
func (s *sqliteStore) GetAdjacentEdges(ctx context.Context, ids []string) ([]Edge, error) {
	var out []Edge
	seen := map[Edge]bool{}
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]
		in := placeholders(len(chunk))
		args := append(stringArgs(chunk), stringArgs(chunk)...)
		rows, err := s.db.QueryContext(ctx,
			`SELECT from_id, to_id, kind, guard FROM edges WHERE from_id IN (`+in+`)
			 UNION
			 SELECT from_id, to_id, kind, guard FROM edges WHERE to_id IN (`+in+`)
			 ORDER BY from_id, to_id, kind`, args...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: GetAdjacentEdges: %w", err)
		}
		for rows.Next() {
			var e Edge
			if err := rows.Scan(&e.From, &e.To, &e.Kind, &e.Guard); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan edge: %w", err)
			}
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// ListClassMethods returns the methods a class declares — method nodes whose
// ID is the class ID, a dot, and a name (plus a signature). Nested classes'
// methods are excluded: "a.Outer.Inner.run()" belongs to a.Outer.Inner.
func (s *sqliteStore) ListClassMethods(ctx context.Context, classID string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes
		 WHERE kind = 'method' AND id LIKE ? ESCAPE '\' ORDER BY id`,
		escapeLike(classID)+".%")
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListClassMethods: %w", err)
	}
	nodes, err := scanNodes(rows)
	if err != nil {
		return nil, err
	}
	out := nodes[:0]
	for _, n := range nodes {
		signature := strings.SplitN(n.ID, "(", 2)[0]
		if dot := strings.LastIndex(signature, "."); dot >= 0 && signature[:dot] == classID {
			out = append(out, n)
		}
	}
	return out, nil
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

// GetDependencies is GetDependents read the other way round: every file
// filePath itself names as a dependency, one hop, not transitive. Used to
// rank FindDefinitions' fallback-by-name candidates — a file filePath
// actually imports from is a better guess than an unrelated file that
// happens to declare a type with the same simple name.
func (s *sqliteStore) GetDependencies(ctx context.Context, filePath string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT depends_on_file_path FROM file_dependencies WHERE file_path = ? ORDER BY depends_on_file_path`,
		filePath)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetDependencies: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("codegraph: scan dependency: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFieldAccessesAt joins through nodes to filter by file: field_accesses
// itself carries no file_path, only the accessing node's ID (node_id), which
// may be a method OR — for a static field initializer's own call scope — the
// class itself. Either way that node has a file_path, so the join scopes the
// line correctly without the caller needing to know which kind of node
// recorded the access.
func (s *sqliteStore) GetFieldAccessesAt(ctx context.Context, filePath string, line int, fieldName string) ([]FieldAccess, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fa.class_id, fa.node_id, fa.field_name, fa.kind, fa.line
		  FROM field_accesses fa
		  JOIN nodes n ON fa.node_id = n.id
		 WHERE n.file_path = ? AND fa.line = ? AND fa.field_name = ?
		 ORDER BY fa.class_id`,
		filePath, line, fieldName)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetFieldAccessesAt: %w", err)
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

// GetCallSitesAt joins through nodes for the same reason GetFieldAccessesAt
// does: call_sites has no file_path of its own, only from_id, so the join is
// what lets a caller ask "what is called from this exact line of this exact
// file" without first looking up which node encloses the line. Not deduped —
// FindDefinitions' call-site tier wants every call expression on the line,
// including two calls to the same-named method.
func (s *sqliteStore) GetCallSitesAt(ctx context.Context, filePath string, line int) ([]CallSite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cs.from_id, cs.to_id, cs.line, cs.unresolved
		  FROM call_sites cs
		  JOIN nodes n ON cs.from_id = n.id
		 WHERE n.file_path = ? AND cs.line = ?
		 ORDER BY cs.to_id`,
		filePath, line)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetCallSitesAt: %w", err)
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

// GetCallSitesTo returns every call site whose to_id is toID — the reverse
// of GetCallSites (by caller) and GetCallSitesAt (by exact location).
// FindReferences' method tier needs exactly this direction ("who calls this
// method"), which neither of those answers: GetCallSitesAt needs a location
// under the cursor, GetCallSites needs the CALLER's own ID. idx_call_sites_to
// keeps this side of the join as cheap as idx_call_sites_from already made
// the from_id side.
func (s *sqliteStore) GetCallSitesTo(ctx context.Context, toID string) ([]CallSite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, line, unresolved, role FROM call_sites WHERE to_id = ? ORDER BY from_id, line`,
		toID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetCallSitesTo: %w", err)
	}
	defer rows.Close()

	var out []CallSite
	for rows.Next() {
		var c CallSite
		if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved, &c.Role); err != nil {
			return nil, fmt.Errorf("codegraph: scan call site: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetStepSites reads call sites with their step, batched like GetNodes. Order
// is source order within a caller; column breaks a tie on one line.
func (s *sqliteStore) GetStepSites(ctx context.Context, fromIDs []string) ([]CallSite, error) {
	var out []CallSite
	for start := 0; start < len(fromIDs); start += batchSize {
		chunk := fromIDs[start:min(start+batchSize, len(fromIDs))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT from_id, to_id, line, unresolved, col, text, callee_key, receiver, result_use, frames, role, target
			   FROM call_sites WHERE from_id IN (`+placeholders(len(chunk))+`)
			  ORDER BY from_id, line, col, to_id`, stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: GetStepSites: %w", err)
		}
		for rows.Next() {
			var c CallSite
			var frames string
			if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved, &c.Col, &c.Text, &c.CalleeKey,
				&c.Receiver, &c.ResultUse, &frames, &c.Role, &c.Target); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan step site: %w", err)
			}
			if frames != "" {
				// A row that cannot be read keeps its call, without frames:
				// one bad row must not take the method's outline with it.
				if err := json.Unmarshal([]byte(frames), &c.Frames); err != nil {
					c.Frames = nil
				}
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("codegraph: read rows: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// CallStats counts callers per callee key. Dispatch links (a role) are
// not calls anyone wrote, and a site with no key says nothing about a callee.
// The answer is cached until the next ReplaceIndex; callers must not change
// the map it returns.
func (s *sqliteStore) CallStats(ctx context.Context) (CallStats, error) {
	s.statsMu.Lock()
	cached, gen := s.stats, s.statsGen
	s.statsMu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	stats := CallStats{Callers: map[string]int{}}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE kind = 'method'`).Scan(&stats.Methods); err != nil {
		return CallStats{}, fmt.Errorf("codegraph: count methods: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT callee_key, COUNT(DISTINCT from_id) FROM call_sites WHERE callee_key != '' AND role = '' GROUP BY callee_key`)
	if err != nil {
		return CallStats{}, fmt.Errorf("codegraph: CallStats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return CallStats{}, fmt.Errorf("codegraph: scan call stats: %w", err)
		}
		stats.Callers[key] = n
	}
	if err := rows.Err(); err != nil {
		return CallStats{}, fmt.Errorf("codegraph: read call stats: %w", err)
	}
	s.statsMu.Lock()
	if s.statsGen == gen {
		s.stats = &stats
	}
	s.statsMu.Unlock()
	return stats, nil
}

// ListEffectsFrom gathers the four extractor tables into one shape, keyed by
// the line each access was seen at — the line GetSteps matches a call on.
func (s *sqliteStore) ListEffectsFrom(ctx context.Context, fromIDs []string) ([]StepEffectAt, error) {
	var out []StepEffectAt
	for start := 0; start < len(fromIDs); start += batchSize {
		chunk := fromIDs[start:min(start+batchSize, len(fromIDs))]
		in := placeholders(len(chunk))
		args := make([]any, 0, 4*len(chunk))
		for range 4 {
			args = append(args, stringArgs(chunk)...)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT from_id, line, col, 'kafka', role, topic FROM kafka_accesses WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, col, 'http', 'call', TRIM(http_method || ' ' || url) FROM outbound_calls WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, col, 'redis', access, key FROM redis_accesses WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, col, 'db', access, CASE WHEN schema_name = '' THEN table_name ELSE schema_name || '.' || table_name END
			  FROM table_accesses WHERE from_id IN (`+in+`)
			ORDER BY 1, 2, 3, 4, 5`, args...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: ListEffectsFrom: %w", err)
		}
		for rows.Next() {
			var e StepEffectAt
			if err := rows.Scan(&e.From, &e.Line, &e.Col, &e.Effect.Kind, &e.Effect.Role, &e.Effect.Target); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan effect: %w", err)
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("codegraph: read rows: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// FieldWriters reads field_accesses' writes; a method absent from the map
// writes no field.
func (s *sqliteStore) FieldWriters(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT DISTINCT node_id FROM field_accesses WHERE kind = 'write' AND node_id IN (`+placeholders(len(chunk))+`)`,
			stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: FieldWriters: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan field writer: %w", err)
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("codegraph: read rows: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// OverridesOf reads only the dispatch links of the given methods: what
// GetSteps needs of a callee it did not load.
func (s *sqliteStore) OverridesOf(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT from_id, to_id FROM call_sites WHERE role = 'override' AND from_id IN (`+placeholders(len(chunk))+`)
			  ORDER BY from_id, to_id`, stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: OverridesOf: %w", err)
		}
		for rows.Next() {
			var from, to string
			if err := rows.Scan(&from, &to); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan override: %w", err)
			}
			out[from] = append(out[from], to)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("codegraph: read overrides: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// GetCallSites returns every call expression in one method, in source order.
// Repeats are preserved: a method that calls the same target twice has two
// rows, and losing one would put the trace's events in the wrong order.
func (s *sqliteStore) GetCallSites(ctx context.Context, fromID string) ([]CallSite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, line, unresolved, role FROM call_sites WHERE from_id = ? ORDER BY line`,
		fromID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetCallSites: %w", err)
	}
	defer rows.Close()

	var out []CallSite
	for rows.Next() {
		var c CallSite
		if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved, &c.Role); err != nil {
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

// ListNodesInFile returns every node declared in filePath, outermost first at
// each start line, so a class precedes the members it contains.
func (s *sqliteStore) ListNodesInFile(ctx context.Context, filePath string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes
		 WHERE file_path = ? AND line > 0
		 ORDER BY line ASC, end_line DESC, id ASC`, filePath)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListNodesInFile: %w", err)
	}
	defer rows.Close()
	var nodes []Node
	for rows.Next() {
		var n Node
		var paramsJSON string
		if err := rows.Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine, &n.Summary, &paramsJSON); err != nil {
			return nil, fmt.Errorf("codegraph: scan node: %w", err)
		}
		if err := json.Unmarshal([]byte(paramsJSON), &n.Params); err != nil {
			return nil, fmt.Errorf("codegraph: unmarshal params for node %q: %w", n.ID, err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// FindNodesByName is a candidate filter, not the authority on the match: a
// label is "typeId.methodName" for a method or the bare qualified name for a
// class (see ProjectAnalyzer's node-id doc comment), so "label = identifier OR
// label LIKE '%.identifier'" already excludes an unrelated method like
// "listAll" when identifier is "list". It is still only a pre-filter because
// SQLite's LIKE is case-insensitive for ASCII regardless of column collation,
// while FindDefinitions' contract is case-sensitive — callers must re-check
// every row against matchesIdentifier, the single authoritative definition of
// "does this label name that identifier", before trusting a match.
func (s *sqliteStore) FindNodesByName(ctx context.Context, identifier string) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes
		 WHERE label = ? OR label LIKE ? ESCAPE '\'
		 ORDER BY id`,
		identifier, "%."+escapeLike(identifier))
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindNodesByName: %w", err)
	}
	return scanNodes(rows)
}

// maxConfigRows caps one config lookup. A prefix like "spring" matches
// thousands of keys; the answer is a place to start reading, not the file.
const maxConfigRows = 2000

func (s *sqliteStore) FindConfigKeys(ctx context.Context, canon string, prefix bool) ([]ConfigKey, error) {
	var where string
	var args []any
	switch {
	case canon == "":
		where = "1 = 1"
	case prefix:
		where = `canon LIKE ? OR env_canon LIKE ?`
		args = []any{canon + "%", "% " + canon + "%"}
	default:
		where = `canon = ? OR env_canon LIKE ?`
		args = []any{canon, "% " + canon + " %"}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, file, line, value, redacted, profile, env_vars FROM config_keys WHERE `+where+` ORDER BY file, line LIMIT ?`,
		append(args, maxConfigRows)...)
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindConfigKeys: %w", err)
	}
	defer rows.Close()
	var out []ConfigKey
	for rows.Next() {
		var k ConfigKey
		var redacted int
		var envJSON string
		if err := rows.Scan(&k.Key, &k.File, &k.Line, &k.Value, &redacted, &k.Profile, &envJSON); err != nil {
			return nil, fmt.Errorf("codegraph: scan config key: %w", err)
		}
		k.Redacted = redacted == 1
		if err := json.Unmarshal([]byte(envJSON), &k.EnvVars); err != nil {
			return nil, fmt.Errorf("codegraph: unmarshal env vars of %q: %w", k.Key, err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *sqliteStore) FindConfigUsages(ctx context.Context, canons []string, prefix bool, kinds []string) ([]ConfigUsage, error) {
	var conds []string
	var args []any
	if len(canons) > 0 {
		var alternatives []string
		for _, c := range canons {
			if prefix {
				alternatives = append(alternatives, "canon LIKE ?")
				args = append(args, c+"%")
			} else {
				alternatives = append(alternatives, "canon = ?")
				args = append(args, c)
			}
		}
		conds = append(conds, "("+strings.Join(alternatives, " OR ")+")")
	}
	if len(kinds) > 0 {
		conds = append(conds, "kind IN ("+placeholders(len(kinds))+")")
		args = append(args, stringArgs(kinds)...)
	}
	where := "1 = 1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, kind, from_id, field, file, line, default_value, detail FROM config_usages WHERE `+where+` ORDER BY kind, key, from_id, line LIMIT ?`,
		append(args, maxConfigRows)...)
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindConfigUsages: %w", err)
	}
	defer rows.Close()
	var out []ConfigUsage
	for rows.Next() {
		var u ConfigUsage
		if err := rows.Scan(&u.Key, &u.Kind, &u.From, &u.Field, &u.File, &u.Line, &u.DefaultValue, &u.Detail); err != nil {
			return nil, fmt.Errorf("codegraph: scan config use: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *sqliteStore) FindEdgesByGuard(ctx context.Context, terms []string, fromPrefix string) ([]Edge, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	var alternatives []string
	var args []any
	for _, t := range terms {
		alternatives = append(alternatives, `guard LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(t)+"%")
	}
	where := "(" + strings.Join(alternatives, " OR ") + ")"
	if fromPrefix != "" {
		where += ` AND from_id LIKE ? ESCAPE '\'`
		args = append(args, escapeLike(fromPrefix)+"%")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, kind, guard FROM edges WHERE guard <> '' AND `+where+` ORDER BY from_id, to_id LIMIT ?`,
		append(args, maxConfigRows)...)
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindEdgesByGuard: %w", err)
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind, &e.Guard); err != nil {
			return nil, fmt.Errorf("codegraph: scan edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) FindEntityTables(ctx context.Context, canon string, prefix bool) ([]EntityTable, error) {
	op, arg := "=", canon
	if prefix {
		op, arg = "LIKE", canon+"%"
	}
	// Every row of a matching entity — its join tables are tables of it too —
	// so match on the entity first, then take all of its rows.
	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_id, entity_name, table_name, schema_name, kind, file, line FROM entity_tables
		 WHERE entity_id IN (SELECT entity_id FROM entity_tables WHERE canon_table `+op+` ? OR canon_entity `+op+` ? OR canon_class `+op+` ?)
		    OR canon_table `+op+` ?
		 ORDER BY entity_id, kind, table_name LIMIT ?`,
		arg, arg, arg, arg, maxConfigRows)
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindEntityTables: %w", err)
	}
	defer rows.Close()
	var out []EntityTable
	for rows.Next() {
		var e EntityTable
		if err := rows.Scan(&e.Entity, &e.EntityName, &e.Table, &e.Schema, &e.Kind, &e.File, &e.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan entity table: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) FindTableAccesses(ctx context.Context, canons []string, prefix bool, access string) ([]TableAccess, error) {
	if len(canons) == 0 {
		return nil, nil
	}
	var alternatives []string
	var args []any
	for _, c := range canons {
		if prefix {
			alternatives = append(alternatives, "canon LIKE ?")
			args = append(args, c+"%")
		} else {
			alternatives = append(alternatives, "canon = ?")
			args = append(args, c)
		}
	}
	where := "(" + strings.Join(alternatives, " OR ") + ")"
	if access != "" {
		where += " AND access = ?"
		args = append(args, access)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, table_name, schema_name, access, via, detail, file, line FROM table_accesses WHERE `+where+
			` ORDER BY table_name, access DESC, from_id, line LIMIT ?`,
		append(args, maxConfigRows)...)
	if err != nil {
		return nil, fmt.Errorf("codegraph: FindTableAccesses: %w", err)
	}
	defer rows.Close()
	var out []TableAccess
	for rows.Next() {
		var a TableAccess
		if err := rows.Scan(&a.From, &a.Table, &a.Schema, &a.Access, &a.Via, &a.Detail, &a.File, &a.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan table access: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListOutboundCalls(ctx context.Context) ([]OutboundCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, file, line, client, http_method, service_name, url FROM outbound_calls ORDER BY file, line, url`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListOutboundCalls: %w", err)
	}
	defer rows.Close()
	var out []OutboundCall
	for rows.Next() {
		var o OutboundCall
		if err := rows.Scan(&o.From, &o.File, &o.Line, &o.Client, &o.HTTPMethod, &o.ServiceName, &o.URL); err != nil {
			return nil, fmt.Errorf("codegraph: scan outbound call: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListRedisAccesses(ctx context.Context) ([]RedisAccess, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, key, access, command, file, line FROM redis_accesses ORDER BY key, file, line`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListRedisAccesses: %w", err)
	}
	defer rows.Close()
	var out []RedisAccess
	for rows.Next() {
		var a RedisAccess
		if err := rows.Scan(&a.From, &a.Key, &a.Access, &a.Command, &a.File, &a.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan redis access: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListKafkaAccesses(ctx context.Context) ([]KafkaAccess, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, topic, role, client, group_id, is_pattern, file, line FROM kafka_accesses ORDER BY topic, role, file, line`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListKafkaAccesses: %w", err)
	}
	defer rows.Close()
	var out []KafkaAccess
	for rows.Next() {
		var a KafkaAccess
		var pattern int
		if err := rows.Scan(&a.From, &a.Topic, &a.Role, &a.Client, &a.GroupID, &pattern, &a.File, &a.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan kafka access: %w", err)
		}
		a.Pattern = pattern == 1
		out = append(out, a)
	}
	return out, rows.Err()
}
