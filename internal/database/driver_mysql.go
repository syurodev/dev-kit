package database

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/url"
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
	return &mysqlConn{db: db, tlsName: tlsName}, nil
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
		return "skip-verify", nil
	default:
		if cfg.TLSServerName == "" {
			return "true", nil
		}
		name := fmt.Sprintf("devkit-%d", tlsConfigSeq.Add(1))
		if err := mysqldriver.RegisterTLSConfig(name, &tls.Config{
			ServerName: cfg.TLSServerName,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
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
