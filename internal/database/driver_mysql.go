package database

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// mysqlDriver serves both MySQL and MariaDB: same wire protocol, same driver.
// The engine is kept so behaviour differences stay nameable rather than assumed
// away — v1 only claims what has been exercised: connect, query, schema browse.
type mysqlDriver struct{ engine string }

var tlsConfigSeq atomic.Uint64

func (d mysqlDriver) Dial(ctx context.Context, cfg ConnConfig) (Conn, error) {
	dsn, tlsName, err := mysqlDSN(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		releaseMySQLTLS(tlsName)
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &mysqlConn{db: db, tlsName: tlsName, engine: d.engine}, nil
}

// mysqlDSN builds the connection string and registers a TLS config when one is
// needed. Split out from Dial so the parameters can be asserted without a
// server.
func mysqlDSN(cfg ConnConfig) (dsn string, tlsName string, err error) {
	params := url.Values{}
	// Times come back as strings like every other value; the alternative is
	// driver-specific time handling that the shared Rows shape cannot express.
	params.Set("parseTime", "false")

	tlsName, err = registerMySQLTLS(cfg)
	if err != nil {
		return "", "", err
	}
	if tlsName != "" {
		params.Set("tls", tlsName)
	}
	if cfg.ReadOnly {
		// go-sql-driver treats unknown parameters as session system variables,
		// so this issues SET transaction_read_only=1 on every new connection —
		// the same per-connection guarantee pgx gets from AfterConnect.
		params.Set("transaction_read_only", "1")
	}
	for key, value := range cfg.Options {
		params.Set(key, value)
	}

	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s",
		cfg.Username, cfg.Password,
		net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
		cfg.Database, params.Encode()), tlsName, nil
}

// registerMySQLTLS maps sslmode onto the driver's TLS handling. Unlike pgx,
// go-sql-driver has no system-CA mode with a hostname override, so a verified
// connection through a tunnel needs a registered config.
func registerMySQLTLS(cfg ConnConfig) (string, error) {
	switch cfg.SSLMode {
	case "", "disable":
		return "", nil
	case "require", "prefer":
		if cfg.RootCAPEM != "" {
			// A bundle was supplied but this mode never checks one. Saying so
			// beats connecting unverified while the profile claims otherwise.
			return "", ErrCACertificateWithoutTLS
		}
		return "skip-verify", nil
	default:
		if cfg.TLSServerName == "" && cfg.RootCAPEM == "" {
			return "true", nil
		}
		tlsConfig := &tls.Config{
			ServerName: cfg.TLSServerName,
			MinVersion: tls.VersionTLS12,
		}
		if cfg.RootCAPEM != "" {
			pool, err := rootCAPool(cfg.RootCAPEM)
			if err != nil {
				return "", err
			}
			tlsConfig.RootCAs = pool
		}
		name := fmt.Sprintf("devkit-%d", tlsConfigSeq.Add(1))
		if err := mysqldriver.RegisterTLSConfig(name, tlsConfig); err != nil {
			return "", err
		}
		return name, nil
	}
}

func releaseMySQLTLS(name string) {
	switch name {
	case "", "true", "skip-verify":
		return
	default:
		mysqldriver.DeregisterTLSConfig(name)
	}
}

type mysqlConn struct {
	db      *sql.DB
	tlsName string
	// Which of the two engines this is. They share every Conn method, and
	// diverge only at ANALYZE — see inspector_mysql_plan.go.
	engine string
}

func (c *mysqlConn) Ping(ctx context.Context) error { return c.db.PingContext(ctx) }

func (c *mysqlConn) Close() {
	_ = c.db.Close()
	releaseMySQLTLS(c.tlsName)
}

func (c *mysqlConn) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	return &mysqlRows{rows: rows, columns: columns}, nil
}

// Begin pins one of the pool's four connections for the transaction's life.
//
// db.Conn takes a connection OUT of the pool and hands it back only on Close,
// which is what makes every statement inside the transaction land on the same
// session. Without it BEGIN would be a statement like any other, run wherever
// the pool felt like.
func (c *mysqlConn) Begin(ctx context.Context) (Tx, error) {
	pinned, err := c.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := pinned.BeginTx(ctx, nil)
	if err != nil {
		_ = pinned.Close()
		return nil, err
	}
	return &mysqlTx{pinned: pinned, tx: tx}, nil
}

type mysqlTx struct {
	pinned *sql.Conn
	tx     *sql.Tx
}

func (t *mysqlTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	return &mysqlRows{rows: rows, columns: columns}, nil
}

func (t *mysqlTx) Commit(ctx context.Context) error {
	// The pinned connection goes back to the pool either way, or four commits
	// would exhaust it.
	defer t.pinned.Close()
	_ = ctx
	return t.tx.Commit()
}

func (t *mysqlTx) Rollback(ctx context.Context) error {
	defer t.pinned.Close()
	_ = ctx
	return t.tx.Rollback()
}

// quoteMySQLIdentifier wraps a name in backticks, doubling any it contains.
//
// Every name here comes from the catalogue the server itself reported, so this
// is not guarding against a hostile user — it is guarding against ordinary
// names the grammar cannot take bare: a reserved word, a space, a hyphen. The
// doubling rule also means a name containing a backtick cannot close the
// identifier early, which is the one case where an ordinary name would
// otherwise become a second statement.
func quoteMySQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// TableDDL asks the server for the statement it would use to recreate the
// table.
//
// SHOW CREATE TABLE, not a reconstruction: MySQL keeps the definition and
// gives it back whole, with the engine, the charset, the collation, the
// AUTO_INCREMENT and every constraint exactly as the server holds them. Any
// version assembled here from information_schema would be a second opinion
// about the same table, and the one people paste into a migration.
//
// The identifier is quoted rather than bound: SHOW CREATE TABLE takes a name,
// not a parameter, and quoteMySQLIdentifier is what makes a name with a
// backtick or a reserved word safe.
func (c *mysqlConn) TableDDL(ctx context.Context, schema, table string) (string, error) {
	target := quoteMySQLIdentifier(schema) + "." + quoteMySQLIdentifier(table)
	rows, err := c.db.QueryContext(ctx, "SHOW CREATE TABLE "+target)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no definition for %s", target)
	}
	// Two columns for a table (Table, Create Table) and four for a view
	// (View, Create View, character_set_client, collation_connection). Scanning
	// by position into the second either way, because the column NAMES differ
	// between the two and the statement is always the second.
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	cells := make([]any, len(columns))
	values := make([]sql.NullString, len(columns))
	for i := range cells {
		cells[i] = &values[i]
	}
	if err := rows.Scan(cells...); err != nil {
		return "", err
	}
	if len(values) < 2 {
		return "", fmt.Errorf("unexpected SHOW CREATE TABLE shape for %s", target)
	}
	return values[1].String, rows.Err()
}

func (c *mysqlConn) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('mysql','information_schema','performance_schema','sys')
		ORDER BY schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (c *mysqlConn) ListTables(ctx context.Context, schema string) ([]TableInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_schema, table_name, lower(table_type)
		FROM information_schema.tables
		WHERE table_schema = ?
		ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableInfo
	for rows.Next() {
		var info TableInfo
		if err := rows.Scan(&info.Schema, &info.Name, &info.Kind); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

func (c *mysqlConn) ListColumns(ctx context.Context, schema, table string) ([]ColumnInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable = 'YES', ordinal_position
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnInfo
	for rows.Next() {
		var info ColumnInfo
		if err := rows.Scan(&info.Name, &info.DataType, &info.Nullable, &info.Position); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

type mysqlRows struct {
	rows    *sql.Rows
	columns []string
}

func (r *mysqlRows) Columns() []string { return r.columns }
func (r *mysqlRows) Next() bool        { return r.rows.Next() }
func (r *mysqlRows) Err() error        { return r.rows.Err() }
func (r *mysqlRows) Close()            { _ = r.rows.Close() }

func (r *mysqlRows) Values() ([]string, error) {
	raw := make([]any, len(r.columns))
	pointers := make([]any, len(r.columns))
	for i := range raw {
		pointers[i] = &raw[i]
	}
	if err := r.rows.Scan(pointers...); err != nil {
		return nil, err
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i] = stringify(v)
	}
	return out, nil
}

func (c *mysqlConn) ListForeignKeys(ctx context.Context, schema string) ([]ForeignKey, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_schema, table_name, column_name,
		       referenced_table_schema, referenced_table_name, referenced_column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = ? AND referenced_table_name IS NOT NULL
		ORDER BY table_name, ordinal_position`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForeignKey
	for rows.Next() {
		var fk ForeignKey
		if err := rows.Scan(&fk.Schema, &fk.Table, &fk.Column,
			&fk.ReferencedSchema, &fk.ReferencedTable, &fk.ReferencedColumn); err != nil {
			return nil, err
		}
		out = append(out, fk)
	}
	return out, rows.Err()
}

// ListIndexes reports the schema's indexes.
//
// A functional index leaves COLUMN_NAME null in STATISTICS, and an index read
// as covering fewer columns than it does would claim a uniqueness the database
// never promised — so a null anywhere drops the whole index rather than part of
// it. Prefix indexes (SUB_PART) are kept: uniqueness over a prefix implies
// uniqueness over the full value, so the conclusion still holds.
func (c *mysqlConn) ListIndexes(ctx context.Context, schema string) ([]IndexInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_schema, table_name, index_name, non_unique, column_name
		FROM information_schema.statistics
		WHERE table_schema = ?
		ORDER BY table_name, index_name, seq_in_index`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IndexInfo
	dropped := map[string]bool{}
	for rows.Next() {
		var row IndexInfo
		var nonUnique int
		var column sql.NullString
		if err := rows.Scan(&row.Schema, &row.Table, &row.Name, &nonUnique, &column); err != nil {
			return nil, err
		}
		row.Unique = nonUnique == 0
		row.Primary = row.Name == "PRIMARY"

		key := row.Table + "\x00" + row.Name
		if !column.Valid {
			dropped[key] = true
			continue
		}
		if dropped[key] {
			continue
		}
		out = appendIndexColumn(out, row, column.String)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dropIndexes(out, dropped), nil
}
