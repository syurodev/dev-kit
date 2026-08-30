package database

import (
	"context"
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
	Values() ([]string, error)
	Err() error
	Close()
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
