package database

import (
	"context"
	"crypto/x509"
	"fmt"
)

// Supported engines. MariaDB shares the MySQL wire protocol and driver, but is
// named separately so behaviour differences stay visible rather than assumed.
const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
	EngineMariaDB  = "mariadb"
)

// ConnConfig is everything a driver needs to dial. It is structured rather than
// a DSN string: each engine spells its DSN differently, and a tunnel has to
// change where we connect without changing what we verify.
type ConnConfig struct {
	Engine   string
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
	// RootCAPEM is a PEM bundle to verify the server certificate against,
	// instead of the machine's trust store. Managed services sign with their
	// own authority — an RDS certificate chains to an Amazon RDS CA that is
	// not installed on a typical desktop — so without this, verify-full is
	// unreachable for them and the only working choice is the one that skips
	// verification entirely. Empty means "use the system trust store".
	//
	// Carried as PEM rather than a path: a path only means something on the
	// machine that typed it, and profiles sync between machines.
	RootCAPEM string

	// TLSServerName overrides the name checked against the server certificate.
	// Set only when a tunnel points the connection at loopback while the
	// certificate is still issued for the profile's real host.
	TLSServerName string
	// ReadOnly asks the engine to reject writes on every connection it opens.
	ReadOnly bool
	// Options is the escape hatch for engine-specific parameters that do not
	// generalise. Drivers ignore keys they do not know.
	Options map[string]string
}

// TableInfo describes one table or view.
type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
}

// ForeignKey is one declared relationship. Four fields only: where it comes
// from and where it points.
type ForeignKey struct {
	Schema           string `json:"schema"`
	Table            string `json:"table"`
	Column           string `json:"column"`
	ReferencedSchema string `json:"referenced_schema"`
	ReferencedTable  string `json:"referenced_table"`
	ReferencedColumn string `json:"referenced_column"`
}

// ColumnInfo describes one column.
type ColumnInfo struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Position int    `json:"position"`
}

// Rows is a forward-only cursor of already-stringified values. Stringifying in
// the driver is what lets one result shape serve every engine; it is also why
// JSON export cannot recover original types.
type Rows interface {
	Columns() []string
	Next() bool
	// Values is the row as text, nil for SQL NULL (#47). A NULL used to come
	// back as the four letters, the same as a string that holds them.
	Values() ([]*string, error)
	Err() error
	Close()
	// RowsAffected is how many rows the statement changed, read once the rows
	// are closed; -1 when the driver cannot say.
	RowsAffected() int64
}

// Conn is the whole engine surface the service is allowed to use. Anything not
// every engine can do belongs in an optional interface a driver may also
// implement — SessionInspector and QueryPlanner — never here.
//
// ListForeignKeys is separate from ListColumns on purpose: the sidebar opens
// one table at a time and does not want relationships, while an ERD loads a
// whole schema at once and wants nothing else.
type Conn interface {
	Ping(ctx context.Context) error
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	ListSchemas(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context, schema string) ([]TableInfo, error)
	ListColumns(ctx context.Context, schema, table string) ([]ColumnInfo, error)
	ListForeignKeys(ctx context.Context, schema string) ([]ForeignKey, error)
	Close()
}

// Tx is one open transaction, pinned to a single physical connection.
//
// Pinning is the whole point and the reason this is not just "run BEGIN".
// Both drivers hand out a POOL behind Conn — database/sql with four
// connections, pgxpool with its own — so a bare BEGIN opens a transaction on
// whichever connection served it and the next statement is very likely to run
// somewhere else: outside the transaction, while the transaction sits open
// holding a connection until something rolls it back. A Tx owns its connection
// and gives it back on Commit or Rollback.
type Tx interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Transactor is optional on Conn, like SessionInspector and QueryPlanner: a
// driver that cannot pin a connection simply does not implement it, and manual
// commit is refused for that engine rather than faked.
type Transactor interface {
	Begin(ctx context.Context) (Tx, error)
}

// Driver opens connections for one engine family.
type Driver interface {
	Dial(ctx context.Context, cfg ConnConfig) (Conn, error)
}

// ErrUnsupportedEngine means no driver is registered for a profile's engine.
type ErrUnsupportedEngine struct{ Engine string }

func (e ErrUnsupportedEngine) Error() string {
	return fmt.Sprintf("no database driver for engine %q", e.Engine)
}

func driverFor(engine string) (Driver, error) {
	switch engine {
	case EnginePostgres, "":
		// Empty means a profile created before engine existed; those are all
		// Postgres, which is why the field defaults rather than being required.
		return postgresDriver{}, nil
	case EngineMySQL, EngineMariaDB:
		return mysqlDriver{engine: engine}, nil
	default:
		return nil, ErrUnsupportedEngine{Engine: engine}
	}
}

// DefaultPort is the port a new profile gets when the user leaves it blank.
func DefaultPort(engine string) int {
	switch engine {
	case EngineMySQL, EngineMariaDB:
		return 3306
	default:
		return 5432
	}
}

// rootCAPool turns a PEM bundle into a pool to verify against. It refuses a
// bundle with nothing in it: silently falling back to the system trust store
// would turn a typo in a pasted certificate into a connection that looks
// verified and is not.
func rootCAPool(pemBundle string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(pemBundle)) {
		return nil, ErrInvalidCACertificate
	}
	return pool, nil
}
