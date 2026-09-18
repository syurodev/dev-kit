package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// #45. The modes that try TLS and settle for plaintext are refused outright.
func TestSSLModeIsAnAllowlist(t *testing.T) {
	for _, mode := range []string{"prefer", "allow", "PREFER", "verify"} {
		if _, err := normalizeSSLMode("db.internal", mode, false); !errors.Is(err, ErrUnknownSSLMode) {
			t.Errorf("sslmode %q: err = %v, want ErrUnknownSSLMode", mode, err)
		}
	}
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		if got, err := normalizeSSLMode("db.internal", mode, false); err != nil || got != mode {
			t.Errorf("sslmode %q = %q, %v", mode, got, err)
		}
	}
}

// #45. A driver option is a DSN parameter, and a DSN parameter can undo the
// TLS and read-only settings put beside it.
func TestDriverOptionsCannotWeakenTheConnection(t *testing.T) {
	for _, options := range []map[string]string{
		{"sslmode": "disable"},
		{"sslrootcert": "/tmp/evil.pem"},
		{"default_transaction_read_only": "off"},
	} {
		cfg := ConnConfig{
			Engine: EnginePostgres, Host: "127.0.0.1", Port: 1, Database: "d",
			Username: "u", Password: "p", SSLMode: "verify-full", Options: options,
		}
		if _, err := (postgresDriver{}).Dial(context.Background(), cfg); !errors.Is(err, ErrDriverOptionNotAllowed) {
			t.Errorf("postgres option %v: err = %v, want ErrDriverOptionNotAllowed", options, err)
		}
	}
	for _, options := range []map[string]string{
		{"allowAllFiles": "true"},
		{"multiStatements": "true"},
		{"transaction_read_only": "0"},
		{"tls": "false"},
	} {
		cfg := baseMySQL()
		cfg.Options = options
		if _, _, err := mysqlDSN(cfg); !errors.Is(err, ErrDriverOptionNotAllowed) {
			t.Errorf("mysql option %v: err = %v, want ErrDriverOptionNotAllowed", options, err)
		}
	}
}

func TestAllowedDriverOptionsStillApply(t *testing.T) {
	cfg := baseMySQL()
	cfg.Options = map[string]string{"charset": "utf8mb4"}
	dsn, tlsName, err := mysqlDSN(cfg)
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	releaseMySQLTLS(tlsName)
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("dsn lost the charset option: %s", dsn)
	}

	conn, err := postgresDriver{}.Dial(context.Background(), ConnConfig{
		Engine: EnginePostgres, Host: "127.0.0.1", Port: 1, Database: "d",
		Username: "u", Password: "p", SSLMode: "verify-full",
		Options: map[string]string{"application_name": "devkit"},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(conn.Close)
	config := conn.(*postgresConn).pool.Config().ConnConfig
	if config.RuntimeParams["application_name"] != "devkit" {
		t.Fatalf("application_name = %q", config.RuntimeParams["application_name"])
	}
	if config.TLSConfig == nil {
		t.Fatal("an option beside verify-full left the connection without TLS")
	}
	// #45. Every literal the app builds assumes it.
	if config.RuntimeParams["standard_conforming_strings"] != "on" {
		t.Fatal("standard_conforming_strings is not set at startup")
	}
}

// #45. Sprintf left these unescaped, so a "?" or "/" changed what followed.
func TestMySQLDSNKeepsWhatItIsGiven(t *testing.T) {
	cfg := baseMySQL()
	cfg.Database = "bill?ing/2024"
	cfg.Password = "p@ss:w/rd?x=1"
	dsn, tlsName, err := mysqlDSN(cfg)
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	releaseMySQLTLS(tlsName)
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%s): %v", dsn, err)
	}
	if parsed.DBName != cfg.Database || parsed.Passwd != cfg.Password || parsed.Addr != "db.internal:3306" {
		t.Fatalf("parsed back as db=%q pass=%q addr=%q", parsed.DBName, parsed.Passwd, parsed.Addr)
	}
	if parsed.Params["x"] != "" {
		t.Fatal("the password leaked a parameter into the DSN")
	}
}

// #46. Only an error the server sent says the connection works.
func TestAnsweredByServer(t *testing.T) {
	if !answeredByServer(NewStatementError(&pgconn.PgError{Code: "42P01"})) {
		t.Error("a Postgres error is the server answering")
	}
	if !answeredByServer(&mysqldriver.MySQLError{Number: 1045}) {
		t.Error("a MySQL error is the server answering")
	}
	if answeredByServer(NewStatementError(errors.New("read tcp: connection reset by peer"))) {
		t.Error("a broken socket is not the server answering")
	}
}
