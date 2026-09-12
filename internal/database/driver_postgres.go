package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresDriver struct{}

func (postgresDriver) Dial(ctx context.Context, cfg ConnConfig) (Conn, error) {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
		Path:   cfg.Database,
	}
	query := u.Query()
	query.Set("sslmode", cfg.SSLMode)
	for key, value := range cfg.Options {
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()

	poolConfig, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = 4
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = 30 * time.Minute

	// Through a tunnel the DSN points at loopback while the certificate is
	// still issued for the real host. Pin the name instead of weakening TLS.
	if cfg.TLSServerName != "" && poolConfig.ConnConfig.TLSConfig != nil {
		poolConfig.ConnConfig.TLSConfig.ServerName = cfg.TLSServerName
	}
	// Set on the parsed config rather than through a sslrootcert DSN parameter:
	// that one names a file, and the bundle lives in the profile so it travels
	// with it. A bundle supplied under an sslmode that builds no TLSConfig
	// (disable) is a contradiction worth reporting rather than dropping.
	if cfg.RootCAPEM != "" {
		if poolConfig.ConnConfig.TLSConfig == nil {
			return nil, ErrCACertificateWithoutTLS
		}
		pool, err := rootCAPool(cfg.RootCAPEM)
		if err != nil {
			return nil, err
		}
		poolConfig.ConnConfig.TLSConfig.RootCAs = pool
	}
	if cfg.ReadOnly {
		// Runs for every connection the pool opens, which is why toggling
		// read_only has to drop the pool rather than only rewrite metadata.
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET default_transaction_read_only = on")
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	return &postgresConn{pool: pool}, nil
}

type postgresConn struct{ pool *pgxpool.Pool }

func (c *postgresConn) Ping(ctx context.Context) error { return c.pool.Ping(ctx) }
func (c *postgresConn) Close()                         { c.pool.Close() }

func (c *postgresConn) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}
	return &postgresRows{rows: rows, columns: columns}, nil
}

// Begin takes a connection out of the pool and keeps it for the transaction.
//
// pgx's pool.Begin does the acquiring itself and gives the connection back on
// Commit or Rollback — the same pinning mysqlConn.Begin has to arrange by
// hand, for the same reason: a bare BEGIN would open a transaction on whatever
// connection served it and leave the next statement somewhere else.
func (c *postgresConn) Begin(ctx context.Context) (Tx, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &postgresTx{tx: tx}, nil
}

type postgresTx struct{ tx pgx.Tx }

func (t *postgresTx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}
	return &postgresRows{rows: rows, columns: columns}, nil
}

func (t *postgresTx) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *postgresTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

func (c *postgresConn) ListSchemas(ctx context.Context) ([]string, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT nspname FROM pg_namespace
		WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
		ORDER BY nspname`)
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

func (c *postgresConn) ListTables(ctx context.Context, schema string) ([]TableInfo, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT table_schema, table_name, lower(table_type)
		FROM information_schema.tables
		WHERE table_schema = $1
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

func (c *postgresConn) ListColumns(ctx context.Context, schema, table string) ([]ColumnInfo, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable = 'YES', ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
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

type postgresRows struct {
	rows    pgx.Rows
	columns []string
}

func (r *postgresRows) Columns() []string { return r.columns }
func (r *postgresRows) Next() bool        { return r.rows.Next() }
func (r *postgresRows) Err() error        { return r.rows.Err() }
func (r *postgresRows) Close()            { r.rows.Close() }

func (r *postgresRows) Values() ([]string, error) {
	values, err := r.rows.Values()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = stringify(v)
	}
	return out, nil
}

func (c *postgresConn) ListForeignKeys(ctx context.Context, schema string) ([]ForeignKey, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT kcu.table_schema, kcu.table_name, kcu.column_name,
		       ccu.table_schema, ccu.table_name, ccu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name
		 AND kcu.constraint_schema = tc.constraint_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_name = tc.constraint_name
		 AND ccu.constraint_schema = tc.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1
		ORDER BY kcu.table_name, kcu.ordinal_position`, schema)
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

// ListIndexes reports only indexes that can be reasoned about safely.
//
// Two kinds are excluded deliberately. An expression index (indkey holds a 0)
// would come back with fewer columns than it has, and a unique index read as
// covering fewer columns than it does claims a uniqueness the database never
// promised. A partial index is unique only over its predicate, so it says
// nothing about the table as a whole. Both would turn into a one-to-one edge
// that is simply wrong.
func (c *postgresConn) ListIndexes(ctx context.Context, schema string) ([]IndexInfo, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT n.nspname, t.relname, i.relname, ix.indisunique, ix.indisprimary,
		       a.attname
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = $1
		  AND t.relkind IN ('r', 'p')
		  AND ix.indpred IS NULL
		  AND 0 <> ALL (ix.indkey)
		ORDER BY t.relname, i.relname, k.ord`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IndexInfo
	for rows.Next() {
		var row IndexInfo
		var column string
		if err := rows.Scan(&row.Schema, &row.Table, &row.Name,
			&row.Unique, &row.Primary, &column); err != nil {
			return nil, err
		}
		out = appendIndexColumn(out, row, column)
	}
	return out, rows.Err()
}
