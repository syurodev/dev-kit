package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// textResults asks the server for every result column in text format.
//
// Rows hands back strings, and the text format IS Postgres's own string for a
// value — the one psql prints and the one it parses back. Decoding binary into
// Go values and printing those with %v gave `[18 0 0 …]` for a uuid and
// `{12345 -2 false finite true}` for a numeric: garbage on screen, and a
// garbage literal in every cursor, edit and filter built from the cell (#37).
// Still the extended protocol, so one statement per call as before.
var textResults = pgx.QueryResultFormats{pgx.TextFormatCode}

func (c *postgresConn) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	rows, err := c.pool.Query(ctx, sql, append([]any{textResults}, args...)...)
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
	rows, err := t.tx.Query(ctx, sql, append([]any{textResults}, args...)...)
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

/*
TableDDL rebuilds a table's definition from the catalogues.

Rebuilt, because Postgres does not keep the statement that made the table —
unlike MySQL, which hands back its own SHOW CREATE TABLE. Three reads: the
columns, the constraints, and the indexes that are not already a constraint's.
The parts that are genuinely hard — a check expression, a foreign key's ON
DELETE, a partial index's WHERE — are not reassembled here at all:
pg_get_constraintdef and pg_get_indexdef are the server's own printers, and
anything this file wrote instead would be a second opinion about the same
object.

Constraints are folded INTO the CREATE TABLE rather than trailed after it as
ALTERs the way pg_dump does. pg_dump is ordering a restore; this is answering
"what is this table", and one block reads as one table.

Identifiers are quoted by format('%I.%I', ...) on the server, so a schema or a
table called "order" or holding a double quote is the server's problem to spell
and not this file's to get wrong.
*/
func (c *postgresConn) TableDDL(ctx context.Context, schema, table string) (string, error) {
	// The quoted name, and the same expression cast to the catalogue's own
	// identifier type. The cast is not optional: attrelid, conrelid and indrelid
	// are oids, and comparing one with text is an error rather than a coercion.
	const quoted = `format('%I.%I', $1::text, $2::text)`
	const target = quoted + `::regclass`

	var name string
	if err := c.pool.QueryRow(ctx, `SELECT `+quoted, schema, table).Scan(&name); err != nil {
		return "", err
	}

	type column struct {
		name, dataType, deflt string
		notNull               bool
		identity, generated   string
	}
	rows, err := c.pool.Query(ctx, `
		SELECT quote_ident(a.attname), format_type(a.atttypid, a.atttypmod), a.attnotnull,
		       coalesce(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity, a.attgenerated
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = `+target+`
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, schema, table)
	if err != nil {
		return "", err
	}
	var columns []column
	for rows.Next() {
		var col column
		if err := rows.Scan(&col.name, &col.dataType, &col.notNull, &col.deflt,
			&col.identity, &col.generated); err != nil {
			rows.Close()
			return "", err
		}
		columns = append(columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("no table %s", name)
	}

	// PRIMARY KEY, then UNIQUE, then FOREIGN KEY, then CHECK: the order someone
	// reads them in, rather than the order the catalogue happens to hold them.
	constraintRows, err := c.pool.Query(ctx, `
		SELECT quote_ident(conname), pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = `+target+`
		ORDER BY CASE contype WHEN 'p' THEN 0 WHEN 'u' THEN 1 WHEN 'f' THEN 2 ELSE 3 END, conname`,
		schema, table)
	if err != nil {
		return "", err
	}
	var constraints []string
	for constraintRows.Next() {
		var conname, def string
		if err := constraintRows.Scan(&conname, &def); err != nil {
			constraintRows.Close()
			return "", err
		}
		constraints = append(constraints, "CONSTRAINT "+conname+" "+def)
	}
	constraintRows.Close()
	if err := constraintRows.Err(); err != nil {
		return "", err
	}

	// Only the indexes that are not already a constraint's: a PRIMARY KEY and a
	// UNIQUE each own an index, and printing both would define them twice.
	indexRows, err := c.pool.Query(ctx, `
		SELECT pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		WHERE i.indrelid = `+target+`
		  AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)
		ORDER BY 1`, schema, table)
	if err != nil {
		return "", err
	}
	var indexes []string
	for indexRows.Next() {
		var def string
		if err := indexRows.Scan(&def); err != nil {
			indexRows.Close()
			return "", err
		}
		indexes = append(indexes, def+";")
	}
	indexRows.Close()
	if err := indexRows.Err(); err != nil {
		return "", err
	}

	parts := make([]string, 0, len(columns)+len(constraints))
	for _, col := range columns {
		line := col.name + " " + col.dataType
		switch {
		case col.generated == "s":
			// The expression lives in the default slot but is not a default:
			// emitting DEFAULT here would be a column that can be written to.
			line += " GENERATED ALWAYS AS (" + col.deflt + ") STORED"
		case col.identity == "a":
			line += " GENERATED ALWAYS AS IDENTITY"
		case col.identity == "d":
			line += " GENERATED BY DEFAULT AS IDENTITY"
		case col.deflt != "":
			line += " DEFAULT " + col.deflt
		}
		if col.notNull {
			line += " NOT NULL"
		}
		parts = append(parts, line)
	}
	parts = append(parts, constraints...)

	var out strings.Builder
	out.WriteString("CREATE TABLE " + name + " (\n")
	for i, part := range parts {
		out.WriteString("    " + part)
		if i < len(parts)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString(");")
	for _, index := range indexes {
		out.WriteString("\n\n" + index)
	}
	return out.String(), nil
}

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

// Values reads the text the server sent (see textResults) as it is. The raw
// buffers are reused on the next row, so each is copied into its string.
func (r *postgresRows) Values() ([]string, error) {
	raw := r.rows.RawValues()
	fields := r.rows.FieldDescriptions()
	out := make([]string, len(raw))
	for i, v := range raw {
		switch {
		case v == nil:
			out[i] = stringify(nil)
		// The one type whose text form is terser than people read it: t/f. Both
		// spellings parse back as a boolean literal.
		case fields[i].DataTypeOID == pgtype.BoolOID:
			out[i] = strconv.FormatBool(string(v) == "t")
		default:
			out[i] = string(v)
		}
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
